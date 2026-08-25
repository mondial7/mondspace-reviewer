package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/web"
	gitsnap "github.com/mondial7/mondspace-reviewer/internal/adapter/snapshot/git"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	pgstore "github.com/mondial7/mondspace-reviewer/internal/adapter/store/postgres"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// runWeb serves the review as a localhost web app (ADR 0004). It reuses the same
// net-change-per-file engine as the TUI; only the presentation differs.
func runWeb(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:7777", "address to listen on (localhost only by default)")
	out := fs.String("out", ".mondspace-reviewer", "store root directory (jsonl store)")
	var repos repoList
	fs.Var(&repos, "repo", "repository to review (repeatable: --repo=a --repo=b)")
	session := fs.String("session", "", "session id")
	schema := fs.String("pg-schema", pgstore.DefaultSchema, "Postgres schema (never public)")
	summarizerURL := fs.String("summarizer-url", defaultSummarizerURL, "OpenAI-compatible summarizer endpoint")
	model := fs.String("model", defaultModel, "summarizer model")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	// With no --repo, look around: a checkout opens itself, and a directory of
	// checkouts offers its children. Nothing is prompted for at launch — the
	// repositories in the workspace are chosen in the app, on /status, where the
	// choice can also be changed without a restart.
	candidates := repos
	if len(repos) == 0 {
		candidates = gitsnap.DiscoverRepos(".")
		if len(candidates) == 0 {
			candidates = []string{"."}
		}
		repos = candidates
		if len(repos) > openAtLaunch {
			// Opening forty checkouts costs a git crawl each. Open the first few
			// and offer the rest in the app rather than asking a question the
			// terminal may not be there to answer.
			repos = repos[:openAtLaunch]
		}
	}

	targets := discoverTargets(ctx, repos, *out)
	if len(targets) == 0 {
		return fmt.Errorf("nothing to review: no git history found in %s",
			strings.Join(repos, ", "))
	}

	// A session is no longer required, or even special: the newest thing worth
	// reviewing is whatever git says it is (ADR 0017). --session still names one,
	// because a session is still a target.
	initial := targets[0].ID
	if *session != "" {
		initial = *session
	}
	entry, known := targetIndex[initial]
	if !known {
		return fmt.Errorf("no such target %q in this workspace", initial)
	}

	load := targetLoader()
	view, err := load(ctx, initial)
	if err != nil {
		return err
	}

	repo, storeRoot := entry.repo, entry.out
	// Postgres is opt-in via MSR_POSTGRES_DSN; otherwise the JSONL store is used.
	store, closeStore, err := openStore(ctx, storeRoot, *schema)
	if err != nil {
		return err
	}
	defer closeStore()

	snap := gitsnap.New(repo, initial)
	firstStats, err := snap.Numstat(ctx, entry.target.From, entry.target.To)
	if err != nil {
		firstStats = nil // a review that cannot be measured still renders
	}

	sum := chooseSummarizer(*summarizerURL, *model)
	var sess domain.Session
	if entry.session != "" {
		sess, _ = store.Load(entry.session)
	}

	handler := web.NewServer(view, targetNotes{}).
		WithAgent(agentStatus(ctx, sum, *summarizerURL, *model)).
		WithWorkspace(discoverWorkspace(repos, *out)).
		WithTargets(targets).
		WithLoader(load).
		WithVersions(snap.FileVersions, snap.DiffAt).
		WithRepos(openRepos(), addRepo(*out)).
		WithCandidates(unopenedRepos(candidates)).
		WithDescribe(describeAnyTarget(sum)).
		WithDescribeFile(describeOneFile(sum)).
		WithRemoveRepo(removeRepo(*out)).
		WithCompare(compareRefs()).
		WithExchanges(exchangeStore(store), sess.Exchanges).
		WithAsk(webAskFunc(sess, view.Units, view.Diffs, snap, sum)).
		WithReanalyse(webReanalyseFunc(snap, sum, *model)).
		WithAudit(workspaceAudit{writeTo: filepath.Join(storeRoot, initial, "audit.jsonl")})

	// Narration is the only thing that calls the model unbidden, and it runs at
	// most once per review. Any target can be narrated now, so there is one path
	// rather than a special case for whichever was served first.
	narrate := func(ctx context.Context, targetID string) {
		if targetID == "" {
			targetID = initial
		}
		narrateTarget(ctx, handlerRef(), sum, targetID, *model)
	}
	handler = handler.WithNarrate(narrate)
	setHandler(handler)

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if view.Narrative.Source != domain.NarrativeModel {
		// Never read, or the stored story is stale. Read it once now; from here
		// on it is the reviewer's call. Going through the server means this and a
		// retry click can never overlap.
		handler.NarrateNow(context.Background(), initial)
	}

	// A cockpit left open on a second screen must not go stale: what it watches
	// may still be moving, so the numbers are recomputed on a tick and pushed to
	// open pages. Reading git every 15s is cheap; a model is never involved.
	go refreshReview(ctx, reviewRefresher{
		handler: handler, snap: snap, store: store, sum: sum,
		sessionID: initial, repo: repo,
		storeRel: storeRelativeTo(repo, storeRoot),
		baseline: entry.target.From, model: *model,
		fingerprint: usecase.ReviewFingerprint(firstStats), narrate: narrate,
	})
	go refreshAgent(ctx, handler, sum, *summarizerURL, *model)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "reviewing %s %q — http://%s\n",
		entry.target.Kind, usecase.Brief(entry.target.Title, 48), ln.Addr())

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// openStore returns the Postgres store when MSR_POSTGRES_DSN is set, otherwise
// the append-only JSONL store.
func openStore(ctx context.Context, out, schema string) (port.Store, func(), error) {
	dsn := os.Getenv("MSR_POSTGRES_DSN")
	if dsn == "" {
		return jsonl.New(out), func() {}, nil
	}
	pg, err := pgstore.Open(ctx, dsn, schema)
	if err != nil {
		return nil, nil, err
	}
	return pg, pg.Close, nil
}

