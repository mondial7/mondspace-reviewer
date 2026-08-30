package web_test

import (
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/web"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestTheTimelineIsOneListInTimeOrder(t *testing.T) {
	// Tags and recorded runs are checkpoints too, and a reviewer looking for
	// "the one from Tuesday" should not have to know which kind it was.
	now := time.Now()
	entries := []usecase.LogEntry{
		{Commit: domain.Commit{Hash: "aaaa1111", Subject: "newest", TS: now}, Ref: "aaaa1111"},
		{Commit: domain.Commit{Hash: "cccc3333", Subject: "oldest", TS: now.Add(-4 * time.Hour)}, Ref: "cccc3333"},
	}
	targets := []web.TargetSummary{
		{ID: "s1", Ref: "s1", Kind: domain.TargetSession, Title: "a recorded run",
			TS: now.Add(-2 * time.Hour)},
	}

	got := web.Timeline(entries, targets)

	if len(got) != 3 {
		t.Fatalf("got %d rows, want the two commits and the run: %+v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i].TS.After(got[i-1].TS) {
			t.Errorf("row %d is newer than the one above it: %+v", i, got)
		}
	}
	if got[1].Title != "a recorded run" {
		t.Errorf("the run belongs between the two commits, got %+v", got[1])
	}
}

func TestATagLandsOnTheCommitItPointsAt(t *testing.T) {
	// A tag and the commit it tags are one point in history. Two rows a second
	// apart saying the same thing is not a timeline, it is a duplicate.
	now := time.Now()
	entries := []usecase.LogEntry{
		{Commit: domain.Commit{Hash: "aaaa1111bbbb", Subject: "the release", TS: now}, Ref: "aaaa1111"},
	}
	targets := []web.TargetSummary{
		{ID: "t", Ref: "v6.1.0", Kind: domain.TargetTag, Title: "v6.1.0",
			Commit: "aaaa1111bbbb", TS: now},
	}

	got := web.Timeline(entries, targets)

	if len(got) != 1 {
		t.Fatalf("got %d rows, want the commit wearing its tag: %+v", len(got), got)
	}
	if len(got[0].Tags) != 1 || got[0].Tags[0] != "v6.1.0" {
		t.Errorf("the commit should wear the tag: %+v", got[0])
	}
}

func TestATagOnNothingHereKeepsItsOwnRow(t *testing.T) {
	// The log is bounded; a tag older than the window still has to be reachable.
	now := time.Now()
	entries := []usecase.LogEntry{
		{Commit: domain.Commit{Hash: "aaaa1111", Subject: "recent", TS: now}, Ref: "aaaa1111"},
	}
	targets := []web.TargetSummary{
		{ID: "t", Ref: "v1.0.0", Kind: domain.TargetTag, Title: "v1.0.0",
			Commit: "ffff9999", TS: now.Add(-500 * time.Hour)},
	}

	got := web.Timeline(entries, targets)

	if len(got) != 2 || got[1].Ref != "v1.0.0" {
		t.Errorf("a tag nothing here points at needs its own row: %+v", got)
	}
}

func TestTheLiveTargetIsNeverARow(t *testing.T) {
	// It has one already, at the top, and it is not a point in history.
	got := web.Timeline(nil, []web.TargetSummary{
		{ID: "live", Ref: "live", Kind: domain.TargetLive, Title: "Live", TS: time.Now()},
	})

	if len(got) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
}
