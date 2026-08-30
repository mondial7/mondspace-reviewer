package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// fakeModel answers every question with the same sentence, over the
// OpenAI-compatible shape msr speaks.
func fakeModel(t *testing.T, answer string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"` + answer + `"}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

func TestAskingFromTheCommandLineJoinsTheReviewsConversation(t *testing.T) {
	// The app has recorded questions and answers since v6, and /search and
	// `msr mcp` both read them. A question asked from a terminal vanished the
	// moment it was answered — the same review, two different memories.
	root := t.TempDir()
	store := jsonl.New(root)
	if err := store.AppendNote(domain.Note{
		ID: "n1", SessionID: "abc123", Kind: domain.NoteQuestion, Text: "why here?",
	}); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"ask", "--scope=session", "--target=abc123", "--out=" + root,
		"--summarizer-url=" + fakeModel(t, "because of the retry loop"),
		"--model=fake",
		"does the retry have a stated reason?",
	}
	var out strings.Builder
	if err := run(context.Background(), args, nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "because of the retry loop") {
		t.Fatalf("the answer should still be printed:\n%s", out.String())
	}

	loaded, err := store.Load("abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Exchanges) != 1 {
		t.Fatalf("the store holds %d exchanges, want the one just asked", len(loaded.Exchanges))
	}
	got := loaded.Exchanges[0]
	if !strings.Contains(got.Question, "stated reason") || !strings.Contains(got.Answer, "retry loop") {
		t.Errorf("got %+v", got)
	}
}

func TestWithNoTargetTheCommandLineAsksAboutTheOpenReview(t *testing.T) {
	// `msr web` leaves a pointer to the review being read (ADR 0031). Typing
	// out a target id you are already looking at is a thing to make unnecessary.
	root := t.TempDir()
	if err := jsonl.New(root).AppendNote(domain.Note{
		ID: "n1", SessionID: "open-one", Kind: domain.NoteQuestion, Text: "?",
	}); err != nil {
		t.Fatal(err)
	}
	if err := markOpen(root, openReview{TargetID: "open-one", Title: "the open one"}); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"ask", "--scope=session", "--out=" + root,
		"--summarizer-url=" + fakeModel(t, "answered"), "--model=fake",
		"anything?",
	}
	if err := run(context.Background(), args, nil, &strings.Builder{}); err != nil {
		t.Fatalf("run: %v", err)
	}

	loaded, _ := jsonl.New(root).Load("open-one")
	if len(loaded.Exchanges) != 1 {
		t.Errorf("the question should have landed on the open review, got %d exchanges",
			len(loaded.Exchanges))
	}
}

func TestExportingWithNoTargetTakesTheOpenReview(t *testing.T) {
	root := t.TempDir()
	if err := jsonl.New(root).AppendNote(domain.Note{
		ID: "n1", SessionID: "open-one", Kind: domain.NoteObjection, Text: "this retries forever",
	}); err != nil {
		t.Fatal(err)
	}
	if err := markOpen(root, openReview{TargetID: "open-one"}); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := run(context.Background(), []string{"export", "--out=" + root}, nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "this retries forever") {
		t.Errorf("export should have found the open review:\n%s", out.String())
	}
}

func TestWithNothingOpenTheCommandSaysWhatItNeeds(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := run(context.Background(), []string{"export", "--out=" + root}, nil, &strings.Builder{})
	if err == nil {
		t.Fatal("want an error when there is nothing to export")
	}
	if !strings.Contains(err.Error(), "--target") {
		t.Errorf("the error should name the flag that fixes it: %v", err)
	}
}
