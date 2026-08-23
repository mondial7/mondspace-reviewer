package usecase_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func targetFixtures() ([]domain.Commit, []domain.Tag) {
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	commits := []domain.Commit{
		{Hash: "d4", Parent: "c3", Subject: "Add the cockpit (#42)", TS: base.Add(3 * time.Hour)},
		{Hash: "c3", Parent: "b2", Subject: "Wire the panel (#42)", TS: base.Add(2 * time.Hour)},
		{Hash: "b2", Parent: "a1", Subject: "Fix the retry", TS: base.Add(time.Hour)},
		{Hash: "a1", Subject: "init", TS: base},
	}
	tags := []domain.Tag{
		{Name: "v2.0.0", Hash: "c3", TS: base.Add(2 * time.Hour)},
		{Name: "v1.0.0", Hash: "a1", TS: base},
	}
	return commits, tags
}

func TestEveryCommitIsReviewableOnItsOwn(t *testing.T) {
	commits, _ := targetFixtures()

	got := usecase.BuildTargets("repo", commits, nil, nil, false)

	var commitTargets []domain.Target
	for _, tgt := range got {
		if tgt.Kind == domain.TargetCommit {
			commitTargets = append(commitTargets, tgt)
		}
	}
	if len(commitTargets) != 4 {
		t.Fatalf("got %d commit targets, want one per commit", len(commitTargets))
	}

	newest := commitTargets[0]
	if newest.From.Commit != "c3" || newest.To.Commit != "d4" {
		t.Errorf("range = %s..%s, want parent..commit", newest.From.Commit, newest.To.Commit)
	}
	// A root commit has no parent, so it is diffed against the empty tree.
	root := commitTargets[3]
	if root.From.Commit == "" {
		t.Errorf("a root commit needs an explicit empty-tree baseline, got %+v", root.From)
	}
}

func TestATagIsTheRangeSinceThePreviousOne(t *testing.T) {
	// "What shipped in v2.0.0" is the question a tag answers, and the answer is
	// everything since the tag before it.
	commits, tags := targetFixtures()

	got := usecase.BuildTargets("repo", commits, tags, nil, false)

	var tagged *domain.Target
	for i, tgt := range got {
		if tgt.Kind == domain.TargetTag && strings.Contains(tgt.Title, "v2.0.0") {
			tagged = &got[i]
		}
	}
	if tagged == nil {
		t.Fatalf("no target for v2.0.0: %+v", got)
	}
	if tagged.From.Commit != "a1" || tagged.To.Commit != "c3" {
		t.Errorf("v2.0.0 range = %s..%s, want v1.0.0..v2.0.0", tagged.From.Commit, tagged.To.Commit)
	}
}

func TestCommitsSharingAPullRequestBecomeOneTarget(t *testing.T) {
	// Two commits landing #42 are one piece of work, and reviewing them
	// separately is exactly the fragmentation this is meant to fix.
	commits, _ := targetFixtures()

	got := usecase.BuildTargets("repo", commits, nil, nil, false)

	var prs []domain.Target
	for _, tgt := range got {
		if tgt.Kind == domain.TargetPR {
			prs = append(prs, tgt)
		}
	}
	if len(prs) != 1 {
		t.Fatalf("got %d pull-request targets, want 1: %+v", len(prs), prs)
	}
	if prs[0].From.Commit != "b2" || prs[0].To.Commit != "d4" {
		t.Errorf("#42 range = %s..%s, want to span both its commits",
			prs[0].From.Commit, prs[0].To.Commit)
	}
	if prs[0].Commits != 2 {
		t.Errorf("Commits = %d, want 2", prs[0].Commits)
	}
}

func TestASessionIsOneTargetAmongTheOthers(t *testing.T) {
	// The session keeps its place — it answers "what did that run do" — but it
	// sits beside the commits rather than containing them.
	commits, _ := targetFixtures()
	sessions := []domain.Target{{
		ID: "sess", Kind: domain.TargetSession, Title: "add token validation",
		From: domain.SnapshotRef{Commit: "a1"}, TS: commits[0].TS,
	}}

	got := usecase.BuildTargets("repo", commits, nil, sessions, false)

	var found bool
	for _, tgt := range got {
		if tgt.Kind == domain.TargetSession && tgt.Title == "add token validation" {
			found = true
		}
	}
	if !found {
		t.Errorf("the session should appear as a target: %+v", got)
	}
}

