package usecase_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// fakeSnapshotter hands out sequential commit refs so bracketing is checkable,
// and returns a fixed diff so flagging can be exercised.
type fakeSnapshotter struct {
	n        int
	diffText string
}

func (s *fakeSnapshotter) Snapshot(_ context.Context, label string) (domain.SnapshotRef, error) {
	s.n++
	return domain.SnapshotRef{Commit: fmt.Sprintf("c%d", s.n), Label: label}, nil
}

func (s *fakeSnapshotter) Diff(context.Context, domain.SnapshotRef, domain.SnapshotRef, []string) (domain.Diff, error) {
	return domain.Diff{Text: s.diffText}, nil
}

func TestReviewLiveAttachesFlagsFromDiff(t *testing.T) {
	events := []domain.Event{
		ev("e1", domain.KindEdit, "api.go"), // non-test source → no-test
		ev("e2", domain.KindBatchEnd),
	}
	snap := &fakeSnapshotter{diffText: "+// TODO: revisit\n"} // added TODO → todo
	pres := &fakePresenter{}

	if err := usecase.ReviewLive(context.Background(), &fakeSource{events: events}, snap, &fakeStore{}, pres); err != nil {
		t.Fatalf("ReviewLive: %v", err)
	}

	if len(pres.units) != 1 {
		t.Fatalf("got %d units, want 1", len(pres.units))
	}
	flags := pres.units[0].Flags
	if !hasFlag(flags, domain.FlagNoTest) || !hasFlag(flags, domain.FlagTodo) {
		t.Errorf("unit flags = %v, want no-test and todo", flags)
	}
}

func TestReviewLiveFlagsFailedToolCalls(t *testing.T) {
	failed := ev("e1", domain.KindBash, "api.go")
	failed.Failed = true
	events := []domain.Event{
		failed,
		ev("e2", domain.KindBatchEnd),
	}
	snap := &fakeSnapshotter{}
	pres := &fakePresenter{}

	if err := usecase.ReviewLive(context.Background(), &fakeSource{events: events}, snap, &fakeStore{}, pres); err != nil {
		t.Fatalf("ReviewLive: %v", err)
	}

	if len(pres.units) != 1 {
		t.Fatalf("got %d units, want 1", len(pres.units))
	}
	if !hasFlag(pres.units[0].Flags, domain.FlagFailed) {
		t.Errorf("unit flags = %v, want failed", pres.units[0].Flags)
	}
}

func TestReviewLiveDoesNotFlagFailedWhenNoEventFailed(t *testing.T) {
	events := []domain.Event{
		ev("e1", domain.KindBash, "api.go"),
		ev("e2", domain.KindBatchEnd),
	}
	snap := &fakeSnapshotter{}
	pres := &fakePresenter{}

	if err := usecase.ReviewLive(context.Background(), &fakeSource{events: events}, snap, &fakeStore{}, pres); err != nil {
		t.Fatalf("ReviewLive: %v", err)
	}

	if hasFlag(pres.units[0].Flags, domain.FlagFailed) {
		t.Errorf("unit flags = %v, did not want failed", pres.units[0].Flags)
	}
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
