package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewSinceCommandPlainListsNetChangeWithNoSession(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "a.go")
	runGit(t, repo, "commit", "-qm", "init")
	runGit(t, repo, "tag", "before-work")

	// Work happens with no session recorded at all: change a.go, add b.go.
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "b.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	args := []string{
		"review", "--plain",
		"--since=before-work",
		"--repo=" + repo,
		"--out=" + t.TempDir(),
	}
	if err := run(context.Background(), args, nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "a.go") || !strings.Contains(got, "b.go") {
		t.Errorf("output missing changed files:\n%s", got)
	}
	if !strings.Contains(got, "since-before-work") {
		t.Errorf("unit ids should be seeded from a synthesized since-<ref> session id, got:\n%s", got)
	}
}
