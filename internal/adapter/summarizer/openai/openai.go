// Package openai is a Summarizer backed by any OpenAI-compatible chat endpoint,
// defaulting to a local LM Studio server. It returns an error on any failure so
// callers can degrade to the mechanical headline.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

type Summarizer struct {
	baseURL string
	model   string
	client  *http.Client
}

func New(baseURL, model string) *Summarizer {
	return &Summarizer{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  http.DefaultClient,
	}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReply struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

const systemPrompt = `You summarize a single unit of code change for a reviewer.
Reply with exactly two lines and nothing else:
WHAT: <one concise line describing what changed>
WHY: <one concise line inferring why, or "unknown">`

func (s *Summarizer) Headline(ctx context.Context, u domain.Unit, d domain.Diff) (domain.Headline, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model:       s.model,
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt(u, d)},
		},
	})
	if err != nil {
		return domain.Headline{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return domain.Headline{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return domain.Headline{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return domain.Headline{}, fmt.Errorf("summarizer returned status %d", resp.StatusCode)
	}

	var reply chatReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return domain.Headline{}, err
	}
	if len(reply.Choices) == 0 {
		return domain.Headline{}, fmt.Errorf("summarizer returned no choices")
	}

	return parseHeadline(reply.Choices[0].Message.Content), nil
}

// Answer is implemented in the next behaviour.
func (s *Summarizer) Answer(ctx context.Context, question string, c domain.AskContext) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func userPrompt(u domain.Unit, d domain.Diff) string {
	var b strings.Builder
	b.WriteString("Files: " + strings.Join(u.Files, ", ") + "\n")
	b.WriteString("Mechanical summary: " + u.Headline.Text + "\n")
	b.WriteString("Diff:\n" + d.Text)
	return b.String()
}

// parseHeadline reads the model's two-line reply. WhySrc is left inferred; the
// usecase orchestrator enforces the stated-vs-inferred discipline.
func parseHeadline(content string) domain.Headline {
	h := domain.Headline{WhySrc: domain.WhyInferred}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "WHAT:"):
			h.Text = strings.TrimSpace(strings.TrimPrefix(line, "WHAT:"))
		case strings.HasPrefix(line, "WHY:"):
			why := strings.TrimSpace(strings.TrimPrefix(line, "WHY:"))
			if why != "" && why != "unknown" {
				h.Why = why
			}
		}
	}
	return h
}
