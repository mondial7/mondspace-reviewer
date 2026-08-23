// Package web serves the review as a localhost web application (ADR 0004).
//
// It renders server-side HTML with html/template — no build step, no client
// framework, and so far no JavaScript at all (expansion uses native <details>).
// The review semantics come entirely from the usecase layer; this package only
// presents them.
package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
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

// Exchange is one question and its answer. Review is iterative dialogue, so the
// thread is kept and handed back to the assistant as context (issue #12).
type Exchange struct {
	Question string
	Answer   string
	TS       time.Time
}

// AskFunc answers a question given everything already discussed.
type AskFunc func(ctx context.Context, question string, history []Exchange) (string, error)

// ReanalyseFunc re-summarises one unit, returning the headline and the model
// that produced it. Re-running with a better model is cheap because the diff is
// stable (issue #10).
type ReanalyseFunc func(ctx context.Context, u domain.Unit) (domain.Headline, string, error)

// NarrateFunc regenerates the session's story. It is slow — several model calls
// — so it runs in the background and open pages are told when it lands.
type NarrateFunc func(ctx context.Context)

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
	Model    string
	Endpoint string
	Online   bool
	Checked  time.Time
	Usage    port.TokenUsage
}

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

	mu     sync.RWMutex
	sess   Session
	loader Loader
	// loaded caches sessions opened during this run, so switching back and forth
	// costs nothing after the first visit.
	loaded    map[string]Session
	workspace []SessionSummary
	thread    []Exchange
	models    map[string]string // unit ID -> model that produced its headline
	ask       AskFunc
	reanalyse ReanalyseFunc
	audit     AuditLog
	narrate   NarrateFunc
	narrating bool
	agent     AgentStatus

	// subs are live subscribers (server-sent events). Each gets a buffered
	// channel so a slow reader can never block a request handler.
	subs   map[chan string]struct{}
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
		subs:   map[chan string]struct{}{},
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
	want := r.URL.Query().Get("session")

	s.mu.RLock()
	current := s.sess
	cached, isCached := s.loaded[want]
	loader := s.loader
	s.mu.RUnlock()

	if want == "" || want == current.ID || !validSessionID(want) {
		return current
	}
	if isCached {
		return cached
	}
	if loader == nil {
		return current
	}

	loadedSess, err := loader(r.Context(), want)
	if err != nil || loadedSess.ID == "" {
		return current
	}
	s.mu.Lock()
	s.loaded[want] = loadedSess
	s.mu.Unlock()
	return loadedSess
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
	s.mux.HandleFunc("GET /sessions", s.handleWorkspace)
	s.mux.HandleFunc("POST /ask", s.handleAsk)
	s.mux.HandleFunc("POST /units/{id}/reanalyse", s.handleReanalyse)
	s.mux.HandleFunc("GET /units/{id}/diff", s.handleUnitDiff)
	s.mux.HandleFunc("GET /cockpit", s.handleCockpit)
	s.mux.HandleFunc("GET /activity", s.handleActivity)
	s.mux.HandleFunc("GET /status", s.handleStatus)
	s.mux.HandleFunc("POST /story/narrate", s.handleNarrate)
	s.mux.HandleFunc("GET /events", s.handleEvents)
}

