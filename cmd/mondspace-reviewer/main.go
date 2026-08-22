// Command mondspace-reviewer (msr) is a terminal review companion for an
// autonomous coding agent. M0 exposes the walking skeleton: replay a recorded
// log, cluster it, store it, and print the units with the plain presenter.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oklog/ulid/v2"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/plain"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/tui"
	gitsnap "github.com/mondial7/mondspace-reviewer/internal/adapter/snapshot/git"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/source/hooks"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/source/replay"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/summarizer/null"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/summarizer/openai"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
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
	case "ask":
		return runAsk(ctx, args, stdout)
	case "export":
		return runExport(args, stdout)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runReview(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	source := fs.String("source", "replay", "event source (replay|hooks)")
	file := fs.String("file", "", "recorded log to replay; for hooks, the events.jsonl to tail")
	usePlain := fs.Bool("plain", false, "use the plain presenter")
	useTUI := fs.Bool("tui", false, "review a stored session in the interactive TUI")
	verbose := fs.Bool("verbose", false, "list each unit's member events and snapshot refs")
	fs.BoolVar(verbose, "v", false, "shorthand for --verbose")
	out := fs.String("out", ".mondspace-reviewer", "store root directory")
	repo := fs.String("repo", ".", "repository to snapshot (hooks source / tui)")
	session := fs.String("session", "", "session id (hooks/tui)")
	summarizerURL := fs.String("summarizer-url", defaultSummarizerURL, "OpenAI-compatible summarizer endpoint")
	model := fs.String("model", defaultModel, "summarizer model")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	if *useTUI {
		if *session == "" {
			return fmt.Errorf("--session is required for --tui")
		}
		snap := gitsnap.New(*repo, *session)
		sum := chooseSummarizer(*summarizerURL, *model)
		return runTUIReview(jsonl.New(*out), snap, sum, *session, *repo, stdout)
	}
	if !*usePlain {
		return fmt.Errorf("--plain or --tui is required")
	}

	store := jsonl.New(*out)
	pres := plain.New(stdout).RelativeTo(*repo)
	if *verbose {
		pres.Verbose()
	}

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

// Default summarizer endpoint (SPEC §3): a local LM Studio server.
const (
	defaultSummarizerURL = "http://192.168.101.99:1234/v1"
	defaultModel         = "qwen/qwen3.5-9b"
)

// runExport writes a review report for a stored session as Markdown or JSON.
func runExport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	format := fs.String("format", "md", "output format (md|json)")
	out := fs.String("out", ".mondspace-reviewer", "store root directory")
	session := fs.String("session", "", "session id")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *session == "" {
		return fmt.Errorf("--session is required")
	}

	sess, err := jsonl.New(*out).Load(*session)
	if err != nil {
		return err
	}
	report := usecase.BuildReport(sess)

	switch *format {
	case "md":
		_, err = io.WriteString(stdout, usecase.ExportMarkdown(report))
		return err
	case "json":
		data, err := usecase.ExportJSON(report)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, string(data))
		return err
	default:
		return fmt.Errorf("unknown format %q (want md|json)", *format)
	}
}

// runAsk answers one question about a stored session and prints the answer.
// Scriptable interrogation: no TUI, no terminal.
func runAsk(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	scope := fs.String("scope", "unit", "ask scope (unit|session)")
	out := fs.String("out", ".mondspace-reviewer", "store root directory")
	session := fs.String("session", "", "session id")
	unitID := fs.String("unit", "", "unit id (unit scope)")
	repo := fs.String("repo", ".", "repository to diff (unit scope)")
	summarizerURL := fs.String("summarizer-url", defaultSummarizerURL, "summarizer endpoint")
	model := fs.String("model", defaultModel, "summarizer model")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *session == "" {
		return fmt.Errorf("--session is required")
	}
	question := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if question == "" {
		return fmt.Errorf("a question is required")
	}

	sess, err := jsonl.New(*out).Load(*session)
	if err != nil {
		return err
	}

	askScope := domain.AskScope(*scope)
	var unit domain.Unit
	var diff domain.Diff
	if askScope == domain.AskUnit {
		unit = findUnit(sess.Units, *unitID)
		if d, err := gitsnap.New(*repo, *session).Diff(ctx, unit.From, unit.To, unit.Files); err == nil {
			diff = d
		}
	}

	askCtx := usecase.BuildAskContext(askScope, sess, unit, diff)
	answer, err := chooseSummarizer(*summarizerURL, *model).Answer(ctx, question, askCtx)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, answer)
	return err
}

