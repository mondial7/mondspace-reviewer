// Package switchable makes the reviewer's model reconfigurable while the
// application is running.
//
// Every model call in the app captures its summarizer when it is wired up —
// narration, description, questions, re-analysis, the status probe. Changing the
// endpoint or the model used to mean restarting. This keeps the reference they
// hold stable and swaps what it delegates to underneath.
package switchable

import (
	"context"
	"fmt"
	"sync"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

// Summarizer delegates to whichever summarizer is currently configured.
type Summarizer struct {
	mu      sync.RWMutex
	current port.Summarizer
}

// New wraps a summarizer so it can be replaced later.
func New(initial port.Summarizer) *Summarizer {
	return &Summarizer{current: initial}
}

// Swap replaces the summarizer every existing reference will use from now on.
// Calls already in flight finish against the one they started with.
func (s *Summarizer) Swap(next port.Summarizer) {
	s.mu.Lock()
	s.current = next
	s.mu.Unlock()
}

// Current is the summarizer in use, for a caller that needs the real thing.
func (s *Summarizer) Current() port.Summarizer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

func (s *Summarizer) Headline(ctx context.Context, u domain.Unit, d domain.Diff) (domain.Headline, error) {
	return s.Current().Headline(ctx, u, d)
}

func (s *Summarizer) Answer(ctx context.Context, question string, c domain.AskContext) (string, error) {
	return s.Current().Answer(ctx, question, c)
}

// AnswerSchema forwards to a summarizer that can enforce a schema, and falls
// back to an unconstrained answer when the current one cannot — the same
// degradation the OpenAI adapter makes when an endpoint rejects a schema.
//
// Forwarding matters: the usecase layer type-asserts for this, and a wrapper
// that did not implement it would silently turn schema-enforced narration back
// into parsing JSON out of prose.
func (s *Summarizer) AnswerSchema(ctx context.Context, question string, c domain.AskContext, schema port.JSONSchema) (string, error) {
	current := s.Current()
	if answerer, ok := current.(port.SchemaAnswerer); ok {
		return answerer.AnswerSchema(ctx, question, c, schema)
	}
	return current.Answer(ctx, question, c)
}

// Answered forwards the attribution of the last call, so a result can be
// labelled with the engine that produced it. A summarizer that does not account
// for itself reports nothing, which reads as "unattributed" rather than as a
// claim about which engine answered.
func (s *Summarizer) Answered() (domain.Engine, bool, string) {
	if reporter, ok := s.Current().(port.EngineReporter); ok {
		return reporter.Answered()
	}
	return "", false, ""
}

// Usage is what the current summarizer has spent. A summarizer that does not
// account for itself reports nothing, which is the truth rather than a zero.
func (s *Summarizer) Usage() port.TokenUsage {
	if reporter, ok := s.Current().(port.UsageReporter); ok {
		return reporter.Usage()
	}
	return port.TokenUsage{}
}

// Ping reports whether the current summarizer can be reached. One that cannot be
// pinged is reported as unreachable rather than healthy: answering nil here
// would light the status page green for something that answers nothing.
func (s *Summarizer) Ping(ctx context.Context) error {
	if pinger, ok := s.Current().(port.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return fmt.Errorf("no summarizer configured")
}
