package usecase

import (
	"sort"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// PendingWork says what has arrived since a review was opened, and how much of
// it the reviewer has already formed a view on (ADR 0020).
//
// It is pure: the caller supplies the review being read, its notes, and the
// files that have changed beyond it. What makes this worth a function rather
// than a count is the classification — "three files changed" is a fact, and
// "one of them is the file you marked ok" is a reason to stop and decide.
func PendingWork(units []domain.Unit, notes []domain.Note, changed []domain.FileStat,
	from, to domain.SnapshotRef, since time.Time) domain.Pending {

	// Which files the open review covers, and which of those carry a note that
	// still stands. A superseded note has been dealt with already; counting it
	// would keep warning about something the reviewer has moved past.
	inReview := make(map[string]string, len(units))
	for _, u := range units {
		for _, f := range u.Files {
			inReview[f] = u.ID
		}
	}
	annotated := map[string]bool{}
	for _, n := range notes {
		if n.SupersededBy != "" {
			continue
		}
		for _, u := range units {
			if u.ID != n.UnitID {
				continue
			}
			for _, f := range u.Files {
				annotated[f] = true
			}
		}
	}

	out := domain.Pending{From: from, To: to, Since: since}
	for _, c := range changed {
		_, known := inReview[c.Path]
		out.Files = append(out.Files, domain.PendingFile{
			Path: c.Path, Added: c.Added, Removed: c.Removed,
			InReview: known, Annotated: annotated[c.Path],
		})
	}

	// Ordering is the cheapest way to make the important thing the first thing
	// read: what you ruled on, then what you were looking at, then the rest.
	sort.SliceStable(out.Files, func(i, j int) bool {
		a, b := out.Files[i], out.Files[j]
		if a.Annotated != b.Annotated {
			return a.Annotated
		}
		if a.InReview != b.InReview {
			return a.InReview
		}
		return a.Path < b.Path
	})
	return out
}
