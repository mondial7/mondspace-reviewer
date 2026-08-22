package usecase_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

// fakeSnapshotter hands out sequential commit refs so bracketing is checkable.
type fakeSnapshotter struct{ n int }

func (s *fakeSnapshotter) Snapshot(_ context.Context, label string) (domain.SnapshotRef, error) {
	s.n++
	return domain.SnapshotRef{Commit: fmt.Sprintf("c%d", s.n), Label: label}, nil
}

func (s *fakeSnapshotter) Diff(context.Context, domain.SnapshotRef, domain.SnapshotRef, []string) (domain.Diff, error) {
	return domain.Diff{}, nil
}

func TestReviewLiveBracketsUnitsWithConsecutiveSnapshots(t *testing.T) {
	events := []domain.Event{
		ev("e1", domain.KindEdit, "a.go"),
		ev("e2", domain.KindBatchEnd),
		ev("e3", domain.KindEdit, "b.go"),
		ev("e4", domain.KindBatchEnd),
	}
	snap := &fakeSnapshotter{}
	pres := &fakePresenter{}

	err := usecase.ReviewLive(context.Background(), &fakeSource{events: events}, snap, &fakeStore{}, pres)
	if err != nil {
		t.Fatalf("ReviewLive: %v", err)
	}

	if len(pres.units) != 2 {
		t.Fatalf("got %d units, want 2", len(pres.units))
	}
	// Baseline snapshot c1, then one snapshot per sealed unit: c2, c3.
	if got := pres.units[0]; got.From.Commit != "c1" || got.To.Commit != "c2" {
		t.Errorf("unit 0 brackets = %s..%s, want c1..c2", got.From.Commit, got.To.Commit)
	}
	if got := pres.units[1]; got.From.Commit != "c2" || got.To.Commit != "c3" {
		t.Errorf("unit 1 brackets = %s..%s, want c2..c3", got.From.Commit, got.To.Commit)
	}
}
