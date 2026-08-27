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
