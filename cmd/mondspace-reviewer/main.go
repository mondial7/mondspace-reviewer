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
	"github.com/mondial7/mondspace-reviewer/internal/adapter/source/opencode"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/source/replay"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/summarizer/claudecli"
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
	// No arguments is a request for help, not an error: someone typing `msr` is
	// asking what it does.
	if len(args) == 0 {
		greet("help")
		return runHelp(nil, stdout)
	}

	// On stderr, and only for the commands somebody is watching — stdout is
	// whatever the next thing in the pipe is reading.
	greet(args[0])

	switch args[0] {
	case "help", "--help", "-h":
		return runHelp(args[1:], stdout)
	case "version", "--version":
		return runVersion(stdout)
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
	case "web":
		return runWeb(ctx, args, stdout)
	case "mcp":
		return runMCP(ctx, args, stdin, stdout)
	case "gc":
		return runGC(ctx, args, stdout)
	default:
		return fmt.Errorf("unknown command %q — try `msr help`", args[0])
	}
}

func runReview(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	source := fs.String("source", "replay", "event source (replay|hooks|opencode)")
	file := fs.String("file", "", "recorded log to replay; for hooks/opencode, the events log to tail")
	usePlain := fs.Bool("plain", false, "use the plain presenter")
	useTUI := fs.Bool("tui", false, "review a stored session in the interactive TUI")
	verbose := fs.Bool("verbose", false, "list each unit's member events and snapshot refs")
	fs.BoolVar(verbose, "v", false, "shorthand for --verbose")
	out := fs.String("out", ".mondspace-reviewer", "store root directory")
	repo := fs.String("repo", ".", "repository to snapshot (hooks source / tui)")
	session := fs.String("session", "", "session id (hooks/tui)")
	since := fs.String("since", "", "review the net change from this ref (commit/branch/tag), bypassing sessions entirely")
	until := fs.String("until", "", "bound --since's far end at this ref (default: the current working tree)")
	showAll := fs.Bool("all", false, "include files .msrignore would keep out")
	summarizerURL := fs.String("summarizer-url", defaultSummarizerURL, "OpenAI-compatible summarizer endpoint")
	model := fs.String("model", defaultModel, "summarizer model")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	if *since != "" {
		return runSince(ctx, *usePlain, *useTUI, *verbose, *showAll, *session, *repo, *out, *since, *until, *summarizerURL, *model, stdout)
	}

	if *useTUI {
		// The TUI predates the cockpit and is no longer developed. It still works;
		// it will not gain anything added since v3.
		fmt.Fprintln(os.Stderr,
			"msr: the terminal UI is unmaintained — `msr web` has the current review")
		if *session == "" {
			return fmt.Errorf("--session is required for --tui")
		}
		snap := gitsnap.New(*repo, *session)
		sum := chooseSummarizer(*summarizerURL, *model)
		if *source == "hooks" || *source == "opencode" {
			events := *file
			if events == "" {
				events = filepath.Join(*out, *session, "events.jsonl")
			}
			src, err := liveSource(*source, events)
			if err != nil {
				return err
			}
			return runLiveTUI(ctx, jsonl.New(*out), snap, sum, *session, *repo, *out, src, stdout)
		}
		return runFileReview(ctx, jsonl.New(*out), snap, sum, *session, *repo, *out, stdout)
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
	case "hooks", "opencode":
		if *session == "" {
			return fmt.Errorf("--session is required for the %s source", *source)
		}
		events := *file
		if events == "" {
			events = filepath.Join(*out, *session, "events.jsonl")
		}
		src, err := liveSource(*source, events)
		if err != nil {
			return err
		}
		snap := gitsnap.New(*repo, *session)
		return usecase.ReviewLive(ctx, src, snap, store, pres)
	default:
		return fmt.Errorf("unknown source %q (msr supports replay|hooks|opencode)", *source)
	}
}

