package mcp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/mcp"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// space is a workspace held in memory: the tools are about what msr knows, and
// where it is kept is not part of what is being tested.
type space struct {
	open   mcp.Review
	openTo error
	all    []mcp.Review
}

func (s space) Open() (mcp.Review, error)  { return s.open, s.openTo }
func (s space) All() ([]mcp.Review, error) { return s.all, nil }

func note(kind domain.NoteKind, file, text string) domain.Note {
	return domain.Note{
		ID: text, Kind: kind, File: file, Text: text,
		TS: time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC),
	}
}

// tool runs one of the msr tools by name and returns the text it produced.
func tool(t *testing.T, w mcp.Workspace, name string, args map[string]any) string {
	t.Helper()
	for _, candidate := range mcp.Tools(w) {
		if candidate.Name == name {
			text, err := candidate.Call(t.Context(), args)
			if err != nil {
				return "error: " + err.Error()
			}
			return text
		}
	}
	t.Fatalf("no tool named %q; have %v", name, names(mcp.Tools(w)))
	return ""
}

func names(tools []mcp.Tool) []string {
	var out []string
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

func TestFeedbackIsWhatAHumanWroteAndStillWants(t *testing.T) {
	// The whole point of the surface: an agent asking what to fix gets the
	// reviewer's outstanding asks, not their approvals and not the model's
	// guesses (ADR 0031).
	w := space{open: mcp.Review{
		ID: "abc123", Title: "add retries", Ref: "abc123",
		Notes: []domain.Note{
			note(domain.NoteObjection, "http.go", "this retries forever"),
			note(domain.NoteOK, "http.go", "reads fine"),
			note(domain.NoteQuestion, "main.go", "why the extra goroutine?"),
			note(domain.NoteNote, "main.go", "thinking aloud"),
		},
		Analyses: []domain.Analysis{{
			Kind: "security", At: time.Now(), Verdict: "one thing",
			Findings: []domain.Finding{{File: "http.go", Note: "token in a log line"}},
		}},
	}}

	got := tool(t, w, "review_feedback", nil)

	for _, want := range []string{"this retries forever", "why the extra goroutine?"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"reads fine", "thinking aloud", "token in a log line"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("should not carry %q:\n%s", unwanted, got)
		}
	}
}
