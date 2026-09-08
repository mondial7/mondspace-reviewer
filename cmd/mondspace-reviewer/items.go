package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/config"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/delivery"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/scanner/local"
	gitsnap "github.com/mondial7/mondspace-reviewer/internal/adapter/snapshot/git"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/items"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
	"github.com/mondial7/mondspace-reviewer/internal/usecase/handoff"
)

// The commands over the findings store (ADR 0044, ADR 0045, ADR 0046).
//
// `scan` fills it, `findings` reads and rules on it, `push` hands a selection
// to whoever is going to act on it. All three are non-interactive on purpose:
// this is the half of msr that a script, a hook or an agent can drive, and the
// lenses are views over the same store rather than the only way to reach it.

// runScan runs the installed analysers over a range and reconciles what they
// said into the store.
//
// The second run over unchanged code writes nothing, which is the property the
// whole store is built on: this is a pass, not a log.
func runScan(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository to scan")
	since := fs.String("since", "HEAD", "the ref to measure the change against")
	until := fs.String("until", "", "the far end of the range (default: the working tree)")
	dir := fs.String("dir", "", "shared directory holding the findings store (default <repo>/.mondspace)")
	branch := fs.String("branch", "", "record findings against this branch (default: the one checked out)")
	all := fs.Bool("all", false, "include files .msrignore would keep out")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	shared := sharedDir(*repo, *dir)
	snap := gitsnap.New(*repo, "scan-"+shortRef(*since))
	// Neither store is code under review. The session log is excluded by
	// buildFileUnits; the shared directory is excluded here, and it matters
	// more, because a repository that commits its findings file would
	// otherwise raise findings about its own findings.
	units, diffs, err := sinceFileUnits(ctx, snap, "scan-"+shortRef(*since), *repo, shared, *since, *until)
	if err != nil {
		return err
	}
	units = outsideStore(units, *repo, ".mondspace-reviewer")
	if !*all {
		if rules, err := snap.Ignored(ctx, gitsnap.IgnoreFile, pathsOf(units)); err == nil {
			units, _ = usecase.SplitIgnored(units, rules)
		}
	}
	if len(units) == 0 {
		fmt.Fprintln(stdout, "nothing changed in that range")
		return nil
	}

	analysers, err := config.LoadAnalysers(*repo)
	if err != nil {
		return err
	}
	scanner := local.New(*repo, analysers)
	baseline, _, err := resolveSinceRange(ctx, snap, *since, *until)
	if err != nil {
		return err
	}

	found := scanner.Look(ctx, pathsOf(units), usecase.FilePrints(units, diffs), baseline.Commit)
	found = usecase.MarkNew(found, units, diffs)
	found = append(found, usecase.FlagFindings(units, diffs)...)

	at := time.Now().UTC()
	sight := usecase.Sighting{
		Branch:    branchName(ctx, snap, *branch),
		SessionID: "",
		Commit:    baseline.Commit,
		At:        at,
		Mint:      newULID,
		Body:      fileBodies(*repo),
	}

	store := items.New(shared)
	stored, err := store.All()
	if err != nil {
		return err
	}
	changed := usecase.Reconcile(stored, sight.FromReported(found), usecase.Pass{
		At:        at,
		Producers: ranProducers(scanner),
		Paths:     pathSet(pathsOf(units)),
	})
	if err := store.Append(changed...); err != nil {
		return err
	}

	return reportScan(stdout, store, sight.Branch, changed)
}

// reportScan says what the pass did, in the terms the store thinks in.
func reportScan(stdout io.Writer, store *items.Store, branch string, changed []contract.Item) error {
	raised, closed := 0, 0
	for _, item := range changed {
		if item.CurrentState() == contract.StateFixed {
			closed++
			continue
		}
		raised++
	}
	fmt.Fprintf(stdout, "%d raised or updated, %d closed on %s\n", raised, closed, branchLabel(branch))

	onBranch, err := store.OnBranch(branch)
	if err != nil {
		return err
	}
	shown, held := usecase.Surface(onBranch, usecase.SurfaceCap, "")
	for _, item := range shown {
		fmt.Fprintf(stdout, "  %-9s %-28s %s\n", item.Severity.Normalise(), item.Where(), item.Title)
	}
	if held > 0 {
		fmt.Fprintf(stdout, "  … and %d more, stored\n", held)
	}
	for _, rule := range usecase.RulesWorthSuppressing(onBranch) {
		fmt.Fprintf(stdout, "note: %s has been dismissed %d times — consider turning it off in %s\n",
			rule, usecase.DismissalsBeforeSuppressing, config.AnalyserFile)
	}
	return nil
}

