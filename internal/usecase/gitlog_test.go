package usecase_test

import (
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func logCommits() []domain.Commit {
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	return []domain.Commit{
		{Hash: "cccccccccccc", Subject: "Alice: add the cache", Author: "Alice", TS: base},
		{Hash: "bbbbbbbbbbbb", Subject: "Fix the parser", Author: "You", TS: base.Add(-time.Hour)},
		{Hash: "aaaaaaaaaaaa", Subject: "Scaffold", Author: "You", TS: base.Add(-2 * time.Hour)},
	}
}

func TestTheLogMarksWhereTheReviewerIs(t *testing.T) {
	// The whole point of the card: where am I, against everything that has
	// landed (issue #18).
	now := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)

	got := usecase.BuildLog(logCommits(), "bbbbbbbb", nil, nil, now)

	if len(got) != 3 {
		t.Fatalf("got %d entries", len(got))
	}
	if !got[1].Reviewing {
		t.Errorf("entry 1 = %+v, want it marked as the one being reviewed", got[1])
	}
	if got[0].Reviewing || got[2].Reviewing {
		t.Error("only one entry is the one being reviewed")
	}
}

func TestTheLogSaysWhatIsAlreadyOnTheRemote(t *testing.T) {
	// A commit that is only local is a different thing from one a colleague can
	// already see, and the card should not make the reviewer guess which.
	onRemote := map[string]bool{"aaaaaaaaaaaa": true, "bbbbbbbbbbbb": true}
	now := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)

	got := usecase.BuildLog(logCommits(), "", onRemote, nil, now)

	if got[0].OnRemote {
		t.Error("the newest commit is not on the remote and should not say it is")
	}
	if !got[1].OnRemote || !got[2].OnRemote {
		t.Error("the older commits are on the remote")
	}
}

func TestTheLogSaysWhatHasBeenReviewed(t *testing.T) {
	// So the card answers "what is left" as well as "where am I".
	reviewed := map[string]bool{"aaaaaaaaaaaa": true}
	now := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)

	got := usecase.BuildLog(logCommits(), "", nil, reviewed, now)

	if got[0].SignedOff || got[1].SignedOff {
		t.Error("only the oldest has been signed off")
	}
	if !got[2].SignedOff {
		t.Errorf("entry 2 = %+v, want it marked reviewed", got[2])
	}
}

func TestTheLogIsReadyToOpen(t *testing.T) {
	// Every row is a link to a review, so it carries the ref the picker speaks.
	now := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)

	got := usecase.BuildLog(logCommits(), "", nil, nil, now)

	if got[0].Ref != "cccccccc" {
		t.Errorf("Ref = %q, want the short hash", got[0].Ref)
	}
	// now is an hour after the newest commit, so the one before it is 2h old.
	if got[1].Ago != "2h ago" {
		t.Errorf("Ago = %q", got[1].Ago)
	}
}

func TestNothingToReportFromAQuietRemote(t *testing.T) {
	state := domain.RemoteState{Upstream: "origin/main", UpstreamHash: "aaa"}

	if got := usecase.RemoteNews(state, state); len(got) != 0 {
		t.Errorf("got %+v, want silence", got)
	}
}

func TestTheFirstLookAtARemoteIsNotNews(t *testing.T) {
	// Same discipline as the repository watcher: opening a page must not
	// announce everything that was already there.
	next := domain.RemoteState{
		Upstream: "origin/main", UpstreamHash: "bbb", Behind: 4,
		Branches: []string{"origin/main", "origin/feature-x"},
	}

	if got := usecase.RemoteNews(domain.RemoteState{}, next); len(got) != 0 {
		t.Errorf("got %+v, want silence on the first look", got)
	}
}

func TestSomebodyElsePushing(t *testing.T) {
	// The reason the issue exists: a colleague pushed while you were reading.
	prev := domain.RemoteState{Upstream: "origin/main", UpstreamHash: "aaa"}
	next := domain.RemoteState{
		Upstream: "origin/main", UpstreamHash: "bbb", Behind: 3,
		LastSubject: "Alice: add the cache", LastAuthor: "Alice",
	}

	got := usecase.RemoteNews(prev, next)

	if len(got) != 1 {
		t.Fatalf("got %+v, want one piece of news", got)
	}
	if got[0].Kind != domain.PulseRemote {
		t.Errorf("kind = %q, want a remote pulse", got[0].Kind)
	}
	want := "3 new commits on origin/main · Alice"
	if got[0].Text != want {
		t.Errorf("text = %q,\n    want %q", got[0].Text, want)
	}
}

