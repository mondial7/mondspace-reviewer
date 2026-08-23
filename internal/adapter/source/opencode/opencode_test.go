package opencode_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/source/opencode"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

func TestDecodeMapsToolEditToKindEdit(t *testing.T) {
	line := `{"id":"evt1","sessionId":"ses1","timestamp":"2026-08-23T10:00:00Z","type":"tool.edit","tool":"edit","files":["auth/token.go"],"reasoning":"extract validation behind an interface"}`

	e, ok := opencode.Decode(line)
	if !ok {
		t.Fatalf("Decode returned ok=false for a well-formed tool.edit line")
	}

	if e.ID != "evt1" {
		t.Errorf("ID = %q, want evt1", e.ID)
	}
	if e.SessionID != "ses1" {
		t.Errorf("SessionID = %q, want ses1", e.SessionID)
	}
	if e.Kind != domain.KindEdit {
		t.Errorf("Kind = %q, want edit", e.Kind)
	}
	if len(e.Files) != 1 || e.Files[0] != "auth/token.go" {
		t.Errorf("Files = %v, want [auth/token.go]", e.Files)
	}
	if e.StatedIntent != "extract validation behind an interface" {
		t.Errorf("StatedIntent = %q, want the reasoning text", e.StatedIntent)
	}
}

func TestDecodeMapsEventTypes(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantOK     bool
		wantKind   domain.Kind
		wantFiles  []string
		wantIntent string
		wantTool   string
		wantFailed bool
	}{
		{
			name:       "tool.write",
			line:       `{"id":"e2","sessionId":"s","type":"tool.write","tool":"write","files":["auth/port.go"],"reasoning":"new port"}`,
			wantOK:     true,
			wantKind:   domain.KindWrite,
			wantFiles:  []string{"auth/port.go"},
			wantIntent: "new port",
		},
		{
			name:       "user.prompt",
			line:       `{"id":"e3","sessionId":"s","type":"user.prompt","text":"add token validation"}`,
			wantOK:     true,
			wantKind:   domain.KindPrompt,
			wantIntent: "add token validation",
		},
		{
			name:     "tool.bash success",
			line:     `{"id":"e4","sessionId":"s","type":"tool.bash","command":"go test ./...","exitCode":0}`,
			wantOK:   true,
			wantKind: domain.KindBash,
			wantTool: "go test ./...",
		},
		{
			name:       "tool.bash failure",
			line:       `{"id":"e5","sessionId":"s","type":"tool.bash","command":"go test ./...","exitCode":1}`,
			wantOK:     true,
			wantKind:   domain.KindBash,
			wantTool:   "go test ./...",
			wantFailed: true,
		},
		{
			name:     "step.finish",
			line:     `{"id":"e6","sessionId":"s","type":"step.finish"}`,
			wantOK:   true,
			wantKind: domain.KindBatchEnd,
		},
		{
			name:   "unknown type is skipped",
			line:   `{"id":"e7","sessionId":"s","type":"session.idle"}`,
			wantOK: false,
		},
		{
			name:   "malformed JSON is skipped",
			line:   `{ not json`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, ok := opencode.Decode(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if e.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", e.Kind, tt.wantKind)
			}
			if tt.wantFiles != nil && (len(e.Files) != len(tt.wantFiles) || e.Files[0] != tt.wantFiles[0]) {
				t.Errorf("Files = %v, want %v", e.Files, tt.wantFiles)
			}
			if e.StatedIntent != tt.wantIntent {
				t.Errorf("StatedIntent = %q, want %q", e.StatedIntent, tt.wantIntent)
			}
			if tt.wantTool != "" && e.Tool != tt.wantTool {
				t.Errorf("Tool = %q, want %q", e.Tool, tt.wantTool)
			}
			if e.Failed != tt.wantFailed {
				t.Errorf("Failed = %v, want %v", e.Failed, tt.wantFailed)
			}
		})
	}
}
