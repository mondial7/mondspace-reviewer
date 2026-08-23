package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/summarizer/openai"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
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

func TestSendsBearerTokenWhenKeySet(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, chatResponse("WHAT: x\nWHY: unknown"))
	}))
	defer srv.Close()

	// With a key set, every request carries the bearer token.
	if _, err := openai.New(srv.URL, "m").WithAPIKey("sk-secret").Headline(context.Background(), domain.Unit{}, domain.Diff{}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer sk-secret")
	}

	// Without a key, no Authorization header is sent (tokenless endpoints).
	gotAuth = "unset"
	if _, err := openai.New(srv.URL, "m").Headline(context.Background(), domain.Unit{}, domain.Diff{}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty when no key set", gotAuth)
	}
}

// captureBody runs a server that records the request body and replies with
// content, so a test can assert on exactly what was sent to the model.
func captureBody(t *testing.T, content string) (url string, body func() map[string]any) {
	t.Helper()
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		io.WriteString(w, chatResponse(content))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() map[string]any {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("request body was not JSON: %v\n%s", err, raw)
		}
		return m
	}
}

func TestWithoutThinkingDisablesTheModelsThinkingPhase(t *testing.T) {
	// A reasoning model spends most of a small context thinking before it emits
	// any output. LM Studio forwards chat_template_kwargs to the chat template,
	// where enable_thinking=false suppresses that phase.
	url, body := captureBody(t, "WHAT: x\nWHY: unknown")

	if _, err := openai.New(url, "m").WithoutThinking().Headline(context.Background(), domain.Unit{}, domain.Diff{}); err != nil {
		t.Fatal(err)
	}

	kwargs, ok := body()["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("no chat_template_kwargs in request: %v", body())
	}
	if kwargs["enable_thinking"] != false {
		t.Errorf("enable_thinking = %v, want false", kwargs["enable_thinking"])
	}
}

func TestThinkingIsLeftAloneByDefault(t *testing.T) {
	// Not every OpenAI-compatible server understands the field, and a model with
	// room to think gives better prose, so it is opt-in.
	url, body := captureBody(t, "WHAT: x\nWHY: unknown")

	if _, err := openai.New(url, "m").Headline(context.Background(), domain.Unit{}, domain.Diff{}); err != nil {
		t.Fatal(err)
	}

	if _, present := body()["chat_template_kwargs"]; present {
		t.Errorf("chat_template_kwargs should be absent by default: %v", body())
	}
}

func narrativeSchema() port.JSONSchema {
	return port.JSONSchema{
		Name: "narrative",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"title": map[string]any{"type": "string"}},
			"required":   []any{"title"},
		},
	}
}

func TestAnswerSchemaConstrainsTheReplyToASchema(t *testing.T) {
	// LM Studio compiles the schema into a grammar (GGUF) or Outlines (MLX), so
	// the reply is JSON by construction rather than by hopeful parsing.
	url, body := captureBody(t, `{"title":"Locking down auth"}`)

	got, err := openai.New(url, "m").AnswerSchema(context.Background(), "tell the story", domain.AskContext{}, narrativeSchema())
	if err != nil {
		t.Fatalf("AnswerSchema: %v", err)
	}
	if got != `{"title":"Locking down auth"}` {
		t.Errorf("answer = %q, want the model's content", got)
	}

	format, ok := body()["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("no response_format in request: %v", body())
	}
	if format["type"] != "json_schema" {
		t.Errorf("response_format.type = %v, want json_schema", format["type"])
	}
	spec, ok := format["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("no json_schema in response_format: %v", format)
	}
	if spec["name"] != "narrative" {
		t.Errorf("json_schema.name = %v, want narrative", spec["name"])
	}
	if spec["strict"] != true {
		t.Errorf("json_schema.strict = %v, want true", spec["strict"])
	}
	if _, present := spec["schema"].(map[string]any); !present {
		t.Errorf("json_schema.schema missing: %v", spec)
	}
}

func TestAnswerSchemaRetriesUnconstrainedWhenTheServerRejectsTheSchema(t *testing.T) {
	// Not every OpenAI-compatible endpoint implements structured output. One that
	// does not must not turn a working narration into a failure.
	var attempts []bool // whether each attempt carried a response_format
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		constrained := strings.Contains(string(raw), "response_format")
		attempts = append(attempts, constrained)
		if constrained {
			http.Error(w, `{"error":"response_format is not supported"}`, http.StatusBadRequest)
			return
		}
		io.WriteString(w, chatResponse(`{"title":"T"}`))
	}))
	defer srv.Close()

	got, err := openai.New(srv.URL, "m").AnswerSchema(context.Background(), "q", domain.AskContext{}, narrativeSchema())
	if err != nil {
		t.Fatalf("AnswerSchema should fall back to an unconstrained call: %v", err)
	}
	if got != `{"title":"T"}` {
		t.Errorf("answer = %q, want the retried reply", got)
	}
	if want := []bool{true, false}; len(attempts) != 2 || attempts[0] != want[0] || attempts[1] != want[1] {
		t.Errorf("attempts = %v, want one constrained then one plain", attempts)
	}
}

func TestAnswerSchemaDoesNotRetryAServerFailure(t *testing.T) {
	// A 5xx says the server broke, not that it rejected the schema; retrying
	// unconstrained would just hide the real fault.
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := openai.New(srv.URL, "m").AnswerSchema(context.Background(), "q", domain.AskContext{}, narrativeSchema()); err == nil {
		t.Error("expected the 500 to surface")
	}
	if calls != 1 {
		t.Errorf("made %d calls, want 1 — a 5xx is not a schema rejection", calls)
	}
}

func TestAnswerLeavesTheReplyFormatFreeByDefault(t *testing.T) {
	url, body := captureBody(t, "prose is fine here")

	if _, err := openai.New(url, "m").Answer(context.Background(), "q", domain.AskContext{}); err != nil {
		t.Fatal(err)
	}

	if _, present := body()["response_format"]; present {
		t.Errorf("response_format should be absent for a free-text answer: %v", body())
	}
}

func TestAnswerReadsAReplyTheServerFiledAsReasoning(t *testing.T) {
	// Measured against LM Studio with qwen/qwen3.5-9b: a schema-constrained reply
	// arrives complete (finish_reason "stop") but in reasoning_content, with
	// content empty — the grammar constrains sampling inside the template's
	// thinking block. Treating that as an empty reply is what made narration
	// silently fall back.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"",`+
			`"reasoning_content":"{\"title\":\"T\",\"prose\":\"p\"}"}}]}`)
	}))
	defer srv.Close()

	got, err := openai.New(srv.URL, "m").AnswerSchema(context.Background(), "q", domain.AskContext{}, narrativeSchema())
	if err != nil {
		t.Fatalf("AnswerSchema: %v", err)
	}
	if got != `{"title":"T","prose":"p"}` {
		t.Errorf("answer = %q, want the reply the server filed as reasoning", got)
	}
}

func TestContentWinsOverReasoningWhenBothArePresent(t *testing.T) {
	// Reasoning is the model thinking aloud; content is its answer. When there is
	// an answer, the thinking must never displace it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"the answer",`+
			`"reasoning_content":"let me think about this"}}]}`)
	}))
	defer srv.Close()

	got, err := openai.New(srv.URL, "m").Answer(context.Background(), "q", domain.AskContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "the answer" {
		t.Errorf("answer = %q, want the content", got)
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
