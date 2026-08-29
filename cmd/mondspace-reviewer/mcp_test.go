package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// writeReview puts one review's human record in the store, the way the web app
// would have.
func writeReview(t *testing.T, root, targetID string, notes ...domain.Note) {
	t.Helper()
	store := jsonl.New(root)
	for _, n := range notes {
		n.SessionID = targetID
		if err := store.AppendNote(n); err != nil {
			t.Fatalf("AppendNote: %v", err)
		}
	}
}

func TestTheWebAppLeavesAPointerToWhatIsOpen(t *testing.T) {
	// The MCP server is a separate process with no way to ask the running app
	// anything. A file in the store is how the two meet.
	root := t.TempDir()

	if err := markOpen(root, openReview{
		TargetID: "abc123", Title: "add retries", Ref: "abc123", Repo: "msr",
	}); err != nil {
		t.Fatalf("markOpen: %v", err)
	}

	got, ok := whatIsOpen(root)
	if !ok {
		t.Fatal("the pointer should be readable back")
	}
	if got.TargetID != "abc123" || got.Title != "add retries" {
		t.Errorf("got %+v", got)
	}
}

func TestWithNoPointerTheMostRecentlyWrittenReviewIsTheOpenOne(t *testing.T) {
	// msr web may never have run — the store is still on disk, and guessing
	// from it beats refusing to answer.
	root := t.TempDir()
	writeReview(t, root, "older", domain.Note{ID: "1", Kind: domain.NoteQuestion, Text: "older"})
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, "older", "notes.jsonl"), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	writeReview(t, root, "newer", domain.Note{ID: "2", Kind: domain.NoteQuestion, Text: "newer"})

	got, ok := whatIsOpen(root)
	if !ok {
		t.Fatal("a store with reviews in it should yield one")
	}
	if got.TargetID != "newer" {
		t.Errorf("TargetID = %q, want the one last written to", got.TargetID)
	}
}

func TestMCPServesTheOpenReviewOverStdio(t *testing.T) {
	root := t.TempDir()
	writeReview(t, root, "abc123",
		domain.Note{ID: "1", Kind: domain.NoteObjection, File: "http.go", Text: "this retries forever"},
		domain.Note{ID: "2", Kind: domain.NoteOK, File: "http.go", Text: "reads fine"})
	if err := markOpen(root, openReview{TargetID: "abc123", Title: "add retries"}); err != nil {
		t.Fatalf("markOpen: %v", err)
	}

	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"review_feedback","arguments":{}}}`,
	}, "\n"))
	var out strings.Builder

	if err := run(context.Background(), []string{"mcp", "--out", root}, in, &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	var replies []map[string]any
	dec := json.NewDecoder(strings.NewReader(out.String()))
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("decoding %q: %v", out.String(), err)
		}
		replies = append(replies, m)
	}
	if len(replies) != 2 {
		t.Fatalf("got %d replies from %q", len(replies), out.String())
	}

	body, _ := json.Marshal(replies[1])
	if !strings.Contains(string(body), "this retries forever") {
		t.Errorf("the open review's feedback should come back: %s", body)
	}
	if strings.Contains(string(body), "reads fine") {
		t.Errorf("an approval is not outstanding feedback: %s", body)
	}
}

func TestOpeningAReviewInTheAppIsWhatTheMCPServerFollows(t *testing.T) {
	// The two processes never talk. Opening a review has to leave the trace, or
	// an agent would be answered about whatever was open last week.
	dir := gcTestRepo(t)
	first := gcGit(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gcGit(t, dir, "commit", "-aqm", "second")
	second := gcGit(t, dir, "rev-parse", "HEAD")

	root := filepath.Join(dir, ".mondspace-reviewer")
	id := "range-" + second[:8]
	registerTarget(id, targetEntry{repo: dir, out: root, target: domain.Target{
		ID: id, Repo: dir, Kind: domain.TargetCommit, Title: "second commit",
		Ref:  second[:8],
		From: domain.SnapshotRef{Commit: first}, To: domain.SnapshotRef{Commit: second},
	}})

	if _, err := targetLoader()(context.Background(), id); err != nil {
		t.Fatalf("loading the target: %v", err)
	}

	open, ok := whatIsOpen(root)
	if !ok {
		t.Fatal("opening a review should have left a pointer")
	}
	if open.TargetID != id || open.Title != "second commit" {
		t.Errorf("got %+v", open)
	}
}