// runFindings lists what stands, and records what the reviewer made of it.
//
//	msr findings                     what is open on this branch
//	msr findings dismiss <id>...     looked at, not a problem
//	msr findings accept <id>...      acknowledged, queued for work
func runFindings(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("findings", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository")
	dir := fs.String("dir", "", "shared directory holding the findings store (default <repo>/.mondspace)")
	branch := fs.String("branch", "", "which branch's findings (default: the one checked out)")
	everywhere := fs.Bool("all-branches", false, "every branch, not just this one")
	minSeverity := fs.String("min-severity", "", "only findings at least this severe (low|medium|high)")
	settled := fs.Bool("settled", false, "include what has been dismissed or fixed")
	format := fs.String("format", "text", "output format (text|jsonl)")
	flags, words := partition(fs, args[1:])
	if err := fs.Parse(flags); err != nil {
		return err
	}

	verb, ids := "list", []string(nil)
	if len(words) > 0 {
		verb, ids = words[0], words[1:]
	}

	store := items.New(sharedDir(*repo, *dir))
	switch verb {
	case "list":
		list, err := selection(ctx, store, *repo, *branch, *everywhere)
		if err != nil {
			return err
		}
		if !*settled {
			list, _ = usecase.Surface(list, 0, contract.Severity(*minSeverity))
		}
		if *format == "jsonl" {
			body, err := usecase.ExportItemsJSONL(list)
			if err != nil {
				return err
			}
			_, err = stdout.Write(body)
			return err
		}
		return listFindings(stdout, list)
	case "dismiss", "accept":
		return ruleOn(stdout, store, verb, ids)
	default:
		return fmt.Errorf("unknown findings command %q — try list, accept or dismiss", verb)
	}
}

func listFindings(stdout io.Writer, list []contract.Item) error {
	if len(list) == 0 {
		fmt.Fprintln(stdout, "nothing outstanding")
		return nil
	}
	for _, item := range list {
		state := string(item.CurrentState())
		if item.Verdict != "" {
			state = string(item.Verdict)
		}
		fmt.Fprintf(stdout, "%s  %-8s %-9s %-28s %s\n",
			item.ID, state, item.Severity.Normalise(), item.Where(), item.Title)
	}
	return nil
}

// ruleOn records a verdict or an acceptance against ids the reviewer named.
//
// Writing the whole record back rather than a patch, because the store is
// append-only and the last line for an id is what that id is (ADR 0045).
func ruleOn(stdout io.Writer, store *items.Store, verb string, ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%s needs at least one item id", verb)
	}
	stored, err := store.All()
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}

	var ruled []contract.Item
	for _, item := range stored {
		if !wanted[item.ID] {
			continue
		}
		delete(wanted, item.ID)
		switch verb {
		case "dismiss":
			item.Verdict = contract.VerdictDismissed
		case "accept":
			item.Verdict = contract.VerdictConfirmed
			if item.CurrentState() == contract.StateOpen {
				item.State = contract.StateAccepted
			}
		}
		ruled = append(ruled, item)
	}
	if len(wanted) > 0 {
		return fmt.Errorf("no such item: %s", strings.Join(keys(wanted), ", "))
	}
	if err := store.Append(ruled...); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%sed %d item(s)\n", strings.TrimSuffix(verb, "s"), len(ruled))
	return nil
}

