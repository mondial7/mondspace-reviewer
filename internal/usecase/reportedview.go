package usecase

import (
	"fmt"
	"sort"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// What the deterministic layer looks like on the page (ADR 0043).
//
// Not a fourth card. The three model cards each answer one question about the
// whole change and belong beside each other; this answers "is there anything
// mechanically wrong with the file I am reading", which belongs against the
// file. So: findings roll up per file, one sentence sums up the review, and the
// file list can be filtered to the files that have any.

// FileFindings is one file's deterministic findings.
type FileFindings struct {
	File string
	// New is what this change introduced, and is what is shown by default.
	New []domain.Reported
	// Standing was already there. Counted, offered behind a toggle, never
	// silently dropped.
	Standing []domain.Reported
	Worst    domain.Severity
}

// Any reports whether this file has anything to say at all.
func (f FileFindings) Any() bool { return len(f.New) > 0 }

// ReportedView is the whole deterministic layer for one review.
type ReportedView struct {
	// ByFile is every file with new findings, in the order the review lists
	// them. A file with nothing new is not here.
	ByFile []FileFindings
	// Files is how many files have new findings, and Of how many are in the
	// review — "3 of 14 files have findings".
	Files, Of int
	// New and Standing are the totals.
	New, Standing int
	// Worst is the highest severity among the new findings.
	Worst domain.Severity
	// Tools is which analysers actually spoke, so a reviewer can tell "gosec
	// found nothing" from "gosec never ran".
	Tools []string
}

// Summary is the one line for the review. Empty when there is nothing to say,
// which is the usual and correct outcome.
func (v ReportedView) Summary() string {
	if v.Of == 0 {
		return ""
	}
	if v.Files == 0 {
		if v.Standing > 0 {
			return fmt.Sprintf("Nothing new in these %d %s. %s already here.",
				v.Of, plural("file", v.Of), count(v.Standing, "finding"))
		}
		return fmt.Sprintf("Nothing to report in %d %s.", v.Of, plural("file", v.Of))
	}
	return fmt.Sprintf("%d of %d %s %s findings.",
		v.Files, v.Of, plural("file", v.Of), has(v.Files))
}

// has agrees the verb with the count, because "1 of 14 files have findings" is
// the kind of thing that makes a page look machine-written.
func has(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}

// Any reports whether anything new was found.
func (v ReportedView) Any() bool { return v.New > 0 }

// FindingsFor is one file's findings, for the page to render against it.
func (v ReportedView) FindingsFor(file string) FileFindings {
	for _, f := range v.ByFile {
		if f.File == file {
			return f
		}
	}
	return FileFindings{File: file}
}

// GroupReported rolls a flat list of findings up into what the page needs.
//
// Dismissed findings do not count towards anything — not the per-file total,
// not the summary line, not the worst severity. A layer still saying "3
// findings" after all three were dismissed has not listened, which is exactly
// the discipline the model's findings already follow (ADR 0030).
func GroupReported(findings []domain.Reported, units []domain.Unit) ReportedView {
	byFile := map[string]*FileFindings{}
	tools := map[string]bool{}

	for _, f := range findings {
		tools[f.Tool] = true
		if !f.Stands() {
			continue
		}
		at, known := byFile[f.File]
		if !known {
			at = &FileFindings{File: f.File}
			byFile[f.File] = at
		}
		if f.New {
			at.New = append(at.New, f)
			continue
		}
		at.Standing = append(at.Standing, f)
	}

	view := ReportedView{Of: len(units)}
	// The review's own order, so the findings appear where the files do rather
	// than in whatever order the tools happened to answer.
	for _, u := range units {
		for _, path := range u.Files {
			at, known := byFile[path]
			if !known {
				continue
			}
			delete(byFile, path)

			sortReported(at.New)
			sortReported(at.Standing)
			at.Worst = worstOf(at.New)
			view.New += len(at.New)
			view.Standing += len(at.Standing)
			if len(at.New) > 0 {
				view.Files++
				view.ByFile = append(view.ByFile, *at)
			}
		}
	}
	// Anything a tool reported against a path the review does not list. It is
	// counted as pre-existing rather than dropped: a finding msr cannot place
	// is still a finding, and pretending otherwise is how a count comes to
	// disagree with itself.
	for _, at := range byFile {
		view.Standing += len(at.New) + len(at.Standing)
	}

	view.Worst = worstOfFiles(view.ByFile)
	for name := range tools {
		view.Tools = append(view.Tools, name)
	}
	sort.Strings(view.Tools)
	return view
}

// sortReported orders a file's findings worst first, then by line, so reading
// down a file's list is reading down the file.
func sortReported(in []domain.Reported) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Severity.Rank() != in[j].Severity.Rank() {
			return in[i].Severity.Rank() < in[j].Severity.Rank()
		}
		return in[i].Line < in[j].Line
	})
}

func worstOf(in []domain.Reported) domain.Severity {
	worst := domain.Severity("")
	for _, f := range in {
		if worst == "" || f.Severity.Rank() < worst.Rank() {
			worst = f.Severity.Normalise()
		}
	}
	return worst
}

func worstOfFiles(files []FileFindings) domain.Severity {
	worst := domain.Severity("")
	for _, f := range files {
		if f.Worst == "" {
			continue
		}
		if worst == "" || f.Worst.Rank() < worst.Rank() {
			worst = f.Worst
		}
	}
	return worst
}

// ApplyDismissals stamps stored rulings onto a fresh set of findings.
func ApplyDismissals(findings []domain.Reported, rulings map[string]domain.Verdict) []domain.Reported {
	if len(rulings) == 0 {
		return findings
	}
	out := make([]domain.Reported, 0, len(findings))
	for _, f := range findings {
		if v, ruled := rulings[f.Key()]; ruled {
			f.Verdict = v
		}
		out = append(out, f)
	}
	return out
}
