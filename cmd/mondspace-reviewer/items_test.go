package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/items"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// A repository with one finding in it: an error assigned and thrown away on a
// line this change added, which is one of msr's own rules and therefore fires
// on any machine, with or without a linter installed.
func repoWithAFinding(t *testing.T) (repo, shared string) {
	t.Helper()
	repo, shared = t.TempDir(), t.TempDir()

	runGit(t, repo, "init", "-q")
	// The exported declaration is written on its own lines and never touched
	// again, so the only thing the change does is swallow an error.
	write(t, repo, "a.go", "package a\n\nfunc F() {\n}\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "init")
	runGit(t, repo, "tag", "start")

	write(t, repo, "a.go", "package a\n\nimport \"os\"\n\nfunc F() {\n\t_ = os.Remove(\"x\")\n}\n")
	return repo, shared
}

func msr(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := run(context.Background(), args, nil, &out); err != nil {
		t.Fatalf("msr %s: %v\n%s", strings.Join(args, " "), err, out.String())
	}
	return out.String()
}

// The first id in a `msr findings` listing, which is the first column.
func firstID(t *testing.T, listing string) string {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] != "nothing" {
			return fields[0]
		}
	}
	t.Fatalf("no item id in:\n%s", listing)
	return ""
}

func TestScanStoresWhatItFound(t *testing.T) {
	repo, shared := repoWithAFinding(t)

	got := msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)

	if !strings.Contains(got, "a.go") {
		t.Errorf("the scan found nothing in a.go:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(shared, "findings.jsonl")); err != nil {
		t.Errorf("nothing was written to the store: %v", err)
	}
}

// Running it twice over an unchanged tree produces nothing the second time.
func TestScanTwiceWritesNothingTheSecondTime(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)

	body, err := os.ReadFile(filepath.Join(shared, "findings.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	got := msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)

	if !strings.HasPrefix(got, "0 raised or updated, 0 closed") {
		t.Errorf("the second scan reported %q, want nothing raised or closed", strings.SplitN(got, "\n", 2)[0])
	}
	after, err := os.ReadFile(filepath.Join(shared, "findings.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(body) {
		t.Errorf("the store grew by %d bytes on a scan that found nothing new", len(after)-len(body))
	}
}

// A dismissed finding is never raised again.
func TestDismissedFindingsStayDismissed(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	id := firstID(t, msr(t, "findings", "--repo="+repo, "--dir="+shared))

	msr(t, "findings", "dismiss", id, "--repo="+repo, "--dir="+shared)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)

	if got := msr(t, "findings", "--repo="+repo, "--dir="+shared); !strings.Contains(got, "nothing outstanding") {
		t.Errorf("a dismissed finding came back:\n%s", got)
	}
}

// A finding whose code has gone is closed, and nobody had to say so.
func TestAFixedFindingCloses(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)

	// Fixed without changing the signature: an exported declaration that moved
	// would raise a finding of its own and this test is about the first one
	// closing.
	write(t, repo, "a.go", "package a\n\nimport (\n\t\"log\"\n\t\"os\"\n)\n\nfunc F() {\n\tif err := os.Remove(\"x\"); err != nil {\n\t\tlog.Print(err)\n\t}\n}\n")
	got := msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)

	if !strings.Contains(got, "1 closed") {
		t.Errorf("the finding was not closed after the code changed:\n%s", got)
	}
	if listing := msr(t, "findings", "--repo="+repo, "--dir="+shared); !strings.Contains(listing, "nothing outstanding") {
		t.Errorf("a fixed finding is still outstanding:\n%s", listing)
	}
}