// storeRelativeTo expresses the store directory relative to the repo, so its
// files can be excluded from the review.
func storeRelativeTo(repo, out string) string {
	repoAbs, err := filepath.Abs(repo)
	if err != nil {
		return out
	}
	outAbs, err := filepath.Abs(out)
	if err != nil {
		return out
	}
	rel, err := filepath.Rel(repoAbs, outAbs)
	if err != nil {
		return out
	}
	return rel
}

// webAskFunc adapts the reviewer-assistant to the bounded-context ask usecase,
// threading the conversation so far into each question (issue #12).
func webAskFunc(sess domain.Session, units []domain.Unit, diffs map[string]domain.Diff, snap port.Snapshotter, sum port.Summarizer) web.AskFunc {
	return func(ctx context.Context, question string, history []web.Exchange) (string, error) {
		askCtx := usecase.BuildAskContext(domain.AskSession, sess, domain.Unit{}, domain.Diff{})
		// The units the page shows are rebuilt from git, not the ones the store
		// happens to hold — a retroactive review has none in the store at all, so
		// asking about it was asking about nothing.
		askCtx.Units = units
		// Units and notes alone are metadata; asked what changed, the assistant
		// correctly answered that it could not say. The digest is what makes a
		// session-scoped question answerable.
		askCtx = usecase.WithChanges(askCtx, units, diffs, askDigestLines)
		return sum.Answer(ctx, withHistory(question, history), askCtx)
	}
}

// askDigestLines is how much of the session's change reaches a question. Room
// enough to answer "what changed" without becoming a prompt no local context
// window can hold.
const askDigestLines = 220

// withHistory prefixes the running conversation so follow-ups have context.
func withHistory(question string, history []web.Exchange) string {
	if len(history) == 0 {
		return question
	}
	var b strings.Builder
	b.WriteString("Earlier in this review:\n")
	for _, e := range history {
		b.WriteString("Q: " + e.Question + "\nA: " + e.Answer + "\n")
	}
	b.WriteString("\nNow: " + question)
	return b.String()
}

// discoverSessions lists the reviews present in the store root, so the workspace
// can show every session across repos (issue #8).
func discoverSessions(out, repo string) []web.SessionSummary {
	entries, err := os.ReadDir(out)
	if err != nil {
		return nil
	}
	store := jsonl.New(out)
	var sessions []web.SessionSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := store.Load(e.Name())
		if err != nil {
			continue
		}
		// A directory with no events is not a session. Side files — an audit log,
		// a stored narrative — can create one, and an empty impostor that sorts
		// first will shadow the real session of that name in another repository.
		if len(s.Events) == 0 {
			continue
		}
		summary := web.SessionSummary{
			ID:     e.Name(),
			Repo:   filepath.Base(mustAbs(repo)),
			Agent:  agentOf(s),
			Prompt: s.Prompt,
			Files:  len(s.Units),
		}
		for _, u := range s.Units {
			summary.Flags += len(u.Flags)
		}
		for _, n := range s.Notes {
			if n.Kind == domain.NoteQuestion || n.Kind == domain.NoteObjection {
				summary.Open++
			}
		}
		if len(s.Events) > 0 {
			summary.Started = s.Events[0].TS
		}
		sessions = append(sessions, summary)
	}
	return sessions
}

