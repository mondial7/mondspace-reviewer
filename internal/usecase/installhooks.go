package usecase

import "encoding/json"

// hookEntry is one Claude Code hook registration: an optional tool matcher and
// the commands to run.
type hookEntry struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

func ingestCmd(command, kind string) []hookEntry {
	return []hookEntry{{Hooks: []hookCommand{{Type: "command", Command: command + " ingest --kind=" + kind}}}}
}

func ingestEditCmd(command string) []hookEntry {
	return []hookEntry{{
		Matcher: "Write|Edit|MultiEdit",
		Hooks:   []hookCommand{{Type: "command", Command: command + " ingest --kind=edit"}},
	}}
}

// msrHooks is the fixed set of hooks the reviewer installs (SPEC §8), each
// invoking the given command (an absolute binary path, resolvable under /bin/sh).
func msrHooks(command string) map[string]any {
	return map[string]any{
		"UserPromptSubmit":   ingestCmd(command, "prompt"),
		"PostToolUse":        ingestEditCmd(command),
		"PostToolUseFailure": ingestEditCmd(command),
		"PostToolBatch":      ingestCmd(command, "batch_end"),
	}
}

// InstallHooks merges the reviewer's hooks into an existing settings.json
// (nil/empty means a fresh file), returning the indented JSON to write. command
// is what each hook runs; because hooks execute under /bin/sh (no aliases, bare
// PATH), it should be an absolute path to the binary.
func InstallHooks(existing []byte, command string) ([]byte, error) {
	settings := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &settings); err != nil {
			return nil, err
		}
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for event, entry := range msrHooks(command) {
		hooks[event] = entry
	}
	settings["hooks"] = hooks

	return json.MarshalIndent(settings, "", "  ")
}
