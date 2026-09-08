package handoff_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/usecase/handoff"
)

var at = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func item(id, path string, line int) contract.Item {
	return contract.Item{
		ID:        id,
		Source:    contract.SourceAnalyser,
		Producer:  "gosec",
		RuleID:    "G404",
		Location:  contract.Location{Path: path, StartLine: line, EndLine: line},
		Severity:  contract.SeverityHigh,
		Title:     "weak random",
		Directive: "use crypto/rand",
		Snippet:   "token := rand.Int()",
	}
}

// An agent working down one file at a time re-reads less than one bouncing
// between four.
func TestAssembleOrdersByFileThenLine(t *testing.T) {
	brief := handoff.Assemble("batch-1", "main", at, []contract.Item{
		item("c", "b.go", 10),
		item("a", "a.go", 40),
		item("b", "a.go", 5),
	})

	var got []string
	for _, item := range brief.Items {
		got = append(got, item.ID)
	}
	if want := "b a c"; strings.Join(got, " ") != want {
		t.Errorf("order = %v, want %s", got, want)
	}
}

// A selection with a dismissed finding in it sends the rest rather than
// failing, and never sends the dismissed one.
func TestAssembleDropsWhatMayNotBePushed(t *testing.T) {
	dismissed := item("a", "a.go", 1)
	dismissed.Verdict = contract.VerdictDismissed
	inFlight := item("b", "a.go", 2)
	inFlight.State = contract.StatePushed

	brief := handoff.Assemble("batch-1", "main", at, []contract.Item{dismissed, inFlight, item("c", "a.go", 3)})

	if len(brief.Items) != 1 || brief.Items[0].ID != "c" {
		t.Errorf("brief = %+v, want only the pushable item", brief.Items)
	}
}

func TestAssembleFlagsOverlappingDirectives(t *testing.T) {
	wide := item("a", "a.go", 10)
	wide.Location.EndLine = 20
	inside := item("b", "a.go", 15)

	brief := handoff.Assemble("batch-1", "main", at, []contract.Item{wide, inside})

	if len(brief.Conflicts) != 1 {
		t.Fatalf("found %d conflicts, want 1", len(brief.Conflicts))
	}
	if brief.Conflicts[0].A.ID != "a" || brief.Conflicts[0].B.ID != "b" {
		t.Errorf("conflict = %+v", brief.Conflicts[0])
	}
}

// Two findings in one file that are not about the same lines are not a
// conflict, and neither is a file-wide item — flagging those would make the
// warning constant and therefore meaningless.
func TestAssembleDoesNotFlagWhatDoesNotOverlap(t *testing.T) {
	fileWide := item("c", "a.go", 0)

	brief := handoff.Assemble("batch-1", "main", at, []contract.Item{
		item("a", "a.go", 10), item("b", "a.go", 40), fileWide, item("d", "b.go", 10),
	})

	if len(brief.Conflicts) != 0 {
		t.Errorf("flagged %+v, want nothing", brief.Conflicts)
	}
}

func TestMarkPushed(t *testing.T) {
	brief := handoff.Assemble("batch-7", "main", at, []contract.Item{item("a", "a.go", 1)})

	got := handoff.MarkPushed(brief, "human")

	if len(got) != 1 {
		t.Fatalf("marked %d items, want 1", len(got))
	}
	if got[0].CurrentState() != contract.StatePushed {
		t.Errorf("state = %q", got[0].CurrentState())
	}
	if got[0].PushBatch != "batch-7" || got[0].PushedBy != "human" {
		t.Errorf("batch = %q, by = %q", got[0].PushBatch, got[0].PushedBy)
	}
	if got[0].PushedAt == nil || !got[0].PushedAt.Equal(at) {
		t.Errorf("pushed at %v, want %v", got[0].PushedAt, at)
	}
	// Handing it over is not evidence that anything was done about it.
	if !got[0].Stands() {
		t.Error("a pushed item stopped standing before anything verified it")
	}
	// And it cannot go out twice.
	if got[0].Pushable() {
		t.Error("a pushed item is still pushable")
	}
}

// The retry is usually a second command after the first failed halfway, so
// idempotence is checked against the store rather than remembered in a process.
func TestAlreadySent(t *testing.T) {
	sent := item("a", "a.go", 1)
	sent.PushBatch = "batch-7"

	if !handoff.AlreadySent([]contract.Item{sent}, "batch-7") {
		t.Error("a batch already in the store was not recognised")
	}
	if handoff.AlreadySent([]contract.Item{sent}, "batch-8") {
		t.Error("a batch nobody has sent was called sent")
	}
}

func TestRenderCarriesEnoughToActOn(t *testing.T) {
	brief := handoff.Assemble("batch-1", "main", at, []contract.Item{item("a", "internal/a.go", 12)})

	got := handoff.Render(brief)

	for _, want := range []string{"batch-1", "internal/a.go", "internal/a.go:12", "use crypto/rand", "token := rand.Int()", "`a`", "gosec G404"} {
		if !strings.Contains(got, want) {
			t.Errorf("the brief does not mention %q:\n%s", want, got)
		}
	}
}

func TestRenderWarnsAboutOverlaps(t *testing.T) {
	wide := item("a", "a.go", 10)
	wide.Location.EndLine = 20

	got := handoff.Render(handoff.Assemble("batch-1", "main", at, []contract.Item{wide, item("b", "a.go", 15)}))

	if !strings.Contains(got, "Overlapping directives") {
		t.Errorf("the brief does not warn about the overlap:\n%s", got)
	}
}

func TestEmptyBrief(t *testing.T) {
	if !handoff.Assemble("batch-1", "main", at, nil).Empty() {
		t.Error("a brief with nothing in it did not report itself empty")
	}
}
