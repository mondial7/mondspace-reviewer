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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	u := domain.Unit{
		ID:       "u1",
		Files:    []string{"auth/token.go"},
		Headline: domain.Headline{Text: "1 edit across 1 file"},
	}
	d := domain.Diff{Text: "+type TokenValidator interface {\n+\tValidate(tok string) error\n+}\n"}

	h, err := openai.New(base, model).Headline(ctx, u, d)
	if err != nil {
		t.Fatalf("Headline against %s: %v", base, err)
	}
	if h.Text == "" {
		t.Errorf("expected a non-empty WHAT from the model, got %+v", h)
	}
	t.Logf("model headline: %+v", h)
}