// liveSource builds the EventSource for a live (non-replay) source name,
// tailing the same events log format regardless of which agent produced it.
func liveSource(name, eventsPath string) (port.EventSource, error) {
	switch name {
	case "hooks":
		return hooks.New(eventsPath), nil
	case "opencode":
		return opencode.New(eventsPath), nil
	default:
		return nil, fmt.Errorf("unknown source %q (msr supports replay|hooks|opencode)", name)
	}
}

// Default summarizer endpoint: a local llama-server (ADR 0019).
//
// One model answers all three workloads. That is measured rather than assumed:
// Qwen3-4B-Instruct-2507 has no thinking mode at all, and it still gets the
// enum-constrained narration schema right 6 times out of 6 in about 7 seconds.
// A 9B alongside it made both slower, because two resident models on 24 GB cost
// roughly four times the latency of one.
//
// Start it with:
//
//	llama-server -hf bartowski/Qwen_Qwen3-4B-Instruct-2507-GGUF:Q6_K \
//	  --host 127.0.0.1 --port 8081 -c 32768 -fa on \
//	  --cache-type-k q8_0 --cache-type-v q8_0 --jinja
//
// Deliberately no --reasoning-format none: it is unnecessary here (this model
// never emits a reasoning channel) and on a model that does think it makes
// json_schema requests fail outright with "failed to initialize samplers".
//
// Set MSR_API_KEY if the endpoint requires a bearer token.
const (
	defaultSummarizerURL = "http://127.0.0.1:8081/v1"
	defaultModel         = "qwen3-4b-instruct-2507"
)

// runExport writes a review report for a stored session as Markdown or JSON.
func runExport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	format := fs.String("format", "md", "output format (md|json|slack)")
	out := fs.String("out", ".mondspace-reviewer", "store root directory")
	target := fs.String("target", "", "which review (a target id or session id; default: the one open in `msr web`)")
	session := fs.String("session", "", "session id — an older name for --target")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	reviewID, err := whichReview(*out, *target, *session)
	if err != nil {
		return err
	}

	sess, err := jsonl.New(*out).Load(reviewID)
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
	case "slack":
		_, err = fmt.Fprintln(stdout, usecase.ExportSlack(report))
		return err
	default:
		return fmt.Errorf("unknown format %q (want md|json|slack)", *format)
	}
}

// runGC deletes throwaway review refs (refs/mondspace/review/<session>,
// SPEC §7) for sessions that are done. With --session it deletes just that
// session's ref; otherwise it deletes the ref for every session whose store
// directory under --out is gone (its logs no longer exist). --dry-run prints
// what would be removed without touching any ref.
func runGC(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	session := fs.String("session", "", "delete only this session's review ref")
	repo := fs.String("repo", ".", "repository containing the review refs")
	out := fs.String("out", ".mondspace-reviewer", "store root directory")
	dryRun := fs.Bool("dry-run", false, "print what would be removed, without deleting")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	snap := gitsnap.New(*repo, "")

	var targets []string
	if *session != "" {
		targets = []string{*session}
	} else {
		refs, err := snap.ReviewRefs(ctx)
		if err != nil {
			return err
		}
		stored, err := storedSessions(*out)
		if err != nil {
			return err
		}
		targets = usecase.SessionsToGC(refs, stored)
	}

	if len(targets) == 0 {
		_, err := fmt.Fprintln(stdout, "nothing to garbage-collect: no review refs are eligible")
		return err
	}

	for _, id := range targets {
		ref := "refs/mondspace/review/" + id
		if *dryRun {
			if _, err := fmt.Fprintf(stdout, "would delete %s\n", ref); err != nil {
				return err
			}
			continue
		}
		if err := snap.DeleteReviewRef(ctx, id); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "deleted %s\n", ref); err != nil {
			return err
		}
	}
	return nil
}

