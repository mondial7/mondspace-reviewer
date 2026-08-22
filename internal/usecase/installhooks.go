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

func cmd(kind string) []hookEntry {
	return []hookEntry{{Hooks: []hookCommand{{Type: "command", Command: "msr ingest --kind=" + kind}}}}
}

// msrHooks is the fixed set of hooks the reviewer installs (SPEC §8).
func msrHooks() map[string]any {
	return map[string]any{
		"UserPromptSubmit": cmd("prompt"),
		"PostToolUse": []hookEntry{{
			Matcher: "Write|Edit|MultiEdit",
			Hooks:   []hookCommand{{Type: "command", Command: "msr ingest --kind=edit"}},
		}},
		"PostToolUseFailure": []hookEntry{{
			Matcher: "Write|Edit|MultiEdit",
			Hooks:   []hookCommand{{Type: "command", Command: "msr ingest --kind=edit"}},
		}},
		"PostToolBatch": cmd("batch_end"),
	}
}

// InstallHooks merges the reviewer's hooks into an existing settings.json
// (nil/empty means a fresh file), returning the indented JSON to write.
func InstallHooks(existing []byte) ([]byte, error) {
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
	for event, entry := range msrHooks() {
		hooks[event] = entry
	}
	settings["hooks"] = hooks

	return json.MarshalIndent(settings, "", "  ")
}
