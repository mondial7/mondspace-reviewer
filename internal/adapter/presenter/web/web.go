// Package web serves the review as a localhost web application (ADR 0004).
//
// It renders server-side HTML with html/template — no build step, no client
// framework, and so far no JavaScript at all (expansion uses native <details>).
// The review semantics come entirely from the usecase layer; this package only
// presents them.
package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

//go:embed assets/*
var assets embed.FS

//go:embed templates/*.html
var templates embed.FS

// Session is everything the web app renders for one review.
type Session struct {
	ID     string
	Prompt string
	Repo   string
	Units  []domain.Unit
	Notes  []domain.Note
	Diffs  map[string]domain.Diff
	// Stats and Histories travel with the session rather than beside it: the
	// cockpit can be showing any session in the workspace, and numbers from a
	// different one would be worse than no numbers at all.
	Stats     domain.SessionStats
	Histories map[string]domain.FileHistory
	Narrative domain.Narrative
	// Target is what is being reviewed: a commit, a tag, a pull request, the
	// working tree, or a recorded run (ADR 0017).
	Target domain.Target
	// Hidden is what .msrignore kept out of this review. It travels with the
	// session so the page can always say what is missing and why: leaving files
	// out silently is the one thing this feature must not do (ADR 0027).
	Hidden []usecase.Hidden
}

// TargetSummary is one row of the workspace: something worth reviewing. Most
// come from git — a commit, a tag, a pull request, the working tree — and a
// recorded session is one kind among them rather than the index (ADR 0017).
type TargetSummary struct {
	ID string
	// Ref is how a person names this point in history — a tag name, a short
	// commit hash, a session id. It is what the picker submits, so a URL reads
	// /?target=v5.1.0 rather than a hex id nobody can recognise.
	Ref      string
	Repo     string
	Kind     domain.TargetKind
	Title    string
	Subtitle string
	TS       time.Time
	Sessions int
	// Reviewed marks a target someone has finished with, so the picker shows
	// at a glance what is still open (ADR 0021).
	Reviewed bool
}

// SessionSummary is one row of the workspace: a review that exists, wherever it
// came from. Sessions span repos and agents (issue #8).
type SessionSummary struct {
	ID      string
	Repo    string
	Agent   string
	Prompt  string
	Files   int
	Flags   int
	Open    int // unresolved questions and objections
	Started time.Time
}

// Exchange is one question and its answer. It is domain.Exchange rather than a
// copy: the conversation is persisted and reloaded, so a second definition here
// would be a second place for the two to drift apart.
type Exchange = domain.Exchange

// ExchangeStore persists the review conversation. Declared where it is consumed,
// so this adapter depends on no other.
type ExchangeStore interface {
	AppendExchange(domain.Exchange) error
}

// AskFunc answers a question given everything already discussed.
// It receives the review being read: asking a question while looking at one
// target and being answered about another is worse than not answering.
type AskFunc func(ctx context.Context, targetID, question string, history []Exchange) (string, error)

// ReanalyseFunc re-summarises one unit, returning the headline and the model
// that produced it. Re-running with a better model is cheap because the diff is
// stable (issue #10).
type ReanalyseFunc func(ctx context.Context, u domain.Unit) (domain.Headline, string, error)

// NarrateFunc regenerates the session's story. It is slow — several model calls
// — so it runs in the background and open pages are told when it lands.
type NarrateFunc func(ctx context.Context, targetID string)

// AuditEntry records one reviewer interaction. Once reviews are shared, these
// are records, not a cache: who did what, when (issue #11).
type AuditEntry struct {
	TS        time.Time
	SessionID string
	UnitID    string
	Action    string // annotate | ask | reanalyse | narrate
	Detail    string
	Model     string // the model that served a call, when one did
	Millis    int64  // how long the call took, when it was a model call
	Failed    bool
}

// AuditLog persists interactions. Declared where consumed, so this adapter
// depends on no other adapter.
type AuditLog interface {
	Append(AuditEntry) error
}

// AuditReader is an optional capability of an AuditLog: reading back what was
// recorded, which is what /activity shows. A log that can only be appended to
// still works; its page is simply empty.
type AuditReader interface {
	Entries() ([]AuditEntry, error)
}

// AgentStatus is the reviewer's own model: which one, where, whether it is up
// right now, and what it has spent. It is the answer to "is the thing that
// writes my summaries actually working", which nothing else on the site shows.
type AgentStatus struct {
	Model      string
	Endpoint   string
	NoThinking bool
	Online     bool
	Checked    time.Time
	Usage      port.TokenUsage
	// Workloads is which model answers each job, when they do not all share one
	// (ADR 0019). Empty when a single model answers everything, so the common
	// arrangement stays the quiet one on the page.
	Workloads []WorkloadModel
}

// WorkloadModel is one job and the model that answers it.
type WorkloadModel struct {
	Workload string
	Endpoint string
	Model    string
}

// VersionLister and VersionDiffer let the overlay step through a file's history.
// Declared where they are consumed, so this adapter depends on no other.
type VersionLister func(ctx context.Context, path string, limit int) ([]domain.Commit, error)

// VersionDiffer is what one commit did to one file.
type VersionDiffer func(ctx context.Context, commit, path string) (domain.Diff, error)

// RepoStatus is one repository the workspace holds.
type RepoStatus struct {
	Name     string
	Path     string
	Sessions int
}

// AddRepoFunc starts watching another repository without a restart, returning
// the whole workspace and repository list afterwards rather than a fragment.
type AddRepoFunc func(path string) ([]TargetSummary, []RepoStatus, error)

// DescribeFunc writes what one group of changes is for, on demand. The
// automatic pass is bounded (ADR 0014), so most groups in a large session are
// left undescribed; this is how a reviewer asks for one.
// It receives the unit ids the page is actually showing rather than a group id
// to look up again: the command rebuilds units from git, the page renders the
// ones it loaded, and on a repository being worked in those drift apart within
// seconds — which made every description fail with "no such group".
type DescribeFunc func(ctx context.Context, targetID string, unitIDs []string) (string, error)

// Loader materialises one session's review on demand. A workspace may span
// several repositories and many sessions; building every one at start-up would
// mean a git diff per file per session, so they are loaded when first opened.
type Loader func(ctx context.Context, sessionID string) (Session, error)

// Annotator persists a reviewer's annotation. It is declared where it is
// consumed so this adapter depends on no other adapter.
type Annotator interface {
	AppendNote(domain.Note) error
}

// Server renders and serves one review session. Handlers run concurrently, so
// session state is guarded.
type Server struct {
	mux   *http.ServeMux
	tmpl  *template.Template
	notes Annotator

	mu           sync.RWMutex
	sess         Session
	loader       Loader
	versions     VersionLister
	versionOf    VersionDiffer
	describe     DescribeFunc
	describeFile DescribeFileFunc
	removeRepo   AddRepoFunc
	compare      CompareFunc
	configure    ConfigureFunc
	agentErr     string
	work         []Work
	exchanges    ExchangeStore
	// loaded caches sessions opened during this run, so switching back and forth
	// costs nothing after the first visit.
	loaded     map[string]Session
	workspace  []SessionSummary
	targets    []TargetSummary
	repos      []RepoStatus
	candidates []RepoStatus
	addRepo    AddRepoFunc
	repoErr    string
	// thread is every conversation, keyed by the review it was about. It was a
	// flat list, which meant questions asked about one review were shown under
	// another and handed to the model as history for it — the exact thing the
	// comment above AskFunc says is worse than not answering.
	thread          map[string][]Exchange
	conversationsOf ConversationsOf
	models          map[string]string // unit ID -> model that produced its headline
	ask             AskFunc
	reanalyse       ReanalyseFunc
	audit           AuditLog
	narrate         NarrateFunc
	narrating       bool
	agent           AgentStatus

	// pending is work that arrived after this review was opened, waiting for
	// the reviewer to decide what to do with it (ADR 0020).
	pending        domain.Pending
	include        IncludeFunc
	split          SplitFunc
	signoff        SignoffFunc
	signoffOf      SignoffOf
	signErr        string
	runAnalysis    RunAnalysisFunc
	analysisOf     AnalysisOf
	logOf          LogOf
	showAll        ShowAllFunc
	branchesOf     BranchesOf
	remoteWatch    RemoteWatchState
	setRemoteWatch SetRemoteWatch

	// subs are live subscribers (server-sent events). Each gets a buffered
	// channel so a slow reader can never block a request handler.
	subs   map[chan sseEvent]struct{}
	pulses int
	nextID int

	newID func() string
	now   func() time.Time
}

// NewServer builds the HTTP handler for a session. notes may be nil, in which
// case annotations are held in memory only.
func NewServer(sess Session, notes Annotator) *Server {
	s := &Server{
		mux:    http.NewServeMux(),
		tmpl:   template.Must(template.New("").Funcs(funcs()).ParseFS(templates, "templates/*.html")),
		sess:   sess,
		notes:  notes,
		models: map[string]string{},
		loaded: map[string]Session{},
		thread: map[string][]Exchange{},
		subs:   map[chan sseEvent]struct{}{},
		newID:  func() string { return ulid.Make().String() },
		now:    func() time.Time { return time.Now().UTC() },
	}
	s.routes()
	return s
}

// WithClock overrides id and time generation for deterministic tests.
func (s *Server) WithClock(newID func() string, now func() time.Time) *Server {
	s.newID, s.now = newID, now
	return s
}

// WithAsk wires the reviewer-assistant. Without it, the ask panel is not shown.
func (s *Server) WithAsk(fn AskFunc) *Server {
	s.ask = fn
	return s
}

// WithReanalyse wires per-unit re-analysis; without it, the button is not shown.
func (s *Server) WithReanalyse(fn ReanalyseFunc) *Server {
	s.reanalyse = fn
	return s
}

// WithAudit records every reviewer interaction.
func (s *Server) WithAudit(a AuditLog) *Server {
	s.audit = a
	return s
}

// WithNarrate wires re-narration to an explicit control. Narration is the most
// expensive thing the app does, so it is never triggered by navigation: the
// reviewer asks for it, or it does not happen.
func (s *Server) WithNarrate(fn NarrateFunc) *Server {
	s.narrate = fn
	return s
}

// WithCandidates supplies checkouts found nearby that are not open. Offering
// them in the app is what replaced asking at launch: choosing costs a click
// rather than a restart, and a script never has to answer a question.
func (s *Server) WithCandidates(repos []RepoStatus) *Server {
	s.candidates = repos
	return s
}

// WithTargets supplies everything worth reviewing across the workspace, newest
// first. It is what the picker lists and what the palette searches.
func (s *Server) WithTargets(targets []TargetSummary) *Server {
	s.targets = targets
	return s
}

// SetTargets replaces what the picker offers while the server is running.
//
// The list is discovered once at start-up, which was fine when nothing could
// change it. It is not fine now: a commit that lands while a cockpit is open
// becomes a target the reviewer is told about, and a toast pointing at a target
// the picker has never heard of goes nowhere.
func (s *Server) SetTargets(targets []TargetSummary) {
	s.mu.Lock()
	s.targets = targets
	s.mu.Unlock()
	s.broadcast("targets")
}

// WithRepos supplies the repositories the workspace holds, and optionally a way
// to open another one while the server is running.
func (s *Server) WithRepos(repos []RepoStatus, add AddRepoFunc) *Server {
	s.repos, s.addRepo = repos, add
	return s
}

// ConfigureFunc applies a new model configuration and persists it. It returns an
// error the reviewer will read, so it should say what went wrong rather than
// only that something did.
type ConfigureFunc func(domain.AgentConfig) error