// storedSessions lists the session IDs that still have a store directory
// under root — a missing directory (root itself, or an individual session)
// is not an error: it just means nothing is stored there yet.
func storedSessions(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// runAsk answers one question about a stored session and prints the answer.
// Scriptable interrogation: no TUI, no terminal.
func runAsk(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	scope := fs.String("scope", "unit", "ask scope (unit|session)")
	out := fs.String("out", ".mondspace-reviewer", "store root directory")
	target := fs.String("target", "", "which review (a target id or session id; default: the one open in `msr web`)")
	session := fs.String("session", "", "session id — an older name for --target")
	unitID := fs.String("unit", "", "unit id (unit scope)")
	repo := fs.String("repo", ".", "repository to diff (unit scope)")
	summarizerURL := fs.String("summarizer-url", defaultSummarizerURL, "summarizer endpoint")
	model := fs.String("model", defaultModel, "summarizer model")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	question := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if question == "" {
		return fmt.Errorf("a question is required")
	}
	reviewID, err := whichReview(*out, *target, *session)
	if err != nil {
		return err
	}

	store := jsonl.New(*out)
	sess, err := store.Load(reviewID)
	if err != nil {
		return err
	}

	askScope := domain.AskScope(*scope)
	var unit domain.Unit
	var diff domain.Diff
	if askScope == domain.AskUnit {
		unit = findUnit(sess.Units, *unitID)
		if d, err := gitsnap.New(*repo, reviewID).Diff(ctx, unit.From, unit.To, unit.Files); err == nil {
			diff = d
		}
	}

	askCtx := usecase.BuildAskContext(askScope, sess, unit, diff)

	// A local model takes seconds to minutes to answer. Silence for that long
	// reads as a hang.
	finish := terminalProgress().step("asking " + *model)
	answer, err := chooseSummarizer(*summarizerURL, *model).Answer(ctx, question, askCtx)
	finish(err)

	// Asked and answered is part of the review, whichever way it was asked. The
	// app has kept these since v6, and /search and `msr mcp` both read them; a
	// question typed into a terminal used to vanish the moment it was answered.
	record := domain.Exchange{
		ID: newULID(), SessionID: reviewID, TS: time.Now(),
		Question: question, Answer: answer, Failed: err != nil,
	}
	if err != nil {
		record.Answer = err.Error()
	}
	if keepErr := store.AppendExchange(record); keepErr != nil {
		fmt.Fprintln(os.Stderr, "msr: could not store the exchange:", keepErr)
	}
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(stdout, answer)
	return err
}

// whichReview resolves which review a command is about.
//
// --target is the current name, --session the older one, and with neither it is
// the review `msr web` last opened — which is the one you are looking at while
// you type the command (ADR 0031).
func whichReview(root, target, session string) (string, error) {
	if id := firstNonEmpty(target, session); id != "" {
		return id, nil
	}
	if open, ok := whatIsOpen(root); ok {
		return open.TargetID, nil
	}
	return "", fmt.Errorf("which review? pass --target=<id>, or open one in `msr web` first "+
		"(nothing has been reviewed under %s yet)", root)
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
	sess.Units = usecase.SuppressCoveredNoTest(sess.Units)
	notes := usecase.MarkSuperseded(sess.Units, sess.Notes)
	return tui.New(sess.Units, notes, store), nil
}

// chooseSummarizer probes the configured endpoint; if it is unreachable the
// reviewer degrades to the null (mechanical-only) summarizer. An API key from
// MSR_API_KEY (bearer token) is used for authenticated endpoints.
//
// MSR_NO_THINKING=1 asks the server to skip the model's reasoning phase. It is
// opt-in because it trades prose quality for speed, and only a model whose chat
// template reads enable_thinking honours it.
func chooseSummarizer(baseURL, model string) port.Summarizer {
	// A second engine behind the same port, chosen where the model is chosen:
	// the endpoint field already answers "which thing answers this", and a
	// scheme is enough to say "the Claude Code CLI on this machine" without a
	// second setting to fall out of step with it (ADR 0035).
	if strings.HasPrefix(strings.TrimSpace(baseURL), "claude:") {
		return claudecli.New(os.Getenv("MSR_CLAUDE_BIN"), model)
	}

	apiKey := os.Getenv("MSR_API_KEY")
	noThinking := os.Getenv("MSR_NO_THINKING") == "1"
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
			sum := openai.New(baseURL, model).WithAPIKey(apiKey)
			if noThinking {
				sum = sum.WithoutThinking()
			}
			return sum
		}
	}
	return null.New()
}

