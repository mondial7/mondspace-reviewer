package usecase_test

import (
	"fmt"
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

	got, dropped := usecase.CompactDiff(domain.Diff{Text: b.String()}, 12)

	if dropped == 0 {
		t.Fatal("a 200-line diff should report what it left out")
	}
	lines := strings.Split(strings.TrimRight(got.Text, "\n"), "\n")
	if len(lines) > 12 {
		t.Errorf("compacted diff has %d lines, want at most 12", len(lines))
	}
	// Hunk headers are the map of the change: losing them loses the shape.
	if !strings.Contains(got.Text, "@@ -1,200 +1,200 @@") {
		t.Errorf("the first hunk header must survive:\n%s", got.Text)
	}
	// Every line that comes back is a line of the diff. The count of what was
	// left out is returned, not written into the text: a sentence pretending to
	// be a diff line is something a reviewer cannot click, cannot annotate and
	// cannot tell from code.
	for _, line := range lines {
		if strings.HasPrefix(line, "…") {
			t.Errorf("the elision was fabricated as a diff line: %q", line)
		}
	}
	// The count has to be the truth: it is the number the page offers to show.
	total := len(strings.Split(strings.TrimRight(b.String(), "\n"), "\n"))
	if want := total - len(lines); dropped != want {
		t.Errorf("dropped = %d, want %d (%d in, %d shown)", dropped, want, total, len(lines))
	}
}

func TestCompactDiffDropsGitFileHeaders(t *testing.T) {
	// A feed shows the filename above the diff, so git's file plumbing is pure
	// noise — and in a 14-line budget it was eating five of them.
	d := domain.Diff{Text: "diff --git a/x.go b/x.go\n" +
		"new file mode 100644\n" +
		"index 0000000..f8632dd\n" +
		"--- /dev/null\n" +
		"+++ b/x.go\n" +
		"@@ -0,0 +1,2 @@\n+package x\n+// hello\n"}

	got, _ := usecase.CompactDiff(d, 12)

	for _, noise := range []string{"diff --git", "new file mode", "index 0000000", "/dev/null"} {
		if strings.Contains(got.Text, noise) {
			t.Errorf("git plumbing %q should not reach the feed:\n%s", noise, got.Text)
		}
	}
	// The change itself, and the hunk header that locates it, must survive.
	for _, keep := range []string{"@@ -0,0 +1,2 @@", "+package x", "+// hello"} {
		if !strings.Contains(got.Text, keep) {
			t.Errorf("the actual change %q was lost:\n%s", keep, got.Text)
		}
	}
}

func TestCompactDiffLeavesAShortDiffAlone(t *testing.T) {
	d := domain.Diff{Text: "@@ -1 +1 @@\n-old\n+new\n"}

	got, dropped := usecase.CompactDiff(d, 12)

	if dropped != 0 {
		t.Errorf("a short diff dropped %d lines, want none", dropped)
	}
	if got.Text != d.Text {
		t.Errorf("a short diff must be returned unchanged, got %q", got.Text)
	}
}

func TestReviewFingerprintTracksContentNotJustTheFileSet(t *testing.T) {
	// A live review diffs against the working tree, so a unit's "to" snapshot is
	// empty and never moves. Fingerprinting units alone would therefore miss
	// every edit that did not add or remove a file — which is most of them.
	before := []domain.FileStat{
		{Path: "auth/token.go", Added: 10, Removed: 2},
		{Path: "http/mw.go", Added: 4, Removed: 0},
	}
	after := []domain.FileStat{
		{Path: "auth/token.go", Added: 18, Removed: 2}, // same file, more work
		{Path: "http/mw.go", Added: 4, Removed: 0},
	}

	if usecase.ReviewFingerprint(before) == usecase.ReviewFingerprint(after) {
		t.Error("more lines in the same file must change the fingerprint")
	}
	if usecase.ReviewFingerprint(before) != usecase.ReviewFingerprint(before) {
		t.Error("the fingerprint must be stable for identical input")
	}
}

