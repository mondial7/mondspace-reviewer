package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestRequestsCapTheirOwnLength(t *testing.T) {
	// LM Studio's documented mitigation for a model stuck inside an unclosed
	// structure is a token cap. Without one the ceiling is the server's default,
	// which is how a narration burned 299 tokens and returned finish_reason
	// "length" with nothing in it.
	url, body := captureBody(t, "WHAT: x\nWHY: unknown")

	if _, err := openai.New(url, "m").Headline(context.Background(), domain.Unit{}, domain.Diff{}); err != nil {
		t.Fatal(err)
	}

	max, ok := body()["max_tokens"].(float64)
	if !ok {
		t.Fatalf("no max_tokens in request: %v", body())
	}
	if max <= 0 {
		t.Errorf("max_tokens = %v, want a positive cap", max)
	}
}

func TestWithMaxTokensOverridesTheCap(t *testing.T) {
	url, body := captureBody(t, "x")

	if _, err := openai.New(url, "m").WithMaxTokens(2048).Answer(context.Background(), "q", domain.AskContext{}); err != nil {
		t.Fatal(err)
	}

	if got := body()["max_tokens"]; got != float64(2048) {
		t.Errorf("max_tokens = %v, want 2048", got)
	}
}

func TestASchemaRejectionIsNamedWhenTheRetryAlsoFails(t *testing.T) {
	// The unconstrained retry is the right resilience, but it must not bury why
	// it happened: a server that cannot translate the schema is a different fault
	// from a model that answered badly, and the two need different fixes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "cannot convert schema", http.StatusBadRequest)
	}))
	defer srv.Close()

	_, err := openai.New(srv.URL, "m").AnswerSchema(context.Background(), "q", domain.AskContext{}, narrativeSchema())

	if err == nil {
		t.Fatal("expected an error when both attempts fail")
	}
	if !strings.Contains(err.Error(), "schema") {
		t.Errorf("error should say the schema was rejected, got: %v", err)
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

func usageResponse(content string, prompt, completion, reasoning int) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": content}},
		},
		"usage": map[string]any{
			"prompt_tokens":     prompt,
			"completion_tokens": completion,
			"total_tokens":      prompt + completion,
			"completion_tokens_details": map[string]any{
				"reasoning_tokens": reasoning,
			},
		},
	})
	return string(b)
}

func TestSummarizerAccumulatesTokenUsage(t *testing.T) {
	// The server reports what every call cost and msr was throwing it away, so
	// the one number a local-model user most wants — how much thinking is being
	// spent — was invisible.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Millisecond) // a local call is otherwise sub-millisecond
		io.WriteString(w, usageResponse("WHAT: x\nWHY: unknown", 100, 50, 40))
	}))
	defer srv.Close()

	sum := openai.New(srv.URL, "m")
	for i := 0; i < 3; i++ {
		if _, err := sum.Headline(context.Background(), domain.Unit{}, domain.Diff{}); err != nil {
			t.Fatal(err)
		}
	}

	got := sum.Usage()
	if got.Calls != 3 {
		t.Errorf("Calls = %d, want 3", got.Calls)
	}
	if got.Prompt != 300 || got.Completion != 150 {
		t.Errorf("tokens = %d prompt / %d completion, want 300/150", got.Prompt, got.Completion)
	}
	// Reasoning is the interesting half on a thinking model: it is what exhausts
	// a small context, and it is reported separately from the answer.
	if got.Reasoning != 120 {
		t.Errorf("Reasoning = %d, want 120", got.Reasoning)
	}
	if got.Millis <= 0 {
		t.Errorf("Millis = %d, want the accumulated call time", got.Millis)
	}
}

func TestUsageCountsFailuresSeparatelyFromCalls(t *testing.T) {
	// A failing endpoint costs no tokens but is exactly what a status page needs
	// to show, so failures are counted rather than dropped.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	sum := openai.New(srv.URL, "m")
	_, _ = sum.Answer(context.Background(), "q", domain.AskContext{})

	got := sum.Usage()
	if got.Failures != 1 {
		t.Errorf("Failures = %d, want 1", got.Failures)
	}
	if got.Calls != 1 {
		t.Errorf("Calls = %d, want the attempt to be counted", got.Calls)
	}
}

func TestPingReportsWhetherTheEndpointIsReachable(t *testing.T) {
	// The status page asks "is the reviewer's model online?" — which is a live
	// question, not one answered once at start-up.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("Ping should ask /models, got %s", r.URL.Path)
		}
		io.WriteString(w, `{"data":[{"id":"m"}]}`)
	}))
	defer up.Close()

	if err := openai.New(up.URL, "m").Ping(context.Background()); err != nil {
		t.Errorf("Ping against a healthy server: %v", err)
	}
	if err := openai.New("http://127.0.0.1:1", "m").Ping(context.Background()); err == nil {
		t.Error("Ping should fail when nothing is listening")
	}
}

func TestFreeTextAnswerNeverReturnsTheModelsThinking(t *testing.T) {
	// A reasoning model that runs out of budget mid-thought leaves content empty
	// and a "Thinking Process:" monologue in reasoning_content. Showing that as
	// the answer is worse than saying nothing: it reads like a reply, and it is
	// the model talking to itself.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"finish_reason":"length","message":{"role":"assistant",`+
			`"content":"","reasoning_content":"Thinking Process:\n1. Analyze the request…"}}]}`)
	}))
	defer srv.Close()

	_, err := openai.New(srv.URL, "m").Answer(context.Background(), "what changed?", domain.AskContext{})

	if err == nil {
		t.Fatal("expected an error when the model produced only reasoning")
	}
	if !strings.Contains(err.Error(), "reasoning") {
		t.Errorf("the error should say what happened, got: %v", err)
	}
	// And it must say how to fix it, since the cause is a budget, not a fault.
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("the error should point at the token budget, got: %v", err)
	}
}

func TestSchemaAnswerStillReadsReasoningBecauseTheJSONIsThere(t *testing.T) {
	// The schema case is the opposite: LM Studio puts the constrained JSON in
	// reasoning_content and leaves content empty, so that IS the answer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant",`+
			`"content":"","reasoning_content":"{\"title\":\"T\"}"}}]}`)
	}))
	defer srv.Close()

	got, err := openai.New(srv.URL, "m").AnswerSchema(context.Background(), "q", domain.AskContext{}, narrativeSchema())
	if err != nil {
		t.Fatalf("AnswerSchema: %v", err)
	}
	if got != `{"title":"T"}` {
		t.Errorf("answer = %q, want the constrained JSON", got)
	}
}

func TestAskGetsARoomierBudgetThanAHeadline(t *testing.T) {
	// A one-line headline and a reviewer's answer are not the same size of task,
	// and the ask path is exactly where running out mid-thought showed up.
	url, body := captureBody(t, "an answer")

	if _, err := openai.New(url, "m").Answer(context.Background(), "q", domain.AskContext{}); err != nil {
		t.Fatal(err)
	}
	ask, _ := body()["max_tokens"].(float64)

	url2, body2 := captureBody(t, "WHAT: x\nWHY: unknown")
	if _, err := openai.New(url2, "m").Headline(context.Background(), domain.Unit{}, domain.Diff{}); err != nil {
		t.Fatal(err)
	}
	headline, _ := body2()["max_tokens"].(float64)

	if ask <= headline {
		t.Errorf("ask budget %v should exceed the headline budget %v", ask, headline)
	}
}