// runPush hands a selection to an implementation agent (ADR 0046).
//
//	msr push <id>...
//	msr push --batch --min-severity high
//	msr push --dry-run
func runPush(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository")
	dir := fs.String("dir", "", "shared directory holding the findings store (default <repo>/.mondspace)")
	branch := fs.String("branch", "", "which branch's findings (default: the one checked out)")
	batchMode := fs.Bool("batch", false, "push everything matching the filters rather than named ids")
	state := fs.String("state", "", "with --batch, only items in this state (open|accepted)")
	minSeverity := fs.String("min-severity", "", "with --batch, only findings at least this severe")
	to := fs.String("to", "file", "where to send it (file|stdout|clipboard)")
	batchID := fs.String("batch-id", "", "the batch id (default: a new one)")
	dryRun := fs.Bool("dry-run", false, "render the brief and send nothing")
	flags, ids := partition(fs, args[1:])
	if err := fs.Parse(flags); err != nil {
		return err
	}

	store := items.New(sharedDir(*repo, *dir))
	stored, err := store.All()
	if err != nil {
		return err
	}
	snap := gitsnap.New(*repo, "push")
	on := branchName(ctx, snap, *branch)

	chosen, err := chooseForPush(stored, on, *batchMode, ids, *state, contract.Severity(*minSeverity))
	if err != nil {
		return err
	}

	batch := *batchID
	if batch == "" {
		batch = "batch-" + newULID()
	}
	if handoff.AlreadySent(stored, batch) {
		fmt.Fprintf(stdout, "batch %s has already been sent — nothing to do\n", batch)
		return nil
	}

	brief := handoff.Assemble(batch, on, time.Now().UTC(), chosen)
	if brief.Empty() {
		fmt.Fprintln(stdout, "nothing to push")
		return nil
	}
	for _, clash := range brief.Conflicts {
		fmt.Fprintf(stdout, "warning: %s and %s touch the same lines in %s\n", clash.A.ID, clash.B.ID, clash.Path)
	}

	if *dryRun {
		_, err := io.WriteString(stdout, handoff.Render(brief))
		return err
	}

	adapter, err := delivery.Pick(*to, sharedDir(*repo, *dir), stdout)
	if err != nil {
		return err
	}
	// Delivered before anything is marked as sent: an item recorded as pushed
	// that never reached anybody is the one state nothing later can detect.
	if err := adapter.Send(brief); err != nil {
		return fmt.Errorf("delivering the brief: %w", err)
	}
	if err := store.Append(handoff.MarkPushed(brief, "human")...); err != nil {
		return err
	}
	// A human pushing by hand stands auto-mode down for the rest of the
	// session: two things steering one agent is worse than either (ADR 0047).
	suspendAuto(sharedDir(*repo, *dir), "")

	fmt.Fprintf(stdout, "pushed %d item(s) as %s", len(brief.Items), batch)
	if written, ok := adapter.(*delivery.File); ok {
		fmt.Fprintf(stdout, " → %s", written.Written)
	}
	fmt.Fprintln(stdout)
	return nil
}

// chooseForPush is either the ids somebody named or everything that matches
// the filters. Naming ids and asking for a batch at once is a contradiction
// worth refusing rather than guessing at.
func chooseForPush(stored []contract.Item, branch string, batchMode bool, ids []string,
	state string, floor contract.Severity) ([]contract.Item, error) {

	if batchMode && len(ids) > 0 {
		return nil, fmt.Errorf("--batch pushes what matches the filters; drop the ids or drop --batch")
	}
	if !batchMode && len(ids) == 0 {
		return nil, fmt.Errorf("push needs item ids, or --batch with filters")
	}

	if !batchMode {
		wanted := map[string]bool{}
		for _, id := range ids {
			wanted[id] = true
		}
		var chosen []contract.Item
		for _, item := range stored {
			if wanted[item.ID] {
				delete(wanted, item.ID)
				chosen = append(chosen, item)
			}
		}
		if len(wanted) > 0 {
			return nil, fmt.Errorf("no such item: %s", strings.Join(keys(wanted), ", "))
		}
		return chosen, nil
	}

	var chosen []contract.Item
	for _, item := range stored {
		if item.Branch != branch || !item.Pushable() {
			continue
		}
		if state != "" && string(item.CurrentState()) != state {
			continue
		}
		if floor != "" && !item.Severity.AtLeast(floor) {
			continue
		}
		chosen = append(chosen, item)
	}
	return chosen, nil
}

// selection is the items a read command is about.
func selection(ctx context.Context, store *items.Store, repo, branch string, everywhere bool) ([]contract.Item, error) {
	if everywhere {
		return store.All()
	}
	return store.OnBranch(branchName(ctx, gitsnap.New(repo, "findings"), branch))
}

// sharedDir is where the findings store lives: `.mondspace` beside the
// repository, unless somebody said otherwise (ADR 0045).
func sharedDir(repo, dir string) string {
	if dir != "" {
		return dir
	}
	return filepath.Join(repo, items.SharedDir)
}