// agentOf reports which agent produced a session, from its event source.
func agentOf(s domain.Session) string {
	for _, e := range s.Events {
		if e.Source != "" {
			return e.Source
		}
	}
	return "unknown"
}

func mustAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// webReanalyseFunc re-summarises a unit on demand, reporting which model did it.
func webReanalyseFunc(snap port.Snapshotter, sum port.Summarizer, model string) web.ReanalyseFunc {
	return func(ctx context.Context, u domain.Unit) (domain.Headline, string, error) {
		diff, err := snap.Diff(ctx, u.From, u.To, u.Files)
		if err != nil {
			diff = domain.Diff{}
		}
		return usecase.Summarize(ctx, sum, u, diff), model, nil
	}
}

// auditFile appends interactions to an append-only JSONL log beside the session,
// so a review carries its own provenance (issue #11).
type auditFile string

// workspaceAudit writes to the open session's log and reads back every session's,
// across every repository. Provenance belongs with the session it describes, but
// "what has been happening" is a question about the whole workspace — filtering
// the activity page to one session hid work the reviewer had just done.
type workspaceAudit struct{ writeTo string }

func (a workspaceAudit) Append(e web.AuditEntry) error { return auditFile(a.writeTo).Append(e) }

func (a workspaceAudit) Entries() ([]web.AuditEntry, error) {
	seen := map[string]bool{}
	paths := []string{a.writeTo}
	seen[a.writeTo] = true

	for id, entry := range workspaceIndex {
		path := filepath.Join(entry.out, id, "audit.jsonl")
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}

	var all []web.AuditEntry
	for _, path := range paths {
		got, err := auditFile(path).Entries()
		if err != nil {
			continue // one unreadable log must not blank the whole page
		}
		all = append(all, got...)
	}
	// Oldest first; the page reverses it. Sorting here is what makes several
	// logs read as one history rather than as concatenated files.
	sort.SliceStable(all, func(i, j int) bool { return all[i].TS.Before(all[j].TS) })
	return all, nil
}

// Entries reads one log back. A log that has never been written to is not an
// error — it is an empty history.
func (a auditFile) Entries() ([]web.AuditEntry, error) {
	f, err := os.Open(string(a))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []web.AuditEntry
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scan.Scan() {
		line := bytes.TrimSpace(scan.Bytes())
		if len(line) == 0 {
			continue
		}
		var e web.AuditEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // a torn line must not hide the rest of the history
		}
		entries = append(entries, e)
	}
	return entries, scan.Err()
}

