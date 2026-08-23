//go:build integration

package openai_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/summarizer/openai"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

// TestContractAgainstRealServer talks to a live OpenAI-compatible endpoint.
// It runs only under -tags=integration and skips unless a URL is configured.
//
//	MSR_SUMMARIZER_URL=http://192.168.101.99:1234/v1 MSR_MODEL=qwen/qwen3.5-9b \
//	    go test -tags=integration ./internal/adapter/summarizer/openai/...
func TestContractAgainstRealServer(t *testing.T) {
	base := os.Getenv("MSR_SUMMARIZER_URL")
	if base == "" {
		t.Skip("set MSR_SUMMARIZER_URL to run the summarizer contract test")
	}
	model := os.Getenv("MSR_MODEL")
	if model == "" {
		model = "qwen/qwen3.5-9b"
	}

	// Local model inference can be slow, so each call gets its own budget.
	newCtx := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 120*time.Second)
	}

	u := domain.Unit{
		ID:       "u1",
		Files:    []string{"auth/token.go"},
		Headline: domain.Headline{Text: "1 edit across 1 file"},
	}
	d := domain.Diff{Text: "+type TokenValidator interface {\n+\tValidate(tok string) error\n+}\n"}

	sum := openai.New(base, model).WithAPIKey(os.Getenv("MSR_API_KEY"))

	hctx, hcancel := newCtx()
	defer hcancel()
	h, err := sum.Headline(hctx, u, d)
	if err != nil {
		t.Fatalf("Headline against %s: %v", base, err)
	}
	if h.Text == "" {
		t.Errorf("expected a non-empty WHAT from the model, got %+v", h)
	}
	t.Logf("model headline: %+v", h)

	// Exercise interrogation too, with the same bounded-context discipline.
	actx, acancel := newCtx()
	defer acancel()
	ans, err := sum.Answer(actx, "what does this unit change?", domain.AskContext{
		Scope: domain.AskUnit, Prompt: "add token validation", Units: []domain.Unit{u}, Diff: d,
	})
	if err != nil {
		t.Fatalf("Answer against %s: %v", base, err)
	}
	if ans == "" {
		t.Errorf("expected a non-empty answer from the model")
	}
	t.Logf("model answer: %s", ans)
}

// TestStructuredOutputAgainstRealServer proves the two claims the story view
// rests on: that the server really enforces the schema, and that it accepts the
// request to skip the model's thinking phase.
func TestStructuredOutputAgainstRealServer(t *testing.T) {
	base := os.Getenv("MSR_SUMMARIZER_URL")
	if base == "" {
		t.Skip("set MSR_SUMMARIZER_URL to run the summarizer contract test")
	}
	model := os.Getenv("MSR_MODEL")
	if model == "" {
		model = "qwen/qwen3.5-9b"
	}

	schema := port.JSONSchema{
		Name: "chapter",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string"},
				"prose": map[string]any{"type": "string"},
				// An enum the model must choose from: the server compiles this
				// into a grammar, so an invented area cannot be emitted at all.
				"area": map[string]any{"type": "string", "enum": []string{"auth", "http", "root"}},
			},
			"required":             []string{"title", "prose", "area"},
			"additionalProperties": false,
		},
	}

	sum := openai.New(base, model).WithAPIKey(os.Getenv("MSR_API_KEY")).WithoutThinking()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	started := time.Now()
	reply, err := sum.AnswerSchema(ctx, "Give a chapter title and one sentence about auth/token.go gaining token validation.", domain.AskContext{}, schema)
	if err != nil {
		t.Fatalf("AnswerSchema against %s: %v", base, err)
	}
	t.Logf("structured reply in %s: %s", time.Since(started).Round(time.Millisecond), reply)

	// The whole point: the reply parses as JSON with no salvage step.
	var got struct {
		Title string `json:"title"`
		Prose string `json:"prose"`
		Area  string `json:"area"`
	}
	if err := json.Unmarshal([]byte(reply), &got); err != nil {
		t.Fatalf("reply was not bare JSON, so the schema was not enforced: %v\n%s", err, reply)
	}
	if got.Title == "" || got.Prose == "" {
		t.Errorf("schema required title and prose, got %+v", got)
	}
	switch got.Area {
	case "auth", "http", "root":
	default:
		t.Errorf("area = %q, which the enum should have made impossible", got.Area)
	}
}
