package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"

	gitsnap "github.com/mondial7/mondspace-reviewer/internal/adapter/snapshot/git"
)

func gitCommitAt(t *testing.T, dir, date, msg string) {
	t.Helper()
	cmd := exec.Command("git", "commit", "-qm", msg)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
}

func TestBaselineNetChangesAndDiff(t *testing.T) {
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q")
	gitCmd(t, dir, "config", "user.email", "t@t")
	gitCmd(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "a.txt")
	gitCommitAt(t, dir, "2026-01-01T00:00:00", "init")

	// The agent's session (committed later): change a.txt and add b.txt.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\nv2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "-A")
	gitCommitAt(t, dir, "2026-07-01T00:00:00", "work")

	s := gitsnap.New(dir, "sess")
	ctx := context.Background()

	// Baseline: the commit at/before a mid-session time is the init commit.
	baseline, err := s.Baseline(ctx, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Baseline: %v", err)
	}
	if baseline.Commit == "" {
		t.Fatal("Baseline returned empty commit")
	}

	// Net changed files since the baseline: a.txt (changed) and b.txt (added).
	files, err := s.ChangedFiles(ctx, baseline, domain.SnapshotRef{})
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	got := strings.Join(files, ",")
	if !strings.Contains(got, "a.txt") || !strings.Contains(got, "b.txt") {
		t.Errorf("ChangedFiles = %v, want a.txt and b.txt", files)
	}

	// A worktree diff (empty `to`) shows the net change of a file.
	d, err := s.Diff(ctx, baseline, domain.SnapshotRef{}, []string{"a.txt"})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(d.Text, "+v2") {
		t.Errorf("net diff missing the added line:\n%s", d.Text)
	}
}

