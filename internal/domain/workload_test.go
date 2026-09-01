package domain_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

func TestOneServerNeedsNoPerWorkloadSettings(t *testing.T) {
	// The common case stays the simple one: name an endpoint and a model, and
	// every workload uses them. Splitting across two servers is a choice, not
	// something the configuration makes you express.
	c := domain.AgentConfig{Endpoint: "http://127.0.0.1:8080/v1", Model: "qwen"}

	for _, w := range domain.Workloads {
		got := c.For(w)
		if got.Endpoint != "http://127.0.0.1:8080/v1" || got.Model != "qwen" {
			t.Errorf("%s = %+v, want the shared endpoint and model", w, got)
		}
	}
}

func TestAWorkloadCanBeSentSomewhereElse(t *testing.T) {
	// The point of the split: narration goes to the bigger model on its own
	// port, everything else to the small fast one.
	c := domain.AgentConfig{
		Endpoint: "http://127.0.0.1:8081/v1", Model: "qwen3-4b",
		Overrides: map[domain.Workload]domain.ModelRef{
			domain.Narration: {Endpoint: "http://127.0.0.1:8082/v1", Model: "qwen3.5-9b"},
		},
	}

	if got := c.For(domain.Narration); got.Endpoint != "http://127.0.0.1:8082/v1" || got.Model != "qwen3.5-9b" {
		t.Errorf("narration = %+v, want the 9B on 8082", got)
	}
	for _, w := range []domain.Workload{domain.Describe, domain.Ask} {
		if got := c.For(w); got.Endpoint != "http://127.0.0.1:8081/v1" || got.Model != "qwen3-4b" {
			t.Errorf("%s = %+v, want the shared 4B on 8081", w, got)
		}
	}
}

func TestAnOverrideFallsBackFieldByField(t *testing.T) {
	// Two models served by one llama-server is a real arrangement: same port,
	// different model name. Requiring the endpoint to be repeated to change the
	// model would invite it being repeated wrongly.
	c := domain.AgentConfig{
		Endpoint: "http://127.0.0.1:8080/v1", Model: "small",
		Overrides: map[domain.Workload]domain.ModelRef{
			domain.Narration: {Model: "big"},
			domain.Ask:       {Endpoint: "http://127.0.0.1:9999/v1"},
		},
	}

	if got := c.For(domain.Narration); got.Endpoint != "http://127.0.0.1:8080/v1" || got.Model != "big" {
		t.Errorf("narration = %+v, want the shared endpoint with the big model", got)
	}
	if got := c.For(domain.Ask); got.Endpoint != "http://127.0.0.1:9999/v1" || got.Model != "small" {
		t.Errorf("ask = %+v, want the other endpoint with the shared model", got)
	}
}

func TestAnUnknownWorkloadStillGetsTheDefault(t *testing.T) {
	// Better a working model than a silent nil: a workload added later must not
	// take the reviewer's assistant offline until the config catches up.
	c := domain.AgentConfig{Endpoint: "http://e/v1", Model: "m"}
	if got := c.For(domain.Workload("something-new")); got.Model != "m" {
		t.Errorf("got %+v, want the default", got)
	}
}

func TestSplitSaysWhetherMoreThanOneServerIsInPlay(t *testing.T) {
	// The status page has to be honest about how many models are actually
	// answering, and it is the kind of thing that is easy to get wrong by eye.
	one := domain.AgentConfig{Endpoint: "http://a/v1", Model: "m"}
	if one.Split() {
		t.Error("one endpoint and one model is not a split")
	}

	sameEverything := domain.AgentConfig{
		Endpoint: "http://a/v1", Model: "m",
		Overrides: map[domain.Workload]domain.ModelRef{
			domain.Narration: {Endpoint: "http://a/v1", Model: "m"},
		},
	}
	if sameEverything.Split() {
		t.Error("an override that changes nothing is not a split")
	}

	real := domain.AgentConfig{
		Endpoint: "http://a/v1", Model: "m",
		Overrides: map[domain.Workload]domain.ModelRef{
			domain.Narration: {Model: "other"},
		},
	}
	if !real.Split() {
		t.Error("a workload on a different model is a split")
	}
}

// ── The routing table (ADR 0039) ────────────────────────────────────────────

func TestEveryJobIsRoutedSomewhere(t *testing.T) {
	// The table is the answer to "where does this go", so a job missing from it
	// is a question with no answer rather than a sensible default.
	want := []domain.Job{
		domain.JobStory, domain.JobSecurity, domain.JobBreaking,
		domain.JobGroup, domain.JobFile, domain.JobAsk,
	}
	for _, j := range want {
		r, ok := domain.RouteFor(j)
		if !ok {
			t.Errorf("%s is not in the routing table", j)
			continue
		}
		if r.Engine == "" || r.Workload == "" || r.Why == "" {
			t.Errorf("%s: every row needs an engine, a workload and a reason: %+v", j, r)
		}
	}
}

func TestJudgementGoesToTheCLIAndVolumeStaysLocal(t *testing.T) {
	// The whole shape of the table in one assertion. If this changes, it should
	// be because somebody decided to change it.
	for _, j := range []domain.Job{domain.JobStory, domain.JobSecurity, domain.JobBreaking} {
		r, _ := domain.RouteFor(j)
		if r.Engine != domain.EngineCLI {
			t.Errorf("%s goes to %s, want the cli", j, r.Engine)
		}
		if r.Fallback != domain.EngineLocal {
			t.Errorf("%s falls back to %q, want the local model — a review must never block", j, r.Fallback)
		}
	}
	for _, j := range []domain.Job{domain.JobGroup, domain.JobFile, domain.JobAsk} {
		r, _ := domain.RouteFor(j)
		if r.Engine != domain.EngineLocal {
			t.Errorf("%s goes to %s, want the local model", j, r.Engine)
		}
		if r.Fallback != "" {
			t.Errorf("%s falls back to %q; a paid call per file is a bill nobody asked for", j, r.Fallback)
		}
	}
}

func TestEveryJobOnOneWorkloadAgreesOnItsEngine(t *testing.T) {
	// A workload is the unit a summarizer is built for, so two jobs on one
	// workload wanting different engines is not something the runtime can
	// express — it would silently answer one of them from the wrong place.
	seen := map[domain.Workload]domain.Engine{}
	for _, r := range domain.Routing {
		if was, known := seen[r.Workload]; known && was != r.Engine {
			t.Errorf("%s is routed to both %s and %s; the workload has to split first",
				r.Workload, was, r.Engine)
		}
		seen[r.Workload] = r.Engine
	}
	for w, engine := range seen {
		if domain.EngineOn(w) != engine {
			t.Errorf("EngineOn(%s) = %s, want %s", w, domain.EngineOn(w), engine)
		}
	}
}
