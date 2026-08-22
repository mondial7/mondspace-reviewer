package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

func TestBuildTUIModelLoadsAndPersistsAnnotations(t *testing.T) {
	root := t.TempDir()
	store := jsonl.New(root)
	for _, u := range []domain.Unit{
		{ID: "s-u001", SessionID: "s", Files: []string{"a.go"}, Sealed: true},
		{ID: "s-u002", SessionID: "s", Files: []string{"b.go"}, Sealed: true},
	} {
		if err := store.AppendUnit(u); err != nil {
			t.Fatal(err)
		}
	}

	model, err := buildTUIModel(store, "s")
	if err != nil {
		t.Fatalf("buildTUIModel: %v", err)
	}
	if model.VisibleCount() != 2 {
		t.Fatalf("model shows %d units, want 2", model.VisibleCount())
	}

	// Objecting to the current unit must land in notes.jsonl.
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	data, err := os.ReadFile(filepath.Join(root, "s", "notes.jsonl"))
	if err != nil {
		t.Fatalf("notes.jsonl not written: %v", err)
	}
	if !strings.Contains(string(data), `"kind":"objection"`) || !strings.Contains(string(data), `"unit_id":"s-u001"`) {
		t.Errorf("note not persisted correctly: %s", data)
	}
}