func (a auditFile) Append(e web.AuditEntry) error {
	if err := os.MkdirAll(filepath.Dir(string(a)), 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(string(a), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// narrativeCache is the optional ability of a store to remember a session's
// story between runs. A store without it still works; its stories are simply
// re-narrated each launch.
type narrativeCache interface {
	SaveNarrative(domain.Narrative) error
	LoadNarrative(sessionID string) (domain.Narrative, error)
}

// Every store must remember stories. This is asserted rather than left to the
// runtime type switch because the failure is silent: a store that does not
// satisfy it simply re-narrates on every launch, costing several model calls
// with nothing to show that anything is wrong.
var (
	_ narrativeCache = (*jsonl.Store)(nil)
	_ narrativeCache = (*pgstore.Store)(nil)
)

// reviewRefresher is everything the background refresh needs. It is a struct
// because the alternative is a nine-argument function nobody can call correctly.
type reviewRefresher struct {
	handler     *web.Server
	snap        *gitsnap.Snapshotter
	store       port.Store
	sum         port.Summarizer
	sessionID   string
	repo        string
	storeRel    string
	baseline    domain.SnapshotRef
	model       string
	fingerprint string
	narrate     func(context.Context, string)
}

// reviewTick is how often the review is checked for movement. One `git diff
// --numstat` per tick is cheap; nothing else runs unless something changed.
const reviewTick = 15 * time.Second

// renarrateEvery bounds how often the model is asked to re-read a session that
// is still moving. Without it an active agent would trigger a narration every
// tick, which is exactly the overload ADR 0014 set out to stop. Stats, units and
// history still refresh every tick — those cost git, not a model.
const renarrateEvery = 5 * time.Minute

// refreshReview keeps a cockpit current while the session it watches is still
// being worked on. Each tick asks git one cheap question — has anything changed?
// — and does the expensive work only when the answer is yes.
func refreshReview(ctx context.Context, r reviewRefresher) {
	ticker := time.NewTicker(reviewTick)
	defer ticker.Stop()

	lastNarration := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats, err := r.snap.Numstat(ctx, r.baseline, domain.SnapshotRef{})
			if err != nil {
				continue // a transient git failure must not kill the ticker
			}
			print := usecase.ReviewFingerprint(stats)
			changed := print != r.fingerprint

			sess, err := r.store.Load(r.sessionID)
			if err != nil {
				continue
			}

			units, diffs, err := usecase.BuildFileUnits(ctx, r.snap, r.sessionID,
				r.baseline, domain.SnapshotRef{}, func(f string) bool {
					return f == r.storeRel || strings.HasPrefix(f, r.storeRel+"/")
				})
			if err != nil {
				continue
			}

			if changed {
				// The review moved. Say so in the log: a page that redraws itself
				// with different content and no record of why is impossible to
				// reason about afterwards.
				r.handler.Record(web.AuditEntry{
					SessionID: r.sessionID, Action: "review-changed",
					Detail: fmt.Sprintf("%d files now changed (%s → %s)",
						len(units), short(r.fingerprint), short(print)),
				})
				r.fingerprint = print

				r.handler.SetSession(web.Session{
					ID: r.sessionID, Prompt: sess.Prompt, Repo: r.repo,
					Units: units, Notes: usecase.MarkSuperseded(units, sess.Notes),
					Diffs: diffs,
				}, usecase.FileHistories(sess.Events, units))
			}

			commits, _ := r.snap.CommitsSince(ctx, firstEventTime(sess))
			r.handler.SetStats(usecase.ComputeStats(sess, units, diffs, commits, time.Now()))

			// Re-reading the session costs several model calls, so it is bounded
			// however fast the agent works.
			if changed && time.Since(lastNarration) >= renarrateEvery {
				lastNarration = time.Now()
				r.handler.Record(web.AuditEntry{
					SessionID: r.sessionID, Action: "renarrate-queued",
					Detail: "the review changed; asking the model to re-read it",
				})
				r.handler.NarrateNow(ctx, r.sessionID)
			}
		}
	}
}

// short abbreviates a fingerprint for a log line.
func short(fingerprint string) string {
	if len(fingerprint) <= 8 {
		return fingerprint
	}
	return fingerprint[:8]
}

// agentStatus reports the reviewer's own model: which one, where, whether it
// answers right now, and what it has spent. Liveness and usage are optional
// capabilities — a summarizer without them still yields a complete panel, just
// a quieter one.
func agentStatus(ctx context.Context, sum port.Summarizer, endpoint, model string) web.AgentStatus {
	status := web.AgentStatus{Model: model, Endpoint: endpoint, Checked: time.Now()}

	if p, ok := sum.(port.Pinger); ok {
		probe, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		status.Online = p.Ping(probe) == nil
	}
	if u, ok := sum.(port.UsageReporter); ok {
		status.Usage = u.Usage()
	}
	return status
}

// refreshAgent re-probes the model while the page is open. "Is it online" is a
// live question: an endpoint that answered at start-up says nothing about the
// one that died five minutes later.
func refreshAgent(ctx context.Context, handler *web.Server, sum port.Summarizer, endpoint, model string) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			handler.SetAgent(agentStatus(ctx, sum, endpoint, model))
		}
	}
}

// repoList collects a repeatable --repo flag, so one server can hold a workspace
// spanning any number of repositories.
type repoList []string

func (r *repoList) String() string     { return strings.Join(*r, ",") }
func (r *repoList) Set(v string) error { *r = append(*r, v); return nil }

// workspaceEntry is a session and the repository it belongs to. The pairing is
// what makes multi-repository work: a session id alone does not say which git
// tree its files live in.
type workspaceEntry struct {
	summary web.SessionSummary
	repo    string
	out     string
}

var workspaceIndex = map[string]workspaceEntry{}

// targetEntry is something worth reviewing and where it lives. Targets are keyed
// by an id derived from their range (ADR 0017), so the same commit or tag always
// resolves to the same review.
type targetEntry struct {
	target domain.Target
	repo   string
	out    string
	// session is set only for a session target, which is the one kind that has
	// a store of its own to read intent and notes from.
	session string
}

var targetIndex = map[string]targetEntry{}

