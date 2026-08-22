package jsonl_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

func TestAppendsAreAdditiveAcrossInstances(t *testing.T) {
	root := t.TempDir()

	first := jsonl.New(root)
	if err := first.AppendEvent(domain.Event{ID: "e1", SessionID: "s", Kind: domain.KindEdit}); err != nil {
		t.Fatal(err)
	}

	// A fresh Store, as if the process had restarted, must append, not truncate.
	second := jsonl.New(root)
	if err := second.AppendEvent(domain.Event{ID: "e2", SessionID: "s", Kind: domain.KindEdit}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, "s", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (append must not truncate)", len(lines))
	}
	if !strings.Contains(lines[0], `"id":"e1"`) || !strings.Contains(lines[1], `"id":"e2"`) {
		t.Errorf("lines out of order or missing: %q", lines)
	}
}

func TestAppendUnitWritesToUnitsFile(t *testing.T) {
	root := t.TempDir()
	s := jsonl.New(root)

	u := domain.Unit{ID: "sess-basic-u001", SessionID: "sess-basic", EventIDs: []string{"e1"}, Sealed: true}
	if err := s.AppendUnit(u); err != nil {
		t.Fatalf("AppendUnit: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "sess-basic", "units.jsonl"))
	if err != nil {
		t.Fatalf("reading units.jsonl: %v", err)
	}
	if !strings.Contains(string(data), `"id":"sess-basic-u001"`) {
		t.Errorf("units.jsonl missing unit id: %s", data)
	}
}

func TestAppendEventWritesOneLineCreatingDir(t *testing.T) {
	root := t.TempDir()
	s := jsonl.New(root)

	e := domain.Event{ID: "e1", SessionID: "sess-basic", Kind: domain.KindEdit, Files: []string{"a.go"}}
	if err := s.AppendEvent(e); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	path := filepath.Join(root, "sess-basic", "events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading events.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if !strings.Contains(lines[0], `"id":"e1"`) {
		t.Errorf("line does not contain event id: %s", lines[0])
	}
}