func TestPushWritesABriefAndMarksTheItems(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	id := firstID(t, msr(t, "findings", "--repo="+repo, "--dir="+shared))

	got := msr(t, "push", id, "--repo="+repo, "--dir="+shared)

	if !strings.Contains(got, "pushed 1 item(s)") {
		t.Fatalf("push said %q", got)
	}
	written := filepath.Join(shared, "handoff")
	entries, err := os.ReadDir(written)
	if err != nil || len(entries) != 1 {
		t.Fatalf("the brief was not written to %s: %v", written, err)
	}
	brief, err := os.ReadFile(filepath.Join(written, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(brief), "a.go") {
		t.Errorf("the brief does not name the file:\n%s", brief)
	}

	// It is in flight, so it does not go out again.
	if listing := msr(t, "findings", "--repo="+repo, "--dir="+shared); !strings.Contains(listing, "pushed") {
		t.Errorf("the item is not recorded as pushed:\n%s", listing)
	}
}

// Re-running a push with the same batch id sends nothing.
func TestPushIsIdempotentPerBatch(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	id := firstID(t, msr(t, "findings", "--repo="+repo, "--dir="+shared))
	msr(t, "push", id, "--repo="+repo, "--dir="+shared, "--batch-id=batch-1")

	got := msr(t, "push", id, "--repo="+repo, "--dir="+shared, "--batch-id=batch-1")

	if !strings.Contains(got, "already been sent") {
		t.Errorf("a repeated batch was sent again: %q", got)
	}
}

// A batch that was pushed and not fixed is re-raised rather than silently
// closed: it is still standing on the next scan.
func TestAPushedItemThatWasNotFixedComesBack(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	id := firstID(t, msr(t, "findings", "--repo="+repo, "--dir="+shared))
	msr(t, "push", id, "--repo="+repo, "--dir="+shared)

	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)

	listing := msr(t, "findings", "--repo="+repo, "--dir="+shared)
	if !strings.Contains(listing, id) {
		t.Errorf("the pushed item is gone from the listing:\n%s", listing)
	}
}

func TestPushDryRunSendsNothing(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	id := firstID(t, msr(t, "findings", "--repo="+repo, "--dir="+shared))

	got := msr(t, "push", id, "--repo="+repo, "--dir="+shared, "--dry-run")

	if !strings.Contains(got, "Review brief") {
		t.Errorf("--dry-run printed no brief:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(shared, "handoff")); !os.IsNotExist(err) {
		t.Error("--dry-run wrote a brief to disk")
	}
	if listing := msr(t, "findings", "--repo="+repo, "--dir="+shared); strings.Contains(listing, "pushed") {
		t.Errorf("--dry-run marked something as pushed:\n%s", listing)
	}
}

// A dismissed item can never be pushed.
func TestPushRefusesADismissedItem(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	id := firstID(t, msr(t, "findings", "--repo="+repo, "--dir="+shared))
	msr(t, "findings", "dismiss", id, "--repo="+repo, "--dir="+shared)

	got := msr(t, "push", id, "--repo="+repo, "--dir="+shared)

	if !strings.Contains(got, "nothing to push") {
		t.Errorf("push said %q, want it to refuse", got)
	}
}

func TestExportRendersTheStore(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)

	for _, format := range []string{"agent", "plan", "markdown", "github-issues", "jsonl"} {
		got := msr(t, "export", "--format="+format, "--repo="+repo, "--dir="+shared)
		if !strings.Contains(got, "a.go") {
			t.Errorf("--format=%s does not mention the file:\n%s", format, got)
		}
	}
}

// The findings-store formats leave out what has been settled, because they are
// handed to something that will act on them.
func TestExportLeavesOutWhatIsSettled(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	id := firstID(t, msr(t, "findings", "--repo="+repo, "--dir="+shared))
	msr(t, "findings", "dismiss", id, "--repo="+repo, "--dir="+shared)

	got := msr(t, "export", "--format=agent", "--repo="+repo, "--dir="+shared)

	if !strings.Contains(got, "Nothing outstanding") {
		t.Errorf("a dismissed finding was exported to an agent:\n%s", got)
	}
}

func TestFindingsRefusesAnUnknownId(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)

	var out bytes.Buffer
	err := run(context.Background(), []string{"findings", "dismiss", "nope", "--repo=" + repo, "--dir=" + shared}, nil, &out)

	if err == nil {
		t.Error("dismissing an id that does not exist was accepted")
	}
}

