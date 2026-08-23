package usecase_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func cockpitSession() domain.Session {
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	return domain.Session{
		ID:     "s",
		Prompt: "add token validation",
		Events: []domain.Event{
			{ID: "e1", TS: base, Kind: domain.KindPrompt},
			{ID: "e2", TS: base.Add(12 * time.Minute), Kind: domain.KindEdit},
		},
	}
}

func TestSessionStatsCountsWhatTheReviewerAsksAbout(t *testing.T) {
	units := []domain.Unit{
		{ID: "s-f001", Files: []string{"auth/token.go"}},
		{ID: "s-f002", Files: []string{"auth/token_test.go"}},
	}
	diffs := map[string]domain.Diff{
		"s-f001": {Text: "@@ -1,2 +1,3 @@\n-old\n+new\n+extra\n"},
		"s-f002": {Text: "@@ -0,0 +1,2 @@\n+package auth\n+// tests\n"},
	}
	commits := []domain.Commit{
		{Hash: "aaa", Subject: "Add a TokenValidator"},
		{Hash: "bbb", Subject: "Merge pull request #42 from x/y"},
	}
	now := time.Date(2026, 8, 23, 9, 30, 0, 0, time.UTC)

	got := usecase.ComputeStats(cockpitSession(), units, diffs, commits, now)

	if got.Files != 2 {
		t.Errorf("Files = %d, want 2", got.Files)
	}
	if got.Added != 4 || got.Removed != 1 {
		t.Errorf("lines = +%d -%d, want +4 -1", got.Added, got.Removed)
	}
	if got.Commits != 2 {
		t.Errorf("Commits = %d, want 2", got.Commits)
	}
	// A merged pull request is the one forge fact a git log actually carries.
	if got.PullRequests != 1 {
		t.Errorf("PullRequests = %d, want 1 (from the merge commit subject)", got.PullRequests)
	}
	// Open for 30 minutes: from the first event to now.
	if got.Open != 30*time.Minute {
		t.Errorf("Open = %v, want 30m", got.Open)
	}
}

func TestSessionStatsCountsSquashMergedPullRequests(t *testing.T) {
	// GitHub's squash merge writes "Subject (#42)" rather than a merge commit,
	// and is the common case for an agent's work landing.
	commits := []domain.Commit{
		{Hash: "a", Subject: "Add a TokenValidator (#42)"},
		{Hash: "b", Subject: "Fix the retry (#43)"},
		{Hash: "c", Subject: "No pull request here"},
		{Hash: "d", Subject: "Duplicate reference (#42)"},
	}

	got := usecase.ComputeStats(cockpitSession(), nil, nil, commits, time.Now())

	// Distinct pull requests, not distinct commits: two commits can land one PR.
	if got.PullRequests != 2 {
		t.Errorf("PullRequests = %d, want 2 distinct (#42, #43)", got.PullRequests)
	}
}

func TestSessionIsLiveWhileSomethingIsStillHappening(t *testing.T) {
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	sess := domain.Session{ID: "s", Events: []domain.Event{{TS: base}}}

	// Something happened a moment ago: the session is alive and more will come.
	if got := usecase.ComputeStats(sess, nil, nil, nil, base.Add(30*time.Second)); !got.Live {
		t.Error("a session with a just-now event should read as live")
	}
	// Nothing for a long while: it is finished, and the page should be calm.
	if got := usecase.ComputeStats(sess, nil, nil, nil, base.Add(2*time.Hour)); got.Live {
		t.Error("a long-idle session should not read as live")
	}
}

func TestSessionStatsSurvivesAnEmptySession(t *testing.T) {
	// A session with no events must not report a duration since the zero time.
	got := usecase.ComputeStats(domain.Session{ID: "s"}, nil, nil, nil, time.Now())

	if got.Open != 0 || got.Live {
		t.Errorf("an empty session should be idle with no duration, got %+v", got)
	}
}

func TestCompactDiffKeepsTheShapeAndElidesTheBulk(t *testing.T) {
	var b strings.Builder
	b.WriteString("@@ -1,200 +1,200 @@\n")
	for i := 0; i < 200; i++ {
		b.WriteString("+line\n")
	}
	b.WriteString("@@ -400,3 +400,3 @@\n-gone\n+here\n")

	got, compacted := usecase.CompactDiff(domain.Diff{Text: b.String()}, 12)

	if !compacted {
		t.Fatal("a 200-line diff should be reported as compacted")
	}
	lines := strings.Split(strings.TrimRight(got.Text, "\n"), "\n")
	if len(lines) > 14 { // the cap, plus the elision marker
		t.Errorf("compacted diff has %d lines, want at most ~12", len(lines))
	}
	// Hunk headers are the map of the change: losing them loses the shape.
	if !strings.Contains(got.Text, "@@ -1,200 +1,200 @@") {
		t.Errorf("the first hunk header must survive:\n%s", got.Text)
	}
	// The reader must be told something was left out, never silently.
	if !strings.Contains(got.Text, "…") && !strings.Contains(got.Text, "more line") {
		t.Errorf("an elision must be visible:\n%s", got.Text)
	}
}

func TestCompactDiffLeavesAShortDiffAlone(t *testing.T) {
	d := domain.Diff{Text: "@@ -1 +1 @@\n-old\n+new\n"}

	got, compacted := usecase.CompactDiff(d, 12)

	if compacted {
		t.Error("a short diff should not be reported as compacted")
	}
	if got.Text != d.Text {
		t.Errorf("a short diff must be returned unchanged, got %q", got.Text)
	}
}