// WithConfigure lets the reviewer point msr at a different endpoint or model
// without editing a file and restarting.
func (s *Server) WithConfigure(fn ConfigureFunc) *Server {
	s.configure = fn
	return s
}

// handleConfigure applies the settings from the status page.
func (s *Server) handleConfigure(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	configure := s.configure
	s.mu.RUnlock()
	if configure == nil {
		http.NotFound(w, r)
		return
	}

	want := domain.AgentConfig{
		Endpoint:   strings.TrimSpace(r.FormValue("endpoint")),
		Model:      strings.TrimSpace(r.FormValue("model")),
		NoThinking: r.FormValue("no_thinking") != "",
	}
	// A workload with either box filled goes to its own model; both empty means
	// it shares the settings above. Blank has to mean "shared" rather than "an
	// override to nothing", because emptying the box is how a reviewer collapses
	// two servers back into one.
	for _, w := range domain.Workloads {
		ref := domain.ModelRef{
			Endpoint: strings.TrimSpace(r.FormValue("endpoint_" + string(w))),
			Model:    strings.TrimSpace(r.FormValue("model_" + string(w))),
		}
		if ref == (domain.ModelRef{}) {
			continue
		}
		if want.Overrides == nil {
			want.Overrides = map[domain.Workload]domain.ModelRef{}
		}
		want.Overrides[w] = ref
	}
	err := configure(want)

	s.mu.Lock()
	if err != nil {
		s.agentErr = err.Error()
	} else {
		s.agentErr = ""
		// Show it immediately; the next probe fills in whether it answers.
		s.agent.Endpoint, s.agent.Model = want.Endpoint, want.Model
	}
	s.mu.Unlock()

	if err == nil {
		s.Record(AuditEntry{Action: "configure",
			Detail: want.Model + " at " + want.Endpoint, Model: want.Model})
	}
	s.broadcast("agent")
	http.Redirect(w, r, "/status", http.StatusSeeOther)
}

// WithRemoveRepo wires closing a repository, which stops watching it without
// touching anything it holds.
func (s *Server) WithRemoveRepo(fn AddRepoFunc) *Server {
	s.removeRepo = fn
	return s
}

// handleAddRepo starts watching another repository. A path that is not a
// checkout is reported on the page rather than failing silently: it comes from
// a form, and a typo that quietly did nothing is worse than an error.
func (s *Server) handleAddRepo(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	add := s.addRepo
	s.mu.RUnlock()
	if add == nil {
		http.NotFound(w, r)
		return
	}

	targets, repos, err := add(r.FormValue("path"))

	s.mu.Lock()
	if err != nil {
		s.repoErr = err.Error()
	} else {
		s.repoErr = ""
		s.targets, s.repos = targets, repos
		// A repository that just joined is no longer a candidate for joining.
		var still []RepoStatus
		for _, c := range s.candidates {
			if c.Path != r.FormValue("path") {
				still = append(still, c)
			}
		}
		s.candidates = still
	}
	s.mu.Unlock()

	if err == nil {
		s.Record(AuditEntry{Action: "repo-opened", Detail: r.FormValue("path")})
	}
	s.broadcast("repos")
	http.Redirect(w, r, "/status", http.StatusSeeOther)
}

// WithExchanges persists the review conversation, and seeds the thread with
// whatever was already said, so a reviewer can pick it up where they left it.
func (s *Server) WithExchanges(store ExchangeStore, earlier []domain.Exchange) *Server {
	s.exchanges = store
	for _, e := range earlier {
		// An exchange with no review recorded predates the scoping and belongs
		// to whichever review was open when it was written, which is the one
		// this server started with.
		id := e.SessionID
		if id == "" {
			id = s.sess.ID
		}
		s.thread[id] = append(s.thread[id], e)
	}
	return s
}

// ConversationsOf reads back what was asked about a review in an earlier run.
type ConversationsOf func(targetID string) []domain.Exchange

// WithConversations wires reading a review's stored conversation, so returning
// to one you asked questions about brings them back rather than showing an
// empty box.
func (s *Server) WithConversations(of ConversationsOf) *Server {
	s.conversationsOf = of
	return s
}

// Conversation is what was asked about one review, oldest first.
//
// Loaded from the store the first time it is wanted and remembered after, so
// paging around the workspace does not re-read a file per request.
func (s *Server) Conversation(targetID string) []Exchange {
	s.mu.RLock()
	thread, known := s.thread[targetID]
	of := s.conversationsOf
	s.mu.RUnlock()
	if known || of == nil {
		return append([]Exchange(nil), thread...)
	}

	earlier := of(targetID)

	s.mu.Lock()
	// Nil rather than absent, so a review with no conversation is not re-read
	// on every page load.
	if _, raced := s.thread[targetID]; !raced {
		s.thread[targetID] = earlier
	}
	out := append([]Exchange(nil), s.thread[targetID]...)
	s.mu.Unlock()
	return out
}

// WithDescribe wires on-demand description of a group of changes.
func (s *Server) WithDescribe(fn DescribeFunc) *Server {
	s.describe = fn
	return s
}

// handleDescribe writes (or rewrites) one group's meaning. It is the only model
// call a reviewer can trigger for a group, and it is deliberately explicit:
// describing every group of a large session automatically is the cost ADR 0014
// exists to bound.
func (s *Server) handleDescribe(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	describe := s.describe
	s.mu.RUnlock()
	if describe == nil {
		http.NotFound(w, r)
		return
	}

	target := s.openSession(r).ID
	groupID := r.PathValue("id")
	started := time.Now()
	units := s.unitsInGroup(target, groupID)
	if len(units) == 0 {
		http.Error(w, "no such group in this review", http.StatusNotFound)
		return
	}

	finish := s.BeginWork("describe", target, "what this change is for")
	meaning, err := describe(context.WithoutCancel(r.Context()), target, units)
	finish(err)

	entry := AuditEntry{Action: "describe", Detail: groupID,
		Millis: time.Since(started).Milliseconds()}
	if err != nil {
		entry.Failed = true
		entry.Detail = groupID + ": " + err.Error()
	} else {
		s.describedGroup(target, groupID, meaning)
	}
	s.Record(entry)
	s.broadcast("narrative")

	http.Redirect(w, r, backTo(r, "#group-"+groupID), http.StatusSeeOther)
}

// DescribeFileFunc says what one file's change is for. A folder's summary is
// where a reviewer starts; "and what happened to this one" is always next.
// It returns the description and the lines worth reading, which come from the
// same model call so they cannot disagree about what the change was.
type DescribeFileFunc func(ctx context.Context, targetID, unitID string) (string, []string, error)

// WithDescribeFile wires per-file description.
func (s *Server) WithDescribeFile(fn DescribeFileFunc) *Server {
	s.describeFile = fn
	return s
}

// handleDescribeFile describes one file, on request. Unlike the group pass this
// is never run in bulk, so there is no budget to bound — only patience.
func (s *Server) handleDescribeFile(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	describe := s.describeFile
	s.mu.RUnlock()
	if describe == nil {
		http.NotFound(w, r)
		return
	}

	target := s.openSession(r).ID
	unitID := r.PathValue("id")
	started := time.Now()
	finish := s.BeginWork("describe", target, "what this file is for")
	meaning, lines, err := describe(context.WithoutCancel(r.Context()), target, unitID)
	finish(err)

	entry := AuditEntry{SessionID: target, UnitID: unitID, Action: "describe-file",
		Millis: time.Since(started).Milliseconds(), Detail: unitID}
	if err != nil {
		entry.Failed = true
		entry.Detail = unitID + ": " + err.Error()
	} else {
		s.describedFile(target, s.fileKey(target, unitID), meaning, lines)
	}
	s.Record(entry)
	s.broadcast("narrative")

	http.Redirect(w, r, backTo(r, "#unit-"+unitID), http.StatusSeeOther)
}

// unitsInGroup resolves a rendered group back to the units it holds, from the
// same session the page rendered — never by recomputing from git.
func (s *Server) unitsInGroup(targetID, groupID string) []string {
	sess := s.sess
	if targetID != "" && targetID != sess.ID {
		s.mu.RLock()
		if cached, ok := s.loaded[targetID]; ok {
			sess = cached
		}
		s.mu.RUnlock()
	}

	ordered := make([]domain.Unit, 0, len(sess.Units))
	for i := len(sess.Units) - 1; i >= 0; i-- {
		ordered = append(ordered, sess.Units[i])
	}
	for _, g := range usecase.GroupChanges(ordered, sess.Diffs) {
		if g.ID != groupID {
			continue
		}
		ids := make([]string, 0, len(g.Units))
		for _, u := range g.Units {
			ids = append(ids, u.ID)
		}
		return ids
	}
	return nil
}

// fileKey is where a file's description lives in the same map a group's does.
func (s *Server) fileKey(targetID, unitID string) string {
	sess := s.sess
	if targetID != "" && targetID != sess.ID {
		s.mu.RLock()
		if cached, ok := s.loaded[targetID]; ok {
			sess = cached
		}
		s.mu.RUnlock()
	}
	for _, u := range sess.Units {
		if u.ID == unitID {
			return usecase.FileKey(u)
		}
	}
	return unitID
}

// handleRemoveRepo stops watching a repository. Its reviews and notes are left
// exactly where they are on disk — this closes a window, it does not delete
// anything.
func (s *Server) handleRemoveRepo(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	remove := s.removeRepo
	s.mu.RUnlock()
	if remove == nil {
		http.NotFound(w, r)
		return
	}

	path := r.FormValue("path")
	targets, repos, err := remove(path)

	s.mu.Lock()
	if err != nil {
		s.repoErr = err.Error()
	} else {
		s.repoErr = ""
		s.targets, s.repos = targets, repos
		s.candidates = append(s.candidates, RepoStatus{Name: filepath.Base(path), Path: path})
	}
	s.mu.Unlock()

	if err == nil {
		s.Record(AuditEntry{Action: "repo-closed", Detail: path})
	}
	s.broadcast("repos")
	http.Redirect(w, r, "/status", http.StatusSeeOther)
}

// CompareFunc reviews an arbitrary range the reviewer chose, returning the id it
// can be opened under. It is the same engine as every other target; the refs
// simply came from two select boxes rather than from git's own list.
type CompareFunc func(ctx context.Context, repo, from, to string) (string, error)

// WithCompare wires comparing two refs.
func (s *Server) WithCompare(fn CompareFunc) *Server {
	s.compare = fn
	return s
}

// handleCompare builds a range target from two refs and opens it.
func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	compare := s.compare
	s.mu.RUnlock()

	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if compare == nil || from == "" || to == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	id, err := compare(r.Context(), r.URL.Query().Get("repo"), from, to)
	if err != nil {
		s.mu.Lock()
		s.repoErr = err.Error()
		s.mu.Unlock()
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/?target="+id, http.StatusSeeOther)
}

// WithVersions wires the file-history overlay: the commits that touched a file,
// and what each did to it. Without them the overlay shows the session's diff
// only, which is still useful.
func (s *Server) WithVersions(list VersionLister, at VersionDiffer) *Server {
	s.versions, s.versionOf = list, at
	return s
}

// WithLoader wires on-demand loading of other sessions, so the cockpit can move
// between them — and between repositories — without a restart.
func (s *Server) WithLoader(fn Loader) *Server {
	s.loader = fn
	return s
}

