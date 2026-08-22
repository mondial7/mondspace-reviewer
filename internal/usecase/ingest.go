package usecase

import (
	"encoding/json"
	"time"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

// hookPayload is the subset of an agent hook's JSON that ingestion reads. The
// domain must not know which agent produced it; this stays a flat, tolerant view.
type hookPayload struct {
	SessionID string `json:"session_id"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
	Prompt   string `json:"prompt"`
	HookName string `json:"hook_event_name"`
}

// BuildEvent maps one hook payload to a domain.Event for the given kind. The id
// and timestamp are injected so the function stays pure and testable.
func BuildEvent(kind domain.Kind, payload []byte, id string, ts time.Time) (domain.Event, error) {
	var p hookPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return domain.Event{}, err
	}

	e := domain.Event{
		ID:        id,
		SessionID: p.SessionID,
		TS:        ts,
		Kind:      kind,
		Tool:      p.ToolName,
		Raw:       json.RawMessage(payload),
	}
	if e.Tool == "" {
		e.Tool = p.HookName
	}
	if p.ToolInput.FilePath != "" {
		e.Files = []string{p.ToolInput.FilePath}
	}
	if kind == domain.KindPrompt {
		e.StatedIntent = p.Prompt
	}
	return e, nil
}
