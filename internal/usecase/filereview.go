package usecase

import (
	"context"
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

// BuildFileUnits turns a session's net change into one reviewable unit per
// changed file — the retroactive review model of ADR 0002. An empty `until`
// diffs against the working tree. Files for which exclude reports true are
// skipped. It returns the units and their diffs.
func BuildFileUnits(
	ctx context.Context,
	differ RangeDiffer,
	sessionID string,
	baseline, until domain.SnapshotRef,
	exclude func(string) bool,
) ([]domain.Unit, map[string]domain.Diff, error) {
	files, err := differ.ChangedFiles(ctx, baseline, until)
	if err != nil {
		return nil, nil, err
	}

	diffs := map[string]domain.Diff{}
	var units []domain.Unit
	for _, f := range files {
		if exclude != nil && exclude(f) {
			continue
		}
		d, err := differ.Diff(ctx, baseline, until, []string{f})
		if err != nil {
			d = domain.Diff{}
		}
		u := domain.Unit{
			ID:        fmt.Sprintf("%s-f%03d", sessionID, len(units)+1),
			SessionID: sessionID,
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
