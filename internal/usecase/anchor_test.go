package usecase_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

const anchorDiff = "@@ -1,4 +1,6 @@\n package auth\n\n-func Valid(t string) bool {\n+func Valid(t, scope string) bool {\n+\treturn t != \"\" && scope != \"\"\n }\n"

func TestANoteWithoutAnAnchorStaysAboutTheWholeFile(t *testing.T) {
	// The existing kind of note. Line-level is an addition, not a replacement:
	// plenty of what a reviewer says is about the file, not a line.
	notes := []domain.Note{{ID: "n1", UnitID: "u1", Text: "this file needs tests"}}

	lines, orphaned := usecase.AnchorNotes(domain.Diff{Text: anchorDiff}, notes)

	for i, l := range lines {
		if len(l.Notes) != 0 {
			t.Errorf("line %d picked up a file-level note", i)
		}
	}
	if len(orphaned) != 0 {
		t.Errorf("orphaned = %+v, want none — it was never anchored", orphaned)
	}
}

func TestANoteAnchorsToTheLineItWasWrittenOn(t *testing.T) {
	notes := []domain.Note{
		{ID: "n1", UnitID: "u1", Anchor: "+func Valid(t, scope string) bool {",
			Text: "this breaks every caller"},
	}

	lines, orphaned := usecase.AnchorNotes(domain.Diff{Text: anchorDiff}, notes)

	if len(orphaned) != 0 {
		t.Fatalf("orphaned = %+v, want it placed", orphaned)
	}
	var found int
	for _, l := range lines {
		if len(l.Notes) == 0 {
			continue
		}
		found++
		if l.Text != "+func Valid(t, scope string) bool {" {
			t.Errorf("the note landed on %q", l.Text)
		}
	}
	if found != 1 {
		t.Errorf("the note appears on %d lines, want 1", found)
	}
}

func TestANoteSurvivesTheLineMovingUpOrDown(t *testing.T) {
	// The whole reason for anchoring to content rather than to a line number:
	// a diff grows above the line you commented on constantly.
	notes := []domain.Note{
		{ID: "n1", Anchor: "+\treturn t != \"\" && scope != \"\"", Text: "no length check"},
	}
	moved := "@@ -1,9 +1,12 @@\n package auth\n\n+import \"strings\"\n+\n // Valid says whether a token is usable.\n-func Valid(t string) bool {\n+func Valid(t, scope string) bool {\n+\treturn t != \"\" && scope != \"\"\n }\n"

	lines, orphaned := usecase.AnchorNotes(domain.Diff{Text: moved}, notes)

	if len(orphaned) != 0 {
		t.Fatalf("orphaned = %+v, want it found further down", orphaned)
	}
	for _, l := range lines {
		if len(l.Notes) == 1 && l.Text != "+\treturn t != \"\" && scope != \"\"" {
			t.Errorf("the note landed on %q", l.Text)
		}
	}
}

func TestANoteOnALineThatIsGoneIsReportedNotDropped(t *testing.T) {
	// A judgement about code that no longer exists must not be silently
	// discarded, and must not be silently shown as though it still applies —
	// the same discipline as a stale sign-off (ADR 0021).
	notes := []domain.Note{
		{ID: "n1", Anchor: "+\tpanic(\"unreachable\")", Text: "why panic here?"},
	}

	lines, orphaned := usecase.AnchorNotes(domain.Diff{Text: anchorDiff}, notes)

	for _, l := range lines {
		if len(l.Notes) != 0 {
			t.Error("a note whose line is gone must not be attached to another")
		}
	}
	if len(orphaned) != 1 || orphaned[0].ID != "n1" {
		t.Fatalf("orphaned = %+v, want the note reported", orphaned)
	}
}

func TestIdenticalLinesAreToldApartByOccurrence(t *testing.T) {
	// Closing braces, blank lines and `return nil` are everywhere. Anchoring on
	// text alone would put every note on the first one.
	diff := "@@\n+\treturn nil\n+}\n+\n+func B() error {\n+\treturn nil\n+}\n"
	notes := []domain.Note{
		{ID: "n1", Anchor: "+\treturn nil", AnchorNth: 1, Text: "the second one"},
	}

	lines, orphaned := usecase.AnchorNotes(domain.Diff{Text: diff}, notes)

	if len(orphaned) != 0 {
		t.Fatalf("orphaned = %+v", orphaned)
	}
	seen := 0
	for _, l := range lines {
		if l.Text != "+\treturn nil" {
			continue
		}
		if seen == 1 && len(l.Notes) != 1 {
			t.Error("the note belongs on the second occurrence")
		}
		if seen == 0 && len(l.Notes) != 0 {
			t.Error("the note is not on the first occurrence")
		}
		seen++
	}
}

func TestAnOccurrenceThatNoLongerExistsFallsBackToTheFirst(t *testing.T) {
	// The line is still there, just not as many times. Better to show the note
	// somewhere true than to orphan a judgement that still applies.
	diff := "@@\n+\treturn nil\n+}\n"
	notes := []domain.Note{{ID: "n1", Anchor: "+\treturn nil", AnchorNth: 3, Text: "still relevant"}}

	lines, orphaned := usecase.AnchorNotes(domain.Diff{Text: diff}, notes)

	if len(orphaned) != 0 {
		t.Fatalf("orphaned = %+v, want it placed on the first occurrence", orphaned)
	}
	placed := false
	for _, l := range lines {
		if len(l.Notes) == 1 && l.Text == "+\treturn nil" {
			placed = true
		}
	}
	if !placed {
		t.Error("the note should fall back to the first occurrence")
	}
}
