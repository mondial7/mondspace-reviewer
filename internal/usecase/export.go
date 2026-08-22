package usecase

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

// ExportMarkdown renders a report as a human-readable Markdown review.
func ExportMarkdown(r domain.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Review — %s\n\n", r.SessionID)
	if r.Prompt != "" {
		fmt.Fprintf(&b, "Task: %s\n\n", r.Prompt)
	}

	b.WriteString("## Review Report\n\n")
	for _, g := range r.Groups {
		fmt.Fprintf(&b, "### %s\n\n", g.Kind)
		for _, it := range g.Items {
			writeItem(&b, it)
		}
		b.WriteString("\n")
	}

	if len(r.Debt) > 0 {
		b.WriteString("## Debt\n\n")
		for _, it := range r.Debt {
			fmt.Fprintf(&b, "- [ ] %s (%s): %s\n", it.UnitID, it.Headline.Text, it.NoteText)
		}
		b.WriteString("\n")
	}

	if len(r.Agenda) > 0 {
		b.WriteString("## Open Agenda\n\n")
		for _, it := range r.Agenda {
			fmt.Fprintf(&b, "- [ ] %s\n", directive(it))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// directive phrases an open concern as an instruction for the next agent run.
func directive(it domain.ReportItem) string {
	verb := "Address the objection on"
	if it.NoteKind == domain.NoteQuestion {
		verb = "Answer the question on"
	}
	line := fmt.Sprintf("%s %s (%s)", verb, it.UnitID, it.Headline.Text)
	if it.NoteText != "" {
		line += ": " + it.NoteText
	}
	return line
}

func writeItem(b *strings.Builder, it domain.ReportItem) {
	fmt.Fprintf(b, "- %s — %s (%s)", it.UnitID, it.Headline.Text, renderWhy(it.Headline))
	if it.NoteText != "" {
		fmt.Fprintf(b, ": %s", it.NoteText)
	}
	b.WriteString("\n")
}

// renderWhy keeps the stated/inferred distinction visible in exports too.
func renderWhy(h domain.Headline) string {
	if h.WhySrc == domain.WhyStated {
		return "stated: " + h.Why
	}
	if h.Why == "" {
		return "inferred"
	}
	return "inferred: " + h.Why
}

// ExportJSON marshals a report as indented JSON.
func ExportJSON(r domain.Report) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