// teaPresenter streams sealed units into a running TUI program.
type teaPresenter struct{ send func(tea.Msg) }

func (t teaPresenter) Present(u domain.Unit, _ []domain.Event) error {
	t.send(tui.UnitAddedMsg{Unit: u})
	return nil
}

// runLiveTUI opens the queue empty and streams units into it as the agent works:
// a background pipeline tails the hook log, clusters, snapshots, and flags, and
// hands each sealed unit to the TUI. Units are a deterministic cache of events,
// so they are rebuilt cleanly on attach.
func runLiveTUI(ctx context.Context, store port.Store, snap port.Snapshotter, sum port.Summarizer, sessionID, repo, out string, src port.EventSource, stdout io.Writer) error {
	_ = os.Remove(filepath.Join(out, sessionID, "units.jsonl"))

	sess, err := store.Load(sessionID)
	if err != nil {
		return err
	}
	notes := usecase.MarkSuperseded(sess.Units, sess.Notes)
	model := tui.New(nil, notes, store).
		RelativeTo(repo).
		WithSummarize(summarizeFunc(snap, sum)).
		WithDiff(diffFunc(snap)).
		WithAsk(askFunc(sess, snap, sum))

	p := tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(stdout))

	liveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_ = usecase.ReviewLive(liveCtx, src, snap, store, teaPresenter{send: p.Send})
	}()

	_, err = p.Run()
	return err
}

// runFileReview is retroactive review: it reconstructs the session's *net*
// change from git — a per-file diff against the commit just before the session —
// so back-and-forth edits collapse into one reviewable change per file, each
// with its real diff, rather than a unit per keystroke.
func runFileReview(ctx context.Context, store port.Store, snap *gitsnap.Snapshotter, sum port.Summarizer, sessionID, repo, out string, stdout io.Writer) error {
	sess, err := store.Load(sessionID)
	if err != nil {
		return err
	}

	baseline, err := snap.Baseline(ctx, firstEventTime(sess))
	if err != nil {
		return err
	}
	units, diffs, err := buildFileUnits(ctx, snap, sessionID, repo, out, baseline, domain.SnapshotRef{})
	if err != nil {
		return err
	}
	sess.Units = units
	notes := usecase.PlaceNotes(units, sess.Notes)

	model := tui.New(units, notes, store).
		RelativeTo(repo).
		WithDiffs(diffs).
		WithSummarize(summarizeFunc(snap, sum)).
		WithDiff(diffFunc(snap)).
		WithAsk(askFunc(sess, snap, sum))
	_, err = tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(stdout)).Run()
	return err
}

// buildFileUnits is the per-file net-diff engine shared by every retroactive
// review path: one unit per file changed between baseline and until (an empty
// until diffs against the current working tree), with its real diff, flags,
// and mechanical headline. `sessionID` only seeds unit IDs — it need not name
// a recorded session.
func buildFileUnits(ctx context.Context, snap *gitsnap.Snapshotter, sessionID, repo, out string, baseline, until domain.SnapshotRef) ([]domain.Unit, map[string]domain.Diff, error) {
	files, err := snap.ChangedFiles(ctx, baseline, until)
	if err != nil {
		return nil, nil, err
	}
	files = excludeStore(files, repo, out) // never review msr's own store

	diffs := map[string]domain.Diff{}
	var units []domain.Unit
	for i, f := range files {
		d, err := snap.Diff(ctx, baseline, until, []string{f})
		if err != nil {
			d = domain.Diff{}
		}
		u := domain.Unit{
			ID:        fmt.Sprintf("%s-f%03d", sessionID, i+1),
			SessionID: sessionID,
			Files:     []string{f},
			From:      baseline,
			To:        until,
			Sealed:    true,
		}
		u.Flags = usecase.Flags(u, d)
		u.Headline = usecase.DiffHeadline(f, d)
		diffs[u.ID] = d
		units = append(units, u)
	}
	units = usecase.SuppressCoveredNoTest(units)
	return units, diffs, nil
}

