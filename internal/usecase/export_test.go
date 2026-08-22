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

func TestExportMarkdownPreservesWhySource(t *testing.T) {
	md := usecase.ExportMarkdown(usecase.BuildReport(reportSession()))

	// s-u001 stated its intent; s-u002 did not.
	if !strings.Contains(md, "stated: swap lib") {
		t.Errorf("stated rationale not preserved:\n%s", md)
	}
	// The inferred unit must never be shown as stated.
	lines := strings.Split(md, "\n")
	for _, l := range lines {
		if strings.Contains(l, "s-u002") && strings.Contains(l, "stated:") {
			t.Errorf("inferred unit shown as stated: %q", l)
		}
	}
}

func TestExportMarkdownSupersededAndUnreviewed(t *testing.T) {
	sess := reportSession()
	sess.Units = append(sess.Units,
		domain.Unit{ID: "s-u004", Files: []string{"auth/token.go"}},
		domain.Unit{ID: "s-u009", Headline: domain.Headline{Text: "untouched"}},
	)
	sess.Notes = append(sess.Notes,
		domain.Note{ID: "n7", UnitID: "s-u001", Kind: domain.NoteObjection, Text: "bad choice"},
	)

	md := usecase.ExportMarkdown(usecase.BuildReport(sess))

	if !strings.Contains(md, "## Superseded") || !strings.Contains(md, "superseded by s-u004") {
		t.Errorf("superseded section missing or unmarked:\n%s", md)
	}
	if !strings.Contains(md, "## Unreviewed") || !strings.Contains(md, "s-u009") {
		t.Errorf("unreviewed section missing:\n%s", md)
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
