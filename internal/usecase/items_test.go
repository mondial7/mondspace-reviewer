package usecase_test

import (
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func sighting(fingerprint, id string) contract.Item {
	return contract.Item{
		ID:          id,
		Fingerprint: fingerprint,
		Source:      contract.SourceAnalyser,
		Producer:    "gosec",
		RuleID:      "G404",
		Location:    contract.Location{Path: "internal/a.go", StartLine: 12, EndLine: 12},
		Severity:    contract.SeverityHigh,
		Title:       "weak random",
		Branch:      "main",
	}
}

func pass(at time.Time) usecase.Pass {
	return usecase.Pass{At: at, Producers: map[string]bool{"gosec": true}}
}

var (
	first  = time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	second = first.Add(time.Hour)
)

// The first acceptance criterion: run it twice over an unchanged tree and the
// second run has nothing to say.
func TestReconcileSecondRunOfAnUnchangedTreeWritesNothing(t *testing.T) {
	stored := usecase.Reconcile(nil, []contract.Item{sighting("fp1", "id1")}, pass(first))
	if len(stored) != 1 {
		t.Fatalf("first run wrote %d items, want 1", len(stored))
	}

	// A second run mints a new id for the same finding, as a producer that
	// cannot know what is already stored must.
	again := usecase.Reconcile(stored, []contract.Item{sighting("fp1", "id2")}, pass(second))

	if len(again) != 0 {
		t.Errorf("second run over an unchanged tree wrote %d items, want none: %+v", len(again), again)
	}
}

// The same finding raised live and then found again by a later pass is one
// record, and it keeps the id the first pass gave it.
func TestReconcileKeepsTheOriginalIdentity(t *testing.T) {
	stored := usecase.Reconcile(nil, []contract.Item{sighting("fp1", "id1")}, pass(first))

	moved := sighting("fp1", "id2")
	moved.Location.StartLine = 40
	moved.Location.EndLine = 40
	got := usecase.Reconcile(stored, []contract.Item{moved}, pass(second))

	if len(got) != 1 {
		t.Fatalf("wrote %d items, want 1", len(got))
	}
	if got[0].ID != "id1" {
		t.Errorf("id = %q, want the id the finding was first raised under", got[0].ID)
	}
	if got[0].FirstSeen != first {
		t.Errorf("FirstSeen = %v, want %v", got[0].FirstSeen, first)
	}
	if got[0].LastSeen != second {
		t.Errorf("LastSeen = %v, want %v", got[0].LastSeen, second)
	}
	if got[0].Location.StartLine != 40 {
		t.Errorf("line = %d, want the line the code is on now", got[0].Location.StartLine)
	}
}

// A dismissed finding is never re-raised on any later run — including one by a
// different tool run, days later, on a fresh session.
func TestReconcileNeverReRaisesADismissal(t *testing.T) {
	dismissed := sighting("fp1", "id1")
	dismissed.Verdict = contract.VerdictDismissed
	dismissed.State = contract.StateOpen

	got := usecase.Reconcile([]contract.Item{dismissed}, []contract.Item{sighting("fp1", "id2")}, pass(second))

	for _, item := range got {
		if item.Stands() {
			t.Errorf("a dismissed fingerprint came back standing: %+v", item)
		}
	}
}

// The same fingerprint on a branch nobody has dismissed it on is still
// dismissed. Suppression follows the finding, not the branch.
func TestReconcileDismissalCrossesBranches(t *testing.T) {
	dismissed := sighting("fp1", "id1")
	dismissed.Verdict = contract.VerdictDismissed

	elsewhere := sighting("fp1", "id2")
	elsewhere.Branch = "feature/x"

	got := usecase.Reconcile([]contract.Item{dismissed}, []contract.Item{elsewhere}, pass(second))

	if len(got) != 1 {
		t.Fatalf("wrote %d items, want 1", len(got))
	}
	if got[0].Verdict != contract.VerdictDismissed {
		t.Errorf("verdict on the other branch = %q, want it born dismissed", got[0].Verdict)
	}
	if got[0].ID != "id2" {
		t.Errorf("id = %q, want a record of its own: one branch may be fixed while the other is not", got[0].ID)
	}
}

// A finding whose code has gone is fixed, and nobody had to say so.
func TestReconcileClosesWhatIsGone(t *testing.T) {
	stored := usecase.Reconcile(nil, []contract.Item{sighting("fp1", "id1")}, pass(first))

	got := usecase.Reconcile(stored, nil, pass(second))

	if len(got) != 1 {
		t.Fatalf("wrote %d items, want 1", len(got))
	}
	if got[0].CurrentState() != contract.StateFixed {
		t.Errorf("state = %q, want %q", got[0].CurrentState(), contract.StateFixed)
	}
	if got[0].FixedAt == nil || !got[0].FixedAt.Equal(second) {
		t.Errorf("FixedAt = %v, want %v", got[0].FixedAt, second)
	}
}

// "It is gone" and "nobody looked" are the same absence, and only one of them
// is a fix.
func TestReconcileDoesNotCloseWhatThisPassCouldNotSee(t *testing.T) {
	stored := usecase.Reconcile(nil, []contract.Item{sighting("fp1", "id1")}, pass(first))

	t.Run("a tool that did not run", func(t *testing.T) {
		other := usecase.Pass{At: second, Producers: map[string]bool{"gitleaks": true}}
		if got := usecase.Reconcile(stored, nil, other); len(got) != 0 {
			t.Errorf("closed %d items on a pass that did not run gosec: %+v", len(got), got)
		}
	})

	t.Run("a file that was not looked at", func(t *testing.T) {
		elsewhere := usecase.Pass{
			At:        second,
			Producers: map[string]bool{"gosec": true},
			Paths:     map[string]bool{"internal/b.go": true},
		}
		if got := usecase.Reconcile(stored, nil, elsewhere); len(got) != 0 {
			t.Errorf("closed %d items in a file the pass never opened: %+v", len(got), got)
		}
	})
}

// A batch that was pushed and did not get fixed comes back, rather than being
// quietly treated as dealt with because somebody sent it somewhere.
func TestReconcileReRaisesAPushedItemThatIsStillThere(t *testing.T) {
	pushed := sighting("fp1", "id1")
	pushed.State = contract.StatePushed
	at := first
	pushed.PushedAt = &at
	pushed.PushBatch = "batch-1"

	got := usecase.Reconcile([]contract.Item{pushed}, []contract.Item{sighting("fp1", "id2")}, pass(second))

	if len(got) != 0 {
		t.Fatalf("wrote %d items for an unchanged pushed finding, want none", len(got))
	}
	if !pushed.Stands() {
		t.Error("a pushed finding stopped standing before anything verified it")
	}
}

// A fixed finding that comes back is the same finding, not a new one.
func TestReconcileReopensAFixedFinding(t *testing.T) {
	fixed := sighting("fp1", "id1")
	fixed.State = contract.StateFixed
	at := first
	fixed.FixedAt = &at

	got := usecase.Reconcile([]contract.Item{fixed}, []contract.Item{sighting("fp1", "id2")}, pass(second))

	if len(got) != 1 {
		t.Fatalf("wrote %d items, want 1", len(got))
	}
	if got[0].CurrentState() != contract.StateOpen {
		t.Errorf("state = %q, want it open again", got[0].CurrentState())
	}
	if got[0].FixedAt != nil {
		t.Error("FixedAt survived the finding coming back")
	}
	if got[0].ID != "id1" {
		t.Errorf("id = %q, want its history rather than a fresh record", got[0].ID)
	}
}

// A directive somebody wrote by hand is the most valuable thing in the review.
func TestReconcileKeepsAHandWrittenDirective(t *testing.T) {
	stored := sighting("fp1", "id1")
	stored.Directive = "use crypto/rand here, the token is a session id"

	sighted := sighting("fp1", "id2")
	sighted.Directive = "replace math/rand"
	sighted.Location.StartLine = 13

	got := usecase.Reconcile([]contract.Item{stored}, []contract.Item{sighted}, pass(second))

	if len(got) != 1 {
		t.Fatalf("wrote %d items, want 1", len(got))
	}
	if got[0].Directive != stored.Directive {
		t.Errorf("directive = %q, want the one the reviewer wrote", got[0].Directive)
	}
}

// One tool reporting a fingerprint twice in one pass is one finding.
func TestReconcileDeduplicatesWithinAPass(t *testing.T) {
	got := usecase.Reconcile(nil, []contract.Item{sighting("fp1", "id1"), sighting("fp1", "id2")}, pass(first))

	if len(got) != 1 {
		t.Errorf("wrote %d items for one fingerprint seen twice, want 1", len(got))
	}
}
