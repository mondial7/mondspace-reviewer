// Package routed puts one engine in front of another, so a question that the
// engine it was meant for cannot answer is still answered — and so the answer
// says which engine gave it (ADR 0039).
//
// It exists for one arrangement: the three readings a reviewer acts on go to
// the Claude Code CLI, and the local model stands behind it for the machine
// where the CLI is not installed, not logged in, or simply not answering today.
// A review must never block on either engine.
package routed

import (
	"context"
	"fmt"
	"sync"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

// Summarizer tries the primary engine and falls back to the second.
//
// It never hides the fallback. A result from a 4B model presented with the same
// confidence as one from the CLI is worse than no result, because the reviewer
// has no way to know how much of it to believe — so what actually answered is
// recorded and put on the card.
type Summarizer struct {
	primary, standby port.Summarizer
	primaryAs        domain.Engine
	standbyAs        domain.Engine

	mu       sync.RWMutex
	answered domain.Engine
	fellBack bool
	why      string
}

// New puts primary in front of standby. A nil standby means there is nothing
// behind this engine, which is a legitimate arrangement — it simply never falls
// back.
func New(primary port.Summarizer, primaryAs domain.Engine,
	standby port.Summarizer, standbyAs domain.Engine) *Summarizer {
	return &Summarizer{
		primary: primary, primaryAs: primaryAs,
		standby: standby, standbyAs: standbyAs,
	}
}

// Answered names the engine that gave the most recent answer, and whether that
// was the fallback rather than the engine the job was routed to.
//
// It is read straight after a call by whoever made it. Two calls in flight at
// once are both routed the same way — the routing does not depend on the
// question — so the answer is the same either way, and this stays a fact about
// the engine rather than a race about the call.
func (s *Summarizer) Answered() (domain.Engine, bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.answered, s.fellBack, s.why
}

func (s *Summarizer) record(engine domain.Engine, fellBack bool, why string) {
	s.mu.Lock()
	s.answered, s.fellBack, s.why = engine, fellBack, why
	s.mu.Unlock()
}

func (s *Summarizer) Answer(ctx context.Context, question string, c domain.AskContext) (string, error) {
	return s.attempt(ctx,
		func(sum port.Summarizer) (string, error) { return sum.Answer(ctx, question, c) })
}

// AnswerSchema forwards to an engine that can enforce a schema and degrades to
// a plain question where it cannot — which is what the CLI needs, since it has
// no grammar to compile a schema into.
//
// Implementing this at all matters: the usecase layer type-asserts for it, and
// a wrapper that did not would silently turn schema-enforced narration back into
// parsing JSON out of prose for the engine that *can* enforce it.
func (s *Summarizer) AnswerSchema(ctx context.Context, question string, c domain.AskContext,
	schema port.JSONSchema) (string, error) {

	return s.attempt(ctx, func(sum port.Summarizer) (string, error) {
		if answerer, ok := sum.(port.SchemaAnswerer); ok {
			return answerer.AnswerSchema(ctx, question, c, schema)
		}
		return sum.Answer(ctx, question, c)
	})
}

// attempt runs one call against the primary, and against the standby if the
// primary could not.
func (s *Summarizer) attempt(ctx context.Context, call func(port.Summarizer) (string, error)) (string, error) {
	if s.primary != nil {
		out, err := call(s.primary)
		if err == nil {
			s.record(s.primaryAs, false, "")
			return out, nil
		}
		if s.standby == nil || ctx.Err() != nil {
			// Nothing behind it, or the reviewer navigated away. A cancelled
			// call must not be retried on the other engine: that is a second
			// call nobody is waiting for, and on a paid engine it is a second
			// bill.
			s.record(s.primaryAs, false, "")
			return "", err
		}

		out, second := call(s.standby)
		if second != nil {
			// Both are gone. The first failure is the one worth reporting: it
			// is the engine this job was actually routed to.
			s.record("", false, err.Error())
			return "", fmt.Errorf("%s: %w (and %s: %v)", s.primaryAs, err, s.standbyAs, second)
		}
		s.record(s.standbyAs, true, err.Error())
		return out, nil
	}

	if s.standby == nil {
		return "", fmt.Errorf("no engine configured")
	}
	out, err := call(s.standby)
	s.record(s.standbyAs, true, "no primary engine configured")
	return out, err
}

// Headline goes straight to the standby when the primary does not offer one.
//
// This is not a failure path. The per-file headline is one call per changed
// file, and the CLI declines it on purpose: a hundred of them through a paid
// session is a bill nobody asked for (ADR 0035).
func (s *Summarizer) Headline(ctx context.Context, u domain.Unit, d domain.Diff) (domain.Headline, error) {
	if s.primary != nil {
		if h, err := s.primary.Headline(ctx, u, d); err == nil {
			return h, nil
		}
	}
	if s.standby == nil {
		return domain.Headline{}, fmt.Errorf("no engine offers headlines")
	}
	return s.standby.Headline(ctx, u, d)
}

// Ping reports the state of the pair: nil while anything can answer.
//
// Reporting the primary's failure here would light the status page red for a
// review that is working perfectly well on the fallback. The settings page
// reports each engine on its own, which is where the difference belongs.
func (s *Summarizer) Ping(ctx context.Context) error {
	first := ping(ctx, s.primary)
	if first == nil {
		return nil
	}
	if second := ping(ctx, s.standby); second == nil {
		return nil
	}
	return first
}

func ping(ctx context.Context, sum port.Summarizer) error {
	if sum == nil {
		return fmt.Errorf("not configured")
	}
	pinger, ok := sum.(port.Pinger)
	if !ok {
		return fmt.Errorf("cannot be reached")
	}
	return pinger.Ping(ctx)
}

// Usage is what both engines have spent behind this handle. What a review cost
// is not a per-engine question here; the settings page breaks it down.
func (s *Summarizer) Usage() port.TokenUsage {
	var total port.TokenUsage
	for _, sum := range []port.Summarizer{s.primary, s.standby} {
		reporter, ok := sum.(port.UsageReporter)
		if !ok {
			continue
		}
		u := reporter.Usage()
		total.Calls += u.Calls
		total.Failures += u.Failures
		total.Prompt += u.Prompt
		total.Completion += u.Completion
		total.Reasoning += u.Reasoning
		total.Millis += u.Millis
	}
	return total
}

// Engines is the pair behind this handle, for a status page that reports them
// separately.
func (s *Summarizer) Engines() (primary, standby port.Summarizer) {
	return s.primary, s.standby
}