func TestOneCommitFromSomebodyElseReadsAsOne(t *testing.T) {
	prev := domain.RemoteState{Upstream: "origin/main", UpstreamHash: "aaa"}
	next := domain.RemoteState{
		Upstream: "origin/main", UpstreamHash: "bbb", Behind: 1,
		LastSubject: "Fix the flake", LastAuthor: "Bob",
	}

	got := usecase.RemoteNews(prev, next)

	if len(got) != 1 || got[0].Text != "1 new commit on origin/main · Bob" {
		t.Errorf("got %+v", got)
	}
}

func TestANewBranchAppearing(t *testing.T) {
	prev := domain.RemoteState{Upstream: "origin/main", UpstreamHash: "aaa",
		Branches: []string{"origin/main"}}
	next := domain.RemoteState{Upstream: "origin/main", UpstreamHash: "aaa",
		Branches: []string{"origin/main", "origin/feature-x"}}

	got := usecase.RemoteNews(prev, next)

	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Text != "New branch origin/feature-x" {
		t.Errorf("text = %q", got[0].Text)
	}
	if got[0].Ref != "origin/feature-x" {
		t.Errorf("a branch pulse should open that branch, got %q", got[0].Ref)
	}
}

func TestABranchDisappearingIsNotWorthSaying(t *testing.T) {
	// Branches are deleted constantly after merging, and a toast for each would
	// be noise about work that is finished.
	prev := domain.RemoteState{Upstream: "origin/main", UpstreamHash: "aaa",
		Branches: []string{"origin/main", "origin/done"}}
	next := domain.RemoteState{Upstream: "origin/main", UpstreamHash: "aaa",
		Branches: []string{"origin/main"}}

	if got := usecase.RemoteNews(prev, next); len(got) != 0 {
		t.Errorf("got %+v, want silence", got)
	}
}

func TestABurstOfBranchesIsCounted(t *testing.T) {
	prev := domain.RemoteState{Upstream: "origin/main", UpstreamHash: "a",
		Branches: []string{"origin/main"}}
	next := domain.RemoteState{Upstream: "origin/main", UpstreamHash: "a",
		Branches: []string{"origin/main", "b1", "b2", "b3", "b4", "b5", "b6"}}

	got := usecase.RemoteNews(prev, next)

	if len(got) > 3 {
		t.Errorf("got %d pulses, want at most 3", len(got))
	}
	if last := got[len(got)-1].Text; last != "…and 4 more branches" {
		t.Errorf("the tail should be counted, got %q", last)
	}
}

func TestIncomingWorkIsListedAndMarked(t *testing.T) {
	// The point of watching a remote: a colleague's commit should appear in the
	// history you are looking at, above your own work, and be obviously not
	// yours yet (issue #18).
	now := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)
	commits := append([]domain.Commit{
		{Hash: "dddddddddddd", Subject: "Alice: add the cache", Author: "Alice", TS: now},
	}, logCommits()...)

	// Everything except Alice's is reachable from HEAD.
	local := map[string]bool{
		"cccccccccccc": true, "bbbbbbbbbbbb": true, "aaaaaaaaaaaa": true,
	}

	got := usecase.BuildLogAcross(commits, "", nil, nil, local, now)

	if !got[0].Incoming {
		t.Errorf("entry 0 = %+v, want it marked incoming", got[0])
	}
	for _, e := range got[1:] {
		if e.Incoming {
			t.Errorf("%+v: your own commits are not incoming", e)
		}
	}
}

func TestWithNoRemoteNothingIsIncoming(t *testing.T) {
	// A repository with no upstream must not paint its whole history as
	// somebody else's work.
	now := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)

	got := usecase.BuildLogAcross(logCommits(), "", nil, nil, nil, now)

	for _, e := range got {
		if e.Incoming {
			t.Errorf("%+v: with no upstream, nothing is incoming", e)
		}
	}
}
