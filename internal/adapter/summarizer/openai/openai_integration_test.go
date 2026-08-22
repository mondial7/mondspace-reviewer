//go:build integration

package openai_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/summarizer/openai"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
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
