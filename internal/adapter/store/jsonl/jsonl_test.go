package jsonl_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

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
