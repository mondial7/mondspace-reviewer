// Package opencode is an EventSource that tails a JSONL log of OpenCode agent
// events and maps them onto domain.Event, so the domain never knows which
// agent produced a session (SPEC §8).
//
// # Payload shape
//
// mondspace-reviewer has no dependency on a live OpenCode install, so this
// adapter defines and documents its own tolerant JSONL shape: one JSON object
// per line, oldest first.
//
//	{
//	  "id":        "evt_01",                    // stable, unique per event
//	  "sessionId": "ses_abc123",
//	  "timestamp": "2026-08-23T10:00:00Z",       // RFC 3339
//	  "type":      "tool.edit",                  // see mapping table below
//	  "tool":      "edit",                       // raw tool/part name, informational
//	  "files":     ["auth/token.go"],            // paths touched (tool.edit / tool.write)
//	  "reasoning": "extract validation behind an interface", // agent's own words, optional
//	  "text":      "add token validation ...",   // prompt text (type "user.prompt")
//	  "command":   "go test ./...",              // shell command (type "tool.bash")
//	  "exitCode":  0                             // present only for "tool.bash"
//	}
//
// # Mapping to domain.Kind
//
//	OpenCode "type"   domain.Kind    notes
//	-------------------------------------------------------------------------
//	user.prompt       KindPrompt     StatedIntent = text
//	tool.edit         KindEdit       Files = files; StatedIntent = reasoning
//	tool.write        KindWrite      Files = files; StatedIntent = reasoning
//	tool.bash         KindBash       Tool = command; Failed = exitCode != 0
//	step.finish       KindBatchEnd   batch boundary, mirrors Claude Code's PostToolBatch
//
// Any other "type" — a future OpenCode event this adapter does not yet know
// about — is skipped rather than fatal, exactly like a malformed line:
// OpenCode's event vocabulary is expected to grow, and a review tool must
// never crash a live session over an event it doesn't recognise yet.
package opencode

import (
	"encoding/json"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// payload is the OpenCode JSONL event shape this adapter parses. It stays a
// flat, tolerant view: unknown fields are ignored by encoding/json, and an
// unknown Type is rejected by Decode rather than the JSON parse itself.
type payload struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Tool      string    `json:"tool"`
	Files     []string  `json:"files"`
	Reasoning string    `json:"reasoning"`
	Text      string    `json:"text"`
	Command   string    `json:"command"`
	ExitCode  *int      `json:"exitCode"`
}

// Decode parses one JSONL line of the OpenCode payload shape and maps it to a
// domain.Event. It returns ok=false for malformed JSON or an unrecognised
// "type", so callers can skip the line rather than fail.
func Decode(line string) (domain.Event, bool) {
	var p payload
	if err := json.Unmarshal([]byte(line), &p); err != nil {
		return domain.Event{}, false
	}

	e := domain.Event{
		ID:        p.ID,
		SessionID: p.SessionID,
		TS:        p.Timestamp,
		Source:    "opencode",
		Raw:       json.RawMessage(line),
	}

	switch p.Type {
	case "tool.edit":
		e.Kind = domain.KindEdit
		e.Tool = p.Tool
		e.Files = p.Files
		e.StatedIntent = p.Reasoning
	case "tool.write":
		e.Kind = domain.KindWrite
		e.Tool = p.Tool
		e.Files = p.Files
		e.StatedIntent = p.Reasoning
	case "tool.bash":
		e.Kind = domain.KindBash
		e.Tool = p.Command
		e.Failed = p.ExitCode != nil && *p.ExitCode != 0
	case "user.prompt":
		e.Kind = domain.KindPrompt
		e.StatedIntent = p.Text
	case "step.finish":
		e.Kind = domain.KindBatchEnd
	default:
		return domain.Event{}, false
	}

	return e, true
}
