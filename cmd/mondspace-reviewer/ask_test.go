package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

func TestAskCommandPrintsAnswer(t *testing.T) {
	root := t.TempDir()
	store := jsonl.New(root)
	if err := store.AppendEvent(domain.Event{ID: "e1", SessionID: "s", Kind: domain.KindPrompt, StatedIntent: "add auth"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendUnit(domain.Unit{ID: "s-u001", SessionID: "s", Files: []string{"a.go"},
		Headline: domain.Headline{Text: "added validator"}}); err != nil {
		t.Fatal(err)
	}

	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			io.WriteString(w, `{"data":[]}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		io.WriteString(w, `{"choices":[{"message":{"content":"The session added auth in s-u001."}}]}`)
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := run(context.Background(), []string{
		"ask", "--scope=session", "--session=s", "--out=" + root,
		"--summarizer-url=" + srv.URL, "--model=m",
		"what", "did", "the", "agent", "do?",
	}, nil, &out)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}

	if !strings.Contains(out.String(), "The session added auth in s-u001.") {
		t.Errorf("stdout missing the answer:\n%s", out.String())
	}
	// The bounded context and the question both reach the model.
	if !strings.Contains(gotBody, "what did the agent do?") || !strings.Contains(gotBody, "add auth") {
		t.Errorf("request missing question/prompt context: %s", gotBody)
	}
}

func TestAskCommandRequiresSession(t *testing.T) {
	if err := run(context.Background(), []string{"ask", "--scope=session", "q"}, nil, &bytes.Buffer{}); err == nil {
		t.Error("ask without --session should error")
	}
}
