// Package openai is a Summarizer backed by any OpenAI-compatible chat endpoint,
// defaulting to a local LM Studio server. It returns an error on any failure so
// callers can degrade to the mechanical headline.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

type Summarizer struct {
	baseURL    string
	model      string
	apiKey     string
	noThinking bool
	maxTokens  int
	client     *http.Client
}

// defaultMaxTokens caps every reply. LM Studio's own guidance for a model that
// gets stuck inside an unclosed structure is a token cap, and an uncapped
// request leaves the ceiling to the server: measured, a narration spent 299
// tokens reasoning and came back with finish_reason "length" and no content.
// The cap is generous enough for a chapter of prose and a small JSON object.
const defaultMaxTokens = 1024

func New(baseURL, model string) *Summarizer {
	return &Summarizer{
		baseURL:   strings.TrimRight(baseURL, "/"),
		model:     model,
		maxTokens: defaultMaxTokens,
		client:    http.DefaultClient,
	}
}

// WithAPIKey sets a bearer token for endpoints that require authentication
// (e.g. an LM Studio server with auth enabled). An empty key sends no header.
func (s *Summarizer) WithAPIKey(key string) *Summarizer {
	s.apiKey = key
	return s
}

// WithoutThinking asks the server to skip the model's reasoning phase, which is
// what exhausts a modest context window before any output appears.
//
// LM Studio forwards chat_template_kwargs to the model's chat template, where
// some templates read enable_thinking. Measured against qwen/qwen3.5-9b it made
// no difference at all — reasoning tokens were identical with and without it, so
// that template ignores the flag. It is kept because other templates honour it,
// and it is off by default: unverified for any given model, and a model with
// room to think writes better prose.
func (s *Summarizer) WithoutThinking() *Summarizer {
	s.noThinking = true
	return s
}

// WithMaxTokens overrides the reply cap. A longer answer needs a larger one; a
// zero or negative value restores the default rather than uncapping, because an
// uncapped reply is how this fails.
func (s *Summarizer) WithMaxTokens(n int) *Summarizer {
	if n <= 0 {
		n = defaultMaxTokens
	}
	s.maxTokens = n
	return s
}

type chatRequest struct {
	Model            string          `json:"model"`
	Messages         []chatMessage   `json:"messages"`
	Temperature      float64         `json:"temperature"`
	MaxTokens        int             `json:"max_tokens,omitempty"`
	Stream           bool            `json:"stream"`
	ChatTemplateArgs map[string]any  `json:"chat_template_kwargs,omitempty"`
	ResponseFormat   *responseFormat `json:"response_format,omitempty"`
}

// responseFormat is the OpenAI structured-output contract. LM Studio compiles
// the schema into a llama.cpp grammar (GGUF) or Outlines (MLX), so the reply is
// valid JSON by construction.
type responseFormat struct {
	Type       string     `json:"type"`
	JSONSchema schemaSpec `json:"json_schema"`
}

type schemaSpec struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReply struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			// Reasoning is the model thinking aloud, which some servers split out
			// of content. A schema-constrained reply from LM Studio arrives here
			// with content empty, because the grammar constrains sampling inside
			// the chat template's thinking block.
			Reasoning string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
}

const systemPrompt = `You summarize a single unit of code change for a reviewer.
Reply with exactly two lines and nothing else:
WHAT: <one concise line describing what changed>
WHY: <one concise line inferring why, or "unknown">`

func (s *Summarizer) Headline(ctx context.Context, u domain.Unit, d domain.Diff) (domain.Headline, error) {
	content, err := s.chat(ctx, systemPrompt, userPrompt(u, d), nil)
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
	return s.chat(ctx, answerSystemPrompt, askPrompt(question, c), nil)
}

// AnswerSchema is Answer with the reply constrained to a JSON schema, satisfying
// port.SchemaAnswerer.
func (s *Summarizer) AnswerSchema(ctx context.Context, question string, c domain.AskContext, schema port.JSONSchema) (string, error) {
	format := &responseFormat{
		Type:       "json_schema",
		JSONSchema: schemaSpec{Name: schema.Name, Strict: true, Schema: schema.Schema},
	}
	prompt := askPrompt(question, c)

	content, err := s.chat(ctx, answerSystemPrompt, prompt, format)
	if errors.Is(err, errRejected) {
		// The endpoint does not implement structured output, or could not
		// translate this schema. Ask again without it: the caller still parses
		// defensively, so a plain reply works — it is only less reliable, which is
		// better than no narration at all.
		content, retryErr := s.chat(ctx, answerSystemPrompt, prompt, nil)
		if retryErr != nil {
			// Both failed. Name the rejection: a server that cannot take the
			// schema is a different fault from a model that answered badly, and
			// they need different fixes.
			return "", fmt.Errorf("schema rejected (%v), and the unconstrained retry failed: %w", err, retryErr)
		}
		return content, nil
	}
	return content, err
}

// errRejected marks a request the server refused as malformed, as opposed to one
// it failed to serve. Only the former is worth retrying differently.
var errRejected = errors.New("request rejected")

// chat runs one chat completion and returns the assistant's message content.
func (s *Summarizer) chat(ctx context.Context, system, user string, format *responseFormat) (string, error) {
	body := chatRequest{
		ResponseFormat: format,
		Model:          s.model,
		Temperature:    0,
		MaxTokens:      s.maxTokens,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}
	if s.noThinking {
		body.ChatTemplateArgs = map[string]any{"enable_thinking": false}
	}

	reqBody, err := json.Marshal(body)
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
	if resp.StatusCode/100 == 4 {
		return "", fmt.Errorf("summarizer returned status %d: %w", resp.StatusCode, errRejected)
	}
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

	msg := reply.Choices[0].Message
	if strings.TrimSpace(msg.Content) == "" {
		// The answer is in the thinking. Better a reply the caller can parse than
		// an empty string that reads, wrongly, as "the model had nothing to say".
		return msg.Reasoning, nil
	}
	return msg.Content, nil
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
