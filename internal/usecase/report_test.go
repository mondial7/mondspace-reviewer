package usecase_test

import (
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

func reportSession() domain.Session {
	return domain.Session{
		ID:     "s",
		Prompt: "add token validation",
		Units: []domain.Unit{
			{ID: "s-u001", Files: []string{"auth/token.go"}, Headline: domain.Headline{Text: "extracted validator", Why: "swap lib", WhySrc: domain.WhyStated}},
			{ID: "s-u002", Files: []string{"http/mw.go"}, Headline: domain.Headline{Text: "wired middleware", WhySrc: domain.WhyInferred}},
			{ID: "s-u003", Files: []string{"db/pool.go"}, Headline: domain.Headline{Text: "added retry", WhySrc: domain.WhyInferred}},
		},
		Notes: []domain.Note{
			{ID: "n1", UnitID: "s-u001", Kind: domain.NoteOK},
			{ID: "n2", UnitID: "s-u002", Kind: domain.NoteObjection, Text: "wrong layer"},
			{ID: "n3", UnitID: "s-u003", Kind: domain.NoteNote, Text: "fyi"},
		},
	}
}

func groupFor(r domain.Report, kind domain.NoteKind) (domain.NoteGroup, bool) {
	for _, g := range r.Groups {
		if g.Kind == kind {
			return g, true
		}
	}
	return domain.NoteGroup{}, false
}

func TestBuildReportOpenAgenda(t *testing.T) {
	sess := reportSession()
	sess.Notes = append(sess.Notes,
		domain.Note{ID: "n6", UnitID: "s-u001", Kind: domain.NoteQuestion, Text: "why an interface?"},
	)

	r := usecase.BuildReport(sess)

	// n2 (objection on u2) and n6 (question on u1) are both live and unresolved.
	if len(r.Agenda) != 2 {
		t.Fatalf("Agenda = %d, want 2 (the objection and the question)", len(r.Agenda))
	}
	kinds := map[domain.NoteKind]bool{}
	for _, it := range r.Agenda {
		kinds[it.NoteKind] = true
	}
	if !kinds[domain.NoteObjection] || !kinds[domain.NoteQuestion] {
		t.Errorf("Agenda kinds = %v, want objection and question", kinds)
	}
	if len(r.Superseded) != 0 {
		t.Errorf("Superseded = %d, want 0 (nothing superseded here)", len(r.Superseded))
	}
}

func TestBuildReportCollectsDebt(t *testing.T) {
	sess := reportSession()
	sess.Notes = append(sess.Notes,
		domain.Note{ID: "n4", UnitID: "s-u003", Kind: domain.NoteDebt, Text: "add a test for the retry"},
		domain.Note{ID: "n5", UnitID: "s-u001", Kind: domain.NoteDebt, Text: "document the interface"},
	)

	r := usecase.BuildReport(sess)

	if len(r.Debt) != 2 {
		t.Fatalf("Debt = %d items, want 2", len(r.Debt))
	}
	if r.Debt[0].NoteText != "add a test for the retry" || r.Debt[1].NoteText != "document the interface" {
		t.Errorf("Debt items = %+v, want both debt notes in order", r.Debt)
	}
}

func TestBuildReportGroupsByNoteKind(t *testing.T) {
	r := usecase.BuildReport(reportSession())

	ok, found := groupFor(r, domain.NoteOK)
	if !found || len(ok.Items) != 1 || ok.Items[0].UnitID != "s-u001" {
		t.Fatalf("ok group = %+v, want one item on s-u001", ok)
	}
	if ok.Items[0].Headline.Text != "extracted validator" {
		t.Errorf("item headline = %q, want the unit's headline", ok.Items[0].Headline.Text)
	}

	obj, found := groupFor(r, domain.NoteObjection)
	if !found || len(obj.Items) != 1 || obj.Items[0].NoteText != "wrong layer" {
		t.Errorf("objection group = %+v, want the note text carried", obj)
	}

	// Kinds with no notes are not emitted as empty groups.
	if _, found := groupFor(r, domain.NoteQuestion); found {
		t.Error("question group should be absent when there are no question notes")
	}
}
