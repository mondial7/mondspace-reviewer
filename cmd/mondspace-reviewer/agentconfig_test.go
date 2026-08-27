package main

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

func TestConfigFileFillsInWhatWasNotAskedFor(t *testing.T) {
	stored := domain.AgentConfig{Endpoint: "http://box:1234/v1", Model: "qwen/qwen3.5-9b"}

	got := resolveAgent(stored, "", "", nil)

	if got.Endpoint != "http://box:1234/v1" || got.Model != "qwen/qwen3.5-9b" {
		t.Errorf("got %+v, want the stored configuration", got)
	}
}

func TestAFlagBeatsTheStoredConfiguration(t *testing.T) {
	// Someone passing --model on the command line means it for this run, whatever
	// the file says, and must not have to edit the file to get it.
	stored := domain.AgentConfig{Endpoint: "http://box:1234/v1", Model: "stored-model"}

	got := resolveAgent(stored, "http://other:1234/v1", "asked-model",
		map[string]bool{"summarizer-url": true, "model": true})

	if got.Endpoint != "http://other:1234/v1" || got.Model != "asked-model" {
		t.Errorf("got %+v, want the flags to win", got)
	}
}

func TestAFlagLeftAtItsDefaultDoesNotOverrideTheFile(t *testing.T) {
	// Go's flag package cannot tell a default from an explicit value, so the
	// caller reports which flags were actually set. Without that, every run would
	// silently override the stored endpoint with the built-in default.
	stored := domain.AgentConfig{Endpoint: "http://box:1234/v1", Model: "stored-model"}

	got := resolveAgent(stored, defaultSummarizerURL, defaultModel, nil)

	if got.Endpoint != "http://box:1234/v1" || got.Model != "stored-model" {
		t.Errorf("got %+v, want the file to stand", got)
	}
}

func TestDefaultsApplyWhenNothingIsConfigured(t *testing.T) {
	got := resolveAgent(domain.AgentConfig{}, defaultSummarizerURL, defaultModel, nil)

	if got.Endpoint != defaultSummarizerURL || got.Model != defaultModel {
		t.Errorf("got %+v, want the built-in defaults", got)
	}
}

func TestPartialConfigurationIsCompletedByDefaults(t *testing.T) {
	// Setting only the endpoint in the app must not blank the model.
	got := resolveAgent(domain.AgentConfig{Endpoint: "http://box:1234/v1"},
		defaultSummarizerURL, defaultModel, nil)

	if got.Endpoint != "http://box:1234/v1" {
		t.Errorf("Endpoint = %q", got.Endpoint)
	}
	if got.Model != defaultModel {
		t.Errorf("Model = %q, want the default to fill in", got.Model)
	}
}

func TestTheDefaultIsOneServerForEveryWorkload(t *testing.T) {
	// Measured, not assumed: the 4B instruct model has no thinking mode and
	// still gets narration's enum-constrained schema right every time, while a
	// second resident model on 24 GB made everything about four times slower
	// (ADR 0019). So the default is one server, and the split is available
	// rather than imposed.
	got := resolveWorkloads(
		resolveAgent(domain.AgentConfig{}, defaultSummarizerURL, defaultModel, nil),
		nil, nil)

	if got.Split() {
		t.Fatalf("got %+v, want one model answering everything", got)
	}
	for _, w := range domain.Workloads {
		if ref := got.For(w); ref.Endpoint != defaultSummarizerURL || ref.Model != defaultModel {
			t.Errorf("%s = %+v, want the default endpoint and model", w, ref)
		}
	}
}

func TestNamingYourOwnEndpointGivesYouOneServer(t *testing.T) {
	// The default split is a package deal. Someone who points msr at a single
	// llama-server must not silently keep a narration override pointing at a
	// port they never started — the failure would be one workload quietly
	// falling back to mechanical prose.
	cfg := resolveAgent(domain.AgentConfig{}, "http://127.0.0.1:8080/v1", defaultModel,
		map[string]bool{"summarizer-url": true})

	got := resolveWorkloads(cfg, nil, nil)

	if got.Split() {
		t.Errorf("got %+v, want every workload on the one endpoint named", got)
	}
}

func TestAWorkloadFlagOverridesEverything(t *testing.T) {
	stored := domain.AgentConfig{
		Endpoint: "http://a/v1", Model: "m",
		Overrides: map[domain.Workload]domain.ModelRef{
			domain.Narration: {Endpoint: "http://stored/v1", Model: "stored"},
		},
	}
	flags := map[domain.Workload]domain.ModelRef{
		domain.Narration: {Endpoint: "http://flag/v1", Model: "flagged"},
	}

	got := resolveWorkloads(stored, flags, map[string]bool{
		"narration-url": true, "narration-model": true,
	})

	if ref := got.For(domain.Narration); ref.Endpoint != "http://flag/v1" || ref.Model != "flagged" {
		t.Errorf("narration = %+v, want the flags to win", ref)
	}
}

func TestAStoredOverrideSurvivesWhenNoFlagIsPassed(t *testing.T) {
	// Configured once on the status page, it must not be undone by the next
	// launch quietly reinstating the default.
	stored := domain.AgentConfig{
		Endpoint: "http://a/v1", Model: "m",
		Overrides: map[domain.Workload]domain.ModelRef{
			domain.Ask: {Model: "the-one-i-chose"},
		},
	}

	got := resolveWorkloads(stored, nil, nil)

	if ref := got.For(domain.Ask); ref.Model != "the-one-i-chose" {
		t.Errorf("ask = %+v, want the stored override", ref)
	}
}
