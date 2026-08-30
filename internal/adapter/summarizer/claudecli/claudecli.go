// Package claudecli answers msr's questions by shelling out to the Claude Code
// CLI, for reviewers who have one and would rather a larger model read their
// diffs than the small local one.
//
// It is an alternative to the OpenAI-compatible adapter, not a replacement.
// msr's default is a model on your own machine and stays that way: this is a
// second engine behind the same port, chosen per workload, and it costs
// whatever the reviewer's Claude subscription costs (ADR 0035).
package claudecli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// Scheme is what a reviewer types where an endpoint would go.
const Scheme = "claude://cli"

// Summarizer runs one question per invocation. There is no session and no
// history: every call msr makes is already self-contained, and a CLI that
// remembered the last one would answer the next from a context nobody chose.
type Summarizer struct {
	bin   string
	model string
}

func New(bin, model string) *Summarizer {
	if strings.TrimSpace(bin) == "" {
		bin = "claude"
	}
	return &Summarizer{bin: bin, model: strings.TrimSpace(model)}
}

// Answer puts one question and returns what came back.
func (s *Summarizer) Answer(ctx context.Context, question string, _ domain.AskContext) (string, error) {
	args := []string{"-p", "--output-format", "text",
		// No tools. The prompt already carries the change, and a reviewer's
		// model that can read the filesystem is a different thing with
		// different risks — msr's whole claim is that it only ever reads what
		// it was given.
		"--allowed-tools", ""}
	if claudeModel(s.model) {
		args = append(args, "--model", s.model)
	}

	cmd := exec.CommandContext(ctx, s.bin, args...)
	// On stdin: an audit prompt carries the diff and runs to several kilobytes,
	// which is the wrong size for argv on any platform and the wrong place for
	// somebody's source code on all of them.
	cmd.Stdin = strings.NewReader(question)

	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude cli (%s): %w: %s", s.bin, err,
			strings.TrimSpace(firstLine(errs.String())))
	}
	return fenced(out.String()), nil
}

// Headline is not offered. The per-file headline is the high-volume call — one
// per changed file — and sending a hundred of them through a paid session is a
// bill nobody asked for. Callers fall back to the mechanical headline, which is
// what the port asks them to do.
func (s *Summarizer) Headline(context.Context, domain.Unit, domain.Diff) (domain.Headline, error) {
	return domain.Headline{}, fmt.Errorf("the claude cli answers whole questions, not per-file headlines")
}

// Ping reports whether the binary is there to be run. Being told the engine you
// chose is missing beats being quietly answered by something else.
func (s *Summarizer) Ping(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, s.bin, "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude cli (%s) did not answer: %w", s.bin, err)
	}
	return nil
}

// claudeModel reports whether a name is one this CLI would recognise.
//
// The model field is shared with the OpenAI-compatible engine, so it usually
// holds whatever is loaded in llama-server — and handing "qwen3-4b-instruct" to
// the CLI fails the entire call over a name the reviewer never chose for it.
// Anything that is not evidently a Claude model means "the field is about the
// other engine", and the CLI is left to use its own default.
func claudeModel(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "opus", "sonnet", "haiku", "opusplan", "default":
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "claude-")
}

// fenced pulls the answer out of a fenced block when there is one.
//
// The CLI writes prose around its JSON and often adds a paragraph after it.
// Every caller then hunts for the first { and the last }, which one sentence
// containing a brace would break — so the shape is normalised here, where
// knowing what this particular tool's output looks like belongs.
func fenced(reply string) string {
	const tick = "```"
	start := strings.Index(reply, tick)
	if start < 0 {
		return strings.TrimSpace(reply)
	}
	rest := reply[start+len(tick):]
	// A fence may name its language on the same line.
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 && !strings.Contains(rest[:nl], tick) {
		rest = rest[nl+1:]
	}
	if end := strings.Index(rest, tick); end >= 0 {
		return strings.TrimSpace(rest[:end])
	}
	return strings.TrimSpace(reply)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
