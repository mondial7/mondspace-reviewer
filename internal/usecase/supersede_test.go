package usecase_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestMarkSupersededFileLevel(t *testing.T) {
	units := []domain.Unit{
		{ID: "u1", Files: []string{"a.go"}},
		{ID: "u2", Files: []string{"b.go"}},
		{ID: "u3", Files: []string{"a.go"}}, // later unit touching a.go again
	}
	notes := []contract.Item{
		contract.Item{Source: contract.SourceHuman, ID: "n1", UnitID: "u1", Kind: contract.KindObjection, Message: "wrong"},
		contract.Item{Source: contract.SourceHuman, ID: "n2", UnitID: "u2", Kind: contract.KindQuestion, Message: "why?"},
		contract.Item{Source: contract.SourceHuman, ID: "n3", UnitID: "u3", Kind: contract.KindOK},
	}

	got := usecase.MarkSuperseded(units, notes)

	byID := map[string]contract.Item{}
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
	if byID["n1"].Message != "wrong" || byID["n1"].Kind != contract.KindObjection {
		t.Errorf("n1 content changed: %+v", byID["n1"])
	}
	if len(got) != 3 {
		t.Errorf("got %d notes, want all 3 preserved", len(got))
	}
}