// A finding raised in a live review is matched — not duplicated — by the
// post-mortem pass that comes later. The live pass is attributed to a session
// and the later one is not, and that difference must not make it a new finding.
func TestALiveFindingIsMatchedByALaterPass(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	store := storeAt(t, shared)
	found := []contract.Item{contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "a.go", StartLine: 6, EndLine: 6}, Producer: "gosec", RuleID: "G404", Message: "weak random", Severity: domain.SeverityHigh, New: true}}
	live := sighting{branch: "main", session: "sess-1", repo: repo,
		producers: map[string]bool{"gosec": true}, paths: []string{"a.go"}}

	if _, err := recordFindings(store, found, live); err != nil {
		t.Fatal(err)
	}
	first, err := store.All()
	if err != nil {
		t.Fatal(err)
	}

	after := live
	after.session = ""
	changed, err := recordFindings(store, found, after)
	if err != nil {
		t.Fatal(err)
	}

	if len(changed) != 0 {
		t.Errorf("the later pass rewrote %d record(s) for a finding it had already seen", len(changed))
	}
	all, err := store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(first) {
		t.Errorf("the store holds %d records for one finding seen twice, want %d", len(all), len(first))
	}
}

// A reviewer's note becomes an item; the reviewer thinking aloud does not.
func TestOnlyNotesThatAreWorkReachTheStore(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	store := storeAt(t, shared)

	for _, note := range []contract.Item{
		contract.Item{Source: contract.SourceHuman, Location: contract.Location{Path: "a.go"}, ID: "n1", Kind: contract.KindObjection, Message: "this swallows the error"},
		contract.Item{Source: contract.SourceHuman, Location: contract.Location{Path: "a.go"}, ID: "n2", Kind: contract.KindOK, Message: "fine"},
	} {
		if err := recordNote(store, note, "main", repo); err != nil {
			t.Fatal(err)
		}
	}

	all, err := store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != "n1" {
		t.Errorf("the store holds %+v, want only the objection", all)
	}
}

func storeAt(t *testing.T, dir string) *items.Store {
	t.Helper()
	return items.New(dir)
}

// Promotion is the planner's job; --promote is the escape hatch for a
// repository that has no planner, and it must not duplicate on a second run.
func TestPromoteWritesTheBacklogOnce(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)

	first := msr(t, "export", "--promote", "--repo="+repo, "--dir="+shared)
	if !strings.Contains(first, "promoted 1 item(s)") {
		t.Fatalf("promote said %q", first)
	}
	if _, err := os.Stat(filepath.Join(shared, "items.jsonl")); err != nil {
		t.Fatalf("no backlog file: %v", err)
	}

	if again := msr(t, "export", "--promote", "--repo="+repo, "--dir="+shared); !strings.Contains(again, "promoted 0 item(s)") {
		t.Errorf("a second promotion said %q, want nothing new", again)
	}
}

// A finding about code this change never touched is stored, counted, and kept
// out of the way. Listing it as work is what ADR 0043 exists to prevent, and
// handing it to an agent is worse: it cannot tell whose work it is.
func TestPreExistingFindingsAreCountedNotListed(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	store := storeAt(t, shared)

	analysed := func(path string, line int, caused bool) contract.Item {
		return contract.Item{
			Source: contract.SourceAnalyser, Producer: "go vet", RuleID: "printf",
			Location: contract.Location{Path: path, StartLine: line, EndLine: line},
			Message:  "wrong verb", Severity: contract.SeverityMedium, New: caused,
		}
	}
	if _, err := recordFindings(store, []contract.Item{
		analysed("untouched.go", 7, false), analysed("a.go", 6, true),
	}, sighting{
		branch: "main", repo: repo,
		producers: map[string]bool{"go vet": true},
		paths:     []string{"untouched.go", "a.go"},
	}); err != nil {
		t.Fatal(err)
	}

	listing := msr(t, "findings", "--repo="+repo, "--dir="+shared)

	if strings.Contains(listing, "untouched.go") {
		t.Errorf("a finding from before this change was listed as work:\n%s", listing)
	}
	if !strings.Contains(listing, "a.go") {
		t.Errorf("the finding this change caused is missing:\n%s", listing)
	}
	if !strings.Contains(listing, "already there") {
		t.Errorf("nothing said the other one exists:\n%s", listing)
	}

	// And it is still there for anybody who asks.
	if shown := msr(t, "findings", "--repo="+repo, "--dir="+shared, "--pre-existing"); !strings.Contains(shown, "untouched.go") {
		t.Errorf("--pre-existing does not show it:\n%s", shown)
	}
}