// branchName is what to record against: what was asked for, or what is checked
// out, or nothing at all on a detached head.
func branchName(ctx context.Context, snap *gitsnap.Snapshotter, asked string) string {
	if asked != "" {
		return asked
	}
	return snap.CurrentBranch(ctx)
}

func branchLabel(branch string) string {
	if branch == "" {
		return "no branch (detached head)"
	}
	return branch
}

// fileBodies reads a file from the repository, once each, for the fingerprint
// window and the snippet.
func fileBodies(repo string) func(string) string {
	cache := map[string]string{}
	return func(path string) string {
		if body, known := cache[path]; known {
			return body
		}
		body, err := os.ReadFile(filepath.Join(repo, path))
		if err != nil {
			cache[path] = ""
			return ""
		}
		cache[path] = string(body)
		return cache[path]
	}
}

// ranProducers is which tools this pass was in a position to speak for.
//
// An analyser that is not installed did not fail to find anything; it did not
// look, and closing its findings because it was silent would be wrong
// (see usecase.Pass).
func ranProducers(scanner *local.Scanner) map[string]bool {
	out := map[string]bool{"msr": true}
	for _, status := range scanner.Report() {
		if status.Present {
			out[status.Name] = true
		}
	}
	return out
}

// outsideStore drops units that are only about msr's own files.
func outsideStore(units []domain.Unit, repo, store string) []domain.Unit {
	kept := units[:0]
	for _, unit := range units {
		if len(excludeStore(unit.Files, repo, filepath.Join(repo, store))) > 0 {
			kept = append(kept, unit)
		}
	}
	return kept
}

// partition separates flags from the words beside them, so that item ids can
// be given before, after or among the flags.
//
// `msr findings dismiss <id> --repo=x` is how somebody types it, and Go's flag
// package stops at the first word — which would silently treat every flag
// after the id as another id.
func partition(fs *flag.FlagSet, args []string) (flags, words []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return flags, append(words, args[i+1:]...)
		}
		if !strings.HasPrefix(arg, "-") {
			words = append(words, arg)
			continue
		}

		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") {
			continue
		}
		declared := fs.Lookup(name)
		if declared == nil {
			continue
		}
		if boolean, ok := declared.Value.(interface{ IsBoolFlag() bool }); ok && boolean.IsBoolFlag() {
			continue
		}
		// A flag that takes a value takes the next word with it, rather than
		// leaving it to be read as an id.
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return flags, words
}

func pathSet(paths []string) map[string]bool {
	out := make(map[string]bool, len(paths))
	for _, path := range paths {
		out[contract.NormalisePath(path)] = true
	}
	return out
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	return out
}

// isItemFormat says which half of `msr export` a format belongs to: the
// session's report, or the findings store.
func isItemFormat(format string) bool {
	switch format {
	case "agent", "plan", "markdown", "github-issues", "jsonl":
		return true
	default:
		return false
	}
}

// exportItems renders the findings store for whoever is consuming it.
func exportItems(ctx context.Context, format, repo, dir, branch, state string,
	everywhere bool, stdout io.Writer) error {

	store := items.New(sharedDir(repo, dir))
	list, err := selection(ctx, store, repo, branch, everywhere)
	if err != nil {
		return err
	}

	// What is settled is left out of every renderer here. These are handed to
	// something that will act on them, and a dismissed finding is one somebody
	// has already decided not to act on.
	var open []contract.Item
	for _, item := range list {
		if !item.Stands() {
			continue
		}
		if state != "" && string(item.CurrentState()) != state {
			continue
		}
		open = append(open, item)
	}

	switch format {
	case "agent":
		_, err := io.WriteString(stdout, usecase.ExportAgent(open))
		return err
	case "plan":
		_, err := io.WriteString(stdout, usecase.ExportPlan(open))
		return err
	case "markdown":
		_, err := io.WriteString(stdout, usecase.ExportItemsMarkdown(open))
		return err
	case "github-issues":
		body, err := usecase.ExportGitHubIssues(open)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, string(body))
		return err
	case "jsonl":
		body, err := usecase.ExportItemsJSONL(open)
		if err != nil {
			return err
		}
		_, err = stdout.Write(body)
		return err
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}
