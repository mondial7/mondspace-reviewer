package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/summarizer/openai"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

func chatResponse(content string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": content}},
		},
	})
	return string(b)
}

func TestHeadlineHonoursContextCancel(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // never respond until the test unblocks it
	}))
	defer srv.Close()
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, err := openai.New(srv.URL, "m").Headline(ctx, domain.Unit{}, domain.Diff{})
	if err == nil {
		t.Error("expected an error when the context is cancelled")
	}
}

func TestAnswerErrorsOnNon2xxAndUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	if _, err := openai.New(srv.URL, "m").Answer(context.Background(), "q", domain.AskContext{}); err == nil {
		t.Error("expected an error on HTTP 502")
	}
	if _, err := openai.New("http://127.0.0.1:1", "m").Answer(context.Background(), "q", domain.AskContext{}); err == nil {
		t.Error("expected an error when unreachable")
	}
}

func TestAnswerPostsQuestionWithContextAndParsesReply(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		io.WriteString(w, chatResponse("The change in s-u001 adds a Validator interface."))
	}))
	defer srv.Close()

	ctx := domain.AskContext{
		Scope:  domain.AskUnit,
		Prompt: "add token validation",
		Units:  []domain.Unit{{ID: "s-u001", Files: []string{"auth/token.go"}}},
		Diff:   domain.Diff{Text: "+type Validator interface{}\n"},
	}

	got, err := openai.New(srv.URL, "m").Answer(context.Background(), "what does u001 do?", ctx)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if !strings.Contains(gotBody, "what does u001 do?") {
		t.Errorf("body missing the question: %s", gotBody)
	}
	if !strings.Contains(gotBody, "s-u001") || !strings.Contains(gotBody, "add token validation") {
		t.Errorf("body missing bounded context (unit id / prompt): %s", gotBody)
	}
	if got != "The change in s-u001 adds a Validator interface." {
		t.Errorf("answer = %q, want the parsed reply", got)
	}
}

func TestHeadlineErrorsOnNon2xxAndUnreachable(t *testing.T) {
	u := domain.Unit{ID: "u1"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := openai.New(srv.URL, "m").Headline(context.Background(), u, domain.Diff{}); err == nil {
		t.Error("expected an error on HTTP 500")
	}

	// An address nobody is listening on: connection error.
	if _, err := openai.New("http://127.0.0.1:1", "m").Headline(context.Background(), u, domain.Diff{}); err == nil {
		t.Error("expected an error when the endpoint is unreachable")
	}
}

func TestHeadlinePostsChatCompletionAndParsesReply(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		io.WriteString(w, chatResponse("WHAT: extracted validation behind a TokenValidator\nWHY: to swap the JWT lib"))
	}))
	defer srv.Close()

	u := domain.Unit{ID: "u1", Files: []string{"auth/token.go"}, Headline: domain.Headline{Text: "2 edits across 1 file"}}
	d := domain.Diff{Text: "+type TokenValidator interface{}\n"}

	got, err := openai.New(srv.URL, "qwen/qwen3.5-9b").Headline(context.Background(), u, d)
	if err != nil {
		t.Fatalf("Headline: %v", err)
	}

	if gotPath != "/chat/completions" {
		t.Errorf("request path = %q, want /chat/completions", gotPath)
	}
	if !strings.Contains(gotBody, "qwen/qwen3.5-9b") {
		t.Errorf("request body missing model: %s", gotBody)
	}
	if !strings.Contains(gotBody, "auth/token.go") || !strings.Contains(gotBody, "TokenValidator") {
		t.Errorf("request body missing unit files/diff: %s", gotBody)
	}
	if got.Text != "extracted validation behind a TokenValidator" {
		t.Errorf("Text = %q, want the parsed WHAT line", got.Text)
	}
	if got.Why != "to swap the JWT lib" {
		t.Errorf("Why = %q, want the parsed WHY line", got.Why)
	}
}
