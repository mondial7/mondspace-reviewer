package usecase_test

import (
	"strings"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

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