func TestReviewFingerprintIgnoresOrderAndNoticesNewFiles(t *testing.T) {
	a := []domain.FileStat{{Path: "a.go", Added: 1}, {Path: "b.go", Added: 2}}
	shuffled := []domain.FileStat{{Path: "b.go", Added: 2}, {Path: "a.go", Added: 1}}
	grown := append(append([]domain.FileStat(nil), a...), domain.FileStat{Path: "c.go", Added: 3})

	if usecase.ReviewFingerprint(a) != usecase.ReviewFingerprint(shuffled) {
		t.Error("git may list files in any order; the fingerprint must not care")
	}
	if usecase.ReviewFingerprint(a) == usecase.ReviewFingerprint(grown) {
		t.Error("a new file must change the fingerprint")
	}
	if usecase.ReviewFingerprint(nil) == "" {
		t.Error("an empty review still has a fingerprint")
	}
}

func TestSessionAskContextCarriesTheChangesThemselves(t *testing.T) {
	// Asked "what are the most important changes?", the assistant replied that
	// the context held only file names and no diffs — and it was right. A
	// question about a session cannot be answered from metadata.
	units := []domain.Unit{
		{ID: "u1", Files: []string{"auth/token.go"}},
		{ID: "u2", Files: []string{"http/mw.go"}},
	}
	diffs := map[string]domain.Diff{
		"u1": {Text: "@@ -1 +1 @@\n+type TokenValidator interface{}\n"},
		"u2": {Text: "@@ -1 +1 @@\n+mux.Use(auth)\n"},
	}
	ctx := usecase.BuildAskContext(domain.AskSession, domain.Session{Units: units}, domain.Unit{}, domain.Diff{})

	got := usecase.WithChanges(ctx, units, diffs, 200)

	for _, want := range []string{"auth/token.go", "TokenValidator", "http/mw.go", "mux.Use"} {
		if !strings.Contains(got.Diff.Text, want) {
			t.Errorf("the digest is missing %q:\n%s", want, got.Diff.Text)
		}
	}
}

func TestSessionAskContextStaysWithinItsBudget(t *testing.T) {
	// A 400-file session must not become a prompt no context window can hold.
	var units []domain.Unit
	diffs := map[string]domain.Diff{}
	for i := 0; i < 400; i++ {
		id := fmt.Sprintf("u%03d", i)
		units = append(units, domain.Unit{ID: id, Files: []string{fmt.Sprintf("pkg%03d/a.go", i)}})
		diffs[id] = domain.Diff{Text: strings.Repeat("+line\n", 200)}
	}

	got := usecase.WithChanges(domain.AskContext{}, units, diffs, 120)

	if lines := strings.Count(got.Diff.Text, "\n"); lines > 140 {
		t.Errorf("digest is %d lines, want roughly the 120 asked for", lines)
	}
	// And it must say what it left out rather than pretending that was all.
	if !strings.Contains(got.Diff.Text, "more file") {
		t.Errorf("the digest should say how much it omitted:\n%s", got.Diff.Text[:300])
	}
}

// The whole reason ChangeFingerprint exists beside ReviewFingerprint: a file
// edited so that its added and removed counts land where they were.
func TestChangeFingerprintSeesWhatChurnCannot(t *testing.T) {
	units := []domain.Unit{{ID: "u1", Files: []string{"a.go"}}}
	before := map[string]domain.Diff{"u1": {Text: "@@\n-old\n+new\n"}}
	after := map[string]domain.Diff{"u1": {Text: "@@\n-old\n+other\n"}}

	stats := []domain.FileStat{{Path: "a.go", Added: 1, Removed: 1}}
	if usecase.ReviewFingerprint(stats) != usecase.ReviewFingerprint(stats) {
		t.Fatal("ReviewFingerprint is not stable")
	}

	if usecase.ChangeFingerprint(units, before) == usecase.ChangeFingerprint(units, after) {
		t.Fatal("a rewritten line did not move the fingerprint")
	}
	if usecase.ChangeFingerprint(units, before) != usecase.ChangeFingerprint(units, before) {
		t.Fatal("the same review fingerprinted differently twice")
	}

	// Order is git's business, not the review's.
	two := []domain.Unit{{ID: "u1", Files: []string{"a.go"}}, {ID: "u2", Files: []string{"b.go"}}}
	flipped := []domain.Unit{two[1], two[0]}
	diffs := map[string]domain.Diff{"u1": {Text: "one"}, "u2": {Text: "two"}}
	if usecase.ChangeFingerprint(two, diffs) != usecase.ChangeFingerprint(flipped, diffs) {
		t.Fatal("file order changed the fingerprint")
	}
}
