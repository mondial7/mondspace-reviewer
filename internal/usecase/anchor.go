package usecase

import (
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// AnchoredLine is one line of a diff with whatever was said about it.
type AnchoredLine struct {
	Kind string // add | del | hunk | ctx
	Text string
	// Nth is which occurrence of this exact text the line is, so a note written
	// on it can be found again among identical lines.
	Nth   int
	Notes []domain.Note
}

// AnchorNotes places line-level notes on the diff they were written about, and
// reports the ones whose line has gone (ADR 0028).
//
// Notes are anchored to the line's *text*, not its number. A diff grows above
// the line you commented on constantly, and a number would drift quietly onto
// something else — a wrong anchor that never looks wrong is worse than an
// obvious one.
//
// A note with no anchor is about the file as a whole and is left alone: that is
// what every note was before this existed, and plenty of what a reviewer says
// belongs to the file rather than to a line.
func AnchorNotes(diff domain.Diff, notes []domain.Note) ([]AnchoredLine, []domain.Note) {
	lines := numberLines(diff)

	// Where each (text, occurrence) sits, and how many times each text appears,
	// so a note written on the fourth `return nil` can be placed when there are
	// only two left.
	at := map[string]map[int]int{}
	for i, l := range lines {
		if at[l.Text] == nil {
			at[l.Text] = map[int]int{}
		}
		at[l.Text][l.Nth] = i
	}

	var orphaned []domain.Note
	for _, n := range notes {
		if n.Anchor == "" {
			continue // about the file, not a line
		}
		occurrences, present := at[n.Anchor]
		if !present {
			// The line itself is gone. A judgement about code that no longer
			// exists must be reported, never silently dropped and never shown
			// as though it still applies (ADR 0021).
			orphaned = append(orphaned, n)
			continue
		}
		i, exact := occurrences[n.AnchorNth]
		if !exact {
			// The line is there, just not as many times. Somewhere true beats
			// orphaning a judgement that still applies.
			i = occurrences[0]
		}
		lines[i].Notes = append(lines[i].Notes, n)
	}
	return lines, orphaned
}

// numberLines splits a diff and records which occurrence of its own text each
// line is.
func numberLines(diff domain.Diff) []AnchoredLine {
	seen := map[string]int{}
	raw := strings.Split(diff.Text, "\n")

	out := make([]AnchoredLine, 0, len(raw))
	for _, text := range raw {
		out = append(out, AnchoredLine{
			Kind: diffLineKind(text), Text: text, Nth: seen[text],
		})
		seen[text]++
	}
	return out
}

// diffLineKind is what a diff line is, for colouring and for deciding what can
// be annotated.
func diffLineKind(line string) string {
	switch {
	case strings.HasPrefix(line, "@@"):
		return "hunk"
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		return "hunk"
	case strings.HasPrefix(line, "+"):
		return "add"
	case strings.HasPrefix(line, "-"):
		return "del"
	default:
		return "ctx"
	}
}