// discoverTargets asks each repository what it has worth reviewing — commits,
// tags, pull requests, the working tree — and folds in the sessions recorded
// against it. Newest first, across every repository at once.
func discoverTargets(ctx context.Context, repos []string, out string) []web.TargetSummary {
	var all []domain.Target

	for _, repo := range repos {
		root := out
		if !filepath.IsAbs(out) {
			root = filepath.Join(repo, out)
		}
		snap := gitsnap.New(repo, "targets")

		commits, _ := snap.RecentCommits(ctx, recentCommitLimit)
		tags, _ := snap.Tags(ctx, tagLimit)
		dirty, _ := snap.IsDirty(ctx)

		// Recorded sessions become targets in their own right: each is the range
		// from just before it started to wherever the work reached.
		var sessions []domain.Target
		store := jsonl.New(root)
		for _, sum := range discoverSessions(root, repo) {
			sess, err := store.Load(sum.ID)
			if err != nil {
				continue
			}
			baseline, err := snap.Baseline(ctx, firstEventTime(sess))
			if err != nil {
				continue
			}
			sessions = append(sessions, domain.Target{
				ID: sum.ID, Repo: repo, Kind: domain.TargetSession,
				Title:    firstNonEmpty(sess.Prompt, sum.ID),
				Subtitle: sum.Agent + " · recorded run",
				From:     baseline, TS: sum.Started,
			})
		}

		for _, t := range usecase.BuildTargets(repo, commits, tags, sessions, dirty) {
			if _, clash := targetIndex[t.ID]; clash {
				continue
			}
			entry := targetEntry{target: t, repo: repo, out: root}
			if t.Kind == domain.TargetSession {
				entry.session = t.ID
			}
			targetIndex[t.ID] = entry
			all = append(all, t)
		}
	}

	usecase.SortTargets(all)

	summaries := make([]web.TargetSummary, 0, len(all))
	for _, t := range all {
		summaries = append(summaries, web.TargetSummary{
			ID: t.ID, Repo: filepath.Base(mustAbs(t.Repo)), Kind: t.Kind,
			Title: t.Title, Subtitle: t.Subtitle, TS: t.TS, Sessions: len(t.Sessions),
		})
	}
	return summaries
}

// openAtLaunch bounds how many discovered repositories are opened without being
// asked for. The rest are offered in the app, where choosing them costs a click
// rather than a restart.
const openAtLaunch = 5

// How much history to offer. Enough to cover recent work without turning the
// picker into an unreadable wall or the launch into a git crawl.
const (
	recentCommitLimit = 40
	tagLimit          = 20
)

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// discoverWorkspace lists every review across every repository given, newest
// first. Each repository keeps its own store unless --out names an absolute
// path, so two projects never collide.
func discoverWorkspace(repos []string, out string) []web.SessionSummary {
	seen := map[string]bool{}
	var all []web.SessionSummary

	for _, repo := range repos {
		if repo == "" || seen[mustAbs(repo)] {
			continue
		}
		seen[mustAbs(repo)] = true

		root := out
		if !filepath.IsAbs(out) {
			root = filepath.Join(repo, out)
		}
		for _, sum := range discoverSessions(root, repo) {
			if _, clash := workspaceIndex[sum.ID]; clash {
				continue // the same id in two repositories: first one wins
			}
			workspaceIndex[sum.ID] = workspaceEntry{summary: sum, repo: repo, out: root}
			all = append(all, sum)
		}
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].Started.After(all[j].Started) })
	return all
}

// targetLoader materialises any target — a commit, a tag, a pull request, the
// working tree, or a recorded session — from whichever repository owns it.
//
// Reviewing a target is exactly what the engine always did: the net change per
// file between two refs. Only the two refs differ, and where the intent comes
// from: a session has an event log, and nothing else does.
func targetLoader() web.Loader {
	return func(ctx context.Context, targetID string) (web.Session, error) {
		entry, known := targetIndex[targetID]
		if !known {
			return web.Session{}, fmt.Errorf("nothing here to review under %q", targetID)
		}
		t := entry.target

		// A session target carries its recorded log: the stated intent, the
		// notes, and the conversation. Every other kind has only git.
		var sess domain.Session
		if entry.session != "" {
			loaded, err := jsonl.New(entry.out).Load(entry.session)
			if err != nil {
				return web.Session{}, err
			}
			sess = loaded
		}

		snap := gitsnap.New(entry.repo, targetID)
		storeRel := storeRelativeTo(entry.repo, entry.out)
		units, diffs, err := usecase.BuildFileUnits(ctx, snap, targetID, t.From, t.To,
			func(f string) bool {
				return f == storeRel || strings.HasPrefix(f, storeRel+"/")
			})
		if err != nil {
			return web.Session{}, err
		}

		commits, _ := snap.CommitsSince(ctx, t.TS)
		if t.Kind != domain.TargetSession {
			// For a range, the commits in it are the ones it spans, which the
			// target already counted rather than re-deriving here.
			commits = commits[:min(len(commits), t.Commits)]
		}

		view := web.Session{
			ID: targetID, Prompt: t.Title, Repo: filepath.Base(mustAbs(entry.repo)),
			Units: units, Notes: usecase.MarkSuperseded(units, sess.Notes), Diffs: diffs,
			Stats:     usecase.ComputeStats(sess, units, diffs, commits, time.Now()),
			Histories: usecase.FileHistories(sess.Events, units),
			Target:    t,
		}

		// A story already written for this range is reused; one never narrated
		// falls back to deterministic grouping rather than an empty column.
		// Opening a target must never trigger a model call (ADR 0014).
		view.Narrative = domain.Narrative{
			SessionID: targetID, Title: usecase.Brief(t.Title, 70),
			Chapters: usecase.GroupByPath(units), Source: domain.NarrativeMechanical,
		}
		if stored, err := jsonl.New(entry.out).LoadNarrative(targetID); err == nil &&
			stored.Fingerprint == usecase.Fingerprint(units) && len(stored.Chapters) > 0 {
			view.Narrative = stored
		}
		return view, nil
	}
}

