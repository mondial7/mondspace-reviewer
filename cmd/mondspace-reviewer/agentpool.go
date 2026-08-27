package main

import (
	"context"
	"sync"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/summarizer/switchable"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

// buildFunc makes a summarizer for one model. It is a parameter rather than a
// call so the pool's sharing can be tested without standing up HTTP servers.
type buildFunc func(domain.ModelRef) port.Summarizer

// agentPool routes each workload to the model that answers for it (ADR 0019).
//
// Two things have to be true at once, and they pull in opposite directions:
//
//   - Each workload needs a *stable* handle. Everything downstream captures its
//     summarizer once, at wiring time, so handing out a new one when the
//     configuration changes would leave every caller holding the old model —
//     which is exactly what switchable exists to prevent.
//   - Distinct *models* must be built once, not once per workload. Three
//     workloads on one model is one connection, one liveness answer and one
//     usage total; building three would triple the probing and make /status
//     report the same spend three times over.
//
// So there is a permanent switchable per workload, and the adapters behind them
// are shared and deduplicated by model.
type agentPool struct {
	mu sync.RWMutex
	// handles are the stable per-workload facades. Created once, never replaced.
	handles map[domain.Workload]*switchable.Summarizer
	// adapters are the real summarizers, one per distinct model.
	adapters map[domain.ModelRef]port.Summarizer
}

func newAgentPool(cfg domain.AgentConfig, build buildFunc) *agentPool {
	p := &agentPool{
		handles:  map[domain.Workload]*switchable.Summarizer{},
		adapters: map[domain.ModelRef]port.Summarizer{},
	}
	p.apply(cfg, build)
	return p
}

// Reconfigure points the workloads at whatever the new settings describe.
//
// Only models that are not already in the pool are built: swapping the
// narration model must not tear down the describe connection, which would reset
// its usage and drop a warm connection for a setting that did not change.
func (p *agentPool) Reconfigure(cfg domain.AgentConfig, build buildFunc) {
	p.apply(cfg, build)
}

func (p *agentPool) apply(cfg domain.AgentConfig, build buildFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()

	wanted := map[domain.ModelRef]bool{}
	for _, w := range domain.Workloads {
		ref := cfg.For(w)
		wanted[ref] = true

		adapter, have := p.adapters[ref]
		if !have {
			adapter = build(ref)
			p.adapters[ref] = adapter
		}
		if h, ok := p.handles[w]; ok {
			h.Swap(adapter)
		} else {
			p.handles[w] = switchable.New(adapter)
		}
	}

	// A model nothing points at any more is dropped, so going back to one
	// server does not leave a second connection being probed for nobody.
	for ref := range p.adapters {
		if !wanted[ref] {
			delete(p.adapters, ref)
		}
	}
}

// For is the summarizer that answers a workload. The handle it returns is
// permanent: callers capture it once and keep getting whichever model the
// configuration currently names.
func (p *agentPool) For(w domain.Workload) port.Summarizer {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if h, ok := p.handles[w]; ok {
		return h
	}
	// A workload the configuration has not heard of must not take the assistant
	// offline; any model beats none.
	for _, h := range p.handles {
		return h
	}
	return nil
}

// modelFor is the model actually answering a workload, for tests and /status.
func (p *agentPool) modelFor(w domain.Workload) port.Summarizer {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if h, ok := p.handles[w]; ok {
		return h.Current()
	}
	return nil
}

// Models is how many distinct models are in the pool.
func (p *agentPool) Models() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.adapters)
}

// Usage is what the assistant has cost across every model. "What has this cost
// me" is not a per-server question, and reporting one of two models would
// understate it by however much the other did.
func (p *agentPool) Usage() port.TokenUsage {
	p.mu.RLock()
	adapters := make([]port.Summarizer, 0, len(p.adapters))
	for _, a := range p.adapters {
		adapters = append(adapters, a)
	}
	p.mu.RUnlock()

	var total port.TokenUsage
	for _, a := range adapters {
		reporter, ok := a.(port.UsageReporter)
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

// Ping reports whether every model in the pool answers. One being down is the
// whole assistant being degraded, so the first failure is the answer.
func (p *agentPool) Ping(ctx context.Context) error {
	p.mu.RLock()
	adapters := make([]port.Summarizer, 0, len(p.adapters))
	for _, a := range p.adapters {
		adapters = append(adapters, a)
	}
	p.mu.RUnlock()

	for _, a := range adapters {
		pinger, ok := a.(port.Pinger)
		if !ok {
			continue
		}
		if err := pinger.Ping(ctx); err != nil {
			return err
		}
	}
	return nil
}
