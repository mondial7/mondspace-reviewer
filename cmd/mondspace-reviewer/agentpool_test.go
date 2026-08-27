package main

import (
	"context"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

// countingSummarizer records that it was built, so the pool's sharing can be
// observed without standing up three HTTP servers.
type countingSummarizer struct {
	ref   domain.ModelRef
	usage port.TokenUsage
}

func (c *countingSummarizer) Headline(context.Context, domain.Unit, domain.Diff) (domain.Headline, error) {
	return domain.Headline{}, nil
}

func (c *countingSummarizer) Answer(context.Context, string, domain.AskContext) (string, error) {
	return "", nil
}

func (c *countingSummarizer) Usage() port.TokenUsage { return c.usage }

func TestOneServerBuildsOneSummarizerForEveryWorkload(t *testing.T) {
	// Three workloads pointed at one model is one connection, one usage total
	// and one liveness answer. Building three would triple the probing and make
	// /status report the same spend three times over.
	built := 0
	cfg := domain.AgentConfig{Endpoint: "http://e/v1", Model: "m"}

	pool := newAgentPool(cfg, func(ref domain.ModelRef) port.Summarizer {
		built++
		return &countingSummarizer{ref: ref}
	})

	if built != 1 {
		t.Errorf("built %d summarizers, want 1", built)
	}
	first := pool.modelFor(domain.Narration)
	for _, w := range domain.Workloads {
		if pool.modelFor(w) != first {
			t.Errorf("%s got its own summarizer; they share a model", w)
		}
	}
}

func TestAWorkloadsHandleOutlivesAChangeOfModel(t *testing.T) {
	// Everything downstream captures its summarizer once, at wiring time. If
	// reconfiguring handed out a new handle, every one of those callers would
	// go on using the old model forever -- the exact failure switchable exists
	// to prevent, reintroduced one level up.
	cfg := domain.AgentConfig{Endpoint: "http://a/v1", Model: "small"}
	pool := newAgentPool(cfg, func(ref domain.ModelRef) port.Summarizer {
		return &countingSummarizer{ref: ref}
	})
	handle := pool.For(domain.Narration)
	before := pool.modelFor(domain.Narration)

	cfg.Overrides = map[domain.Workload]domain.ModelRef{domain.Narration: {Model: "big"}}
	pool.Reconfigure(cfg, func(ref domain.ModelRef) port.Summarizer {
		return &countingSummarizer{ref: ref}
	})

	if pool.For(domain.Narration) != handle {
		t.Error("the handle was replaced; callers holding it would be stranded")
	}
	if pool.modelFor(domain.Narration) == before {
		t.Error("the handle still points at the old model")
	}
	if got := pool.modelFor(domain.Narration).(*countingSummarizer).ref.Model; got != "big" {
		t.Errorf("narration now on %q, want big", got)
	}
}

func TestASplitBuildsOnePerDistinctModel(t *testing.T) {
	// The arrangement the migration is for: narration on the 9B, everything
	// else on the 4B.
	var refs []domain.ModelRef
	cfg := domain.AgentConfig{
		Endpoint: "http://127.0.0.1:8081/v1", Model: "qwen3-4b",
		Overrides: map[domain.Workload]domain.ModelRef{
			domain.Narration: {Endpoint: "http://127.0.0.1:8082/v1", Model: "qwen3.5-9b"},
		},
	}

	pool := newAgentPool(cfg, func(ref domain.ModelRef) port.Summarizer {
		refs = append(refs, ref)
		return &countingSummarizer{ref: ref}
	})

	if len(refs) != 2 {
		t.Fatalf("built %d summarizers %v, want 2", len(refs), refs)
	}
	if pool.modelFor(domain.Narration) == pool.modelFor(domain.Describe) {
		t.Error("narration and describe are on different models and must not share")
	}
	if pool.modelFor(domain.Describe) != pool.modelFor(domain.Ask) {
		t.Error("describe and ask are on the same model and should share")
	}
}

func TestUsageIsTheSumAcrossEveryModel(t *testing.T) {
	// /status answers "what has the assistant cost me", and the answer is not
	// per-server. Reporting only one of two models would understate it by
	// however much the other did.
	cfg := domain.AgentConfig{
		Endpoint: "http://a/v1", Model: "small",
		Overrides: map[domain.Workload]domain.ModelRef{
			domain.Narration: {Model: "big"},
		},
	}
	pool := newAgentPool(cfg, func(ref domain.ModelRef) port.Summarizer {
		return &countingSummarizer{ref: ref, usage: port.TokenUsage{
			Calls: 2, Completion: 100, Reasoning: 10, Prompt: 50, Millis: 5,
		}}
	})

	got := pool.Usage()

	if got.Calls != 4 || got.Completion != 200 || got.Reasoning != 20 || got.Prompt != 100 {
		t.Errorf("usage = %+v, want both models added together", got)
	}
}

func TestReconfiguringRebuildsOnlyWhatChanged(t *testing.T) {
	// Swapping the narration model must not tear down the describe connection:
	// a reviewer changing one setting should not reset the other's usage or
	// drop its warm connection.
	cfg := domain.AgentConfig{Endpoint: "http://a/v1", Model: "small"}
	pool := newAgentPool(cfg, func(ref domain.ModelRef) port.Summarizer {
		return &countingSummarizer{ref: ref}
	})
	describeBefore := pool.modelFor(domain.Describe)

	cfg.Overrides = map[domain.Workload]domain.ModelRef{domain.Narration: {Model: "big"}}
	pool.Reconfigure(cfg, func(ref domain.ModelRef) port.Summarizer {
		return &countingSummarizer{ref: ref}
	})

	if pool.modelFor(domain.Describe) != describeBefore {
		t.Error("describe was rebuilt though its model did not change")
	}
	if pool.modelFor(domain.Narration) == describeBefore {
		t.Error("narration should now be a different summarizer")
	}
}

func TestEveryWorkloadResolvesEvenAfterAnOverrideIsRemoved(t *testing.T) {
	// Going back to one server must leave nothing pointing at a summarizer that
	// is no longer in the pool.
	cfg := domain.AgentConfig{
		Endpoint: "http://a/v1", Model: "small",
		Overrides: map[domain.Workload]domain.ModelRef{domain.Narration: {Model: "big"}},
	}
	pool := newAgentPool(cfg, func(ref domain.ModelRef) port.Summarizer {
		return &countingSummarizer{ref: ref}
	})

	cfg.Overrides = nil
	pool.Reconfigure(cfg, func(ref domain.ModelRef) port.Summarizer {
		return &countingSummarizer{ref: ref}
	})

	first := pool.modelFor(domain.Narration)
	if first == nil {
		t.Fatal("narration lost its summarizer")
	}
	for _, w := range domain.Workloads {
		if pool.modelFor(w) != first {
			t.Errorf("%s should be back on the shared model", w)
		}
	}
	if pool.Models() != 1 {
		t.Errorf("%d models still in the pool, want 1", pool.Models())
	}
}
