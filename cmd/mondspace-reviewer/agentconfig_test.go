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
