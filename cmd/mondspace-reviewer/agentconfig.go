package main

import (
	"os"

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
	return out
}

// flagsSet reports which flags were actually passed, as opposed to left at their
// default.
func flagsSet(visit func(func(name string))) map[string]bool {
	set := map[string]bool{}
	visit(func(name string) { set[name] = true })
	return set
}
