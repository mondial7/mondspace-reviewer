package usecase

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
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

	if len(r.Superseded) > 0 {
		b.WriteString("## Superseded\n\n")
		for _, it := range r.Superseded {
			fmt.Fprintf(&b, "- %s (%s): %s — superseded by %s\n", it.UnitID, it.Headline.Text, it.NoteText, it.SupersededBy)
		}
		b.WriteString("\n")
	}

	if len(r.Unreviewed) > 0 {
		b.WriteString("## Unreviewed\n\n")
		for _, it := range r.Unreviewed {
			fmt.Fprintf(&b, "- %s — %s\n", it.UnitID, it.Headline.Text)
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

// slackListCap bounds each bulleted list in ExportSlack; slackCharCap bounds
// the whole message, matching Slack's own message-length limit.
const (
	slackListCap = 5
	slackCharCap = 3000
)

// ExportSlack renders a concise, single Slack message (mrkdwn only: *bold*,
// _italic_, "• " bullets, no headings, no tables) summarising a review: a
// one-line headline of counts, the top flagged/risky items, then the open
// agenda. Both the item lists and the overall message are capped, and a
// truncation is always announced — never silent.
func ExportSlack(r domain.Report) string {
	var b strings.Builder
	b.WriteString(slackHeadline(r))

	if flagged := slackFlaggedLines(r); len(flagged) > 0 {
		b.WriteString("\n\n*Flagged*\n")
		writeSlackBullets(&b, flagged)
	}

	if len(r.Agenda) > 0 {
		b.WriteString("\n*Open agenda*\n")
		lines := make([]string, len(r.Agenda))
		for i, it := range r.Agenda {
			lines[i] = directive(it)
		}
		writeSlackBullets(&b, lines)
	}

	return capSlackMessage(b.String())
}

// slackHeadline is the one-line summary: how many units were reviewed, how
// many carry a deterministic flag, how many questions/objections are still
// open, and how much debt was logged.
func slackHeadline(r domain.Report) string {
	reviewed, flagged := slackReviewedAndFlaggedCounts(r)
	var questions, objections int
	for _, it := range r.Agenda {
		switch it.NoteKind {
		case domain.NoteQuestion:
			questions++
		case domain.NoteObjection:
			objections++
		}
	}

	title := "*Review*"
	if r.SessionID != "" {
		title = fmt.Sprintf("*Review — %s*", r.SessionID)
	}
	return fmt.Sprintf("%s: %d %s reviewed, %d flagged, %d %s / %d %s open, %d %s",
		title,
		reviewed, plural("file", reviewed),
		flagged,
		questions, plural("question", questions),
		objections, plural("objection", objections),
		len(r.Debt), plural("debt item", len(r.Debt)),
	)
}

// slackReviewedAndFlaggedCounts counts distinct units that received any
// annotation, and how many of those carry at least one deterministic flag. A
// unit annotated more than once (e.g. an objection and a debt note) is
// counted once in each.
func slackReviewedAndFlaggedCounts(r domain.Report) (reviewed, flagged int) {
	seen, flaggedSeen := map[string]bool{}, map[string]bool{}
	for _, g := range r.Groups {
		for _, it := range g.Items {
			if !seen[it.UnitID] {
				seen[it.UnitID] = true
				reviewed++
			}
			if len(it.Flags) > 0 && !flaggedSeen[it.UnitID] {
				flaggedSeen[it.UnitID] = true
				flagged++
			}
		}
	}
	return reviewed, flagged
}

// slackFlaggedLines lists the flagged units once each, in the order they
// first appear across the note-kind groups.
func slackFlaggedLines(r domain.Report) []string {
	var lines []string
	seen := map[string]bool{}
	for _, g := range r.Groups {
		for _, it := range g.Items {
			if len(it.Flags) == 0 || seen[it.UnitID] {
				continue
			}
			seen[it.UnitID] = true
			lines = append(lines, fmt.Sprintf("%s — %s (%s)", it.UnitID, it.Headline.Text, flagNames(it.Flags)))
		}
	}
	return lines
}

func flagNames(flags []domain.Flag) string {
	names := make([]string, len(flags))
	for i, f := range flags {
		names[i] = string(f)
	}
	return strings.Join(names, ", ")
}

// writeSlackBullets renders lines as "• " bullets, capped to slackListCap. A
// truncated list always says how many more were cut, rather than dropping
// them silently.
func writeSlackBullets(b *strings.Builder, lines []string) {
	shown, more := lines, 0
	if len(lines) > slackListCap {
		shown, more = lines[:slackListCap], len(lines)-slackListCap
	}
	for _, l := range shown {
		fmt.Fprintf(b, "• %s\n", l)
	}
	if more > 0 {
		fmt.Fprintf(b, "…and %d more\n", more)
	}
}

// capSlackMessage bounds the whole message to slackCharCap. A message cut to
// fit always says so, appended within the same budget.
func capSlackMessage(s string) string {
	s = strings.TrimRight(s, "\n")
	if len(s) <= slackCharCap {
		return s
	}
	const notice = "\n\n_(truncated to fit Slack's message limit)_"
	limit := slackCharCap - len(notice)
	if limit < 0 {
		limit = 0
	}
	cut := s[:limit]
	for len(cut) > 0 && !utf8.RuneStart(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	return cut + notice
}
