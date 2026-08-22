// Command mondspace-reviewer (msr) is a terminal review companion for an
// autonomous coding agent. M0 exposes the walking skeleton: replay a recorded
// log, cluster it, store it, and print the units with the plain presenter.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/presenter/plain"
	gitsnap "github.com/marcomondini/mondspace-reviewer/internal/adapter/snapshot/git"
	"github.com/marcomondini/mondspace-reviewer/internal/adapter/source/hooks"
	"github.com/marcomondini/mondspace-reviewer/internal/adapter/source/replay"
	"github.com/marcomondini/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "msr:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: msr <review|ingest|install-hooks> ...")
	}
	switch args[0] {
	case "review":
		return runReview(ctx, args, stdout)
	case "ingest":
		return runIngest(args, stdin)
	case "install-hooks":
		return runInstallHooks(args)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runReview(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	source := fs.String("source", "replay", "event source (replay|hooks)")
	file := fs.String("file", "", "recorded log to replay; for hooks, the events.jsonl to tail")
	usePlain := fs.Bool("plain", false, "use the plain presenter")
	out := fs.String("out", ".mondspace-reviewer", "store root directory")
	repo := fs.String("repo", ".", "repository to snapshot (hooks source)")
	session := fs.String("session", "", "session id (hooks source)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if !*usePlain {
		return fmt.Errorf("--plain is required (M1 has no TUI)")
	}

	store := jsonl.New(*out)
	pres := plain.New(stdout)

	switch *source {
	case "replay":
		if *file == "" {
			return fmt.Errorf("--file is required for the replay source")
		}
		return usecase.Review(ctx, replay.New(*file), store, pres)
	case "hooks":
		if *session == "" {
			return fmt.Errorf("--session is required for the hooks source")
		}
		events := *file
		if events == "" {
			events = filepath.Join(*out, *session, "events.jsonl")
		}
		snap := gitsnap.New(*repo, *session)
		return usecase.ReviewLive(ctx, hooks.New(events), snap, store, pres)
	default:
		return fmt.Errorf("unknown source %q (M1 supports replay|hooks)", *source)
	}
}

// runIngest reads one hook payload from stdin and appends an Event. It always
// returns nil: a broken reviewer must never fail the agent's hook. Anything
// that goes wrong is swallowed after best-effort work.
func runIngest(args []string, stdin io.Reader) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	kind := fs.String("kind", "", "event kind (edit|write|bash|prompt|batch_end)")
	out := fs.String("out", ".mondspace-reviewer", "store root directory")
	if err := fs.Parse(args[1:]); err != nil {
		return nil
	}

	payload, err := io.ReadAll(stdin)
	if err != nil {
		return nil
	}
	event, err := usecase.BuildEvent(domain.Kind(*kind), payload, newULID(), time.Now().UTC())
	if err != nil {
		return nil
	}
	_ = jsonl.New(*out).AppendEvent(event)
	return nil
}

func newULID() string {
	return ulid.Make().String()
}

// runInstallHooks writes the reviewer's hooks into <dir>/.claude/settings.json,
// merging with any existing file.
func runInstallHooks(args []string) error {
	fs := flag.NewFlagSet("install-hooks", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory containing .claude/")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	claudeDir := filepath.Join(*dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(claudeDir, "settings.json")

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	merged, err := usecase.InstallHooks(existing)
	if err != nil {
		return err
	}
	return os.WriteFile(path, merged, 0o644)
}