// The store is append-only, so a finding that was raised, accepted and pushed
// is three lines that resolve to one. Housekeeping belongs in the command whose
// job is housekeeping.
func TestGCFoldsTheFindingsFile(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	id := firstID(t, msr(t, "findings", "--repo="+repo, "--dir="+shared))
	msr(t, "findings", "accept", id, "--repo="+repo, "--dir="+shared)
	msr(t, "push", id, "--repo="+repo, "--dir="+shared)

	before := lines(t, filepath.Join(shared, "findings.jsonl"))
	if before < 3 {
		t.Fatalf("the store holds %d line(s); this test needs churn to fold", before)
	}

	// It says what it would do before it does it.
	if dry := msr(t, "gc", "--repo="+repo, "--dir="+shared, "--dry-run"); !strings.Contains(dry, "would fold") {
		t.Errorf("--dry-run said %q", dry)
	}
	if lines(t, filepath.Join(shared, "findings.jsonl")) != before {
		t.Error("--dry-run rewrote the file")
	}

	msr(t, "gc", "--repo="+repo, "--dir="+shared)

	if after := lines(t, filepath.Join(shared, "findings.jsonl")); after != 1 {
		t.Errorf("the store holds %d line(s) after folding, want 1", after)
	}
	// And the item is what it was: everything decided about it survived.
	listing := msr(t, "findings", "--repo="+repo, "--dir="+shared, "--settled")
	if !strings.Contains(listing, id) || !strings.Contains(listing, "pushed") ||
		!strings.Contains(listing, "confirmed") {
		t.Errorf("folding lost what was decided:\n%s", listing)
	}
}

func lines(t *testing.T, path string) int {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(strings.TrimSpace(string(body)), "\n") + 1
}

// What happened to an item and what somebody thought of it are different
// questions (ADR 0044). Printing whichever was written last hid the useful
// half: an item accepted and then pushed read as "confirmed", with nothing to
// say it had already gone to an agent.
func TestTheListingShowsStateAndVerdict(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	id := firstID(t, msr(t, "findings", "--repo="+repo, "--dir="+shared))
	msr(t, "findings", "accept", id, "--repo="+repo, "--dir="+shared)
	msr(t, "push", id, "--repo="+repo, "--dir="+shared)

	listing := msr(t, "findings", "--repo="+repo, "--dir="+shared)

	if !strings.Contains(listing, "pushed") {
		t.Errorf("the listing does not say it is in flight:\n%s", listing)
	}
	if !strings.Contains(listing, "confirmed") {
		t.Errorf("the listing does not say what was decided:\n%s", listing)
	}
}

// `--format=jsonl` is the store itself, for something else to read: it gets
// every item and the flag to sort them out. Every other format is somebody
// being handed work.
func TestJSONLExportKeepsWhatWasAlreadyThere(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	store := storeAt(t, shared)
	old := contract.Item{
		Source: contract.SourceAnalyser, Producer: "go vet", RuleID: "printf",
		Location: contract.Location{Path: "untouched.go", StartLine: 7, EndLine: 7},
		Message:  "wrong verb", Severity: contract.SeverityMedium, New: false,
	}
	if _, err := recordFindings(store, []contract.Item{old}, sighting{
		branch: "main", repo: repo,
		producers: map[string]bool{"go vet": true}, paths: []string{"untouched.go"},
	}); err != nil {
		t.Fatal(err)
	}

	raw := msr(t, "export", "--format=jsonl", "--repo="+repo, "--dir="+shared)
	if !strings.Contains(raw, "untouched.go") {
		t.Errorf("the raw export dropped a finding somebody else may want:\n%s", raw)
	}

	agent := msr(t, "export", "--format=agent", "--repo="+repo, "--dir="+shared)
	if strings.Contains(agent, "untouched.go") {
		t.Errorf("the agent brief carries work this change did not cause:\n%s", agent)
	}
}
