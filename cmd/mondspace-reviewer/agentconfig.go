package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// resolveAgent decides how to reach the reviewer's model, in order of how
// deliberate each source is:
//
//	a flag actually passed  →  the environment  →  the stored config  →  defaults
//
// `set` names the flags the caller actually passed. Go's flag package cannot
// distinguish a default from an explicitly-typed identical value, and without
// that distinction every run would silently overwrite a stored endpoint with the
// built-in one.
func resolveAgent(stored domain.AgentConfig, urlFlag, modelFlag string, set map[string]bool) domain.AgentConfig {
	out := stored

	switch {
	case set["summarizer-url"]:
		out.Endpoint = urlFlag
	case os.Getenv("MSR_SUMMARIZER_URL") != "":
		out.Endpoint = os.Getenv("MSR_SUMMARIZER_URL")
	case out.Endpoint == "":
		out.Endpoint = defaultSummarizerURL
	}

	switch {
	case set["model"]:
		out.Model = modelFlag
	case os.Getenv("MSR_MODEL") != "":
		out.Model = os.Getenv("MSR_MODEL")
	case out.Model == "":
		out.Model = defaultModel
	}

	// The environment can only turn thinking off, never back on: a stored
	// preference is the more deliberate of the two.
	if os.Getenv("MSR_NO_THINKING") == "1" {
		out.NoThinking = true
	}
	out.CLI = resolveCLI(out.CLI)
	return out
}

// resolveCLI decides whether the Claude Code CLI is one of the engines.
//
// It is used when it is installed. That is a deliberate reversal of ADR 0035,
// which made it opt-in and typed by hand: the three readings a reviewer acts on
// are the three a 4B model gets wrong expensively, and a reviewer who has Claude
// Code on their machine should not have to discover a config field to stop being
// shown invented findings (ADR 0039).
//
// It costs money on somebody's subscription, so it is one environment variable
// to turn off and the settings page says exactly what it has spent.
func resolveCLI(stored domain.ModelRef) domain.ModelRef {
	if os.Getenv("MSR_CLAUDE_CLI") == "0" {
		return domain.ModelRef{}
	}
	if stored.Endpoint != "" {
		return stored
	}
	if !claudeCLIInstalled() {
		return domain.ModelRef{}
	}
	// No model name. The Model field belongs to the other engine, and the CLI's
	// own default is what a reviewer who typed nothing meant (ADR 0035).
	return domain.ModelRef{Endpoint: domain.ClaudeCLIEndpoint}
}

// claudeCLIInstalled reports whether there is a binary to run. One PATH lookup
// at start-up; the engine's actual health is a live question the settings page
// asks separately.
func claudeCLIInstalled() bool {
	bin := os.Getenv("MSR_CLAUDE_BIN")
	if strings.TrimSpace(bin) == "" {
		bin = "claude"
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

// flagsSet reports which flags were actually passed, as opposed to left at their
// default.
func flagsSet(visit func(func(name string))) map[string]bool {
	set := map[string]bool{}
	visit(func(name string) { set[name] = true })
	return set
}

// resolveWorkloads layers per-workload overrides onto the shared settings, in
// the same order of deliberateness resolveAgent uses:
//
//	a flag actually passed  →  the environment  →  the stored config  →  defaults
//
// `flags` holds only what was passed; `set` names which flags those were, since
// Go cannot tell an explicit value from a default.
func resolveWorkloads(cfg domain.AgentConfig, flags map[domain.Workload]domain.ModelRef,
	set map[string]bool) domain.AgentConfig {

	out := cfg
	out.Overrides = map[domain.Workload]domain.ModelRef{}
	for w, ref := range cfg.Overrides {
		out.Overrides[w] = ref
	}

	for _, w := range domain.Workloads {
		ref := out.Overrides[w]

		if set[string(w)+"-url"] {
			ref.Endpoint = flags[w].Endpoint
		} else if env := os.Getenv(envFor(w, "URL")); env != "" {
			ref.Endpoint = env
		}

		if set[string(w)+"-model"] {
			ref.Model = flags[w].Model
		} else if env := os.Getenv(envFor(w, "MODEL")); env != "" {
			ref.Model = env
		}

		if ref == (domain.ModelRef{}) {
			delete(out.Overrides, w)
			continue
		}
		out.Overrides[w] = ref
	}

	// No default override: one server answers everything unless someone asks
	// for otherwise. A default pointing at a second port nobody started would
	// leave exactly one workload quietly falling back to mechanical prose,
	// which is the failure hardest to notice from the page.
	if len(out.Overrides) == 0 {
		out.Overrides = nil
	}
	return out
}

// envFor is the environment variable for one workload's endpoint or model —
// MSR_NARRATION_URL, MSR_DESCRIBE_MODEL, and so on.
func envFor(w domain.Workload, field string) string {
	return "MSR_" + strings.ToUpper(string(w)) + "_" + field
}
