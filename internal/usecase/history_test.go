package usecase_test

import (
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestFileHistoryCountsEveryTouchOfAFile(t *testing.T) {
	// A net-change-per-file unit hides how the agent got there. The reviewer
	// asked the obvious next question: how many times, and when last?
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	events := []domain.Event{
		{ID: "e1", TS: base, Kind: domain.KindEdit, Tool: "edit",
			Files: []string{"auth/token.go"}, StatedIntent: "extract a validator"},
		{ID: "e2", TS: base.Add(3 * time.Minute), Kind: domain.KindEdit, Tool: "edit",
			Files: []string{"auth/token.go"}},
		{ID: "e3", TS: base.Add(9 * time.Minute), Kind: domain.KindEdit, Tool: "write",
			Files: []string{"http/middleware.go"}},
		{ID: "e4", TS: base.Add(12 * time.Minute), Kind: domain.KindEdit, Tool: "edit",
			Files: []string{"auth/token.go"}, Failed: true},
	}
	units := []domain.Unit{
		{ID: "s-f001", Files: []string{"auth/token.go"}},
		{ID: "s-f002", Files: []string{"http/middleware.go"}},
	}

	got := usecase.FileHistories(events, units)

	token := got["s-f001"]
	if token.Count != 3 {
		t.Errorf("auth/token.go touched %d times, want 3", token.Count)
	}
	if !token.First.Equal(base) || !token.Last.Equal(base.Add(12*time.Minute)) {
		t.Errorf("span = %v..%v, want the first and last touch", token.First, token.Last)
	}
	// The agent's own words, kept verbatim and attributed as stated (ADR 0003).
	if len(token.Edits) != 3 || token.Edits[0].Intent != "extract a validator" {
		t.Errorf("edits = %+v, want the stated intent preserved", token.Edits)
	}
	// A failed edit is part of the story of a file, and is marked as such.
	if !token.Edits[2].Failed {
		t.Error("a failed edit should be marked failed, not dropped")
	}
	if got["s-f002"].Count != 1 {
		t.Errorf("http/middleware.go touched %d times, want 1", got["s-f002"].Count)
	}
}

func TestFileHistoryMatchesPathsRecordedDifferently(t *testing.T) {
	// Hooks report whatever path the agent used — often absolute — while units
	// are named relative to the repo. Failing to match would silently report
	// "never edited" for every file.
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	events := []domain.Event{
		{ID: "e1", TS: base, Kind: domain.KindEdit,
			Files: []string{"/Users/x/work/repo/auth/token.go"}},
		{ID: "e2", TS: base.Add(time.Minute), Kind: domain.KindEdit,
			Files: []string{"./auth/token.go"}},
	}
	units := []domain.Unit{{ID: "s-f001", Files: []string{"auth/token.go"}}}

	got := usecase.FileHistories(events, units)

	if got["s-f001"].Count != 2 {
		t.Errorf("matched %d events, want both the absolute and the relative path",
			got["s-f001"].Count)
	}
}

func TestFileHistoryIgnoresEventsThatTouchedNothing(t *testing.T) {
	// A prompt or a shell command is not an edit of a file, and counting it
	// would inflate every number on the page.
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	events := []domain.Event{
		{ID: "e1", TS: base, Kind: domain.KindPrompt, StatedIntent: "add auth"},
		{ID: "e2", TS: base, Kind: domain.KindEdit, Tool: "bash"},
	}
	units := []domain.Unit{{ID: "s-f001", Files: []string{"auth/token.go"}}}

	got := usecase.FileHistories(events, units)

	if got["s-f001"].Count != 0 {
		t.Errorf("Count = %d, want 0 for a file nothing touched", got["s-f001"].Count)
	}
	if !got["s-f001"].Last.IsZero() {
		t.Error("a file never touched has no last-edited time")
	}
}

func TestFileHistoryOrdersEditsOldestFirst(t *testing.T) {
	// The history reads like a git log for that file, so it runs in the order
	// the work actually happened.
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	events := []domain.Event{
		{ID: "e2", TS: base.Add(5 * time.Minute), Kind: domain.KindEdit, Files: []string{"a.go"}},
		{ID: "e1", TS: base, Kind: domain.KindEdit, Files: []string{"a.go"}},
	}

	got := usecase.FileHistories(events, []domain.Unit{{ID: "u", Files: []string{"a.go"}}})

	edits := got["u"].Edits
	if len(edits) != 2 || !edits[0].TS.Equal(base) {
		t.Errorf("edits = %+v, want oldest first", edits)
	}
}
