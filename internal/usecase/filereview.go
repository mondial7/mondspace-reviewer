package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// RangeDiffer reports which files changed since a baseline and diffs them. It is
// declared here, where it is consumed, so the usecase layer depends on no
// adapter (ADR 0001).
type RangeDiffer interface {
	ChangedFiles(ctx context.Context, from, to domain.SnapshotRef) ([]string, error)
	Diff(ctx context.Context, from, to domain.SnapshotRef, paths []string) (domain.Diff, error)
}

// BulkDiffer is an optional capability of a RangeDiffer: diffing a whole range
// at once. Callers must keep working without it, like every other optional
// capability here.
//
// It matters at size. One diff per file is one git process per file, and at six
// hundred files that was twenty-eight seconds before a page could render
// (ADR 0029).
type BulkDiffer interface {
	DiffAll(ctx context.Context, from, to domain.SnapshotRef) (map[string]domain.Diff, error)
}

// BuildFileUnits turns a session's net change into one reviewable unit per
// changed file — the retroactive review model of ADR 0002. An empty `until`
// diffs against the working tree. Files for which exclude reports true are
// skipped. It returns the units and their diffs.
// FileUnitID names the unit for one file in one review.
//
// Derived from the path, not from the file's position in the list. Positional
// ids were stable for exactly as long as the file list was: an agent adding
// `api/handler.go` to a review that already had `web/page.go` renumbered every
// unit after it, and a note anchored to `-f001` silently became a note about a
// different file. Unit ids are what annotations, chapters and findings are
// pinned to, and the one promise every one of those rests on is that an id
// means the same thing tomorrow (ADR 0038).
func FileUnitID(reviewID, file string) string {
	sum := sha256.Sum256([]byte(file))
	return fmt.Sprintf("%s-f%s", reviewID, hex.EncodeToString(sum[:4]))
}

func BuildFileUnits(
	ctx context.Context,
	differ RangeDiffer,
	// reviewID seeds the unit ids. It is the id of whatever is being reviewed —
	// a target, which may be a commit, a tag or a session (ADR 0017) — not a
	// session id, which is what it used to be and what the name still said.
	reviewID string,
	baseline, until domain.SnapshotRef,
	exclude func(string) bool,
) ([]domain.Unit, map[string]domain.Diff, error) {
	files, err := differ.ChangedFiles(ctx, baseline, until)
	if err != nil {
		return nil, nil, err
	}

	// One call for the whole range when the differ can do it. The per-file path
	// below still runs for anything it did not return — an untracked file
	// produces no `git diff` output at all, and is diffed on its own.
	var bulk map[string]domain.Diff
	if b, ok := differ.(BulkDiffer); ok {
		bulk, _ = b.DiffAll(ctx, baseline, until)
	}

	diffs := map[string]domain.Diff{}
	var units []domain.Unit
	for _, f := range files {
		if exclude != nil && exclude(f) {
			continue
		}
		d, batched := bulk[f]
		if !batched {
			var err error
			if d, err = differ.Diff(ctx, baseline, until, []string{f}); err != nil {
				d = domain.Diff{}
			}
		}
		u := domain.Unit{
			ID:        FileUnitID(reviewID, f),
			SessionID: reviewID,
			Files:     []string{f},
			From:      baseline,
			Sealed:    true,
		}
		u.Flags = Flags(u, d)
		u.Headline = DiffHeadline(f, d)
		diffs[u.ID] = d
		units = append(units, u)
	}

	return SuppressCoveredNoTest(units), diffs, nil
}
