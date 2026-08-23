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
	repo := fs.String("repo", ".", "repository to review")
	var also repoList
	fs.Var(&also, "repo-also", "another repository to include in the workspace (repeatable)")
	session := fs.String("session", "", "session id")
	schema := fs.String("pg-schema", pgstore.DefaultSchema, "Postgres schema (never public)")
	summarizerURL := fs.String("summarizer-url", defaultSummarizerURL, "OpenAI-compatible summarizer endpoint")
	model := fs.String("model", defaultModel, "summarizer model")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	// With no --session, open the newest review in the workspace: a reviewer
	// arriving at a repository should not have to look up an id first.
	workspace := discoverWorkspace(append([]string{*repo}, also...), *out)
	if *session == "" {
		if len(workspace) == 0 {
			return fmt.Errorf("no reviews found — run `msr review` first, or pass --session")
		}
		*session = workspace[0].ID
	}

	// Postgres is opt-in via MSR_POSTGRES_DSN; otherwise the JSONL store is used.
	store, closeStore, err := openStore(ctx, *out, *schema)
	if err != nil {
		return err
	}
	defer closeStore()

	sess, err := store.Load(*session)
	if err != nil {
		return err
	}

	snap := gitsnap.New(*repo, *session)
	baseline, err := snap.Baseline(ctx, firstEventTime(sess))
	if err != nil {
		return err
	}
	storeRel := storeRelativeTo(*repo, *out)
	units, diffs, err := usecase.BuildFileUnits(ctx, snap, *session, baseline, domain.SnapshotRef{}, func(f string) bool {
		return f == storeRel || strings.HasPrefix(f, storeRel+"/")
	})
	if err != nil {
		return err
	}

	view := web.Session{
		ID:     *session,
		Prompt: sess.Prompt,
		Repo:   *repo,
		Units:  units,
		Notes:  usecase.MarkSuperseded(units, sess.Notes),
		Diffs:  diffs,
	}

	firstStats, err := snap.Numstat(ctx, baseline, domain.SnapshotRef{})
	if err != nil {
		firstStats = nil // a review that cannot be measured still renders
	}

	sum := chooseSummarizer(*summarizerURL, *model)
	mechanical := domain.Narrative{
		SessionID:   *session,
		Title:       sess.Prompt,
		Chapters:    usecase.GroupByPath(units),
		Source:      domain.NarrativeMechanical,
		Fingerprint: usecase.Fingerprint(units),
	}

	// A story already written for this exact review is reused as it stands.
	// Narration costs several model calls, so re-opening the page must not pay
	// for them again — the retry button on the story page is the way to ask.
	cache, cacheable := store.(narrativeCache)
	shown := mechanical
	if cacheable {
		if stored, err := cache.LoadNarrative(*session); err == nil &&
			stored.Fingerprint == mechanical.Fingerprint && len(stored.Chapters) > 0 {
			shown = stored
		}
	}

	// The cockpit's numbers are git facts, gathered once now and refreshed while
	// the page is open. A repository that cannot list commits still gets every
	// other stat rather than an empty panel.
	commits, _ := snap.CommitsSince(ctx, firstEventTime(sess))

	handler := web.NewServer(view, store).
		WithStats(usecase.ComputeStats(sess, units, diffs, commits, time.Now())).
		WithAgent(agentStatus(ctx, sum, *summarizerURL, *model)).
		WithHistories(usecase.FileHistories(sess.Events, units)).
		WithNarrative(shown).
		WithWorkspace(workspace).
		WithLoader(sessionLoader(workspace, *out)).
		WithAsk(webAskFunc(sess, snap, sum)).
		WithReanalyse(webReanalyseFunc(snap, sum, *model)).
		WithAudit(auditFile(filepath.Join(*out, *session, "audit.jsonl")))

	// narrateOnce is the only thing in the app that calls the model unbidden, and
	// it runs at most once per review: on first sight of it, or when the reviewer
	// asks again. Whatever it produces — model story or fallback — is stored with
	// the review's fingerprint, so a failure is not retried by navigating.
	narrateOnce := func(ctx context.Context) {
		narrateCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
		defer cancel()

		started := time.Now()
		narrative, err := usecase.NarrateProgressively(narrateCtx, sum, sess, units,
			handler.SetNarrative) // publish each chapter as the model writes it
		narrative.Fingerprint = mechanical.Fingerprint
		narrative.Model = *model

		// Narration is the one model call the reviewer never triggers, so it is
		// the one most worth recording: without this it is invisible.
		entry := web.AuditEntry{
			SessionID: *session, Action: "narrate", Model: *model,
			Millis: time.Since(started).Milliseconds(),
			Detail: fmt.Sprintf("%d chapters, %s", len(narrative.Chapters), narrative.Source),
		}
		if err != nil {
			entry.Failed = true
			entry.Detail = err.Error()
			fmt.Fprintln(os.Stderr, "msr: story fell back to mechanical grouping:", err)
		}
		handler.Record(entry)
		handler.SetNarrative(narrative)

		if cacheable {
			if err := cache.SaveNarrative(narrative); err != nil {
				fmt.Fprintln(os.Stderr, "msr: could not store the story:", err)
			}
		}
	}
	handler = handler.WithNarrate(narrateOnce)

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if shown.Source != domain.NarrativeModel {
		// Never narrated, or the stored story is stale. Write it once now; from
		// here on it is the reviewer's call. Going through the server means this
		// and a retry click can never overlap.
		handler.NarrateNow(context.Background())
	}

	// A cockpit left open on a second screen must not go stale: the session it is
	// watching may still be running, so the numbers are recomputed on a tick and
	// pushed to open pages. Reading git every 15s is cheap; a model is never
	// involved.
	go refreshReview(ctx, reviewRefresher{
		handler: handler, snap: snap, store: store, sum: sum,
		sessionID: *session, repo: *repo, storeRel: storeRel,
		baseline: baseline, model: *model,
		fingerprint: usecase.ReviewFingerprint(firstStats), narrate: narrateOnce,
	})
	go refreshAgent(ctx, handler, sum, *summarizerURL, *model)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "reviewing %s — http://%s\n", *session, ln.Addr())

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
func webAskFunc(sess domain.Session, snap port.Snapshotter, sum port.Summarizer) web.AskFunc {
	return func(ctx context.Context, question string, history []web.Exchange) (string, error) {
		askCtx := usecase.BuildAskContext(domain.AskSession, sess, domain.Unit{}, domain.Diff{})
		return sum.Answer(ctx, withHistory(question, history), askCtx)
	}
}

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

// Entries reads the log back for the activity page. A log that has never been
// written to is not an error — it is an empty history.
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
	narrate     func(context.Context)
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
				r.handler.NarrateNow(ctx)
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

// repoList collects a repeatable --repo-also flag, so one server can hold a
// workspace spanning several repositories.
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
