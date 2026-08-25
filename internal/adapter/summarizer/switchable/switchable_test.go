package switchable_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/summarizer/switchable"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

// plain answers, and supports none of the optional capabilities.
type plain struct{ answer string }

func (p plain) Headline(context.Context, domain.Unit, domain.Diff) (domain.Headline, error) {
	return domain.Headline{Text: p.answer}, nil
}
func (p plain) Answer(context.Context, string, domain.AskContext) (string, error) {
	return p.answer, nil
}

// full supports everything a summarizer optionally can.
type full struct {
	plain
	schemas int
	pingErr error
}

func (f *full) AnswerSchema(context.Context, string, domain.AskContext, port.JSONSchema) (string, error) {
	f.schemas++
	return "{}", nil
}
func (f *full) Usage() port.TokenUsage     { return port.TokenUsage{Calls: 7} }
func (f *full) Ping(context.Context) error { return f.pingErr }

func TestSwappingChangesWhoAnswers(t *testing.T) {
	// Every model call in the app captured its summarizer at start-up, so
	// changing the endpoint used to mean a restart. The reference stays the
	// same; what it delegates to changes underneath.
	s := switchable.New(plain{answer: "first"})

	got, _ := s.Answer(context.Background(), "q", domain.AskContext{})
	if got != "first" {
		t.Fatalf("answer = %q", got)
	}

	s.Swap(plain{answer: "second"})
	if got, _ = s.Answer(context.Background(), "q", domain.AskContext{}); got != "second" {
		t.Errorf("after swapping, answer = %q, want the new summarizer's", got)
	}
}

func TestOptionalCapabilitiesReachTheRealSummarizer(t *testing.T) {
	// The usecase layer type-asserts for these. A wrapper that did not forward
	// them would silently turn schema-enforced narration back into hopeful
	// parsing, and blank the token accounting.
	f := &full{pingErr: errors.New("down")}
	s := switchable.New(f)

	if _, err := s.AnswerSchema(context.Background(), "q", domain.AskContext{}, port.JSONSchema{}); err != nil {
		t.Fatalf("AnswerSchema: %v", err)
	}
	if f.schemas != 1 {
		t.Errorf("the schema call did not reach the summarizer")
	}
	if s.Usage().Calls != 7 {
		t.Errorf("Usage = %+v, want the summarizer's", s.Usage())
	}
	if err := s.Ping(context.Background()); err == nil {
		t.Error("Ping should report what the summarizer reported")
	}
}

func TestSchemaFallsBackWhenTheSummarizerCannotEnforceOne(t *testing.T) {
	// A summarizer with no structured-output support must still answer, exactly
	// as the adapter does when an endpoint rejects a schema.
	s := switchable.New(plain{answer: "prose"})

	got, err := s.AnswerSchema(context.Background(), "q", domain.AskContext{}, port.JSONSchema{})

	if err != nil {
		t.Fatalf("AnswerSchema: %v", err)
	}
	if got != "prose" {
		t.Errorf("answer = %q, want the unconstrained reply", got)
	}
}

func TestPingSaysSoWhenNothingCanBeAsked(t *testing.T) {
	// A summarizer that cannot be pinged is not therefore healthy. Reporting nil
	// would light the status page green for something that answers nothing.
	if err := switchable.New(plain{}).Ping(context.Background()); err == nil {
		t.Error("a summarizer with no Ping should not report itself reachable")
	}
}

func TestSwappingIsSafeWhileCallsAreInFlight(t *testing.T) {
	// The endpoint is changed from a web request while narration may be running.
	s := switchable.New(plain{answer: "a"})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = s.Answer(context.Background(), "q", domain.AskContext{}) }()
		go func() { defer wg.Done(); s.Swap(plain{answer: "b"}) }()
	}
	wg.Wait()
}
