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
	session := fs.String("session", "", "session id")
	schema := fs.String("pg-schema", pgstore.DefaultSchema, "Postgres schema (never public)")
	summarizerURL := fs.String("summarizer-url", defaultSummarizerURL, "OpenAI-compatible summarizer endpoint")
	model := fs.String("model", defaultModel, "summarizer model")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *session == "" {
		return fmt.Errorf("--session is required")
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

	sum := chooseSummarizer(*summarizerURL, *model)
	// Serve the deterministic story straight away and let the model upgrade it in
	// the background: the page never waits on a model (ADR 0013).
	handler := web.NewServer(view, store).
		WithNarrative(domain.Narrative{
			SessionID: *session,
			Title:     sess.Prompt,
			Chapters:  usecase.GroupByPath(units),
			Source:    domain.NarrativeMechanical,
		}).
		WithWorkspace(discoverSessions(*out, *repo)).
		WithAsk(webAskFunc(sess, snap, sum)).
		WithReanalyse(webReanalyseFunc(snap, sum, *model)).
		WithAudit(auditFile(filepath.Join(*out, *session, "audit.jsonl")))

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		// Narration runs in the background and a local model narrates one area at
		// a time, so the budget is generous; the page never waits on it.
		narrateCtx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()

		started := time.Now()
		narrative, err := usecase.NarrateProgressively(narrateCtx, sum, sess, units,
			handler.SetNarrative) // publish each chapter as the model writes it

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
	}()

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
