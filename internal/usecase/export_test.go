package usecase_test

import (
	"strings"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

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
