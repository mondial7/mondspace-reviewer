package usecase_test

import (
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestPulsesReportWhatMovedInTheRepository(t *testing.T) {
	tests := []struct {
		name       string
		prev, next domain.RepoState
		want       []string // the text of each pulse, in order
	}{
		{
			name: "nothing moved",
			prev: domain.RepoState{Head: "aaa", DirtyPrint: "p1"},
			next: domain.RepoState{Head: "aaa", DirtyPrint: "p1"},
		},
		{
			name: "the first observation is not news",
			// Otherwise every reload greets the reviewer with a toast about
			// commits that were already there when they arrived.
			prev: domain.RepoState{},
			next: domain.RepoState{Head: "aaa", Tags: []string{"v1.0.0"}, DirtyFiles: 3},
		},
		{
			name: "a new commit",
			prev: domain.RepoState{Head: "aaa"},
			next: domain.RepoState{Head: "bbb", Subject: "Fix the parser", Commits: 1},
			want: []string{"New commit · Fix the parser"},
		},
		{
			name: "several commits at once",
			prev: domain.RepoState{Head: "aaa"},
			next: domain.RepoState{Head: "bbb", Subject: "Fix the parser", Commits: 3},
			want: []string{"3 new commits · Fix the parser"},
		},
		{
			name: "a new tag",
			prev: domain.RepoState{Head: "aaa", Tags: []string{"v1.0.0"}},
			next: domain.RepoState{Head: "aaa", Tags: []string{"v2.0.0", "v1.0.0"}},
			want: []string{"New tag v2.0.0"},
		},
		{
			name: "the working tree moved",
			prev: domain.RepoState{Head: "aaa", DirtyPrint: "p1", DirtyFiles: 1},
			next: domain.RepoState{Head: "aaa", DirtyPrint: "p2", DirtyFiles: 4},
			want: []string{"4 files changed since HEAD"},
		},
		{
			name: "one file, said as one file",
			prev: domain.RepoState{Head: "aaa", DirtyPrint: "p1"},
			next: domain.RepoState{Head: "aaa", DirtyPrint: "p2", DirtyFiles: 1},
			want: []string{"1 file changed since HEAD"},
		},
		{
			name: "committing is one piece of news, not two",
			// The working tree necessarily empties when work is committed.
			// Saying "0 files changed" beside "new commit" is noise about the
			// same event.
			prev: domain.RepoState{Head: "aaa", DirtyPrint: "p1", DirtyFiles: 5},
			next: domain.RepoState{Head: "bbb", Subject: "Land it", Commits: 1, DirtyPrint: "", DirtyFiles: 0},
			want: []string{"New commit · Land it"},
		},
		{
			name: "a tag and a commit are both worth saying",
			prev: domain.RepoState{Head: "aaa", Tags: []string{"v1.0.0"}},
			next: domain.RepoState{Head: "bbb", Subject: "Release", Commits: 1, Tags: []string{"v2.0.0", "v1.0.0"}},
			want: []string{"New commit · Release", "New tag v2.0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := usecase.Pulses(tt.prev, tt.next)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d pulses %v, want %d %v", len(got), texts(got), len(tt.want), tt.want)
			}
			for i, want := range tt.want {
				if got[i].Text != want {
					t.Errorf("pulse %d = %q, want %q", i, got[i].Text, want)
				}
			}
		})
	}
}

func TestAPulseCarriesWhereToGo(t *testing.T) {
	// A toast that cannot be acted on is an interruption. Each one names the
	// target that would show the reviewer what it is talking about.
	got := usecase.Pulses(
		domain.RepoState{Head: "aaaaaaaaaaaa", Tags: []string{"v1.0.0"}},
		domain.RepoState{Head: "bbbbbbbbbbbb", Subject: "Release", Commits: 1,
			Tags: []string{"v2.0.0", "v1.0.0"}},
	)
	if len(got) != 2 {
		t.Fatalf("want a commit and a tag pulse, got %v", texts(got))
	}
	if got[0].Ref != "bbbbbbbb" {
		t.Errorf("a commit pulse should open that commit, got %q", got[0].Ref)
	}
	if got[1].Ref != "v2.0.0" {
		t.Errorf("a tag pulse should open that tag, got %q", got[1].Ref)
	}
}

func TestWorkingTreePulsesPointAtTheLiveTarget(t *testing.T) {
	got := usecase.Pulses(
		domain.RepoState{Head: "aaa", DirtyPrint: "p1"},
		domain.RepoState{Head: "aaa", DirtyPrint: "p2", DirtyFiles: 2},
	)
	if len(got) != 1 || got[0].Ref != usecase.LiveRef {
		t.Fatalf("a working-tree pulse should open the live target, got %+v", got)
	}
}

