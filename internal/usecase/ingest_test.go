package usecase_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

func TestBuildEventFromPostToolUseFailure(t *testing.T) {
	payload := []byte(`{
		"session_id": "abc-123",
		"hook_event_name": "PostToolUseFailure",
		"tool_name": "Edit",
		"tool_input": {"file_path": "auth/token.go"}
	}`)

	got, err := usecase.BuildEvent(domain.KindEdit, payload, "id1", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("BuildEvent: %v", err)
	}

	if got.Kind != domain.KindEdit {
		t.Errorf("Kind = %q, want edit", got.Kind)
	}
	if !got.Failed {
		t.Error("Failed = false, want true for a PostToolUseFailure payload")
	}
	if len(got.Files) != 1 || got.Files[0] != "auth/token.go" {
		t.Errorf("Files = %v, want [auth/token.go]", got.Files)
	}
}

func TestBuildEventFromPostToolBatch(t *testing.T) {
	payload := []byte(`{"session_id": "abc-123", "hook_event_name": "PostToolBatch"}`)

	got, err := usecase.BuildEvent(domain.KindBatchEnd, payload, "id1", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("BuildEvent: %v", err)
	}

	if got.Kind != domain.KindBatchEnd {
		t.Errorf("Kind = %q, want batch_end", got.Kind)
	}
	if got.Tool != "PostToolBatch" {
		t.Errorf("Tool = %q, want fallback to hook event name PostToolBatch", got.Tool)
	}
	if len(got.Files) != 0 {
		t.Errorf("Files = %v, want none", got.Files)
	}
}

func TestBuildEventFromUserPromptSubmit(t *testing.T) {
	payload := []byte(`{
		"session_id": "abc-123",
		"hook_event_name": "UserPromptSubmit",
		"prompt": "Add token validation to the auth package."
	}`)

	got, err := usecase.BuildEvent(domain.KindPrompt, payload, "id1", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("BuildEvent: %v", err)
	}

	if got.Kind != domain.KindPrompt {
		t.Errorf("Kind = %q, want prompt", got.Kind)
	}
	if got.StatedIntent != "Add token validation to the auth package." {
		t.Errorf("StatedIntent = %q, want the prompt text", got.StatedIntent)
	}
	if len(got.Files) != 0 {
		t.Errorf("Files = %v, want none", got.Files)
	}
}

func TestBuildEventFromPostToolUseEdit(t *testing.T) {
	payload := []byte(`{
		"session_id": "abc-123",
		"hook_event_name": "PostToolUse",
		"tool_name": "Edit",
		"tool_input": {"file_path": "auth/token.go"}
	}`)
	ts := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	got, err := usecase.BuildEvent(domain.KindEdit, payload, "01ULIDULIDULIDULIDULID0001", ts)
	if err != nil {
		t.Fatalf("BuildEvent: %v", err)
	}

	if got.ID != "01ULIDULIDULIDULIDULID0001" {
		t.Errorf("ID = %q", got.ID)
	}
	if !got.TS.Equal(ts) {
		t.Errorf("TS = %v, want %v", got.TS, ts)
	}
	if got.SessionID != "abc-123" {
		t.Errorf("SessionID = %q, want abc-123", got.SessionID)
	}
	if got.Kind != domain.KindEdit {
		t.Errorf("Kind = %q, want edit", got.Kind)
	}
	if got.Tool != "Edit" {
		t.Errorf("Tool = %q, want Edit", got.Tool)
	}
	if len(got.Files) != 1 || got.Files[0] != "auth/token.go" {
		t.Errorf("Files = %v, want [auth/token.go]", got.Files)
	}
	if !json.Valid(got.Raw) || len(got.Raw) == 0 {
		t.Errorf("Raw should preserve the payload, got %s", got.Raw)
	}
}
