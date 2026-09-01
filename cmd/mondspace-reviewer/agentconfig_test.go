package main

import (
	"github.com/mondial7/mondspace-reviewer/internal/adapter/summarizer/claudecli"
	"testing"
	"time"

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

func TestWithNoClaudeCodeEverythingIsOneLocalServer(t *testing.T) {
	// Measured, not assumed: the 4B instruct model has no thinking mode and
	// still gets narration's enum-constrained schema right every time, while a
	// second resident model on 24 GB made everything about four times slower
	// (ADR 0019). On a machine with no second engine, one server answers
	// everything and always has.
	t.Setenv("MSR_CLAUDE_CLI", "0")
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

func TestWithClaudeCodeTheJudgementJobsGoToIt(t *testing.T) {
	// The routing table, read through the configuration (ADR 0039). Judgement
	// to the CLI; volume stays local, because a paid call per changed file is a
	// bill nobody asked for.
	cfg := resolveWorkloads(
		resolveAgent(domain.AgentConfig{CLI: domain.ModelRef{Endpoint: domain.ClaudeCLIEndpoint}},
			defaultSummarizerURL, defaultModel, nil),
		nil, nil)

	if ref := cfg.For(domain.Narration); ref.Endpoint != domain.ClaudeCLIEndpoint {
		t.Errorf("narration = %+v, want the cli", ref)
	}
	if ref := cfg.For(domain.Narration); ref.Model != "" {
		t.Errorf("narration model = %q, want the cli's own default", ref.Model)
	}
	for _, w := range []domain.Workload{domain.Describe, domain.Ask} {
		if ref := cfg.For(w); ref.Endpoint != defaultSummarizerURL {
			t.Errorf("%s = %+v, want the local model", w, ref)
		}
	}
}

func TestTurningTheCLIOffPutsEverythingBackOnTheLocalModel(t *testing.T) {
	// It costs money on somebody's subscription, so there has to be one switch
	// that stops it, and it has to beat a stored setting.
	t.Setenv("MSR_CLAUDE_CLI", "0")
	cfg := resolveAgent(domain.AgentConfig{CLI: domain.ModelRef{Endpoint: domain.ClaudeCLIEndpoint}},
		defaultSummarizerURL, defaultModel, nil)

	if cfg.UsesCLI() {
		t.Fatalf("got %+v, want the cli out of the arrangement entirely", cfg)
	}
	if ref := cfg.For(domain.Narration); ref.Endpoint != defaultSummarizerURL {
		t.Errorf("narration = %+v, want the local model", ref)
	}
}

func TestNamingYourOwnEndpointGivesYouOneServer(t *testing.T) {
	// The default split is a package deal. Someone who points msr at a single
	// llama-server must not silently keep a narration override pointing at a
	// port they never started — the failure would be one workload quietly
	// falling back to mechanical prose.
	t.Setenv("MSR_CLAUDE_CLI", "0")
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

func TestTheRemoteWatchCanBeTurnedOnAndOffWhileRunning(t *testing.T) {
	// The watcher captured the flag at start-up, so changing it meant a
	// restart. A setting you can only change by restarting is not a setting on
	// a status page (ADR 0026).
	w := newRemoteWatch(false, 2*time.Minute)

	if on, _ := w.Get(); on {
		t.Error("it starts off when it was not asked for")
	}

	w.Set(true, 30*time.Second)
	on, every := w.Get()
	if !on || every != 30*time.Second {
		t.Errorf("Get = %v, %s; want on every 30s", on, every)
	}

	w.Set(false, 30*time.Second)
	if on, _ := w.Get(); on {
		t.Error("turning it off should turn it off")
	}
}

func TestAnAbsurdFetchIntervalIsBroughtBackToSomethingSane(t *testing.T) {
	// This is a network call against someone else's server. A typo in a form
	// field must not turn msr into a hammer.
	w := newRemoteWatch(true, time.Second)
	if _, every := w.Get(); every < minFetchEvery {
		t.Errorf("every = %s, want at least %s", every, minFetchEvery)
	}

	w.Set(true, 0)
	if _, every := w.Get(); every < minFetchEvery {
		t.Errorf("every = %s, want a floor rather than a busy loop", every)
	}
}

func TestShowingEverythingOnlyRebuildsWhenItActuallyChanges(t *testing.T) {
	// The cockpit tells the loader on *every* request whether the reviewer
	// wants the hidden files. Acting on that unconditionally threw away the
	// review cache each time, so a 600-file review was rebuilt from git on
	// every page load and every live refresh — 31 seconds a piece.
	rebuilds := 0
	rebuild := func() { rebuilds++ }

	setShowAllWith(false, rebuild) // same as the default
	if rebuilds != 0 {
		t.Errorf("rebuilt %d times for no change", rebuilds)
	}

	setShowAllWith(true, rebuild)
	if rebuilds != 1 {
		t.Errorf("rebuilt %d times, want once when it changed", rebuilds)
	}

	setShowAllWith(true, rebuild)
	if rebuilds != 1 {
		t.Errorf("rebuilt %d times, want no rebuild for the same value again", rebuilds)
	}

	setShowAllWith(false, rebuild)
	if rebuilds != 2 {
		t.Errorf("rebuilt %d times, want a rebuild when it changed back", rebuilds)
	}
}

func TestTheClaudeSchemeChoosesTheCliRatherThanAnEndpoint(t *testing.T) {
	// A reviewer picks the engine where they already pick the model, and the
	// endpoint field is where "which thing answers this" is said. A scheme is
	// enough to say it, and needs no second setting to fall out of step with.
	got := chooseSummarizer("claude://cli", "")

	if _, ok := got.(*claudecli.Summarizer); !ok {
		t.Fatalf("got %T, want the claude cli adapter", got)
	}
}

func TestAnOrdinaryEndpointIsUnaffected(t *testing.T) {
	if _, ok := chooseSummarizer("http://127.0.0.1:1/v1", "m").(*claudecli.Summarizer); ok {
		t.Error("an http endpoint should not reach the cli adapter")
	}
}