// runSince dispatches --since review to the plain or TUI presenter. It needs
// no --session: with none given it synthesizes one from --since, so unit ids
// stay stable and annotations still land under .mondspace-reviewer/.
func runSince(ctx context.Context, usePlain, useTUI, verbose, showAll bool, session, repo, out, since, until, summarizerURL, model string, stdout io.Writer) error {
	sessionID := session
	if sessionID == "" {
		sessionID = "since-" + shortRef(since)
	}
	snap := gitsnap.New(repo, sessionID)

	switch {
	case useTUI:
		sum := chooseSummarizer(summarizerURL, model)
		return runSinceReview(ctx, jsonl.New(out), snap, sum, sessionID, repo, out, since, until, stdout)
	case usePlain:
		pres := plain.New(stdout).RelativeTo(repo)
		if verbose {
			pres.Verbose()
		}
		return runSincePlain(ctx, snap, pres, sessionID, repo, out, since, until, showAll, stdout)
	default:
		return fmt.Errorf("--plain or --tui is required")
	}
}

// runSincePlain presents a --since review through the plain presenter: the
// same per-file units buildFileUnits produces for any other retroactive
// review, with no TUI and no session required.
func runSincePlain(ctx context.Context, snap *gitsnap.Snapshotter, pres port.Presenter,
	sessionID, repo, out, since, until string, showAll bool, stdout io.Writer) error {

	// Reading a wide range is a git process per file and a pass of the flag
	// rules over each one. On a release-sized range that is long enough to
	// look like nothing is happening.
	finish := terminalProgress().step("reading " + since + "…")
	units, _, err := sinceFileUnits(ctx, snap, sessionID, repo, out, since, until)
	finish(err)
	if err != nil {
		return err
	}

	// The same .msrignore the app reads (ADR 0027). Without this the same
	// repository gave two different reviews depending on how you opened it.
	var hidden []usecase.Hidden
	if !showAll {
		rules, _ := snap.Ignored(ctx, filepath.Join(repo, gitsnap.IgnoreFile), unitPaths(units))
		units, hidden = usecase.SplitIgnored(units, rules)
	}

	for _, u := range units {
		if err := pres.Present(u, nil); err != nil {
			return err
		}
	}
	return reportHidden(stdout, hidden)
}

// unitPaths is every file the units cover, for the ignore check.
func unitPaths(units []domain.Unit) []string {
	var paths []string
	for _, u := range units {
		paths = append(paths, u.Files...)
	}
	return paths
}

// reportHidden names what was kept out and the rule that kept it.
//
// Never silently: a review tool that hides files without saying so is one you
// cannot trust, which is the whole reason .msrignore has no defaults.
func reportHidden(w io.Writer, hidden []usecase.Hidden) error {
	if len(hidden) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\n%s hidden by .msrignore (--all to see them):\n",
		count(len(hidden), "file")); err != nil {
		return err
	}
	for _, h := range hidden {
		if _, err := fmt.Fprintf(w, "  %s  (%s)\n", h.Path, h.Pattern); err != nil {
			return err
		}
	}
	return nil
}