// openSession resolves the ?session= parameter to a session to render. An
// unknown or unloadable id falls back to the one already open: a stale link
// must not leave the reviewer at an error page with no way back.
func (s *Server) openSession(r *http.Request) Session {
	// ?target= is what the picker sends. ?session= is kept because it is what
	// every link printed before v4 used, and a session is still a target.
	want := r.URL.Query().Get("target")
	if want == "" {
		want = r.URL.Query().Get("session")
	}

	s.mu.RLock()
	current := s.sess
	loader := s.loader
	s.mu.RUnlock()

	// No target named means the one already open — which may itself be the live
	// target, and must then be as fresh as any other look at it.
	if want == "" {
		want = current.ID
	}
	if !validSessionID(want) {
		return current
	}
	// A person names a point in history by its ref. Resolve that to an id first,
	// so everything downstream keeps one way of being addressed — the cache
	// included: it is written under the id, so looking it up under the ref
	// missed every time and rebuilt the review on every request.
	if id, ok := s.targetByRef(want); ok {
		want = id
	}
	live := s.isLive(want)

	// Serving the open session from memory is right for every fixed range and
	// wrong for this one: it would show whatever the working tree held when msr
	// started and never move again.
	if want == current.ID && !live {
		return current
	}

	s.mu.RLock()
	cached, isCached := s.loaded[want]
	s.mu.RUnlock()
	if !isCached && want == current.ID {
		cached, isCached = current, true
	}

	// The live target is the one thing a cache must not answer for. Its whole
	// purpose is that it changed since the last look; served from memory it
	// would be the only part of the page that lies.
	if isCached && !live {
		return cached
	}
	if loader == nil {
		return current
	}

	loadedSess, err := loader(r.Context(), want)
	if err != nil || loadedSess.ID == "" {
		if isCached {
			return cached // a transient failure must not blank a live page
		}
		return current
	}
	if isCached && loadedSess.Narrative.Source == "" {
		// Rebuilding gets fresh units and diffs; it must not throw away a story
		// the model has already written about them.
		loadedSess.Narrative = cached.Narrative
	}

	s.mu.Lock()
	if want == s.sess.ID {
		// The open session is what annotating, narrating and asking all read.
		// Refreshing the page's copy without refreshing theirs would let a
		// reviewer act on a file the rest of the server no longer believes in.
		s.sess = loadedSess
	} else {
		s.loaded[want] = loadedSess
	}
	s.mu.Unlock()
	return loadedSess
}

// actionTarget resolves the review an action names, and refuses rather than
// guessing.
//
// openSession falls back to whatever is open when a ref cannot be resolved,
// which is right for rendering — a stale link should not be a dead end. It is
// wrong for anything that acts: auditing or signing off a review other than the
// one named is worse than doing nothing, and it is invisible, because the
// result lands under something nobody was looking at.
func (s *Server) actionTarget(r *http.Request) (string, bool) {
	want := strings.TrimSpace(r.FormValue("target"))
	if want == "" {
		want = strings.TrimSpace(r.URL.Query().Get("target"))
	}
	if want == "" {
		// Nothing named: the review being read is what is meant.
		return s.openSession(r).ID, true
	}

	s.mu.RLock()
	current := s.sess.ID
	_, cached := s.loaded[want]
	s.mu.RUnlock()

	if want == current || cached {
		return want, true
	}
	if id, ok := s.targetByRef(want); ok {
		return id, true
	}
	if !validSessionID(want) {
		return "", false
	}
	// An id this server has not loaded yet is still resolvable by the loader,
	// which is how a target from another repository in the workspace is reached.
	if got := s.openSession(r); got.ID == want {
		return want, true
	}
	return "", false
}

// isLive reports whether a target id follows HEAD rather than naming a fixed
// point in history (ADR 0018).
func (s *Server) isLive(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.targets {
		if t.ID == id {
			return t.Kind == domain.TargetLive
		}
	}
	return false
}

