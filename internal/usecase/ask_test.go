package usecase_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func askSession() domain.Session {
	return domain.Session{
		ID:     "s",
		Prompt: "add token validation",
		Units: []domain.Unit{
			{ID: "s-u001", Files: []string{"auth/token.go"}, Headline: domain.Headline{Text: "extracted validator", Why: "swap lib", WhySrc: domain.WhyStated}},
			{ID: "s-u002", Files: []string{"http/mw.go"}, Headline: domain.Headline{Text: "wired middleware", WhySrc: domain.WhyInferred}},
		},
		Notes: []domain.Note{
			{ID: "n1", UnitID: "s-u001", Kind: domain.NoteObjection, Text: "why an interface?"},
			{ID: "n2", UnitID: "s-u002", Kind: domain.NoteOK},
		},
	}
}

func TestBuildAskContextRecordsStatedIntent(t *testing.T) {
	sess := askSession()

	// s-u001 has a stated why; s-u002 is inferred.
	if ctx := usecase.BuildAskContext(domain.AskUnit, sess, sess.Units[0], domain.Diff{}); !ctx.HasStated {
		t.Error("HasStated should be true for a unit with a stated intent")
	}
	if ctx := usecase.BuildAskContext(domain.AskUnit, sess, sess.Units[1], domain.Diff{}); ctx.HasStated {
		t.Error("HasStated should be false for an inferred-only unit")
	}
}

func TestBuildAskContextSessionScope(t *testing.T) {
	sess := askSession()

	ctx := usecase.BuildAskContext(domain.AskSession, sess, sess.Units[0], domain.Diff{Text: "ignored"})

	if ctx.Scope != domain.AskSession {
		t.Errorf("Scope = %q, want session", ctx.Scope)
	}
	if len(ctx.Units) != 2 {
		t.Errorf("Units = %d, want all 2", len(ctx.Units))
	}
	if len(ctx.Notes) != 2 {
		t.Errorf("Notes = %d, want all 2", len(ctx.Notes))
	}
	if ctx.Diff.Text != "" {
		t.Errorf("session scope must carry no diff, got %q", ctx.Diff.Text)
	}
	if ctx.Prompt != "add token validation" {
		t.Errorf("Prompt = %q", ctx.Prompt)
	}
}

func TestBuildAskContextUnitScope(t *testing.T) {
	sess := askSession()
	diff := domain.Diff{Text: "+type Validator interface{}\n"}

	ctx := usecase.BuildAskContext(domain.AskUnit, sess, sess.Units[0], diff)

	if ctx.Scope != domain.AskUnit {
		t.Errorf("Scope = %q, want unit", ctx.Scope)
	}
	if ctx.Prompt != "add token validation" {
		t.Errorf("Prompt = %q", ctx.Prompt)
	}
	if len(ctx.Units) != 1 || ctx.Units[0].ID != "s-u001" {
		t.Errorf("Units = %+v, want just s-u001", ctx.Units)
	}
	if ctx.Diff.Text != diff.Text {
		t.Errorf("Diff = %q, want the current unit's diff", ctx.Diff.Text)
	}
	if len(ctx.Notes) != 1 || ctx.Notes[0].ID != "n1" {
		t.Errorf("Notes = %+v, want only the current unit's notes", ctx.Notes)
	}
}