func TestChangedFilesBoundedByUntilExcludesLaterCommits(t *testing.T) {
	dir := newRepo(t) // commit 1: a.txt
	gitCmd(t, dir, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("mid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "b.txt")
	gitCommitAt(t, dir, "2026-01-02T00:00:00", "mid") // commit 2: adds b.txt

	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("late\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "c.txt")
	gitCommitAt(t, dir, "2026-01-03T00:00:00", "late") // commit 3: adds c.txt

	s := gitsnap.New(dir, "sess")
	ctx := context.Background()

	initRef, err := s.ResolveRef(ctx, "HEAD~2")
	if err != nil {
		t.Fatalf("ResolveRef HEAD~2: %v", err)
	}
	midRef, err := s.ResolveRef(ctx, "HEAD~1")
	if err != nil {
		t.Fatalf("ResolveRef HEAD~1: %v", err)
	}

	files, err := s.ChangedFiles(ctx, initRef, midRef)
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	got := strings.Join(files, ",")
	if !strings.Contains(got, "b.txt") {
		t.Errorf("ChangedFiles(init, mid) = %v, want b.txt", files)
	}
	if strings.Contains(got, "c.txt") {
		t.Errorf("ChangedFiles(init, mid) = %v, must not include c.txt (added after --until)", files)
	}
}

func TestResolveRefResolvesCommitBranchOrTag(t *testing.T) {
	dir := newRepo(t)
	head := gitCmd(t, dir, "rev-parse", "HEAD")
	gitCmd(t, dir, "branch", "feature")
	gitCmd(t, dir, "tag", "v1")

	s := gitsnap.New(dir, "sess")
	ctx := context.Background()

	for _, ref := range []string{"HEAD", "feature", "v1", head} {
		got, err := s.ResolveRef(ctx, ref)
		if err != nil {
			t.Fatalf("ResolveRef(%q): %v", ref, err)
		}
		if got.Commit != head {
			t.Errorf("ResolveRef(%q).Commit = %q, want %q", ref, got.Commit, head)
		}
	}

	if _, err := s.ResolveRef(ctx, "does-not-exist"); err == nil {
		t.Error("ResolveRef with an unknown ref should error")
	}
}

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo makes a temp git repo with one committed file and returns its path.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q")
	gitCmd(t, dir, "config", "user.email", "t@t")
	gitCmd(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "a.txt")
	gitCmd(t, dir, "commit", "-qm", "init")
	return dir
}

func TestSnapshotHonoursGitignore(t *testing.T) {
	dir := newRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("secret.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("password\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ref, err := gitsnap.New(dir, "sess-1").Snapshot(context.Background(), "s1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// The ignored file must not appear in the snapshot tree.
	tree := gitCmd(t, dir, "ls-tree", "-r", "--name-only", ref.Commit)
	if strings.Contains(tree, "secret.txt") {
		t.Errorf("ignored file captured in snapshot:\n%s", tree)
	}
	if !strings.Contains(tree, "a.txt") {
		t.Errorf("tracked file missing from snapshot:\n%s", tree)
	}
}

func TestDiffStableAfterWorkingFileDeleted(t *testing.T) {
	dir := newRepo(t)
	s := gitsnap.New(dir, "sess-1")

	from, err := s.Snapshot(context.Background(), "before")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	to, err := s.Snapshot(context.Background(), "after")
	if err != nil {
		t.Fatal(err)
	}

	// The working-tree file vanishes before we ask for the diff.
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}

	diff, err := s.Diff(context.Background(), from, to, []string{"a.txt"})
	if err != nil {
		t.Fatalf("Diff must resolve from snapshots even after deletion: %v", err)
	}
	if !strings.Contains(diff.Text, "+changed") {
		t.Errorf("diff lost the historical change:\n%s", diff.Text)
	}
}

func TestDiffBetweenTwoSnapshots(t *testing.T) {
	dir := newRepo(t)
	s := gitsnap.New(dir, "sess-1")

	from, err := s.Snapshot(context.Background(), "before")
	if err != nil {
		t.Fatalf("Snapshot before: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\nv2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	to, err := s.Snapshot(context.Background(), "after")
	if err != nil {
		t.Fatalf("Snapshot after: %v", err)
	}

	diff, err := s.Diff(context.Background(), from, to, []string{"a.txt"})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	if !strings.Contains(diff.Text, "+v2") {
		t.Errorf("diff missing the added line:\n%s", diff.Text)
	}
	if !strings.Contains(diff.Text, "a.txt") {
		t.Errorf("diff missing the file name:\n%s", diff.Text)
	}
}

func TestSnapshotWorksWithoutAmbientGitIdentity(t *testing.T) {
	// Simulate a machine (like a fresh CI runner or container) that refuses to
	// invent a git identity: useConfigOnly, and a repo with no local user.*.
	gcfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(gcfg, []byte("[user]\n\tuseConfigOnly = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gcfg)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "a.txt")
	gitCmd(t, dir, "commit", "-qm", "init") // identity supplied via env, not config

	// The snapshotter must supply its own identity for its throwaway commits.
	if _, err := gitsnap.New(dir, "sess-1").Snapshot(context.Background(), "s1"); err != nil {
		t.Fatalf("Snapshot must work without an ambient git identity: %v", err)
	}
}

func TestReviewRefsListsSessionsWithReviewRefs(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()

	if _, err := gitsnap.New(dir, "sess-b").Snapshot(ctx, "b1"); err != nil {
		t.Fatalf("Snapshot sess-b: %v", err)
	}
	if _, err := gitsnap.New(dir, "sess-a").Snapshot(ctx, "a1"); err != nil {
		t.Fatalf("Snapshot sess-a: %v", err)
	}

	refs, err := gitsnap.New(dir, "irrelevant").ReviewRefs(ctx)
	if err != nil {
		t.Fatalf("ReviewRefs: %v", err)
	}
	if len(refs) != 2 || refs[0] != "sess-a" || refs[1] != "sess-b" {
		t.Errorf("ReviewRefs = %v, want [sess-a sess-b] (sorted)", refs)
	}
}

func TestReviewRefsEmptyWhenNoneExist(t *testing.T) {
	dir := newRepo(t)
	refs, err := gitsnap.New(dir, "irrelevant").ReviewRefs(context.Background())
	if err != nil {
		t.Fatalf("ReviewRefs: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("ReviewRefs = %v, want none", refs)
	}
}

func TestDeleteReviewRefRemovesTheRef(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()

	s := gitsnap.New(dir, "sess-1")
	if _, err := s.Snapshot(ctx, "s1"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	refs, err := s.ReviewRefs(ctx)
	if err != nil || len(refs) != 1 {
		t.Fatalf("ReviewRefs before delete = %v, %v", refs, err)
	}

	if err := s.DeleteReviewRef(ctx, "sess-1"); err != nil {
		t.Fatalf("DeleteReviewRef: %v", err)
	}

	refs, err = s.ReviewRefs(ctx)
	if err != nil {
		t.Fatalf("ReviewRefs after delete: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("ReviewRefs after delete = %v, want none", refs)
	}
}

func TestDeleteReviewRefOnAbsentRefIsNotAnError(t *testing.T) {
	dir := newRepo(t)
	s := gitsnap.New(dir, "irrelevant")
	if err := s.DeleteReviewRef(context.Background(), "never-existed"); err != nil {
		t.Errorf("DeleteReviewRef on an absent ref should not error: %v", err)
	}
}

func TestSnapshotLeavesHeadIndexAndWorktreeUnchanged(t *testing.T) {
	dir := newRepo(t)

	// Create a staged change and a working-tree change to make the state rich.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	headBefore := gitCmd(t, dir, "rev-parse", "HEAD")
	statusBefore := gitCmd(t, dir, "status", "--porcelain")
	stagedBefore := gitCmd(t, dir, "diff", "--cached", "--name-status")

	ref, err := gitsnap.New(dir, "sess-1").Snapshot(context.Background(), "s1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if ref.Commit == "" {
		t.Error("Snapshot returned an empty commit ref")
	}
	if got := gitCmd(t, dir, "rev-parse", "HEAD"); got != headBefore {
		t.Errorf("HEAD moved: %s -> %s", headBefore, got)
	}
	if got := gitCmd(t, dir, "status", "--porcelain"); got != statusBefore {
		t.Errorf("working tree/index changed:\nbefore:\n%s\nafter:\n%s", statusBefore, got)
	}
	if got := gitCmd(t, dir, "diff", "--cached", "--name-status"); got != stagedBefore {
		t.Errorf("staged index changed:\nbefore:\n%s\nafter:\n%s", stagedBefore, got)
	}
	// The snapshot commit must be a real object.
	gitCmd(t, dir, "cat-file", "-e", ref.Commit)
}

func TestCommitsSinceListsOnlyWorkDoneInTheWindow(t *testing.T) {
	// The cockpit reports how many commits a session produced. Commits that
	// predate the session are somebody else's work and must not be counted.
	//
	// Commit dates are set explicitly rather than slept for: `git log --since`
	// resolves to whole seconds and is inclusive, so a test that raced the clock
	// would be flaky at exactly the boundary it is meant to check.
	dir := newRepo(t) // seeds one "init" commit, dated now

	base := time.Now()
	commitAt(t, dir, base.Add(-2*time.Hour), "old.txt", "Work from before the session")
	commitAt(t, dir, base.Add(2*time.Hour), "b.txt", "Add a TokenValidator (#42)")

	// The session began an hour ago: the old commit is out, the new one is in.
	since := base.Add(-time.Hour)

	got, err := gitsnap.New(dir, "sess-1").CommitsSince(context.Background(), since)
	if err != nil {
		t.Fatalf("CommitsSince: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d commits, want only the one made during the session: %+v", len(got), got)
	}
	if got[0].Subject != "Add a TokenValidator (#42)" {
		t.Errorf("Subject = %q", got[0].Subject)
	}
	if got[0].Hash == "" || got[0].TS.IsZero() {
		t.Errorf("a commit needs a hash and a timestamp: %+v", got[0])
	}
}

// commitAt writes a file and commits it with an explicit date on both clocks, so
// a test can place a commit before or after a window without waiting for one.
func commitAt(t *testing.T, dir string, when time.Time, name, subject string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := when.Format(time.RFC3339)
	gitCmd(t, dir, "add", name)
	c := exec.Command("git", "commit", "-qm", subject)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp,
	)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("commit %q: %v\n%s", subject, err, out)
	}
}

func TestCommitsSinceIsEmptyNotAnErrorInARepoWithNoCommits(t *testing.T) {
	// `git log` fails outright on a repo with no HEAD. An empty history is an
	// ordinary state for a fresh project, not a reason to break the page.
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q")

	got, err := gitsnap.New(dir, "sess-1").CommitsSince(context.Background(), time.Now().Add(-time.Hour))

	if err != nil {
		t.Fatalf("an empty history should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d commits, want none", len(got))
	}
}