// validSessionID guards the ?session= parameter. It arrives from a URL and is
// handed to a loader that will use it as a directory name, so it must be a
// single, benign path segment. The store validates again on its own account;
// this stops a traversal attempt before it ever gets that far.
func validSessionID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	return !strings.ContainsAny(id, `/\`) && !strings.Contains(id, "..")
}

// targetByRef resolves a human name for a point in history to a target id.
func (s *Server) targetByRef(ref string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.targets {
		if t.Ref != "" && strings.EqualFold(t.Ref, ref) {
			return t.ID, true
		}
	}
	return "", false
}

// SetSession replaces the units and diffs on a running server, so a review of a
// session that is still being worked on keeps up with it.
func (s *Server) SetSession(sess Session, histories map[string]domain.FileHistory) {
	s.mu.Lock()
	sess.Histories = histories
	// Stats and the story arrive on their own schedules; a rebuilt unit list
	// must not wipe either.
	sess.Stats = s.sess.Stats
	sess.Narrative = s.sess.Narrative
	s.sess = sess
	s.mu.Unlock()
	s.broadcast("units")
}

// WithHistories supplies each file's edit history, so a unit can show how many
// times it was touched and when, not only where it ended up.
func (s *Server) WithHistories(h map[string]domain.FileHistory) *Server {
	s.sess.Histories = h
	return s
}

// clockOrDash renders a timestamp, or an em dash for a file nothing touched —
// which happens whenever the review came from a git range rather than a log.
func clockOrDash(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("15:04:05")
}

// WithAgent supplies the reviewer model's status, shown at /status and totalled
// on the cockpit.
func (s *Server) WithAgent(a AgentStatus) *Server {
	s.agent = a
	return s
}

// SetAgent replaces the reviewer model's status while the server is running, so
// an endpoint going down is visible without a reload.
func (s *Server) SetAgent(a AgentStatus) {
	s.mu.Lock()
	s.agent = a
	s.mu.Unlock()
	s.broadcast("agent")
}

// WithStats supplies the session's numbers, shown on the cockpit. They are
// recomputed by the caller as the session moves, not cached here.
func (s *Server) WithStats(st domain.SessionStats) *Server {
	s.sess.Stats = st
	return s
}

// SetStats replaces the session's numbers while the server is running, so the
// cockpit keeps up with a session that is still being worked on.
func (s *Server) SetStats(st domain.SessionStats) {
	s.mu.Lock()
	s.sess.Stats = st
	s.mu.Unlock()
	s.broadcast("stats")
}

// WithNarrative supplies the session's story, shown at /story.
func (s *Server) WithNarrative(n domain.Narrative) *Server {
	s.sess.Narrative = n
	return s
}

// workLocked is Work() for a caller that already holds the read lock.
func (s *Server) workLocked() []Work {
	out := make([]Work, 0, len(s.work))
	for i := len(s.work) - 1; i >= 0; i-- {
		out = append(out, s.work[i])
	}
	return out
}

// describedGroup records one group's meaning against whichever target it
// belongs to — the one being read, which is not necessarily the one this server
// started with.
func (s *Server) describedGroup(targetID, groupID, meaning string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apply := func(sess *Session) {
		if sess.Narrative.Meanings == nil {
			sess.Narrative.Meanings = map[string]string{}
		}
		sess.Narrative.Meanings[groupID] = meaning
	}
	if targetID == "" || targetID == s.sess.ID {
		apply(&s.sess)
		return
	}
	if cached, ok := s.loaded[targetID]; ok {
		apply(&cached)
		s.loaded[targetID] = cached
	}
}

// describedFile records a file's description and the lines called out with it.
func (s *Server) describedFile(targetID, key, meaning string, lines []string) {
	s.describedGroup(targetID, key, meaning)

	s.mu.Lock()
	defer s.mu.Unlock()
	apply := func(sess *Session) {
		if sess.Narrative.Highlights == nil {
			sess.Narrative.Highlights = map[string][]string{}
		}
		sess.Narrative.Highlights[key] = lines
	}
	if targetID == "" || targetID == s.sess.ID {
		apply(&s.sess)
		return
	}
	if cached, ok := s.loaded[targetID]; ok {
		apply(&cached)
		s.loaded[targetID] = cached
	}
}

// SetNarrativeFor replaces the story of whichever target was narrated, which
// may be one opened from the picker rather than the one served at start-up.
func (s *Server) SetNarrativeFor(targetID string, n domain.Narrative) {
	s.mu.Lock()
	if targetID == "" || targetID == s.sess.ID {
		s.sess.Narrative = n
	} else if cached, ok := s.loaded[targetID]; ok {
		cached.Narrative = n
		s.loaded[targetID] = cached
	}
	s.mu.Unlock()
	s.broadcast("narrative")
}

// SetNarrative replaces the story while the server is running, so a slow model
// can upgrade a mechanical narrative without the page ever waiting on it.
func (s *Server) SetNarrative(n domain.Narrative) {
	s.mu.Lock()
	s.sess.Narrative = n
	s.mu.Unlock()
	s.broadcast("narrative")
}

// WithWorkspace supplies the sessions listed at /sessions.
func (s *Server) WithWorkspace(sessions []SessionSummary) *Server {
	s.workspace = sessions
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	static, err := fs.Sub(assets, "assets")
	if err != nil {
		panic("web: embedded assets missing: " + err.Error())
	}
	s.mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(static))))
	// The cockpit is the landing page: while an agent is still working, the first
	// question is "is anything still happening", not "what shall I review first".
	s.mux.HandleFunc("GET /{$}", s.handleCockpit)
	// The review queue and the story were folded into the cockpit. Old links,
	// bookmarks and anchors still work.
	s.mux.HandleFunc("GET /review", redirectHome)
	s.mux.HandleFunc("GET /story", redirectHome)
	s.mux.HandleFunc("POST /units/{id}/notes", s.handleAnnotate)
	s.mux.HandleFunc("POST /ask", s.handleAsk)
	s.mux.HandleFunc("POST /units/{id}/reanalyse", s.handleReanalyse)
	s.mux.HandleFunc("GET /units/{id}/diff", s.handleUnitDiff)
	s.mux.HandleFunc("GET /units/{id}/versions", s.handleVersions)
	s.mux.HandleFunc("POST /groups/{id}/describe", s.handleDescribe)
	s.mux.HandleFunc("POST /units/{id}/describe", s.handleDescribeFile)
	s.mux.HandleFunc("POST /repos/remove", s.handleRemoveRepo)
	s.mux.HandleFunc("POST /agent", s.handleConfigure)
	s.mux.HandleFunc("GET /compare", s.handleCompare)
	s.mux.HandleFunc("GET /cockpit", s.handleCockpit)
	s.mux.HandleFunc("GET /activity", s.handleActivity)
	s.mux.HandleFunc("GET /status", s.handleStatus)
	s.mux.HandleFunc("GET /tutorial", s.handleTutorial)
	s.mux.HandleFunc("GET /branches", s.handleBranches)
	s.mux.HandleFunc("GET /export", s.handleExport)
	s.mux.HandleFunc("POST /remote", s.handleRemoteWatch)
	s.mux.HandleFunc("POST /repos", s.handleAddRepo)
	// The workspace list folded into the status page.
	s.mux.HandleFunc("GET /sessions", redirectStatus)
	s.mux.HandleFunc("POST /story/narrate", s.handleNarrate)
	s.mux.HandleFunc("POST /review/signoff", s.handleSignoff)
	s.mux.HandleFunc("POST /analysis/{kind}", s.handleAnalysis)
	s.mux.HandleFunc("POST /live/include", s.handleInclude)
	s.mux.HandleFunc("POST /live/split", s.handleSplit)
	s.mux.HandleFunc("GET /events", s.handleEvents)
}

// handleUnitDiff serves one unit's full diff as a fragment. The cockpit shows a
// compacted diff inline and fetches the rest only if asked: a 97-file session
// with every diff inlined would be megabytes of HTML nobody reads.
func (s *Server) handleUnitDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := s.openSession(r)
	diff, known := sess.Diffs[id]
	if !known {
		for _, u := range sess.Units {
			if u.ID == id {
				known = true
				break
			}
		}
	}
	if !known {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "diff.html", splitDiff(diff.Text)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleVersions serves a file's history as the overlay's navigation: the
// commits that touched it, newest first, and the diff of whichever one is
// asked for.
func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	sess := s.openSession(r)

	var path string
	for _, u := range sess.Units {
		if u.ID == r.PathValue("id") && len(u.Files) > 0 {
			path = u.Files[0]
			break
		}
	}
	if path == "" {
		http.NotFound(w, r)
		return
	}

	s.mu.RLock()
	list, at := s.versions, s.versionOf
	s.mu.RUnlock()
	if list == nil {
		http.NotFound(w, r)
		return
	}

	commits, err := list(r.Context(), path, 25)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Which version to show: the one asked for, else the newest.
	want := r.URL.Query().Get("at")
	if want == "" && len(commits) > 0 {
		want = commits[0].Hash
	}

	type versionView struct {
		Hash, Short, When, Subject, Author string
		Current                            bool
	}
	views := make([]versionView, 0, len(commits))
	for _, c := range commits {
		views = append(views, versionView{
			Hash: c.Hash, Short: short(c.Hash), When: c.TS.Local().Format("2006-01-02 15:04"),
			Subject: c.Subject, Author: c.Author, Current: c.Hash == want,
		})
	}

	var lines []diffLine
	if want != "" && at != nil {
		if d, err := at(r.Context(), want, path); err == nil {
			lines = splitDiff(d.Text)
		}
	}
	if len(lines) == 0 {
		lines = splitDiff(sess.Diffs[r.PathValue("id")].Text)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "versions.html", struct {
		UnitID   string
		Path     string
		Versions []versionView
		Diff     []diffLine
	}{r.PathValue("id"), path, views, lines}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// short abbreviates a commit hash for display.
func short(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}

func redirectStatus(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/status", http.StatusMovedPermanently)
}

func redirectHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusMovedPermanently)
}

// Subscribers is the number of live event streams currently attached.
func (s *Server) Subscribers() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subs)
}

// broadcast tells every live page that something changed. It never blocks: a
// subscriber that is not keeping up simply misses this nudge, and the next one
// (or its own reload) brings it back in sync.
func (s *Server) broadcast(event string) {
	s.send(sseEvent{Name: event, Data: "{}"})
}

// sseEvent is one message on the wire. Most events are a bare nudge — the page
// re-fetches itself and swaps what changed — but a pulse carries its words, so
// a toast can appear without a round trip first.
type sseEvent struct {
	Name string
	Data string // a single line of JSON: EventSource will not parse more
}

func (s *Server) send(ev sseEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// SetPending records what has changed since this review was opened, classified
// against what the reviewer has already read and ruled on.
//
// The classification happens here because this is where the units and the notes
// are. "Three files changed" is a fact; "one of them is the file you marked ok"
// is a reason to stop and decide, and only this side knows the difference.
func (s *Server) SetPending(changed []domain.FileStat, from, to domain.SnapshotRef, since time.Time) {
	s.mu.Lock()
	p := usecase.PendingWork(s.sess.Units, s.sess.Notes, changed, from, to, since)
	was := s.pending.Headline()
	s.pending = p
	s.mu.Unlock()

	// Only wake open pages when the sentence would actually read differently.
	// This is recomputed every couple of seconds; broadcasting each time would
	// redraw the page for a line that has not changed.
	if p.Headline() != was {
		s.broadcast("pending")
	}
}

// OpenTargetID is the review currently being read. The watcher needs it to ask
// "what has arrived beyond this", which is a different question from "what has
// changed in the repository".
func (s *Server) OpenTargetID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sess.ID
}

// Pending is what is currently waiting on the reviewer's decision.
func (s *Server) Pending() domain.Pending {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pending
}

// ClearPending forgets the waiting work, because it has been taken into the
// review or the reviewer has moved to it.
func (s *Server) ClearPending() {
	s.mu.Lock()
	s.pending = domain.Pending{}
	s.mu.Unlock()
	s.broadcast("pending")
}

// BranchView is every branch the remote has, with how far each has drifted
// (ADR 0026).
type BranchView struct {
	Base     string
	Branches []domain.Branch
}

// branchRow is a branch as the page reads it: the facts, plus how long ago.
type branchRow struct {
	domain.Branch
	Ago string
}

// BranchesOf lists the branches of the repository a review belongs to. Nil when
// there is no remote to ask about.
type BranchesOf func(targetID string) BranchView

// WithBranches wires the branches page.
func (s *Server) WithBranches(of BranchesOf) *Server {
	s.branchesOf = of
	return s
}

// RemoteWatchState reports whether msr is fetching, and how often.
type RemoteWatchState func() (on bool, every time.Duration)

// SetRemoteWatch changes it while msr runs.
type SetRemoteWatch func(on bool, every time.Duration) error

// WithRemoteWatch wires the fetch toggle. Reading the state and changing it are
// separate, so a build that can report without being able to change still says
// what it is doing.
func (s *Server) WithRemoteWatch(state RemoteWatchState, set SetRemoteWatch) *Server {
	s.remoteWatch, s.setRemoteWatch = state, set
	return s
}

// handleBranches lists what everyone is working on.
func (s *Server) handleBranches(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	of := s.branchesOf
	work := s.workLocked()
	on, every := false, time.Duration(0)
	if s.remoteWatch != nil {
		on, every = s.remoteWatch()
	}
	s.mu.RUnlock()

	if of == nil {
		http.NotFound(w, r)
		return
	}
	view := of(s.openSession(r).ID)

	// Ages are formatted here rather than carried on the domain type: how long
	// ago something was is a presentation question, and the answer differs by
	// where it is shown.
	rows := make([]branchRow, 0, len(view.Branches))
	for _, b := range view.Branches {
		rows = append(rows, branchRow{Branch: b, Ago: usecase.Ago(s.now().Sub(b.TS))})
	}

	s.render(w, "branches.html", struct {
		Base     string
		Repo     string
		Branches []branchRow
		Work     []Work
		Watching bool
		Every    string
	}{
		Base: view.Base, Repo: s.openSession(r).Repo, Branches: rows, Work: work,
		Watching: on, Every: every.String(),
	})
}

// handleRemoteWatch turns fetching on or off while msr runs.
func (s *Server) handleRemoteWatch(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	set := s.setRemoteWatch
	s.mu.RUnlock()
	if set == nil {
		http.NotFound(w, r)
		return
	}

	// An unchecked box sends nothing at all, so absent has to mean off — treat
	// it as "leave as it was" and it could never be turned off.
	on := r.FormValue("watch") != ""

	every := 2 * time.Minute
	if d, err := time.ParseDuration(strings.TrimSpace(r.FormValue("every"))); err == nil && d > 0 {
		every = d
	}

	if err := set(on, every); err != nil {
		s.mu.Lock()
		s.agentErr = err.Error()
		s.mu.Unlock()
	} else {
		s.Record(AuditEntry{Action: "remote-watch",
			Detail: fmt.Sprintf("fetching %s (every %s)", onOff(on), every)})
	}
	http.Redirect(w, r, "/status", http.StatusSeeOther)
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// gitsnapIgnoreFile is what the rules file is called, named here so the page
// can tell a reviewer where the rule they are looking at came from.
const gitsnapIgnoreFile = ".msrignore"

// ShowAllFunc is told when a reviewer wants the hidden files back, so the next
// build of the review includes them.
type ShowAllFunc func(all bool)

// WithShowAll wires the "show them anyway" toggle.
func (s *Server) WithShowAll(fn ShowAllFunc) *Server {
	s.showAll = fn
	return s
}

// Forget drops every cached review, so the next look rebuilds it.
//
// Needed when something changes what a review *contains* rather than what is
// stored about it — turning the ignore rules off is the case that exists — and
// a cache built under the old rules would otherwise answer for it.
func (s *Server) Forget() {
	s.mu.Lock()
	s.loaded = map[string]Session{}
	current, loader := s.sess.ID, s.loader
	s.mu.Unlock()

	if loader == nil || current == "" {
		return
	}
	// The open review is not in that cache — it is the field itself — so it is
	// rebuilt here or it would be the one thing still showing the old rules.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if rebuilt, err := loader(ctx, current); err == nil && rebuilt.ID != "" {
		s.mu.Lock()
		rebuilt.Narrative = s.sess.Narrative
		s.sess = rebuilt
		s.mu.Unlock()
	}
	s.broadcast("units")
}

// LogView is recent history as the card shows it, with where the branch sits
// against its upstream (issue #18).
type LogView struct {
	Entries []usecase.LogEntry
	Remote  domain.RemoteState
}

// LogOf builds the log for whichever review is open.
//
// It takes the target's id as well as its ref because a workspace spans
// repositories: the id says which one to read history from, and a card that
// kept showing the repository msr started in would be showing somebody else's
// commits under this one's name.
type LogOf func(targetID, reviewingRef string) LogView

// WithLog wires the git log card.
func (s *Server) WithLog(of LogOf) *Server {
	s.logOf = of
	return s
}

// RunAnalysisFunc runs one audit over one target. AnalysisOf reads back
// whatever the last run of one recorded.
type RunAnalysisFunc func(ctx context.Context, targetID string, kind domain.AnalysisKind) error

// AnalysisOf reads one audit's stored result, zero when it has never run.
type AnalysisOf func(targetID string, kind domain.AnalysisKind) domain.Analysis

// WithAnalyses wires the audit cards: running one, and reading back what the
// last run found (ADR 0024).
func (s *Server) WithAnalyses(run RunAnalysisFunc, of AnalysisOf) *Server {
	s.runAnalysis, s.analysisOf = run, of
	return s
}

// handleAnalysis runs one audit, and only that one. Each is an independent
// reading of the same change: nothing here reads another audit's result, and
// running one never triggers another.
func (s *Server) handleAnalysis(w http.ResponseWriter, r *http.Request) {
	kind := domain.AnalysisKind(r.PathValue("kind"))
	audit, known := usecase.AuditFor(kind)
	if !known {
		http.NotFound(w, r)
		return
	}

	s.mu.RLock()
	run := s.runAnalysis
	s.mu.RUnlock()
	if run == nil {
		http.NotFound(w, r)
		return
	}

	// Every link on the page carries a ref rather than an id, so this resolves
	// it the way the rest of the app does — but refuses an unknown one rather
	// than auditing whatever happens to be open.
	target, ok := s.actionTarget(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Registered before the goroutine starts, so the card shows "running" on
	// the redirect rather than a tick later.
	done := s.BeginWork(string(kind), target, audit.Title)
	go func() {
		// A panicking audit must not take the server down, and must not leave
		// the card spinning forever.
		defer func() {
			if p := recover(); p != nil {
				done(fmt.Errorf("%s audit panicked: %v", kind, p))
				s.broadcast("analysis")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		done(run(ctx, target, kind))
		s.broadcast("analysis")
	}()

	http.Redirect(w, r, backTo(r, "#analyses"), http.StatusSeeOther)
}

// SignoffFunc records that a reviewer has finished with a target; SignoffOf
// reads back whatever was recorded for one.
type SignoffFunc func(ctx context.Context, v domain.Signoff) error

// SignoffOf reads a target's stored verdict. It returns a zero Signoff for one
// nobody has finished with, which is the ordinary state.
type SignoffOf func(targetID string) domain.Signoff

// WithSignoff wires finishing a review, and reading back that it was finished.
func (s *Server) WithSignoff(sign SignoffFunc, of SignoffOf) *Server {
	s.signoff, s.signoffOf = sign, of
	return s
}

// handleSignoff records that the reviewer is done with what they were reading,
// and whatever they wanted to say about it as a whole.
func (s *Server) handleSignoff(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	sign := s.signoff
	s.mu.RUnlock()
	if sign == nil {
		http.NotFound(w, r)
		return
	}

	target, ok := s.actionTarget(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	sess := s.openSession(r)
	if sess.ID != target {
		// Named a review this server can resolve but is not showing: sign off
		// what was named, never what is on screen.
		http.NotFound(w, r)
		return
	}
	// Fingerprinting what was reviewed is what lets a later visit say whether
	// the code has moved underneath the judgement (ADR 0021).
	stats := make([]domain.FileStat, 0, len(sess.Units))
	for _, u := range sess.Units {
		added, removed := usecase.Churn(sess.Diffs[u.ID])
		for _, f := range u.Files {
			stats = append(stats, domain.FileStat{Path: f, Added: added, Removed: removed})
		}
	}

	v := domain.Signoff{
		TargetID: sess.ID,
		At:       s.now(),
		Comment:  strings.TrimSpace(r.FormValue("comment")),
		Print:    usecase.ReviewFingerprint(stats),
		Files:    len(stats),
	}
	if err := sign(r.Context(), v); err != nil {
		s.mu.Lock()
		s.signErr = err.Error()
		s.mu.Unlock()
	} else {
		s.mu.Lock()
		s.signErr = ""
		s.mu.Unlock()
		s.Record(AuditEntry{SessionID: sess.ID, Action: "signoff",
			Detail: firstNonBlank(v.Comment, "reviewed, no comment")})
	}

	s.broadcast("signoff")
	http.Redirect(w, r, backTo(r, "#reviewcard"), http.StatusSeeOther)
}

func firstNonBlank(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// IncludeFunc extends the open review to cover the work that arrived after it
// was opened. SplitFunc instead opens a review of only that work, returning the
// ref to send the reviewer to.
type IncludeFunc func(ctx context.Context, targetID string) error

// SplitFunc opens a review of only the work that has arrived since.
type SplitFunc func(ctx context.Context, targetID string) (string, error)

// WithLiveActions wires the two ways of resolving work that arrived mid-review.
// The third way — carrying on reading — needs no server at all.
func (s *Server) WithLiveActions(include IncludeFunc, split SplitFunc) *Server {
	s.include, s.split = include, split
	return s
}

// handleInclude folds the waiting work into the review being read.
func (s *Server) handleInclude(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	include := s.include
	s.mu.RUnlock()
	if include == nil {
		http.NotFound(w, r)
		return
	}

	target := strings.TrimSpace(r.FormValue("target"))
	if err := include(r.Context(), target); err != nil {
		// Leave the choice on the page: silently clearing it would lose the
		// notification along with the failure.
		s.Record(AuditEntry{SessionID: target, Action: "include-failed", Detail: err.Error()})
		http.Redirect(w, r, backTo(r, "#pending"), http.StatusSeeOther)
		return
	}

	s.ClearPending()
	s.Record(AuditEntry{SessionID: target, Action: "include",
		Detail: "took the work that arrived into this review"})
	http.Redirect(w, r, "/?target="+url.QueryEscape(target), http.StatusSeeOther)
}

// handleSplit leaves this review as it stands and opens one of only what has
// arrived since it was opened.
func (s *Server) handleSplit(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	split := s.split
	s.mu.RUnlock()
	if split == nil {
		http.NotFound(w, r)
		return
	}

	target := strings.TrimSpace(r.FormValue("target"))
	ref, err := split(r.Context(), target)
	if err != nil {
		s.Record(AuditEntry{SessionID: target, Action: "split-failed", Detail: err.Error()})
		http.Redirect(w, r, backTo(r, "#pending"), http.StatusSeeOther)
		return
	}

	s.ClearPending()
	s.Record(AuditEntry{SessionID: target, Action: "split",
		Detail: "opened a review of only what arrived since"})
	http.Redirect(w, r, "/?target="+url.QueryEscape(ref), http.StatusSeeOther)
}

// Pulse tells every open page what just moved in the repository. Nothing to
// say sends nothing: the watcher looks every couple of seconds and almost
// always finds silence, and waking every page for that would undo the point of
// pushing rather than polling.
func (s *Server) Pulse(pulses []domain.Pulse) {
	if len(pulses) == 0 {
		return
	}
	data, err := json.Marshal(pulses)
	if err != nil {
		return
	}

	s.mu.Lock()
	s.pulses++
	s.mu.Unlock()

	s.send(sseEvent{Name: "pulse", Data: string(data)})
}

// PulsesSent is how many pulse events have gone out, for tests and /status.
func (s *Server) PulsesSent() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pulses
}

// handleEvents streams server-sent events so an open page updates itself as the
// review changes — a narrated chapter, a new annotation, a re-analysed headline
// — without polling.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ch := make(chan sseEvent, 8)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // defeat proxy buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// A heartbeat keeps the connection (and any intermediary) from timing out,
	// and is how a dead client is noticed.
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, ev.Data); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// noteKinds are the annotations a reviewer may attach (SPEC §11).
var noteKinds = map[string]domain.NoteKind{
	"ok":        domain.NoteOK,
	"question":  domain.NoteQuestion,
	"objection": domain.NoteObjection,
	"debt":      domain.NoteDebt,
	"note":      domain.NoteNote,
}

// handleAnnotate attaches a note to a unit and persists it. Annotations anchor
// to unit ids, never file/line — the working tree moves, unit ids do not.
func (s *Server) handleAnnotate(w http.ResponseWriter, r *http.Request) {
	unit, ok := s.unitIn(r, r.PathValue("id"))
	if !ok {
		http.Error(w, "unknown unit", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	kind, ok := noteKinds[r.FormValue("kind")]
	if !ok {
		http.Error(w, "unknown note kind", http.StatusBadRequest)
		return
	}

	note := domain.Note{
		ID:        s.newID(),
		SessionID: unit.SessionID,
		UnitID:    unit.ID,
		Kind:      kind,
		Text:      strings.TrimSpace(r.FormValue("text")),
		TS:        s.now(),
		// Empty when the note is about the file as a whole, which is still the
		// common case (ADR 0028).
		Anchor: r.FormValue("anchor"),
	}
	if nth, err := strconv.Atoi(r.FormValue("nth")); err == nil && nth > 0 {
		note.AnchorNth = nth
	}
	if s.notes != nil {
		if err := s.notes.AppendNote(note); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	// Against the review it was made on, not against whichever one msr started
	// with: a note appended to the wrong session vanishes on the redirect and
	// turns up on something unrelated.
	s.noteOn(unit.SessionID, note)
	s.record(AuditEntry{SessionID: unit.SessionID, UnitID: unit.ID,
		Action: "annotate", Detail: string(note.Kind) + ": " + note.Text})
	s.broadcast("note")

	http.Redirect(w, r, backTo(r, "#unit-"+unit.ID), http.StatusSeeOther)
}

// noteOn adds an annotation to the review it belongs to, wherever that review
// is held — the open one is a field, any other is in the loaded cache.
func (s *Server) noteOn(targetID string, note domain.Note) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if targetID == "" || targetID == s.sess.ID {
		s.sess.Notes = append(s.sess.Notes, note)
		return
	}
	if cached, ok := s.loaded[targetID]; ok {
		cached.Notes = append(cached.Notes, note)
		s.loaded[targetID] = cached
	}
}

// backTo is the page an action should return to: the review it acted on, at the
// thing it acted on.
func backTo(r *http.Request, fragment string) string {
	if target := r.URL.Query().Get("target"); target != "" {
		return "/?target=" + url.QueryEscape(target) + fragment
	}
	return "/" + fragment
}

// unitIn finds a unit in whichever review is being acted on. Looking only in the
// review the server started with is what made every action fail the moment a
// reviewer switched target: the id was real, just not there.
func (s *Server) unitIn(r *http.Request, id string) (domain.Unit, bool) {
	for _, u := range s.openSession(r).Units {
		if u.ID == id {
			return u, true
		}
	}
	return domain.Unit{}, false
}

// unitView is the presentation shape of a unit: everything the template needs,
// already computed, so templates stay logic-free.
type unitView struct {
	ID       string
	Handle   string
	File     string
	Headline string
	Why      string
	WhySrc   string
	Flags    []string
	Added    int
	Removed  int
	Diff     []diffLine
	Notes    []domain.Note
	// Orphaned are notes whose line is no longer in this diff. Shown, and shown
	// as such: a judgement about code that has gone must neither vanish nor
	// read as current (ADR 0021, ADR 0028).
	Orphaned []domain.Note
	Model    string

	// Edits is how the file reached this net change: a net-change review
	// collapses the agent's back-and-forth (ADR 0002), and this opens it back up.
	// Meaning is what this one file's change is for, when a model has been asked.
	Meaning string
	// Name is the file without its directory: the group heading above it already
	// says where it is, and repeating the path in every row is noise.
	Name string
	// Highlights are the one to three lines the model called out as the ones
	// worth looking at, written with the description.
	Highlights []string
	Edits      int
	LastEdited string
	FirstEdit  string
	History    []editView
}

// editView is one recorded touch of a file, prepared for display.
type editView struct {
	When   string
	Tool   string
	Intent string
	Failed bool
}

type diffLine struct {
	Kind string // add | del | hunk | ctx | meta
	Text string
	// Nth is which occurrence of this exact text the line is, so a note written
	// on it can be found again among identical lines (ADR 0028).
	Nth   int
	Notes []domain.Note
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	question := strings.TrimSpace(r.FormValue("question"))
	if question == "" {
		http.Redirect(w, r, backTo(r, "#ask"), http.StatusSeeOther)
		return
	}

	target := s.openSession(r).ID

	answer, failed := "(no assistant configured)", true
	if s.ask != nil {
		// Only what was asked about *this* review. Another review's questions
		// are not context, they are a different conversation.
		history := s.Conversation(target)

		finish := s.BeginWork("ask", target, question)
		got, err := s.ask(context.WithoutCancel(r.Context()), target, question, history)
		finish(err)
		if err != nil {
			answer = err.Error()
		} else {
			answer, failed = got, false
		}
	}

	s.mu.Lock()
	exchange := Exchange{
		ID: s.newID(), SessionID: target, Question: question,
		Answer: answer, Failed: failed, TS: s.now(),
	}
	s.thread[target] = append(s.thread[target], exchange)
	sessionID := target
	keep := s.exchanges
	s.mu.Unlock()

	// A review conversation is part of the review: it must outlive the process.
	if keep != nil {
		if err := keep.AppendExchange(exchange); err != nil {
			fmt.Fprintln(os.Stderr, "msr: could not store the exchange:", err)
		}
	}
	s.record(AuditEntry{SessionID: sessionID, Action: "ask", Detail: question})
	s.broadcast("answer")

	http.Redirect(w, r, backTo(r, "#ask"), http.StatusSeeOther)
}

// handleReanalyse re-summarises one unit with the configured model, replacing
// its headline and recording which model produced it.
func (s *Server) handleReanalyse(w http.ResponseWriter, r *http.Request) {
	unit, ok := s.unitIn(r, r.PathValue("id"))
	if !ok {
		http.Error(w, "unknown unit", http.StatusNotFound)
		return
	}
	if s.reanalyse == nil {
		http.Error(w, "no analyser configured", http.StatusBadRequest)
		return
	}

	finishWork := s.BeginWork("reanalyse", unit.SessionID, strings.Join(unit.Files, ", "))
	headline, model, err := s.reanalyse(context.WithoutCancel(r.Context()), unit)
	finishWork(err)
	if err != nil {
		// A failing model must not lose the existing headline.
		s.record(AuditEntry{SessionID: unit.SessionID, UnitID: unit.ID,
			Action: "reanalyse", Detail: "failed: " + err.Error()})
		http.Redirect(w, r, backTo(r, "#unit-"+unit.ID), http.StatusSeeOther)
		return
	}

	s.mu.Lock()
	for i := range s.sess.Units {
		if s.sess.Units[i].ID == unit.ID {
			s.sess.Units[i].Headline = headline
			break
		}
	}
	s.models[unit.ID] = model
	s.mu.Unlock()
	s.record(AuditEntry{SessionID: unit.SessionID, UnitID: unit.ID,
		Action: "reanalyse", Detail: model})
	s.broadcast("headline")

	http.Redirect(w, r, backTo(r, "#unit-"+unit.ID), http.StatusSeeOther)
}

// record appends to the audit log. A log that cannot be written must not break
// the review, so the error is swallowed after a best-effort attempt.
func (s *Server) record(e AuditEntry) {
	if s.audit == nil {
		return
	}
	e.TS = s.now()
	_ = s.audit.Append(e)
}

// Record logs an interaction that happened outside a request — narration, most
// of all, which runs in the background and is otherwise invisible. Open pages
// are told, so /activity updates itself as calls complete.
func (s *Server) Record(e AuditEntry) {
	s.record(e)
	s.broadcast("activity")
}

// Narrating reports whether a narration is under way, so a page can say so
// rather than looking idle.
func (s *Server) Narrating() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.narrating
}

// beginNarration claims the single narration slot, reporting false if one is
// already running. Model calls are the scarce resource here: a reviewer who
// clicks twice, or two open tabs, must not start two narrations.
func (s *Server) beginNarration() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.narrating || s.narrate == nil {
		return false
	}
	s.narrating = true
	return true
}

func (s *Server) endNarration() {
	s.mu.Lock()
	s.narrating = false
	s.mu.Unlock()
	s.broadcast("narrative")
}

// handleNarrate re-narrates the session on request. It returns straight away:
// the reviewer asked for a story, not for their browser to hang for a minute.
func (s *Server) handleNarrate(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	wired := s.narrate != nil
	s.mu.RUnlock()
	if !wired {
		http.NotFound(w, r)
		return
	}

	// The request context dies when this handler returns, so the background work
	// gets one that outlives it. Already running is not an error: the reviewer
	// gets the story either way.
	// Whatever is open is what gets narrated: a commit and a tag are as
	// narratable as a recorded run (ADR 0017).
	target := s.openSession(r).ID
	s.NarrateNow(context.WithoutCancel(r.Context()), target)
	http.Redirect(w, r, "/?target="+target, http.StatusSeeOther)
}

// NarrateNow starts narration in the background unless one is already running,
// reporting whether it started. Both the startup narration and the reviewer's
// retry go through here, so the two can never overlap.
func (s *Server) NarrateNow(ctx context.Context, targetID string) bool {
	if !s.beginNarration() {
		return false
	}
	// Registered before the goroutine starts, so the page shows the work the
	// instant the request returns rather than whenever the scheduler gets to it.
	finish := s.BeginWork("narrate", targetID, "reading the review")

	go func() {
		defer s.endNarration()
		// A model adapter that panics must not take the server with it, and must
		// not leave the page claiming to still be thinking. Both were possible.
		defer func() {
			if r := recover(); r != nil {
				finish(fmt.Errorf("the narrator failed: %v", r))
				return
			}
			finish(nil) // the narrator records its own outcome in the audit log
		}()
		s.narrate(ctx, targetID)
	}()
	s.broadcast("narrative") // tell open pages it has started
	return true
}

// reviewStatus answers, for whatever is open: has the assistant read this, when,
// how far has it got, and can I ask it to. Switching target is exactly when a
// reviewer needs all four, and before this they were spread across three pages.
type reviewStatus struct {
	State     string // read | reading | unread
	When      string
	Model     string
	Chapters  int
	Described int
	Groups    int
	CanRead   bool
}

// describeReview builds that answer. It is presentation only: every fact comes
// from the stored story or from work already in flight.
func describeReview(n domain.Narrative, groupIDs []string, reading, canRead bool, now time.Time) reviewStatus {
	// Group descriptions and per-file descriptions share one map, keyed
	// differently. Counting the map counted both, so describing a few files
	// pushed the card to "8/4 described" — a ratio that cannot be true.
	described := 0
	for _, id := range groupIDs {
		if strings.TrimSpace(n.Meanings[id]) != "" {
			described++
		}
	}

	st := reviewStatus{
		Model: n.Model, Chapters: len(n.Chapters),
		Described: described, Groups: len(groupIDs),
		CanRead: canRead && !reading,
	}
	switch {
	case reading:
		st.State = "reading"
	case n.Source == domain.NarrativeModel:
		st.State = "read"
		if !n.WrittenAt.IsZero() {
			st.When = humanDuration(now.Sub(n.WrittenAt)) + " ago"
		}
	default:
		st.State = "unread"
	}
	return st
}

// shortOf is the identifying half of a "hash · author" subtitle.
func shortOf(subtitle string) string {
	if hash, _, found := strings.Cut(subtitle, " · "); found {
		return hash
	}
	return usecase.Brief(subtitle, 12)
}

// dirName is how a folder is written on the page. The repository root arrives
// as "." from the grouping, which is correct as a path and wrong as a label.
func dirName(dir string) string {
	if dir == "." || dir == "" {
		return "root"
	}
	return dir
}

// groupIDs is the identity of each group on the page, which is what the
// progress count has to be measured against.
func groupIDs(groups []groupView) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.ID)
	}
	return out
}

// cockpitStats is the session's numbers rendered for display: durations and
// counts as strings, so the template holds no formatting logic.
// render writes a template to a buffer first. Executing straight into the
// response streams output, so an error partway through leaves a half-written
// page already committed to a 200 — which is how a missing field looked like a
// blank cockpit rather than a mistake.
func (s *Server) render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// plural is the noun for a count, so a tile reads "1 file" rather than "1 files".
func plural(noun string, n int) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// statTile is one number worth showing for this review.
type statTile struct {
	Value string
	Label string
	Hint  string
	// Lines renders as a +/- pair rather than a single figure.
	Added, Removed int
	IsLines        bool
	// IsRef marks an identifier rather than a quantity — a hash, a tag name.
	// Counts may wrap mid-token because a wrapped number is still readable; a
	// hash broken across two lines just looks broken.
	IsRef bool
}

// cockpitStats is the panel's numbers, chosen for what is being reviewed.
type cockpitStats struct {
	Live  bool
	Tiles []statTile
}

// statsFor picks the numbers that mean something for this kind of review.
//
// Every kind used to show the same six, which meant a single commit reported
// "1 commit" and "0 PRs" — answering questions nobody asked, and crowding out
// the ones they did. The token total is gone entirely: it is what the assistant
// has spent since msr started, across every review, and sitting it beside this
// review's file counts read as though it belonged to this one. It lives on
// /status, where there is room to say what it is.
func statsFor(kind domain.TargetKind, st domain.SessionStats, subtitle string) cockpitStats {
	out := cockpitStats{Live: st.Live}
	add := func(t statTile) { out.Tiles = append(out.Tiles, t) }

	// Every review is some files and some lines. Nothing else is universal.
	add(statTile{Value: thousands(st.Files), Label: plural("file", st.Files)})
	add(statTile{IsLines: true, Added: st.Added, Removed: st.Removed, Label: "lines"})

	switch kind {
	case domain.TargetCommit:
		if subtitle != "" {
			// The hash alone. A subtitle is "hash · author", and an author name
			// is unbounded — in a tile this narrow it broke the hash across
			// three lines to make room for a name nobody is reading here.
			add(statTile{Value: shortOf(subtitle), Label: "commit", IsRef: true})
		}
	case domain.TargetWorktree:
		add(statTile{Value: "—", Label: "uncommitted", Hint: "not committed yet"})
	case domain.TargetLive:
		// "Watching" rather than a number: this review has no fixed size, and a
		// count that changes under the reader is worse than no count at all.
		add(statTile{Value: "live", Label: "watching HEAD", IsRef: true,
			Hint: "updates as files change, and follows HEAD when you commit"})
	case domain.TargetTag, domain.TargetPR, domain.TargetRange:
		add(statTile{Value: thousands(st.Commits), Label: plural("commit", st.Commits)})
		if st.PullRequests > 0 || kind == domain.TargetTag {
			add(statTile{Value: thousands(st.PullRequests), Label: "PRs",
				Hint: "counted from commit subjects — msr talks to no forge"})
		}
	case domain.TargetSession:
		add(statTile{Value: humanDuration(st.Open), Label: "open",
			Hint: "how long the agent run went on"})
		if st.Commits > 0 {
			add(statTile{Value: thousands(st.Commits), Label: plural("commit", st.Commits)})
		}
		if st.PullRequests > 0 {
			add(statTile{Value: thousands(st.PullRequests), Label: "PRs"})
		}
	default:
		// An unrecognised kind shows whatever is actually there. Better to show a
		// real number than to hide it because the kind was not anticipated.
		if st.Open > 0 {
			add(statTile{Value: humanDuration(st.Open), Label: "open"})
		}
		if st.Commits > 0 {
			add(statTile{Value: thousands(st.Commits), Label: plural("commit", st.Commits)})
		}
		if st.PullRequests > 0 {
			add(statTile{Value: thousands(st.PullRequests), Label: "PRs"})
		}
	}
	return out
}

// feedItem is one change in the cockpit stream: a sentence and its diff, with
// the diff compacted when it would otherwise swamp everything else.
type feedItem struct {
	unitView
	Compacted bool
}

// groupView is a set of files that changed together, with one model-written
// sentence about what they are for.
type groupView struct {
	ID string
	// Dir is the folder the group covers, as a person would name it. Files at
	// the repository root group under ".", which beside real directory names
	// reads as a rendering fault rather than as a place.
	Dir     string
	Meaning string
	Added   int
	Removed int
	Files   []feedItem
}

// handleCockpit renders the session at a glance: whether it is still moving, a
// stream of what changed, and the numbers behind it. It is the one page meant
// to be left open on a second screen while an agent works.
func (s *Server) handleCockpit(w http.ResponseWriter, r *http.Request) {
	// Which session is being read may differ from the one this server started
	// with: a workspace spans repositories, and the switcher moves between them.
	// Asked for before the session is resolved: it changes what gets built.
	if s.showAll != nil {
		s.showAll(r.URL.Query().Get("all") == "1")
	}
	sess := s.openSession(r)

	s.mu.RLock()
	views := s.viewsOf(sess)
	pending := s.pending
	signoffOf := s.signoffOf
	canSignoff := s.signoff != nil
	logOf := s.logOf
	analysisOf := s.analysisOf
	// Reading what an audit found and being able to run one are separate: a
	// stored result is worth showing even where nothing can be run.
	canAnalyse := s.runAnalysis != nil || s.analysisOf != nil
	canRunAnalysis := s.runAnalysis != nil
	stats := sess.Stats
	narrative := sess.Narrative
	work := s.workLocked()
	canDescribe := s.describe != nil
	canDescribeFile := s.describeFile != nil
	workspace := s.workspace
	targets := s.targets
	repos := s.repos
	canCompare := s.compare != nil
	thread := []Exchange(nil) // filled after the lock; loading it may write
	hasAsk := s.ask != nil
	hasReanal := s.reanalyse != nil
	// Any target can be narrated now, not only the one this server started with.
	canRetry := s.narrate != nil && narrative.Source != domain.NarrativeModel
	narrating := s.narrating
	s.mu.RUnlock()

	// What was signed off is compared against what is on screen now, so a
	// verdict about code that has since moved says so rather than reading as
	// current (ADR 0021).
	var signoff usecase.SignoffView
	if signoffOf != nil {
		nowStats := make([]domain.FileStat, 0, len(sess.Units))
		for _, u := range sess.Units {
			added, removed := usecase.Churn(sess.Diffs[u.ID])
			for _, f := range u.Files {
				nowStats = append(nowStats, domain.FileStat{Path: f, Added: added, Removed: removed})
			}
		}
		signoff = usecase.SignoffState(signoffOf(sess.ID),
			usecase.ReviewFingerprint(nowStats), len(nowStats), s.now())
	}

	// Which audits are in flight right now, so a card can say "running" rather
	// than looking idle while a model is thinking about it.
	running := map[string]bool{}
	failedAudit := map[string]string{}
	for _, w := range work {
		switch {
		case w.Running():
			running[w.Kind] = true
			delete(failedAudit, w.Kind)
		case w.Err != "":
			failedAudit[w.Kind] = w.Err
		default:
			// A later success clears an earlier failure.
			delete(failedAudit, w.Kind)
		}
	}
	// Outside the lock: the first read of a review's conversation loads it from
	// the store and remembers it, which is a write.
	thread = s.Conversation(sess.ID)

	// The log is built against the review being read, so it can mark it.
	var gitlog LogView
	if logOf != nil {
		gitlog = logOf(sess.ID, sess.Target.Ref)
	}

	// An audit fingerprints the units it read, so the same function decides
	// whether what it read is still what is on screen.
	analyses := analysisCards(analysisOf, running, failedAudit, sess.ID, usecase.Fingerprint(sess.Units), s.now())

	byID := map[string]unitView{}
	for _, v := range views {
		byID[v.ID] = v
	}

	// Chapters carry only prose here; the files they cover live in the column
	// beside them, and the two are linked by unit id so the scrolls can track
	// each other.
	chapters := make([]chapterView, 0, len(narrative.Chapters))
	for _, c := range narrative.Chapters {
		cv := chapterView{Title: c.Title, Prose: c.Prose}
		for _, id := range c.UnitIDs {
			if v, ok := byID[id]; ok {
				cv.Units = append(cv.Units, v)
			}
		}
		if len(cv.Units) > 0 {
			cv.Anchor = cv.Units[0].ID
		}
		chapters = append(chapters, cv)
	}

	// Newest first: a cockpit answers "what just happened".
	ordered := make([]domain.Unit, 0, len(sess.Units))
	for i := len(sess.Units) - 1; i >= 0; i-- {
		ordered = append(ordered, sess.Units[i])
	}
	viewByID := map[string]unitView{}
	for _, v := range views {
		viewByID[v.ID] = v
	}

	// Files that changed together are shown together: five files under one
	// package is one act of work, not five entries.
	groups := make([]groupView, 0, 8)
	for _, g := range usecase.GroupChanges(ordered, sess.Diffs) {
		gv := groupView{
			ID: g.ID, Dir: dirName(g.Dir), Added: g.Added, Removed: g.Removed,
			Meaning: narrative.Meanings[g.ID],
		}
		for _, u := range g.Units {
			v := viewByID[u.ID]
			v.Meaning = narrative.Meanings[usecase.FileKey(u)]
			v.Highlights = narrative.Highlights[usecase.FileKey(u)]
			compact, wasCompacted := usecase.CompactDiff(sess.Diffs[u.ID], cockpitDiffLines)
			if wasCompacted {
				// Compacting drops lines, which can take an anchored note with
				// them — so it is reported as orphaned rather than lost.
				v.Diff, v.Orphaned = splitDiffWithNotes(compact.Text, notesFor(sess.Notes, u.ID))
			}
			gv.Files = append(gv.Files, feedItem{unitView: v, Compacted: wasCompacted})
		}
		groups = append(groups, gv)
	}

	data := struct {
		Session         Session
		Workspace       []SessionSummary
		Targets         []TargetSummary
		Repos           []RepoStatus
		CanCompare      bool
		Review          reviewStatus
		Stats           cockpitStats
		Narrative       domain.Narrative
		Chapters        []chapterView
		Groups          []groupView
		Tree            []domain.TreeNode
		CanDescribe     bool
		CanDescribeFile bool
		Work            []Work
		Thread          []Exchange
		HasAsk          bool
		HasThread       bool
		HasReanal       bool
		CanRetry        bool
		Narrating       bool
		Pending         domain.Pending
		Signoff         usecase.SignoffView
		HasSignoff      bool
		CanSignoff      bool
		Analyses        []analysisCard
		CanAnalyse      bool
		CanRunAnalysis  bool
		Log             LogView
		HasLog          bool
		Hidden          []usecase.Hidden
		IgnoreFile      string
	}{
		Session: sess, Workspace: workspace, Targets: targets,
		Repos: repos, CanCompare: canCompare,
		Pending: pending,
		Signoff: signoff, HasSignoff: signoffOf != nil, CanSignoff: canSignoff,
		Analyses: analyses, CanAnalyse: canAnalyse, CanRunAnalysis: canRunAnalysis,
		Log: gitlog, HasLog: logOf != nil && len(gitlog.Entries) > 0,
		Hidden: sess.Hidden, IgnoreFile: gitsnapIgnoreFile,
		HasThread: len(thread) > 0,
		Review:    describeReview(narrative, groupIDs(groups), narrating, s.narrate != nil, s.now()),
		Stats:     statsFor(sess.Target.Kind, stats, sess.Target.Subtitle),
		Narrative: narrative, Chapters: chapters, Groups: groups,
		Tree:        usecase.FileTree(sess.Units, sess.Diffs),
		CanDescribe: canDescribe, CanDescribeFile: canDescribeFile, Work: work,
		Thread: thread, HasAsk: hasAsk, HasReanal: hasReanal,
		CanRetry: canRetry, Narrating: narrating,
	}

	s.render(w, "cockpit.html", data)
}

// tokenTotal is the tokens spent so far, or blank if no model call has happened.
func tokenTotal(u port.TokenUsage) string {
	if u.Calls == 0 {
		return ""
	}
	return thousands(u.Prompt + u.Completion)
}

// cockpitDiffLines is how much of one diff the feed shows before compacting. It
// is small on purpose: the cockpit is for noticing a change, and the review page
// is one click away for reading it properly.
const cockpitDiffLines = 14

// humanDuration renders a span the way someone reads a clock, not a stopwatch.
func humanDuration(d time.Duration) string {
	if d <= 0 {
		return "just started"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// handleTutorial explains the page to someone who has just installed msr.
//
// Everything the cockpit does is discoverable by clicking around for twenty
// minutes. This is for the first five: three columns, a picker, flags and a
// keyboard nobody has been told about are a lot to meet at once.
func (s *Server) handleTutorial(w http.ResponseWriter, r *http.Request) {
	s.render(w, "tutorial.html", nil)
}

// handleStatus answers "is everything working": the reviewer's own model, what
// it has cost, and every session known for this project.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	agent := s.agent
	repos := s.repos
	work := s.workLocked()
	candidates := s.candidates
	agentErr := s.agentErr
	canConfigure := s.configure != nil
	// Whether msr is fetching has to be visible: it writes to the repository
	// and talks to the network, and neither should be inferable only from how
	// the process was started (ADR 0026).
	hasWatch, canWatch := s.remoteWatch != nil, s.setRemoteWatch != nil
	watching, watchEvery := false, ""
	if hasWatch {
		on, every := s.remoteWatch()
		// The interval is shown in the syntax the form accepts, so what is on
		// the page can be typed straight back into it. humanDuration drops
		// seconds, which is right for "open 2h 10m" and wrong for "every 45s".
		watching, watchEvery = on, every.String()
	}
	repoErr := s.repoErr
	canAddRepo := s.addRepo != nil
	sessions := s.workspace
	sessionID := s.sess.ID
	repo := s.sess.Repo
	s.mu.RUnlock()

	u := agent.Usage
	// How much of what the model produced was it thinking to itself. This is the
	// number that decides whether a context window is big enough, and it is also
	// the evidence for whether "skip reasoning" did anything.
	reasoningShare := ""
	skipIgnored := false
	if u.Completion > 0 {
		share := u.Reasoning * 100 / u.Completion
		reasoningShare = fmt.Sprintf("%d%%", share)
		// msr always sends the request to skip it; whether the model's chat
		// template reads it is decided inside the server. If reasoning is still
		// being spent, that answers the question — no guessing required.
		skipIgnored = agent.NoThinking && u.Reasoning > 0
	}

	avg := "—"
	if u.Calls > 0 {
		avg = fmt.Sprintf("%.1fs", float64(u.Millis)/float64(u.Calls)/1000)
	}
	checked := "—"
	if !agent.Checked.IsZero() {
		checked = agent.Checked.Local().Format("15:04:05")
	}

	data := struct {
		SessionID          string
		Repo               string
		Agent              AgentStatus
		Calls, Failures    string
		Prompt, Completion string
		Reasoning, Total   string
		Average, Checked   string
		ReasoningShare     string
		SkipIgnored        bool
		Sessions           []SessionSummary
		Repos              []RepoStatus
		Candidates         []RepoStatus
		Work               []Work
		RepoErr            string
		AgentErr           string
		CanAddRepo         bool
		CanRemoveRepo      bool
		CanConfigure       bool
		WorkloadForm       []WorkloadModel
		Watching           bool
		WatchEvery         string
		CanWatch           bool
		HasWatch           bool
	}{
		SessionID: sessionID, Repo: repo, Agent: agent,
		Calls: thousands(u.Calls), Failures: thousands(u.Failures),
		Prompt: thousands(u.Prompt), Completion: thousands(u.Completion),
		Reasoning: thousands(u.Reasoning), Total: thousands(u.Prompt + u.Completion),
		Average: avg, Checked: checked,
		ReasoningShare: reasoningShare, SkipIgnored: skipIgnored,
		Sessions: sessions, Repos: repos, Candidates: candidates, Work: work,
		RepoErr: repoErr, AgentErr: agentErr,
		CanAddRepo: canAddRepo, CanRemoveRepo: s.removeRepo != nil,
		CanConfigure: canConfigure, WorkloadForm: workloadForm(agent),
		Watching: watching, WatchEvery: watchEvery,
		CanWatch: canWatch, HasWatch: hasWatch,
	}

	s.render(w, "status.html", data)
}

// analysisCard is one audit as the page needs it: what it is, whether it has
// run, and what it said (ADR 0024).
type analysisCard struct {
	Kind    string
	Title   string
	Purpose string
	// State is one of "idle", "running", "failed", "clean", "found" or "stale".
	// The card is coloured and worded from this, so "never run", "could not
	// run" and "found nothing" can never look the same — which is the
	// distinction that matters most on a security card.
	State string
	// Worst is the highest severity found, so the card can be coloured from it
	// and a row of cards read without opening any.
	Worst string
	// Tally reads "1 high · 2 medium", which says more in the same space than
	// "3 to look at".
	Tally    string
	When     string
	Verdict  string
	Err      string
	Findings []domain.Finding
}

// analysisCards builds one card per audit on offer, in a fixed order.
//
// Each is read independently and none is told about the others: two readings of
// the same change are worth more than one reading twice.
func analysisCards(of AnalysisOf, running map[string]bool, failed map[string]string,
	targetID, print string, now time.Time) []analysisCard {

	out := make([]analysisCard, 0, len(usecase.Audits()))
	for _, a := range usecase.Audits() {
		card := analysisCard{
			Kind: string(a.Kind), Title: a.Title, Purpose: a.Purpose, State: "idle",
		}
		if running[string(a.Kind)] {
			card.State = "running"
			out = append(out, card)
			continue
		}
		// A failed audit outranks a stored result: whatever is on the card is
		// from before, and saying so is the only way it is not read as current.
		if why, bad := failed[string(a.Kind)]; bad {
			card.State, card.Err = "failed", why
			out = append(out, card)
			continue
		}

		var got domain.Analysis
		if of != nil {
			got = of(targetID, a.Kind)
		}
		if !got.Done() {
			out = append(out, card)
			continue
		}

		card.Verdict, card.Findings = got.Verdict, got.Findings
		card.When = usecase.Ago(now.Sub(got.At))
		card.Worst = string(got.Worst())
		card.Tally = tallyOf(got)
		switch {
		case got.Print != "" && print != "" && got.Print != print:
			// A reading of a version that no longer exists must not present
			// itself as current (ADR 0021).
			card.State = "stale"
		case got.Clean():
			card.State = "clean"
		default:
			card.State = "found"
		}
		out = append(out, card)
	}
	return out
}

// tallyOf writes the findings as counts by severity, worst first, skipping
// levels with nothing in them.
func tallyOf(a domain.Analysis) string {
	counts := a.Tally()
	parts := make([]string, 0, len(domain.Severities))
	for _, sev := range domain.Severities {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, sev))
		}
	}
	return strings.Join(parts, " · ")
}

// workloadForm is a row per workload for the settings form, pre-filled with
// whatever currently overrides the shared model. A workload on the shared model
// gets empty boxes, which is also how it is put back there.
func workloadForm(agent AgentStatus) []WorkloadModel {
	current := map[string]WorkloadModel{}
	for _, w := range agent.Workloads {
		current[w.Workload] = w
	}

	out := make([]WorkloadModel, 0, len(domain.Workloads))
	for _, w := range domain.Workloads {
		row := WorkloadModel{Workload: string(w)}
		if have, ok := current[string(w)]; ok &&
			(have.Endpoint != agent.Endpoint || have.Model != agent.Model) {
			row.Endpoint, row.Model = have.Endpoint, have.Model
		}
		out = append(out, row)
	}
	return out
}

// thousands groups a count so a six-figure token total stays readable.
func thousands(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// activityView is one audit entry prepared for display.
type activityView struct {
	When    string
	Session string
	Action  string
	Detail  string
	Unit    string
	Model   string
	Took    string
	Failed  bool
}

// shortSession abbreviates a session id to something that fits a column. A ULID
// or a UUID is unreadable in full and unnecessary: the first segment is enough
// to tell two sessions apart at a glance.
func shortSession(id string) string {
	if i := strings.Index(id, "-"); i > 0 {
		return id[:i]
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// handleActivity shows the trail of what the reviewer and the model did: every
// annotation, question, re-analysis and narration, with what each model call
// cost. It renders even with no audit log wired, because the nav always links
// here and a dead link is worse than an empty page.
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	var entries []AuditEntry
	if reader, ok := s.audit.(AuditReader); ok && s.audit != nil {
		got, err := reader.Entries()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		entries = got
	}

	// Newest first: a reviewer wants what just happened, not the beginning.
	rows := make([]activityView, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		row := activityView{
			When: e.TS.Local().Format("01-02 15:04:05"), Session: shortSession(e.SessionID),
			Action: e.Action, Detail: e.Detail, Unit: e.UnitID,
			Model: e.Model, Failed: e.Failed,
		}
		if e.Millis > 0 {
			row.Took = fmt.Sprintf("%.1fs", float64(e.Millis)/1000)
		}
		rows = append(rows, row)
	}

	s.mu.RLock()
	sessionID := s.sess.ID
	work := s.workLocked()
	s.mu.RUnlock()

	data := struct {
		SessionID string
		Entries   []activityView
		Work      []Work
	}{SessionID: sessionID, Entries: rows, Work: work}

	s.render(w, "activity.html", data)
}

// chapterView pairs a chapter's prose with the real units it covers, so the
// page can show model narration beside facts that come from git.
type chapterView struct {
	Title string
	Prose string
	Units []unitView
	// Anchor is the first unit this chapter covers, so scrolling the story can
	// bring the matching changes alongside it.
	Anchor string
}

func (s *Server) views() []unitView { return s.viewsOf(s.sess) }

// viewsOf renders any session, not only the one the server started with.
func (s *Server) viewsOf(sess Session) []unitView {
	views := make([]unitView, 0, len(sess.Units))
	for _, u := range sess.Units {
		views = append(views, s.viewIn(sess, u))
	}
	return views
}

func (s *Server) view(u domain.Unit) unitView { return s.viewIn(s.sess, u) }

func (s *Server) viewIn(sess Session, u domain.Unit) unitView {
	d := sess.Diffs[u.ID]
	added, removed := usecase.DiffStats(d)

	flags := make([]string, len(u.Flags))
	for i, f := range u.Flags {
		flags[i] = string(f)
	}

	var notes []domain.Note
	for _, n := range sess.Notes {
		if n.UnitID == u.ID {
			notes = append(notes, n)
		}
	}

	// Line-level notes render on their line; the rest stay in the file's list.
	lineNotes, orphaned := splitDiffWithNotes(d.Text, notes)
	notes = fileLevel(notes)

	why := u.Headline.Why
	if why == "" && u.Headline.WhySrc != domain.WhyStated {
		why = "(none stated)"
	}

	h := sess.Histories[u.ID]
	history := make([]editView, 0, len(h.Edits))
	for _, e := range h.Edits {
		history = append(history, editView{
			When: e.TS.Local().Format("15:04:05"), Tool: e.Tool,
			Intent: e.Intent, Failed: e.Failed,
		})
	}

	return unitView{
		Name:       baseNames(u.Files),
		Edits:      h.Count,
		LastEdited: clockOrDash(h.Last),
		FirstEdit:  clockOrDash(h.First),
		History:    history,
		ID:         u.ID,
		Handle:     shortHandle(u.ID),
		File:       strings.Join(u.Files, ", "),
		Headline:   u.Headline.Text,
		Why:        why,
		WhySrc:     u.Headline.WhySrc,
		Flags:      flags,
		Added:      added,
		Removed:    removed,
		Diff:       lineNotes,
		Orphaned:   orphaned,
		Notes:      notes,
		Model:      s.models[u.ID],
	}
}

// baseNames is a unit's files without their directories.
func baseNames(files []string) string {
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, filepath.Base(filepath.ToSlash(f)))
	}
	return strings.Join(names, ", ")
}

// splitDiff classifies each diff line so the template can style it without logic.
// fileLevel is the notes that are about the file rather than a line. A
// line-level note renders on its line, so listing it again below the diff would
// show the same judgement twice.
func fileLevel(notes []domain.Note) []domain.Note {
	var out []domain.Note
	for _, n := range notes {
		if n.Anchor == "" {
			out = append(out, n)
		}
	}
	return out
}

// notesFor is one unit's notes.
func notesFor(all []domain.Note, unitID string) []domain.Note {
	var out []domain.Note
	for _, n := range all {
		if n.UnitID == unitID {
			out = append(out, n)
		}
	}
	return out
}

// splitDiffWithNotes renders a diff with the line-level notes placed on the
// lines they were written about, and returns the ones whose line has gone.
func splitDiffWithNotes(text string, notes []domain.Note) ([]diffLine, []domain.Note) {
	lines := splitDiff(text)
	if len(notes) == 0 {
		return lines, nil
	}

	// Anchoring is the pure part and lives in the usecase; this only maps the
	// answer onto what the template renders.
	anchored, orphaned := usecase.AnchorNotes(
		domain.Diff{Text: strings.TrimRight(text, "\n")}, notes)

	byText := map[string]map[int][]domain.Note{}
	for _, a := range anchored {
		if len(a.Notes) == 0 {
			continue
		}
		if byText[a.Text] == nil {
			byText[a.Text] = map[int][]domain.Note{}
		}
		byText[a.Text][a.Nth] = a.Notes
	}

	seen := map[string]int{}
	for i := range lines {
		nth := seen[lines[i].Text]
		lines[i].Nth = nth
		lines[i].Notes = byText[lines[i].Text][nth]
		seen[lines[i].Text]++
	}
	return lines, orphaned
}

func splitDiff(text string) []diffLine {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var lines []diffLine
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		kind := "ctx"
		switch {
		case strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---"):
			kind = "meta"
		case strings.HasPrefix(l, "@@"):
			kind = "hunk"
		case strings.HasPrefix(l, "+"):
			kind = "add"
		case strings.HasPrefix(l, "-"):
			kind = "del"
		}
		lines = append(lines, diffLine{Kind: kind, Text: l})
	}
	return lines
}

func shortHandle(id string) string {
	if i := strings.LastIndex(id, "-"); i >= 0 && i+1 < len(id) {
		return id[i+1:]
	}
	return id
}

func funcs() template.FuncMap {
	return template.FuncMap{
		"base": filepath.Base,
		"add":  func(a, b int) int { return a + b },
		// A model answers in markdown whether or not it was asked to. Rendered,
		// not trusted: the text is escaped before any markup is added.
		"markdown": renderMarkdown,
		"clock":    func(t time.Time) string { return t.Local().Format("15:04") },
	}
}

// exportFormats is what the review log can be taken away as, and the headers
// each arrives with.
var exportFormats = map[string]struct {
	contentType string
	ext         string
}{
	"md":    {"text/markdown; charset=utf-8", "md"},
	"json":  {"application/json; charset=utf-8", "json"},
	"slack": {"text/plain; charset=utf-8", "txt"},
}

// handleExport hands over the review log.
//
// "The review log is the product" is stated throughout, and until now the only
// way to get it out was a separate CLI invocation against a session id — which
// is a strange thing to ask of someone already looking at the review (issue
// #19).
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	format := strings.TrimSpace(r.URL.Query().Get("format"))
	if format == "" {
		format = "md"
	}
	kind, known := exportFormats[format]
	if !known {
		http.NotFound(w, r)
		return
	}

	// Of the review being read, never of whichever one happens to be open —
	// the same discipline as every other action on a target.
	sess := s.openSession(r)
	report := usecase.BuildReport(domain.Session{
		ID: sess.ID, Prompt: sess.Prompt, Units: sess.Units, Notes: sess.Notes,
	})

	var body []byte
	switch format {
	case "json":
		out, err := usecase.ExportJSON(report)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body = out
	case "slack":
		body = []byte(usecase.ExportSlack(report))
	default:
		body = []byte(usecase.ExportMarkdown(report))
	}

	// It is a file someone keeps, so it arrives as one with a name rather than
	// as a page they have to select and copy.
	name := fmt.Sprintf("review-%s.%s", safeFileName(sess.ID), kind.ext)
	w.Header().Set("Content-Type", kind.contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write(body)
}

// safeFileName keeps a target id fit for a Content-Disposition filename. Ids are
// hex or ulid today, but a range target's id is derived from text a person
// typed.
func safeFileName(id string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, id)
	if safe == "" {
		return "review"
	}
	return safe
}
