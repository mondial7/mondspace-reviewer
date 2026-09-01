package usecase

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// New findings versus pre-existing ones (ADR 0043).
//
// This is the difference between the deterministic layer being useful and being
// worthless. Every repository of any age has hundreds of lint findings nobody
// is going to act on today; showing them next to a change turns "3 things to
// look at" into "412 things to look at", which is the same as none.
//
// The cheap path is here: intersect each finding's line with the lines this
// change actually added. It is fast enough to run inside a five-second poll,
// approximately right, and wrong in one known direction — it misses a problem
// the change caused somewhere it did not touch. That case is what the accurate
// path is for, and the accurate path costs a `git archive` and a second run.

// hunkHeader matches `@@ -old,count +new,count @@`. The two counts are optional;
// git omits them when they are 1.
var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// AddedLines is the line numbers, in the file as it is now, that this diff
// added or replaced.
//
// Added lines only. A finding sitting on an untouched context line is a finding
// about code that was already there, and the whole point of this is to not
// report those. A deleted line has no number in the new file to report against
// at all.
func AddedLines(d domain.Diff) map[int]bool {
	out := map[int]bool{}
	line := 0
	for _, text := range strings.Split(d.Text, "\n") {
		if m := hunkHeader.FindStringSubmatch(text); m != nil {
			start, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			line = start
			continue
		}
		if line == 0 {
			continue // still in the file header
		}
		switch {
		case strings.HasPrefix(text, "+++"):
			// The header, not an addition. It only ever appears before the
			// first hunk, but a diff of a diff exists and this is cheap.
		case strings.HasPrefix(text, "+"):
			out[line] = true
			line++
		case strings.HasPrefix(text, "-"), strings.HasPrefix(text, "\\"):
			// A removal occupies no line in the new file; so does git's
			// "\ No newline at end of file".
		default:
			line++ // context
		}
	}
	return out
}

// AddedLineText is the text of each added line, by its number in the new file,
// so a finding can be anchored to the line rather than to its position — the
// same discipline an annotation follows, and for the same reason: a diff grows
// above the thing you were looking at constantly (ADR 0028).
func AddedLineText(d domain.Diff) map[int]string {
	out := map[int]string{}
	line := 0
	for _, text := range strings.Split(d.Text, "\n") {
		if m := hunkHeader.FindStringSubmatch(text); m != nil {
			if start, err := strconv.Atoi(m[1]); err == nil {
				line = start
			}
			continue
		}
		if line == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(text, "+++"):
		case strings.HasPrefix(text, "+"):
			out[line] = text
			line++
		case strings.HasPrefix(text, "-"), strings.HasPrefix(text, "\\"):
		default:
			line++
		}
	}
	return out
}

// ChangedLines is every added line in a review, by file path. It is what a set
// of findings is intersected against.
func ChangedLines(units []domain.Unit, diffs map[string]domain.Diff) map[string]map[int]bool {
	out := make(map[string]map[int]bool, len(units))
	for _, u := range units {
		added := AddedLines(diffs[u.ID])
		for _, f := range u.Files {
			out[f] = added
		}
	}
	return out
}

// MarkNew decides, for each finding, whether it is about this change or about
// code that was already there — and anchors the ones that are.
//
// A finding on a file the review does not touch is not new: the review has
// nothing to say about it. A finding with no line at all is judged by its file,
// because a whole-file finding — a leaked credential, a vulnerable dependency —
// is exactly as new as the file's presence in this change.
func MarkNew(findings []domain.Reported, units []domain.Unit,
	diffs map[string]domain.Diff) []domain.Reported {

	changed := ChangedLines(units, diffs)
	text := map[string]map[int]string{}
	for _, u := range units {
		lines := AddedLineText(diffs[u.ID])
		for _, f := range u.Files {
			text[f] = lines
		}
	}

	out := make([]domain.Reported, 0, len(findings))
	for _, f := range findings {
		lines, touched := changed[f.File]
		switch {
		case !touched:
			f.New = false
		case f.Line == 0:
			// About the file as a whole, and the file is in this change.
			f.New = true
		default:
			f.New = lines[f.Line]
			if f.New {
				f.Anchor = text[f.File][f.Line]
			}
		}
		out = append(out, f)
	}
	return out
}

// SplitNew separates what this change introduced from what was already there,
// keeping the order of each.
//
// Both are returned rather than the pre-existing ones being dropped. Silently
// hiding four hundred findings and silently having none are indistinguishable
// from the page, and one of them means the tool is not running (ADR 0043).
func SplitNew(findings []domain.Reported) (fresh, standing []domain.Reported) {
	for _, f := range findings {
		if f.New {
			fresh = append(fresh, f)
			continue
		}
		standing = append(standing, f)
	}
	return fresh, standing
}

// CarryDismissals moves what the reviewer already decided onto a fresh run.
//
// Exactly the discipline the model's findings get (ADR 0030). A deterministic
// tool is *more* likely to raise the same finding again, not less: it is the
// same rule over the same line, so without this a dismissal would last until
// the next poll tick.
func CarryDismissals(fresh, earlier []domain.Reported) []domain.Reported {
	if len(earlier) == 0 {
		return fresh
	}
	judged := make(map[string]domain.Verdict, len(earlier))
	for _, f := range earlier {
		if f.Verdict != "" {
			judged[f.Key()] = f.Verdict
		}
	}

	out := make([]domain.Reported, 0, len(fresh))
	for _, f := range fresh {
		if v, ok := judged[f.Key()]; ok {
			f.Verdict = v
		}
		out = append(out, f)
	}
	return out
}
