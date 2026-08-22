package usecase_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestMarkSupersededFileLevel(t *testing.T) {
	units := []domain.Unit{
		{ID: "u1", Files: []string{"a.go"}},
		{ID: "u2", Files: []string{"b.go"}},
		{ID: "u3", Files: []string{"a.go"}}, // later unit touching a.go again
	}
	notes := []domain.Note{
		{ID: "n1", UnitID: "u1", Kind: domain.NoteObjection, Text: "wrong"},
		{ID: "n2", UnitID: "u2", Kind: domain.NoteQuestion, Text: "why?"},
		{ID: "n3", UnitID: "u3", Kind: domain.NoteOK},
	}

	got := usecase.MarkSuperseded(units, notes)

	byID := map[string]domain.Note{}
	for _, n := range got {
		byID[n.ID] = n
	}
	if byID["n1"].SupersededBy != "u3" {
		t.Errorf("n1 superseded_by = %q, want u3", byID["n1"].SupersededBy)
	}
	if byID["n2"].SupersededBy != "" {
		t.Errorf("n2 should not be superseded, got %q", byID["n2"].SupersededBy)
	}
	if byID["n3"].SupersededBy != "" {
		t.Errorf("n3 (last unit) should not be superseded, got %q", byID["n3"].SupersededBy)
	}

	// The note is surfaced, never deleted or auto-resolved: text and kind stay.
	if byID["n1"].Text != "wrong" || byID["n1"].Kind != domain.NoteObjection {
		t.Errorf("n1 content changed: %+v", byID["n1"])
	}
	if len(got) != 3 {
		t.Errorf("got %d notes, want all 3 preserved", len(got))
	}
}