// count renders "1 file" and "2 files".
func count(n int, thing string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", thing)
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

// runSinceReview is --since review in the interactive TUI. It loads whatever
// session state exists under sessionID (empty when --session was not given,
// or when it names a session msr has not seen before — Store.Load degrades to
// an empty Session rather than erroring), so annotations still persist and
// supersession still works even though there is no event log driving units.
func runSinceReview(ctx context.Context, store port.Store, snap *gitsnap.Snapshotter, sum port.Summarizer, sessionID, repo, out, since, until string, stdout io.Writer) error {
	units, diffs, err := sinceFileUnits(ctx, snap, sessionID, repo, out, since, until)
	if err != nil {
		return err
	}
	sess, err := store.Load(sessionID)
	if err != nil {
		return err
	}
	sess.Units = units
	notes := usecase.PlaceNotes(units, sess.Notes)

	model := tui.New(units, notes, store).
		RelativeTo(repo).
		WithDiffs(diffs).
		WithSummarize(summarizeFunc(snap, sum)).
		WithDiff(diffFunc(snap)).
		WithAsk(askFunc(sess, snap, sum))
	_, err = tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(stdout)).Run()
	return err
}

// sinceFileUnits resolves --since/--until against git and builds the per-file
// units for that range, sharing buildFileUnits with session-based retroactive
// review.
func sinceFileUnits(ctx context.Context, snap *gitsnap.Snapshotter, sessionID, repo, out, since, until string) ([]domain.Unit, map[string]domain.Diff, error) {
	baseline, untilRef, err := resolveSinceRange(ctx, snap, since, until)
	if err != nil {
		return nil, nil, err
	}
	return buildFileUnits(ctx, snap, sessionID, repo, out, baseline, untilRef)
}

// resolveSinceRange resolves --since (required) and --until (optional; the
// zero SnapshotRef means "the current working tree") to concrete refs.
func resolveSinceRange(ctx context.Context, snap *gitsnap.Snapshotter, since, until string) (baseline, untilRef domain.SnapshotRef, err error) {
	baseline, err = snap.ResolveRef(ctx, since)
	if err != nil {
		return domain.SnapshotRef{}, domain.SnapshotRef{}, fmt.Errorf("resolving --since=%q: %w", since, err)
	}
	if until == "" {
		return baseline, domain.SnapshotRef{}, nil
	}
	untilRef, err = snap.ResolveRef(ctx, until)
	if err != nil {
		return domain.SnapshotRef{}, domain.SnapshotRef{}, fmt.Errorf("resolving --until=%q: %w", until, err)
	}
	return baseline, untilRef, nil
}

// shortRef turns a ref into a short, session-id-safe token for synthesizing
// "since-<shortref>" when --since is used with no --session.
func shortRef(ref string) string {
	r := strings.NewReplacer("/", "-", "\\", "-").Replace(ref)
	const maxLen = 24
	if len(r) > maxLen {
		r = r[:maxLen]
	}
	return r
}

// excludeStore drops files under msr's own store directory from the review, so
// the reviewer never sees the tool's bookkeeping as if it were agent work.
func excludeStore(files []string, repo, out string) []string {
	storeRel := out
	if repoAbs, err := filepath.Abs(repo); err == nil {
		if outAbs, err := filepath.Abs(out); err == nil {
			if rel, err := filepath.Rel(repoAbs, outAbs); err == nil {
				storeRel = rel
			}
		}
	}
	inStore := usecase.InStore(storeRel)
	kept := files[:0]
	for _, f := range files {
		if inStore(f) {
			continue
		}
		kept = append(kept, f)
	}
	return kept
}

// firstEventTime is the earliest event timestamp in the session — when the agent
// started work — used to pick the pre-session git baseline.
func firstEventTime(sess domain.Session) time.Time {
	earliest := time.Time{}
	for _, e := range sess.Events {
		if e.TS.IsZero() {
			continue
		}
		if earliest.IsZero() || e.TS.Before(earliest) {
			earliest = e.TS
		}
	}
	if earliest.IsZero() {
		return time.Now()
	}
	return earliest
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

// diffFunc builds the async diff loader used when a unit is expanded.
func diffFunc(snap port.Snapshotter) func(domain.Unit) tea.Msg {
	return func(u domain.Unit) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		diff, err := snap.Diff(ctx, u.From, u.To, u.Files)
		if err != nil {
			diff = domain.Diff{}
		}
		return tui.DiffReadyMsg{UnitID: u.ID, Diff: diff}
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