func TestADirtyTreeIsOfferedFirst(t *testing.T) {
	// Work in progress is the most likely thing a reviewer wants, so it leads.
	commits, _ := targetFixtures()

	got := usecase.BuildTargets("repo", commits, nil, nil, true)

	if len(got) == 0 || got[0].Kind != domain.TargetWorktree {
		t.Fatalf("first target = %+v, want the working tree", got[0])
	}
	if got[0].To.Commit != "" {
		t.Errorf("the working tree has no far end, got %q", got[0].To.Commit)
	}
	// And it is not offered when there is nothing uncommitted.
	clean := usecase.BuildTargets("repo", commits, nil, nil, false)
	for _, tgt := range clean {
		if tgt.Kind == domain.TargetWorktree {
			t.Error("a clean tree should not be offered as a target")
		}
	}
}

func TestTargetIDsAreStableAndDistinct(t *testing.T) {
	// A note attaches to a target, so the same range must review to the same id
	// across restarts and machines — and two ranges must never collide.
	commits, tags := targetFixtures()

	first := usecase.BuildTargets("repo", commits, tags, nil, false)
	second := usecase.BuildTargets("repo", commits, tags, nil, false)

	seen := map[string]bool{}
	for i := range first {
		if first[i].ID == "" {
			t.Fatalf("target has no id: %+v", first[i])
		}
		if first[i].ID != second[i].ID {
			t.Errorf("id for %q is not stable: %q then %q", first[i].Title, first[i].ID, second[i].ID)
		}
		if seen[first[i].ID] {
			t.Errorf("two targets share the id %q", first[i].ID)
		}
		seen[first[i].ID] = true
	}

	// A different repository is a different review of the same range.
	other := usecase.BuildTargets("elsewhere", commits, tags, nil, false)
	if other[0].ID == first[0].ID {
		t.Error("the same range in another repository must not share an id")
	}
}

func TestTargetsCarryTheSessionsThatProducedThem(t *testing.T) {
	// The grouped view: a commit made during a recorded run carries that run, so
	// the intent behind a change is one click away.
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	commits := []domain.Commit{
		{Hash: "b2", Parent: "a1", Subject: "During the run", TS: base.Add(90 * time.Minute)},
		{Hash: "a1", Subject: "Before it", TS: base.Add(-time.Hour)},
	}
	sessions := []domain.Target{{
		ID: "sess-1", Kind: domain.TargetSession, Title: "add auth",
		TS: base, To: domain.SnapshotRef{Label: "worktree"},
	}}

	got := usecase.BuildTargets("repo", commits, nil, sessions, false)

	var during, before domain.Target
	for _, tgt := range got {
		switch tgt.To.Commit {
		case "b2":
			during = tgt
		case "a1":
			before = tgt
		}
	}
	if len(during.Sessions) != 1 || during.Sessions[0] != "sess-1" {
		t.Errorf("the commit made during the run should carry it, got %+v", during.Sessions)
	}
	if len(before.Sessions) != 0 {
		t.Errorf("a commit from before the run must not claim it, got %+v", before.Sessions)
	}
}

func TestPullRequestTitleDropsTheWholeReference(t *testing.T) {
	// "(#42)" is a suffix, not just "(#42" — leaving the bracket behind is the
	// kind of detail that makes a generated title look broken.
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	commits := []domain.Commit{
		{Hash: "b", Parent: "a", Subject: "Add the cockpit (#42)", TS: base.Add(time.Hour)},
		{Hash: "a", Subject: "Wire it (#42)", TS: base},
	}

	got := usecase.BuildTargets("repo", commits, nil, nil, false)

	for _, tgt := range got {
		if tgt.Kind != domain.TargetPR {
			continue
		}
		if tgt.Title != "#42 · Add the cockpit" {
			t.Errorf("Title = %q, want the reference removed cleanly", tgt.Title)
		}
	}
}