// handleUnitDiff serves one unit's full diff as a fragment. The cockpit shows a
// compacted diff inline and fetches the rest only if asked: a 97-file session
// with every diff inlined would be megabytes of HTML nobody reads.
func (s *Server) handleUnitDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.RLock()
	diff, known := s.sess.Diffs[id]
	if !known {
		for _, u := range s.sess.Units {
			if u.ID == id {
				known = true
				break
			}
		}
	}
	s.mu.RUnlock()
	if !known {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "diff.html", splitDiff(diff.Text)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.subs {
		select {
		case ch <- event:
		default:
		}
	}
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

	ch := make(chan string, 8)
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
		case event := <-ch:
			if _, err := fmt.Fprintf(w, "event: %s\ndata: {}\n\n", event); err != nil {
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
	unit, ok := s.unit(r.PathValue("id"))
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
	}
	if s.notes != nil {
		if err := s.notes.AppendNote(note); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.mu.Lock()
	s.sess.Notes = append(s.sess.Notes, note)
	s.mu.Unlock()
	s.record(AuditEntry{SessionID: unit.SessionID, UnitID: unit.ID,
		Action: "annotate", Detail: string(note.Kind) + ": " + note.Text})
	s.broadcast("note")

	http.Redirect(w, r, "/#unit-"+unit.ID, http.StatusSeeOther)
}

func (s *Server) unit(id string) (domain.Unit, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.sess.Units {
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
	Model    string

	// Edits is how the file reached this net change: a net-change review
	// collapses the agent's back-and-forth (ADR 0002), and this opens it back up.
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
	Kind string // add | del | hunk | ctx
	Text string
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	question := strings.TrimSpace(r.FormValue("question"))
	if question == "" {
		http.Redirect(w, r, "/#ask", http.StatusSeeOther)
		return
	}

	answer := "(no assistant configured)"
	if s.ask != nil {
		s.mu.RLock()
		history := append([]Exchange(nil), s.thread...)
		s.mu.RUnlock()

		got, err := s.ask(r.Context(), question, history)
		if err != nil {
			answer = "(" + err.Error() + ")"
		} else {
			answer = got
		}
	}

	s.mu.Lock()
	s.thread = append(s.thread, Exchange{Question: question, Answer: answer, TS: s.now()})
	sessionID := s.sess.ID
	s.mu.Unlock()
	s.record(AuditEntry{SessionID: sessionID, Action: "ask", Detail: question})
	s.broadcast("answer")

	http.Redirect(w, r, "/#ask", http.StatusSeeOther)
}

// handleReanalyse re-summarises one unit with the configured model, replacing
// its headline and recording which model produced it.
func (s *Server) handleReanalyse(w http.ResponseWriter, r *http.Request) {
	unit, ok := s.unit(r.PathValue("id"))
	if !ok {
		http.Error(w, "unknown unit", http.StatusNotFound)
		return
	}
	if s.reanalyse == nil {
		http.Error(w, "no analyser configured", http.StatusBadRequest)
		return
	}

	headline, model, err := s.reanalyse(r.Context(), unit)
	if err != nil {
		// A failing model must not lose the existing headline.
		s.record(AuditEntry{SessionID: unit.SessionID, UnitID: unit.ID,
			Action: "reanalyse", Detail: "failed: " + err.Error()})
		http.Redirect(w, r, "/#unit-"+unit.ID, http.StatusSeeOther)
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

	http.Redirect(w, r, "/#unit-"+unit.ID, http.StatusSeeOther)
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
	s.NarrateNow(context.WithoutCancel(r.Context()))
	http.Redirect(w, r, "/story", http.StatusSeeOther)
}

// NarrateNow starts narration in the background unless one is already running,
// reporting whether it started. Both the startup narration and the reviewer's
// retry go through here, so the two can never overlap.
func (s *Server) NarrateNow(ctx context.Context) bool {
	if !s.beginNarration() {
		return false
	}
	go func() {
		defer s.endNarration()
		s.narrate(ctx)
	}()
	s.broadcast("narrative") // tell open pages it has started
	return true
}

// cockpitStats is the session's numbers rendered for display: durations and
// counts as strings, so the template holds no formatting logic.
type cockpitStats struct {
	Open         string
	Live         bool
	Files        int
	Added        int
	Removed      int
	Commits      int
	PullRequests int
	// Tokens is blank when no model call has been made, so a session that never
	// touched a model shows no tile rather than a misleading zero.
	Tokens string
}

// feedItem is one change in the cockpit stream: a sentence and its diff, with
// the diff compacted when it would otherwise swamp everything else.
type feedItem struct {
	unitView
	Compacted bool
}

// handleCockpit renders the session at a glance: whether it is still moving, a
// stream of what changed, and the numbers behind it. It is the one page meant
// to be left open on a second screen while an agent works.
func (s *Server) handleCockpit(w http.ResponseWriter, r *http.Request) {
	// Which session is being read may differ from the one this server started
	// with: a workspace spans repositories, and the switcher moves between them.
	sess := s.openSession(r)

	s.mu.RLock()
	views := s.viewsOf(sess)
	stats := sess.Stats
	usage := s.agent.Usage
	narrative := sess.Narrative
	workspace := s.workspace
	thread := append([]Exchange(nil), s.thread...)
	hasAsk := s.ask != nil
	hasReanal := s.reanalyse != nil
	canRetry := s.narrate != nil && narrative.Source != domain.NarrativeModel &&
		sess.ID == s.sess.ID // only the session this server is tracking can re-narrate
	narrating := s.narrating
	s.mu.RUnlock()

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
	items := make([]feedItem, 0, len(views))
	for i := len(views) - 1; i >= 0; i-- {
		v := views[i]
		compact, wasCompacted := usecase.CompactDiff(sess.Diffs[v.ID], cockpitDiffLines)
		if wasCompacted {
			v.Diff = splitDiff(compact.Text)
		}
		items = append(items, feedItem{unitView: v, Compacted: wasCompacted})
	}

	data := struct {
		Session   Session
		Workspace []SessionSummary
		Stats     cockpitStats
		Narrative domain.Narrative
		Chapters  []chapterView
		Feed      []feedItem
		Thread    []Exchange
		HasAsk    bool
		HasReanal bool
		CanRetry  bool
		Narrating bool
	}{
		Session: sess, Workspace: workspace,
		Stats: cockpitStats{
			Open:  humanDuration(stats.Open),
			Live:  stats.Live,
			Files: stats.Files, Added: stats.Added, Removed: stats.Removed,
			Commits: stats.Commits, PullRequests: stats.PullRequests,
			Tokens: tokenTotal(usage),
		},
		Narrative: narrative, Chapters: chapters, Feed: items,
		Thread: thread, HasAsk: hasAsk, HasReanal: hasReanal,
		CanRetry: canRetry, Narrating: narrating,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "cockpit.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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

// handleStatus answers "is everything working": the reviewer's own model, what
// it has cost, and every session known for this project.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	agent := s.agent
	sessions := s.workspace
	sessionID := s.sess.ID
	repo := s.sess.Repo
	s.mu.RUnlock()

	u := agent.Usage
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
		Sessions           []SessionSummary
	}{
		SessionID: sessionID, Repo: repo, Agent: agent,
		Calls: thousands(u.Calls), Failures: thousands(u.Failures),
		Prompt: thousands(u.Prompt), Completion: thousands(u.Completion),
		Reasoning: thousands(u.Reasoning), Total: thousands(u.Prompt + u.Completion),
		Average: avg, Checked: checked,
		Sessions: sessions,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "status.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
	When   string
	Action string
	Detail string
	Unit   string
	Model  string
	Took   string
	Failed bool
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
			When: e.TS.Local().Format("15:04:05"), Action: e.Action,
			Detail: e.Detail, Unit: e.UnitID, Model: e.Model, Failed: e.Failed,
		}
		if e.Millis > 0 {
			row.Took = fmt.Sprintf("%.1fs", float64(e.Millis)/1000)
		}
		rows = append(rows, row)
	}

	s.mu.RLock()
	sessionID := s.sess.ID
	s.mu.RUnlock()

	data := struct {
		SessionID string
		Entries   []activityView
	}{SessionID: sessionID, Entries: rows}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "activity.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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

func (s *Server) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	data := struct{ Sessions []SessionSummary }{Sessions: s.workspace}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "sessions.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
		Diff:       splitDiff(d.Text),
		Notes:      notes,
		Model:      s.models[u.ID],
	}
}

// splitDiff classifies each diff line so the template can style it without logic.
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
	}
}