func findUnit(units []domain.Unit, id string) domain.Unit {
	for _, u := range units {
		if u.ID == id {
			return u
		}
	}
	return domain.Unit{}
}

// buildTUIModel loads a stored session, marks superseded notes, and hands the
// units and notes to the interactive model.
func buildTUIModel(store port.Store, sessionID string) (tui.Model, error) {
	sess, err := store.Load(sessionID)
	if err != nil {
		return tui.Model{}, err
	}
	notes := usecase.MarkSuperseded(sess.Units, sess.Notes)
	return tui.New(sess.Units, notes, store), nil
}

// chooseSummarizer probes the configured endpoint; if it is unreachable the
// reviewer degrades to the null (mechanical-only) summarizer. An API key from
// MSR_API_KEY (bearer token) is used for authenticated endpoints.
func chooseSummarizer(baseURL, model string) port.Summarizer {
	apiKey := os.Getenv("MSR_API_KEY")
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return null.New()
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode < 500 {
			return openai.New(baseURL, model).WithAPIKey(apiKey)
		}
	}
	return null.New()
}

func runTUIReview(store port.Store, snap port.Snapshotter, sum port.Summarizer, sessionID, repo string, stdout io.Writer) error {
	sess, err := store.Load(sessionID)
	if err != nil {
		return err
	}
	notes := usecase.MarkSuperseded(sess.Units, sess.Notes)
	model := tui.New(sess.Units, notes, store).
		RelativeTo(repo).
		WithSummarize(summarizeFunc(snap, sum)).
		WithAsk(askFunc(sess, snap, sum))
	_, err = tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(stdout)).Run()
	return err
}

// askFunc builds the interrogation closure: assemble the bounded context (unit
// scope fetches the diff), ask the summarizer, and hand the answer back. Any
// error becomes a readable notice rather than a crash.
func askFunc(sess domain.Session, snap port.Snapshotter, sum port.Summarizer) func(domain.AskScope, domain.Unit, string) tea.Msg {
	return func(scope domain.AskScope, unit domain.Unit, question string) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var diff domain.Diff
		if scope == domain.AskUnit {
			if d, err := snap.Diff(ctx, unit.From, unit.To, unit.Files); err == nil {
				diff = d
			}
		}
		askCtx := usecase.BuildAskContext(scope, sess, unit, diff)
		answer, err := sum.Answer(ctx, question, askCtx)
		if err != nil {
			answer = "(" + err.Error() + ")"
		}
		return tui.AnswerReadyMsg{Text: answer}
	}
}

// summarizeFunc builds the async fill-in closure: fetch a unit's diff, ask the
// summarizer (with WhySrc discipline), and hand the headline back to the queue.
func summarizeFunc(snap port.Snapshotter, sum port.Summarizer) func(domain.Unit) tea.Msg {
	return func(u domain.Unit) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		diff, err := snap.Diff(ctx, u.From, u.To, u.Files)
		if err != nil {
			diff = domain.Diff{}
		}
		return tui.HeadlineReadyMsg{UnitID: u.ID, Headline: usecase.Summarize(ctx, sum, u, diff)}
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
	command := fs.String("command", "", "command each hook runs (default: absolute path to this binary)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	// Hooks run under /bin/sh, which has no shell aliases and a bare PATH, so
	// default to this binary's absolute path rather than a bare name.
	hookCommand := *command
	if hookCommand == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolving binary path for hooks: %w", err)
		}
		hookCommand = exe
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
	merged, err := usecase.InstallHooks(existing, hookCommand)
	if err != nil {
		return err
	}
	return os.WriteFile(path, merged, 0o644)
}
