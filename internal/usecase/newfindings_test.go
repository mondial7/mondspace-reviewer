package usecase_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

const patch = `diff --git a/api/handler.go b/api/handler.go
--- a/api/handler.go
+++ b/api/handler.go
@@ -10,6 +10,8 @@ func Routes() {
 	mux := http.NewServeMux()
 	mux.Handle("/a", a)
+	mux.Handle("/b", b)
+	token := rand.Int()
 	return mux
-	// gone
 }
`

func TestOnlyTheLinesAChangeAddedCount(t *testing.T) {
	got := usecase.AddedLines(domain.Diff{Text: patch})
	// The hunk starts at line 10: two context lines, then the two additions.
	for _, want := range []int{12, 13} {
		if !got[want] {
			t.Errorf("line %d was added and should count: %v", want, got)
		}
	}
	for _, unwanted := range []int{10, 11, 14} {
		if got[unwanted] {
			t.Errorf("line %d is context, not an addition: %v", unwanted, got)
		}
	}
	if len(got) != 2 {
		t.Errorf("got %d added lines, want 2: %v", len(got), got)
	}
}

func TestAnAddedLineKeepsItsTextToAnchorTo(t *testing.T) {
	got := usecase.AddedLineText(domain.Diff{Text: patch})
	if got[13] != "+	token := rand.Int()" {
		t.Errorf("line 13 = %q", got[13])
	}
}

func scannedReview() ([]domain.Unit, map[string]domain.Diff) {
	units := []domain.Unit{{ID: "u1", Files: []string{"api/handler.go"}}}
	return units, map[string]domain.Diff{"u1": {Text: patch}}
}

func TestAFindingOnALineThisChangeAddedIsNew(t *testing.T) {
	units, diffs := scannedReview()
	got := usecase.MarkNew([]domain.Reported{
		{Tool: "gosec", Rule: "G404", File: "api/handler.go", Line: 13, Message: "weak rng"},
		{Tool: "gosec", Rule: "G401", File: "api/handler.go", Line: 11, Message: "already there"},
		{Tool: "gosec", Rule: "G402", File: "other/thing.go", Line: 3, Message: "not in this review"},
	}, units, diffs)

	if !got[0].New {
		t.Error("a finding on a line this change added is new")
	}
	if got[0].Anchor != "+	token := rand.Int()" {
		t.Errorf("a new finding should anchor to its line, got %q", got[0].Anchor)
	}
	if got[1].New {
		t.Error("a finding on a context line was already there")
	}
	if got[2].New {
		t.Error("a finding on a file this review does not touch is not this review's")
	}
}

func TestAWholeFileFindingIsAsNewAsTheFile(t *testing.T) {
	// A leaked credential or a vulnerable dependency has no line to intersect;
	// the file being in this change is the whole of the question.
	units, diffs := scannedReview()
	got := usecase.MarkNew([]domain.Reported{
		{Tool: "gitleaks", Rule: "generic-api-key", File: "api/handler.go", Message: "key"},
	}, units, diffs)
	if !got[0].New {
		t.Error("a whole-file finding on a changed file is new")
	}
}

func TestPreExistingFindingsAreSeparatedNotDiscarded(t *testing.T) {
	// Silently hiding four hundred findings and silently having none look the
	// same from the page, and one of them means the tool is not running.
	fresh, standing := usecase.SplitNew([]domain.Reported{
		{Tool: "t", Rule: "a", File: "x.go", New: true},
		{Tool: "t", Rule: "b", File: "x.go"},
		{Tool: "t", Rule: "c", File: "x.go"},
	})
	if len(fresh) != 1 || len(standing) != 2 {
		t.Errorf("got %d new and %d pre-existing, want 1 and 2", len(fresh), len(standing))
	}
}

func TestADismissalSurvivesTheNextRunOfTheSameTool(t *testing.T) {
	// A deterministic tool is *more* likely to raise the same thing again, not
	// less, so without this a dismissal lasts until the next poll tick.
	earlier := []domain.Reported{{
		Tool: "gosec", Rule: "G404", File: "api/handler.go", Line: 13,
		Message: "weak rng", Verdict: domain.VerdictDismissed,
	}}
	// Same finding, moved down the file because something was added above it.
	fresh := []domain.Reported{{
		Tool: "gosec", Rule: "G404", File: "api/handler.go", Line: 40,
		Message: "weak rng",
	}}

	got := usecase.CarryDismissals(fresh, earlier)
	if got[0].Stands() {
		t.Error("the dismissal did not survive the line moving")
	}
}
