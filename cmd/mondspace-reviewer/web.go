package main

import (
	"context"
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
	units, diffs, err := usecase.BuildFileUnits(ctx, snap, *session, baseline, func(f string) bool {
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

	srv := &http.Server{
		Handler:           web.NewServer(view, store),
		ReadHeaderTimeout: 10 * time.Second,
	}
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