// sessionLoader materialises a session from whichever repository owns it. It is
// called the first time a session is opened and never again for that session,
// so the git work it does is paid once.
func sessionLoader(workspace []web.SessionSummary, out string) web.Loader {
	return func(ctx context.Context, sessionID string) (web.Session, error) {
		entry, known := workspaceIndex[sessionID]
		if !known {
			return web.Session{}, fmt.Errorf("session %q is not in this workspace", sessionID)
		}

		store := jsonl.New(entry.out)
		sess, err := store.Load(sessionID)
		if err != nil {
			return web.Session{}, err
		}

		snap := gitsnap.New(entry.repo, sessionID)
		baseline, err := snap.Baseline(ctx, firstEventTime(sess))
		if err != nil {
			return web.Session{}, err
		}
		storeRel := storeRelativeTo(entry.repo, entry.out)
		units, diffs, err := usecase.BuildFileUnits(ctx, snap, sessionID, baseline,
			domain.SnapshotRef{}, func(f string) bool {
				return f == storeRel || strings.HasPrefix(f, storeRel+"/")
			})
		if err != nil {
			return web.Session{}, err
		}

		commits, _ := snap.CommitsSince(ctx, firstEventTime(sess))

		// A story already written for this session is reused; one that has never
		// been narrated falls back to the deterministic grouping rather than an
		// empty column. Opening a session must never trigger a model call —
		// re-reading it is the tracked session's business (ADR 0014).
		narrative := domain.Narrative{
			SessionID: sessionID, Title: usecase.Brief(sess.Prompt, 70),
			Chapters: usecase.GroupByPath(units), Source: domain.NarrativeMechanical,
		}
		if stored, err := store.LoadNarrative(sessionID); err == nil &&
			stored.Fingerprint == usecase.Fingerprint(units) && len(stored.Chapters) > 0 {
			narrative = stored
		}

		return web.Session{
			ID: sessionID, Prompt: sess.Prompt, Repo: filepath.Base(mustAbs(entry.repo)),
			Units: units, Notes: usecase.MarkSuperseded(units, sess.Notes), Diffs: diffs,
			Stats:     usecase.ComputeStats(sess, units, diffs, commits, time.Now()),
			Histories: usecase.FileHistories(sess.Events, units),
			Narrative: narrative,
		}, nil
	}
}

// targetNotes writes an annotation to the store of whatever it was made against.
// One store cannot serve a workspace: a note on a commit in another repository
// belongs in that repository's store, not the one the server happened to start
// with.
type targetNotes struct{}

func (targetNotes) AppendNote(n domain.Note) error {
	entry, known := targetIndex[n.SessionID]
	if !known {
		return fmt.Errorf("no such review %q", n.SessionID)
	}
	return jsonl.New(entry.out).AppendNote(n)
}

