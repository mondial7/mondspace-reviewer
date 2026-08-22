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

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

type Summarizer struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func New(baseURL, model string) *Summarizer {
	return &Summarizer{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  http.DefaultClient,
	}
}

// WithAPIKey sets a bearer token for endpoints that require authentication
// (e.g. an LM Studio server with auth enabled). An empty key sends no header.
func (s *Summarizer) WithAPIKey(key string) *Summarizer {
	s.apiKey = key
	return s
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
	content, err := s.chat(ctx, systemPrompt, userPrompt(u, d))
	if err != nil {
		return domain.Headline{}, err
	}
	return parseHeadline(content), nil
}

const answerSystemPrompt = `You answer a reviewer's question about an agent's code changes.
Use ONLY the provided context: the task prompt, unit headlines, diffs, and notes.
Cite unit IDs (like s-u001) in your answer. Do NOT invent a stated intent — if the
context does not contain the agent's own words, say the log does not record it.`

func (s *Summarizer) Answer(ctx context.Context, question string, c domain.AskContext) (string, error) {
	return s.chat(ctx, answerSystemPrompt, askPrompt(question, c))
}

// chat runs one chat completion and returns the assistant's message content.
func (s *Summarizer) chat(ctx context.Context, system, user string) (string, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model:       s.model,
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("summarizer returned status %d", resp.StatusCode)
	}

	var reply chatReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return "", err
	}
	if len(reply.Choices) == 0 {
		return "", fmt.Errorf("summarizer returned no choices")
	}
	return reply.Choices[0].Message.Content, nil
}

// askPrompt renders the bounded context and the question into a user message.
func askPrompt(question string, c domain.AskContext) string {
	var b strings.Builder
	b.WriteString("Scope: " + string(c.Scope) + "\n")
	b.WriteString("Task prompt: " + c.Prompt + "\n")
	for _, u := range c.Units {
		b.WriteString("Unit " + u.ID + " [" + strings.Join(u.Files, ", ") + "]: " + u.Headline.Text + "\n")
	}
	if c.Diff.Text != "" {
		b.WriteString("Diff:\n" + c.Diff.Text + "\n")
	}
	for _, n := range c.Notes {
		b.WriteString("Note on " + n.UnitID + " (" + string(n.Kind) + "): " + n.Text + "\n")
	}
	b.WriteString("\nQuestion: " + question)
	return b.String()
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
