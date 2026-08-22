package usecase_test

import (
	"strings"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

func TestExportMarkdownOpenAgenda(t *testing.T) {
	sess := reportSession()
	sess.Notes = append(sess.Notes,
		domain.Note{ID: "n6", UnitID: "s-u001", Kind: domain.NoteQuestion, Text: "why an interface?"},
	)

	md := usecase.ExportMarkdown(usecase.BuildReport(sess))

	if !strings.Contains(md, "## Open Agenda") {
		t.Errorf("missing Open Agenda heading:\n%s", md)
	}
	// Objection on s-u002 and question on s-u001, phrased as directives.
	if !strings.Contains(md, "Address") || !strings.Contains(md, "wrong layer") {
		t.Errorf("objection not phrased as a directive:\n%s", md)
	}
	if !strings.Contains(md, "Answer") || !strings.Contains(md, "why an interface?") {
		t.Errorf("question not phrased as a directive:\n%s", md)
	}
}

func TestExportMarkdownDebtTaskList(t *testing.T) {
	sess := reportSession()
	sess.Notes = append(sess.Notes,
		domain.Note{ID: "n4", UnitID: "s-u003", Kind: domain.NoteDebt, Text: "add a test for the retry"},
	)

	md := usecase.ExportMarkdown(usecase.BuildReport(sess))

	if !strings.Contains(md, "## Debt") {
		t.Errorf("missing Debt heading:\n%s", md)
	}
	if !strings.Contains(md, "- [ ] ") || !strings.Contains(md, "add a test for the retry") {
		t.Errorf("debt should be a checkbox task list:\n%s", md)
	}
}

func TestExportMarkdownReviewReport(t *testing.T) {
	r := usecase.BuildReport(reportSession())

	md := usecase.ExportMarkdown(r)

	for _, want := range []string{
		"# Review",           // title
		"add token validation", // task prompt
		"## Review Report",
		"### ok",
		"s-u001",
		"extracted validator",
		"### objection",
		"s-u002",
		"wrong layer", // the note text
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
}