// unopenedRepos lists checkouts found nearby that are not in the workspace, so
// the app can offer them rather than the launch prompting for them.
func unopenedRepos(candidates []string) []web.RepoStatus {
	open := map[string]bool{}
	for _, entry := range targetIndex {
		open[mustAbs(entry.repo)] = true
	}

	var out []web.RepoStatus
	seen := map[string]bool{}
	for _, path := range candidates {
		abs := mustAbs(path)
		if open[abs] || seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, web.RepoStatus{Name: filepath.Base(abs), Path: abs})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// openRepos lists what the workspace currently holds, for the status page.
func openRepos() []web.RepoStatus {
	seen := map[string]web.RepoStatus{}
	// Counted from the targets, not the sessions: a repository with history and
	// no recorded runs is still open, and reading the session index made every
	// such repository look absent.
	for _, entry := range targetIndex {
		st := seen[entry.repo]
		st.Path = mustAbs(entry.repo)
		st.Name = filepath.Base(mustAbs(entry.repo))
		st.Sessions++
		seen[entry.repo] = st
	}

	repos := make([]web.RepoStatus, 0, len(seen))
	for _, st := range seen {
		repos = append(repos, st)
	}
	sort.SliceStable(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
	return repos
}

// addRepo starts watching another repository without a restart. It refuses
// anything that is not a git checkout: the path comes from a form, and a typo
// that silently added an empty entry would be harder to notice than an error.
func addRepo(out string) web.AddRepoFunc {
	return func(path string) ([]web.TargetSummary, []web.RepoStatus, error) {
		abs, err := filepath.Abs(strings.TrimSpace(path))
		if err != nil {
			return nil, nil, fmt.Errorf("%q is not a usable path", path)
		}
		info, err := os.Stat(filepath.Join(abs, ".git"))
		if err != nil || info == nil {
			return nil, nil, fmt.Errorf("%s is not a git repository", abs)
		}
		for _, entry := range targetIndex {
			if mustAbs(entry.repo) == abs {
				return nil, nil, fmt.Errorf("%s is already open", filepath.Base(abs))
			}
		}

		// discoverTargets adds to the shared index and returns everything, so the
		// caller gets the whole workspace back rather than a fragment.
		known := make([]string, 0, len(targetIndex)+1)
		seen := map[string]bool{}
		for _, entry := range targetIndex {
			if !seen[entry.repo] {
				seen[entry.repo] = true
				known = append(known, entry.repo)
			}
		}
		return discoverTargets(context.Background(), append(known, abs), out), openRepos(), nil
	}
}

// exchangeStore persists the review conversation when the store can. Both the
// JSONL and Postgres stores can; the interface keeps the server from caring.
func exchangeStore(store port.Store) web.ExchangeStore {
	if keeper, ok := store.(web.ExchangeStore); ok {
		return keeper
	}
	return nil
}

// The handler is needed by the target-aware narration below, which is built
// before the handler exists. A single assignment after construction is simpler
// than threading a promise through six builder calls.
var liveHandler *web.Server

func setHandler(h *web.Server) { liveHandler = h }
func handlerRef() *web.Server  { return liveHandler }

// narrateTarget reads a target that is not the one this server started with —
// a commit, a tag, a pull request — and stores the story against it. Reviewing
// any of them is the point of ADR 0017; narrating only one would undercut it.
func narrateTarget(ctx context.Context, handler *web.Server, sum port.Summarizer, targetID, model string) {
	entry, known := targetIndex[targetID]
	if !known || handler == nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()

	units, diffs, err := unitsFor(ctx, entry)
	if err != nil {
		return
	}
	started := time.Now()
	narrative, err := usecase.NarrateProgressively(ctx, sum, domain.Session{
		ID: targetID, Prompt: entry.target.Title,
	}, units, func(partial domain.Narrative) { handler.SetNarrativeFor(targetID, partial) })
	narrative.Fingerprint = usecase.Fingerprint(units)
	narrative.Model = model

	record := web.AuditEntry{
		SessionID: targetID, Action: "narrate", Model: model,
		Millis: time.Since(started).Milliseconds(),
		Detail: fmt.Sprintf("%s %q: %d chapters, %s",
			entry.target.Kind, usecase.Brief(entry.target.Title, 40),
			len(narrative.Chapters), narrative.Source),
	}
	if err != nil {
		record.Failed = true
		record.Detail = err.Error()
	}

	groups := usecase.GroupChanges(units, diffs)
	narrative.Meanings = usecase.DescribeGroups(ctx, sum, domain.Session{Prompt: entry.target.Title},
		groups, func(partial map[string]string) {
			live := narrative
			live.Meanings = partial
			handler.SetNarrativeFor(targetID, live)
		})

	handler.Record(record)
	handler.SetNarrativeFor(targetID, narrative)
	_ = jsonl.New(entry.out).SaveNarrative(narrative)
}

// unitsFor is the net change a target covers. It is the same engine every other
// review uses; only the two refs differ.
func unitsFor(ctx context.Context, entry targetEntry) ([]domain.Unit, map[string]domain.Diff, error) {
	snap := gitsnap.New(entry.repo, entry.target.ID)
	storeRel := storeRelativeTo(entry.repo, entry.out)
	return usecase.BuildFileUnits(ctx, snap, entry.target.ID, entry.target.From, entry.target.To,
		func(f string) bool {
			return f == storeRel || strings.HasPrefix(f, storeRel+"/")
		})
}

// compareRefs turns two refs a reviewer typed into a target and registers it, so
// it opens and behaves exactly like a commit or a tag. An empty `to` means the
// working tree, which is what "compare against what I have now" means.
func compareRefs() web.CompareFunc {
	return func(ctx context.Context, repo, from, to string) (string, error) {
		entry, ok := anyEntryFor(repo)
		if !ok {
			return "", fmt.Errorf("no repository %q is open", repo)
		}
		snap := gitsnap.New(entry.repo, "compare")

		fromRef, err := snap.ResolveRef(ctx, strings.TrimSpace(from))
		if err != nil {
			return "", fmt.Errorf("cannot resolve %q in %s", from, filepath.Base(entry.repo))
		}
		toRef := domain.SnapshotRef{}
		if trimmed := strings.TrimSpace(to); trimmed != "" {
			toRef, err = snap.ResolveRef(ctx, trimmed)
			if err != nil {
				return "", fmt.Errorf("cannot resolve %q in %s", to, filepath.Base(entry.repo))
			}
		}

		title := from + " … " + firstNonEmpty(to, "working tree")
		target := usecase.RangeTarget(entry.repo, title, fromRef, toRef)
		targetIndex[target.ID] = targetEntry{target: target, repo: entry.repo, out: entry.out}
		return target.ID, nil
	}
}

// anyEntryFor finds a repository in the workspace by its short name, which is
// what the page had to hand.
func anyEntryFor(name string) (targetEntry, bool) {
	for _, entry := range targetIndex {
		if name == "" || filepath.Base(mustAbs(entry.repo)) == name {
			return entry, true
		}
	}
	return targetEntry{}, false
}

// describeOneFile says what a single file's change is for, on request. The
// folder's summary is where a reviewer starts; this is the next question, and
// it is asked deliberately rather than run in bulk.
func describeOneFile(sum port.Summarizer) web.DescribeFileFunc {
	return func(ctx context.Context, targetID, unitID string) (string, error) {
		entry, known := targetIndex[targetID]
		if !known {
			return "", fmt.Errorf("nothing here to review under %q", targetID)
		}
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()

		units, diffs, err := unitsFor(ctx, entry)
		if err != nil {
			return "", err
		}
		for _, u := range units {
			if u.ID != unitID {
				continue
			}
			meaning, err := usecase.DescribeFile(ctx, sum,
				domain.Session{Prompt: entry.target.Title}, u, diffs[u.ID])
			if err != nil {
				return "", err
			}
			saveMeaning(entry.out, targetID, usecase.FileKey(u), meaning)
			return meaning, nil
		}
		return "", fmt.Errorf("no such file in this review")
	}
}

// saveMeaning stores one description with the target's story, so it is written
// once and survives a restart.
func saveMeaning(out, targetID, key, meaning string) {
	store := jsonl.New(out)
	stored, err := store.LoadNarrative(targetID)
	if err != nil {
		return
	}
	if stored.Meanings == nil {
		stored.Meanings = map[string]string{}
	}
	stored.Meanings[key] = meaning
	stored.SessionID = targetID
	_ = store.SaveNarrative(stored)
}

// removeRepo stops watching a repository. Nothing on disk is touched: this
// closes a window, and the reviews and notes it holds stay exactly where they
// are, ready for the next time it is opened.
func removeRepo(out string) web.AddRepoFunc {
	return func(path string) ([]web.TargetSummary, []web.RepoStatus, error) {
		abs := mustAbs(path)

		remaining := map[string]bool{}
		found := false
		for id, entry := range targetIndex {
			if mustAbs(entry.repo) == abs {
				delete(targetIndex, id)
				found = true
				continue
			}
			remaining[entry.repo] = true
		}
		if !found {
			return nil, nil, fmt.Errorf("%s is not open", filepath.Base(abs))
		}
		if len(remaining) == 0 {
			return nil, nil, fmt.Errorf("that is the only repository open; add another first")
		}

		repos := make([]string, 0, len(remaining))
		for r := range remaining {
			repos = append(repos, r)
		}
		return discoverTargets(context.Background(), repos, out), openRepos(), nil
	}
}

// describeAnyTarget writes what one group of changes is for, in whichever target
// is open — not only the one the server started with.
func describeAnyTarget(sum port.Summarizer) web.DescribeFunc {
	return func(ctx context.Context, targetID, groupID string) (string, error) {
		entry, known := targetIndex[targetID]
		if !known {
			return "", fmt.Errorf("nothing here to review under %q", targetID)
		}
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()

		units, diffs, err := unitsFor(ctx, entry)
		if err != nil {
			return "", err
		}
		for _, g := range usecase.GroupChanges(units, diffs) {
			if g.ID != groupID {
				continue
			}
			described := usecase.DescribeGroups(ctx, sum,
				domain.Session{Prompt: entry.target.Title}, []domain.ChangeGroup{g}, nil)
			meaning, ok := described[groupID]
			if !ok {
				return "", fmt.Errorf("the model did not describe this change")
			}
			// Saved with that target's story, so it is written once.
			saveMeaning(entry.out, targetID, groupID, meaning)
			return meaning, nil
		}
		return "", fmt.Errorf("no such group in this review")
	}
}
