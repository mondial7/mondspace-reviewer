package usecase_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestInstallHooksMergesWithoutClobbering(t *testing.T) {
	existing := []byte(`{
		"model": "opus",
		"permissions": {"allow": ["Bash"]},
		"hooks": {"PreToolUse": [{"hooks": [{"type": "command", "command": "echo hi"}]}]}
	}`)

	merged, err := usecase.InstallHooks(existing, "msr")
	if err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(merged, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["model"] != "opus" {
		t.Errorf("model key clobbered: %v", settings["model"])
	}
	if _, ok := settings["permissions"]; !ok {
		t.Errorf("permissions key dropped")
	}
	hooks := settings["hooks"].(map[string]any)
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Errorf("pre-existing PreToolUse hook dropped")
	}
	if _, ok := hooks["PostToolUse"]; !ok {
		t.Errorf("our PostToolUse hook not added")
	}
}

func TestInstallHooksWritesAllFourHooks(t *testing.T) {
	// Hooks run under /bin/sh, which has no aliases and a bare PATH — so the
	// command must be resolvable there, e.g. an absolute path to the binary.
	command := "/Users/me/go/bin/mondspace-reviewer"
	merged, err := usecase.InstallHooks(nil, command)
	if err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(merged, &settings); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("no hooks object in settings: %s", merged)
	}

	for _, event := range []string{"UserPromptSubmit", "PostToolUse", "PostToolUseFailure", "PostToolBatch"} {
		if _, present := hooks[event]; !present {
			t.Errorf("missing hook for %s", event)
		}
	}

	// PostToolUse must match the edit tools.
	if !strings.Contains(string(merged), "Write|Edit|MultiEdit") {
		t.Errorf("PostToolUse missing tool matcher:\n%s", merged)
	}
	// Every hook invokes the given command, not a bare (alias-only) name.
	for _, kind := range []string{"--kind=prompt", "--kind=edit", "--kind=batch_end"} {
		if !strings.Contains(string(merged), command+" ingest "+kind) {
			t.Errorf("missing command %q:\n%s", command+" ingest "+kind, merged)
		}
	}
}