func TestManyNewTagsDoNotBuryTheReviewer(t *testing.T) {
	// Fetching a repository can land dozens of tags at once. Three toasts is
	// information; thirty is a denial of service on the reader.
	prev := domain.RepoState{Head: "aaa", Tags: []string{"v0.0.1"}}
	next := domain.RepoState{Head: "aaa", Tags: []string{
		"v9", "v8", "v7", "v6", "v5", "v4", "v3", "v2", "v1", "v0.0.1"}}

	got := usecase.Pulses(prev, next)
	if len(got) > 3 {
		t.Errorf("want at most 3 pulses, got %d: %v", len(got), texts(got))
	}
	if len(got) == 0 {
		t.Fatal("a burst of tags should still say something")
	}
	if last := got[len(got)-1].Text; last != "…and 7 more tags" {
		t.Errorf("the tail should be counted, got %q", last)
	}
}

func texts(ps []domain.Pulse) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Text)
	}
	return out
}

func TestALiveTargetIsDiffedFromWhereHeadIsNow(t *testing.T) {
	// The target list is built once; HEAD moves whenever the agent commits. If
	// the live review kept the baseline it was discovered with, committing
	// would leave the reviewer staring at their just-committed work as though
	// it were still uncommitted — the exact moment the tool must be right.
	discovered := domain.Target{
		Kind: domain.TargetLive, ID: "live-1",
		From: domain.SnapshotRef{Commit: "oldhead", Label: "HEAD"},
	}
	now := domain.SnapshotRef{Commit: "newhead", Label: "HEAD"}

	got := usecase.ResolveLive(discovered, now)

	if got.From.Commit != "newhead" {
		t.Errorf("baseline = %q, want the current HEAD", got.From.Commit)
	}
	if got.ID != "live-1" {
		t.Errorf("following HEAD must not change the review's identity, got %q", got.ID)
	}
}

func TestResolvingLeavesEveryOtherKindAlone(t *testing.T) {
	// A commit or a tag names a fixed point. Moving its baseline would silently
	// re-scope a review someone may already have annotated.
	fixed := domain.Target{
		Kind: domain.TargetCommit,
		From: domain.SnapshotRef{Commit: "parent"},
		To:   domain.SnapshotRef{Commit: "child"},
	}
	if got := usecase.ResolveLive(fixed, domain.SnapshotRef{Commit: "newhead"}); got.From.Commit != "parent" {
		t.Errorf("baseline = %q, want it untouched", got.From.Commit)
	}
}

func TestALiveTargetInARepoWithNoCommitsKeepsWhatItHad(t *testing.T) {
	// Nothing to follow yet. Overwriting the baseline with an empty ref would
	// turn the diff into "everything against nothing".
	live := domain.Target{Kind: domain.TargetLive, From: domain.SnapshotRef{Commit: usecase.EmptyTree}}
	if got := usecase.ResolveLive(live, domain.SnapshotRef{}); got.From.Commit != usecase.EmptyTree {
		t.Errorf("baseline = %q, want the empty tree it started with", got.From.Commit)
	}
}

func TestInStoreRecognisesMsrsOwnBookkeeping(t *testing.T) {
	// msr writes its store inside the repository by default, so without this
	// every review contains msr's own files — and, worse, the live watcher
	// toasts the reviewer about them the moment it saves anything.
	in := usecase.InStore(".mondspace-reviewer")

	for _, f := range []string{
		".mondspace-reviewer",
		".mondspace-reviewer/audit.jsonl",
		".mondspace-reviewer/sessions/s1.jsonl",
	} {
		if !in(f) {
			t.Errorf("%q is the store and should be ignored", f)
		}
	}
	for _, f := range []string{
		"main.go",
		".mondspace-reviewer-notes.md", // a prefix, but not the directory
		"src/.mondspace-reviewer/x",    // not at the root the store lives at
	} {
		if in(f) {
			t.Errorf("%q is the reviewer's own work and must be shown", f)
		}
	}
}

func TestInStoreWithNoPathIgnoresNothing(t *testing.T) {
	// A store kept outside the repository has no path inside it. A predicate
	// that matched on the empty string would hide the entire review.
	in := usecase.InStore("")
	for _, f := range []string{"main.go", "", "a/b.go"} {
		if in(f) {
			t.Errorf("with no store path inside the repo, %q must still be shown", f)
		}
	}
}

func TestSortingKeepsTheLiveTargetOnTop(t *testing.T) {
	// Sorting is by recency, and the live target has no timestamp — it is not a
	// point in history. Left to the general rule it sinks below every commit
	// ever made, which is the opposite of where the thing you are working on
	// right now belongs.
	now := time.Now()
	targets := []domain.Target{
		{Kind: domain.TargetCommit, Title: "older", TS: now.Add(-time.Hour)},
		{Kind: domain.TargetLive, Title: "live"},
		{Kind: domain.TargetCommit, Title: "newer", TS: now},
	}

	usecase.SortTargets(targets)

	if targets[0].Kind != domain.TargetLive {
		t.Fatalf("first = %q (%s), want the live target", targets[0].Title, targets[0].Kind)
	}
	// And everything else still reads newest first.
	if targets[1].Title != "newer" || targets[2].Title != "older" {
		t.Errorf("the rest should stay newest-first, got %q then %q",
			targets[1].Title, targets[2].Title)
	}
}
