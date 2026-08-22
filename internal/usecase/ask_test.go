package usecase_test

import (
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
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
