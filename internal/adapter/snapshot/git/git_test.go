package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// gitAs runs git with a chosen author, which gitCmd cannot do: it pins the
// identity through the environment, and the environment beats config.
func gitAs(t *testing.T, dir, who string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+who, "GIT_AUTHOR_EMAIL="+who+"@example.com",
		"GIT_COMMITTER_NAME="+who, "GIT_COMMITTER_EMAIL="+who+"@example.com",
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
	// Content must differ per commit, or git has nothing to record and the
	// commit fails — which would make the test pass for the wrong reason.
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"+subject+"\n"), 0o644); err != nil {
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

func TestNumstatReportsPerFileChurnIncludingUntracked(t *testing.T) {
	// The cockpit polls this every few seconds, so it must be one cheap call —
	// and it must see brand-new files, which are exactly what an agent creates.
	dir := newRepo(t)
	s := gitsnap.New(dir, "sess-1")

	base, err := s.ResolveRef(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\nv2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\ny\nz\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := s.Numstat(context.Background(), base, domain.SnapshotRef{})
	if err != nil {
		t.Fatalf("Numstat: %v", err)
	}

	byPath := map[string]domain.FileStat{}
	for _, f := range got {
		byPath[f.Path] = f
	}
	if a := byPath["a.txt"]; a.Added != 1 {
		t.Errorf("a.txt = +%d -%d, want the one added line", a.Added, a.Removed)
	}
	if n, ok := byPath["new.txt"]; !ok || n.Added != 3 {
		t.Errorf("untracked new.txt = %+v, want +3 — an agent's new files must count", n)
	}
}

func TestNumstatIsEmptyNotAnErrorWithNoChanges(t *testing.T) {
	dir := newRepo(t)
	s := gitsnap.New(dir, "sess-1")
	base, _ := s.ResolveRef(context.Background(), "HEAD")

	got, err := s.Numstat(context.Background(), base, domain.SnapshotRef{})

	if err != nil {
		t.Fatalf("Numstat on a clean tree should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want nothing changed", got)
	}
}

func TestFileVersionsWalksACommitHistory(t *testing.T) {
	// The overlay steps through a file's versions, so it needs the commits that
	// touched that file and nothing else.
	dir := newRepo(t)
	base := time.Now()
	commitAt(t, dir, base.Add(-3*time.Hour), "other.txt", "Unrelated work")
	commitAt(t, dir, base.Add(-2*time.Hour), "tracked.go", "First version")
	commitAt(t, dir, base.Add(-1*time.Hour), "tracked.go", "Second version")

	got, err := gitsnap.New(dir, "s").FileVersions(context.Background(), "tracked.go", 10)
	if err != nil {
		t.Fatalf("FileVersions: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d versions, want only the commits touching that file: %+v", len(got), got)
	}
	// Newest first: stepping "back" from the current state is the natural motion.
	if got[0].Subject != "Second version" || got[1].Subject != "First version" {
		t.Errorf("versions = %+v, want newest first", got)
	}
	if got[0].Hash == "" || got[0].TS.IsZero() {
		t.Errorf("a version needs a commit and a date: %+v", got[0])
	}
}

func TestDiffAtShowsWhatOneCommitDidToOneFile(t *testing.T) {
	dir := newRepo(t)
	base := time.Now()
	commitAt(t, dir, base.Add(-time.Hour), "tracked.go", "Add tracked.go")

	versions, err := gitsnap.New(dir, "s").FileVersions(context.Background(), "tracked.go", 10)
	if err != nil || len(versions) == 0 {
		t.Fatalf("FileVersions: %v %+v", err, versions)
	}

	diff, err := gitsnap.New(dir, "s").DiffAt(context.Background(), versions[0].Hash, "tracked.go")
	if err != nil {
		t.Fatalf("DiffAt: %v", err)
	}
	if !strings.Contains(diff.Text, "tracked.go") {
		t.Errorf("diff should name the file:\n%s", diff.Text)
	}
	if !strings.Contains(diff.Text, "+x") {
		t.Errorf("diff should show the added content:\n%s", diff.Text)
	}
}

func TestDiscoverReposFindsChildRepositories(t *testing.T) {
	// A workspace directory holding several checkouts is the common shape:
	// ~/work/{api,web,worker}. Pointing msr at the parent should find them.
	root := t.TempDir()
	for _, name := range []string{"api", "web", "worker"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitCmd(t, dir, "init", "-q")
	}
	// A plain directory is not a repository and must not be offered as one.
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := gitsnap.DiscoverRepos(root)

	if len(got) != 3 {
		t.Fatalf("found %d repositories, want 3: %v", len(got), got)
	}
	for i, want := range []string{"api", "web", "worker"} {
		if filepath.Base(got[i]) != want {
			t.Errorf("repo %d = %q, want %q (sorted for a stable prompt)", i, got[i], want)
		}
	}
}

func TestDiscoverReposPrefersTheRootWhenItIsItselfARepository(t *testing.T) {
	// Run inside a checkout and that checkout is the answer, even if it happens
	// to contain vendored or nested repositories.
	root := newRepo(t)
	nested := filepath.Join(root, "vendor", "dep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, nested, "init", "-q")

	got := gitsnap.DiscoverRepos(root)

	if len(got) != 1 || filepath.Base(got[0]) != filepath.Base(root) {
		t.Errorf("got %v, want just the root repository", got)
	}
}

func TestDiscoverReposIgnoresHiddenAndMissingDirectories(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, ".cache", "thing")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, hidden, "init", "-q")

	if got := gitsnap.DiscoverRepos(root); len(got) != 0 {
		t.Errorf("got %v, want nothing from a hidden directory", got)
	}
	if got := gitsnap.DiscoverRepos(filepath.Join(root, "nope")); len(got) != 0 {
		t.Errorf("got %v, want nothing for a path that does not exist", got)
	}
}

func TestRecentCommitsWalksHistoryNewestFirst(t *testing.T) {
	dir := newRepo(t)
	base := time.Now()
	commitAt(t, dir, base.Add(-3*time.Hour), "a.go", "First")
	commitAt(t, dir, base.Add(-2*time.Hour), "b.go", "Second")
	commitAt(t, dir, base.Add(-1*time.Hour), "c.go", "Third")

	got, err := gitsnap.New(dir, "s").RecentCommits(context.Background(), 2)
	if err != nil {
		t.Fatalf("RecentCommits: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d commits, want the limit of 2: %+v", len(got), got)
	}
	if got[0].Subject != "Third" || got[1].Subject != "Second" {
		t.Errorf("commits = %+v, want newest first", got)
	}
	// The parent is what makes a commit reviewable on its own.
	if got[0].Parent == "" {
		t.Errorf("a commit needs its parent to be a range: %+v", got[0])
	}
}

func TestRecentCommitsGivesTheFirstCommitNoParent(t *testing.T) {
	// A root commit has nothing before it. Reviewing it means diffing against
	// the empty tree, which the caller can only do if it knows.
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "a.txt")
	gitCmd(t, dir, "commit", "-qm", "root")

	got, err := gitsnap.New(dir, "s").RecentCommits(context.Background(), 10)
	if err != nil {
		t.Fatalf("RecentCommits: %v", err)
	}

	if len(got) != 1 || got[0].Parent != "" {
		t.Errorf("root commit = %+v, want no parent", got)
	}
}

func TestTagsAreListedNewestFirst(t *testing.T) {
	dir := newRepo(t)
	base := time.Now()
	commitAt(t, dir, base.Add(-2*time.Hour), "a.go", "One")
	gitCmd(t, dir, "tag", "v1.0.0")
	commitAt(t, dir, base.Add(-time.Hour), "b.go", "Two")
	gitCmd(t, dir, "tag", "v1.1.0")

	got, err := gitsnap.New(dir, "s").Tags(context.Background(), 10)
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d tags, want 2: %+v", len(got), got)
	}
	if got[0].Name != "v1.1.0" || got[1].Name != "v1.0.0" {
		t.Errorf("tags = %+v, want newest first", got)
	}
	if got[0].Hash == "" || got[0].TS.IsZero() {
		t.Errorf("a tag needs its commit and date: %+v", got[0])
	}
}

func TestTagsIsEmptyNotAnErrorWithoutAny(t *testing.T) {
	got, err := gitsnap.New(newRepo(t), "s").Tags(context.Background(), 10)

	if err != nil {
		t.Fatalf("an untagged repository is not an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want none", got)
	}
}

func TestIsDirtyReportsUncommittedWork(t *testing.T) {
	dir := newRepo(t)
	s := gitsnap.New(dir, "s")

	if dirty, _ := s.IsDirty(context.Background()); dirty {
		t.Error("a fresh checkout is not dirty")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dirty, _ := s.IsDirty(context.Background()); !dirty {
		t.Error("an edited working tree is dirty")
	}
}

func TestCommitsBetweenCountsWhatIsInARange(t *testing.T) {
	// A tag target asks "what shipped since the previous tag". Asking for commits
	// *since the tag's date* answers the opposite question — everything after it —
	// which is how a release came to report zero commits.
	dir := newRepo(t)
	base := time.Now()
	commitAt(t, dir, base.Add(-4*time.Hour), "a.go", "One")
	gitCmd(t, dir, "tag", "v1.0.0")
	commitAt(t, dir, base.Add(-3*time.Hour), "b.go", "Two")
	commitAt(t, dir, base.Add(-2*time.Hour), "c.go", "Three")
	gitCmd(t, dir, "tag", "v1.1.0")
	commitAt(t, dir, base.Add(-time.Hour), "d.go", "After the release")

	s := gitsnap.New(dir, "s")
	from, _ := s.ResolveRef(context.Background(), "v1.0.0")
	to, _ := s.ResolveRef(context.Background(), "v1.1.0")

	got, err := s.CommitsBetween(context.Background(), from, to)
	if err != nil {
		t.Fatalf("CommitsBetween: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d commits, want the two between the tags: %+v", len(got), got)
	}
	if got[0].Subject != "Three" || got[1].Subject != "Two" {
		t.Errorf("commits = %+v, want newest first and nothing from after the tag", got)
	}
}

func TestCommitsBetweenHandlesAnOpenEnd(t *testing.T) {
	// An empty far end means the working tree, so everything since `from` counts.
	dir := newRepo(t)
	base := time.Now()
	commitAt(t, dir, base.Add(-2*time.Hour), "a.go", "One")
	gitCmd(t, dir, "tag", "v1.0.0")
	commitAt(t, dir, base.Add(-time.Hour), "b.go", "Two")

	s := gitsnap.New(dir, "s")
	from, _ := s.ResolveRef(context.Background(), "v1.0.0")

	got, err := s.CommitsBetween(context.Background(), from, domain.SnapshotRef{})
	if err != nil {
		t.Fatalf("CommitsBetween: %v", err)
	}
	if len(got) != 1 || got[0].Subject != "Two" {
		t.Errorf("got %+v, want everything since the tag", got)
	}
}

func TestNumstatSinceASnapshotIgnoresFilesThatHaveNotMoved(t *testing.T) {
	// A snapshot records untracked files too, because that is where an agent's
	// new work lives. Diffing against one therefore has to compare like with
	// like: `git diff` alone reports every still-untracked file as *deleted*
	// (the real index never had it) and the untracked scan then reports it as
	// added, so one unchanged file arrived twice with opposite signs.
	dir := newRepo(t)
	gitCmd(t, dir, "commit", "--allow-empty", "-m", "root")

	os.WriteFile(filepath.Join(dir, "new.go"), []byte("package a\n\nfunc A() {}\n"), 0o644)
	snap := gitsnap.New(dir, "s")
	pin, err := snap.Snapshot(context.Background(), "pin")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Nothing has happened since the pin.
	got, err := snap.NumstatSince(context.Background(), pin)
	if err != nil {
		t.Fatalf("NumstatSince: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("nothing changed since the pin, got %+v", got)
	}

	// Now something does.
	os.WriteFile(filepath.Join(dir, "new.go"), []byte("package a\n\nfunc A() {}\nfunc B() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "later.go"), []byte("package a\n"), 0o644)

	got, err = snap.NumstatSince(context.Background(), pin)
	if err != nil {
		t.Fatalf("NumstatSince: %v", err)
	}

	seen := map[string]domain.FileStat{}
	for _, f := range got {
		if _, dup := seen[f.Path]; dup {
			t.Errorf("%s reported twice: %+v", f.Path, got)
		}
		seen[f.Path] = f
	}
	if len(seen) != 2 {
		t.Fatalf("got %+v, want new.go and later.go", got)
	}
	if seen["new.go"].Added != 1 || seen["new.go"].Removed != 0 {
		t.Errorf("new.go = %+v, want one line added", seen["new.go"])
	}
	if seen["later.go"].Added != 1 {
		t.Errorf("later.go = %+v, want a new file", seen["later.go"])
	}
}

func TestRemoteReportsWhereTheBranchSitsAgainstItsUpstream(t *testing.T) {
	// The question the log card answers: am I behind, and who moved it
	// (issue #18).
	origin := newRepo(t)
	gitCmd(t, origin, "commit", "--allow-empty", "-m", "shared history")

	clone := t.TempDir()
	gitCmd(t, filepath.Dir(clone), "clone", "--quiet", origin, filepath.Base(clone))
	gitCmd(t, clone, "config", "user.email", "me@example.com")
	gitCmd(t, clone, "config", "user.name", "Me")

	snap := gitsnap.New(clone, "s")
	state, err := snap.Remote(context.Background())
	if err != nil {
		t.Fatalf("Remote: %v", err)
	}
	if state.Upstream == "" {
		t.Fatalf("a clone tracks its origin, got %+v", state)
	}
	if state.Ahead != 0 || state.Behind != 0 {
		t.Errorf("a fresh clone is level, got ahead=%d behind=%d", state.Ahead, state.Behind)
	}

	// Somebody else pushes.
	gitAs(t, origin, "Alice", "commit", "--allow-empty", "-m", "Alice: add the cache")

	// Not visible until the refs are updated — which is the whole reason
	// fetching is a thing msr can be asked to do.
	state, _ = snap.Remote(context.Background())
	if state.Behind != 0 {
		t.Errorf("without fetching there is nothing new to see, got behind=%d", state.Behind)
	}

	if err := snap.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	state, _ = snap.Remote(context.Background())

	if state.Behind != 1 {
		t.Errorf("behind = %d, want 1 after a colleague pushed", state.Behind)
	}
	if state.LastAuthor != "Alice" {
		t.Errorf("LastAuthor = %q, want the person who pushed", state.LastAuthor)
	}
	if state.LastSubject != "Alice: add the cache" {
		t.Errorf("LastSubject = %q", state.LastSubject)
	}
}

func TestFetchLeavesHeadIndexAndWorktreeAlone(t *testing.T) {
	// Fetching is the one thing msr does that writes to the repository, so what
	// it does not write matters (ADR 0025).
	origin := newRepo(t)
	gitCmd(t, origin, "commit", "--allow-empty", "-m", "one")

	clone := t.TempDir()
	gitCmd(t, filepath.Dir(clone), "clone", "--quiet", origin, filepath.Base(clone))
	os.WriteFile(filepath.Join(clone, "wip.txt"), []byte("uncommitted work\n"), 0o644)

	headBefore := gitCmd(t, clone, "rev-parse", "HEAD")
	statusBefore := gitCmd(t, clone, "status", "--porcelain")

	gitCmd(t, origin, "commit", "--allow-empty", "-m", "two")
	if err := gitsnap.New(clone, "s").Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if got := gitCmd(t, clone, "rev-parse", "HEAD"); got != headBefore {
		t.Error("fetching moved HEAD")
	}
	if got := gitCmd(t, clone, "status", "--porcelain"); got != statusBefore {
		t.Errorf("fetching changed the working tree or index:\n%s", got)
	}
	if body, _ := os.ReadFile(filepath.Join(clone, "wip.txt")); string(body) != "uncommitted work\n" {
		t.Error("fetching touched an uncommitted file")
	}
}

func TestRemoteBranchesExcludeTheOriginHeadAlias(t *testing.T) {
	// origin/HEAD is a symbolic alias for the default branch, not a branch
	// anyone pushed — announcing it as new would be a lie on the first fetch.
	origin := newRepo(t)
	gitCmd(t, origin, "commit", "--allow-empty", "-m", "one")

	clone := t.TempDir()
	gitCmd(t, filepath.Dir(clone), "clone", "--quiet", origin, filepath.Base(clone))

	state, _ := gitsnap.New(clone, "s").Remote(context.Background())
	for _, b := range state.Branches {
		if strings.HasSuffix(b, "/HEAD") {
			t.Errorf("branches should not include %q", b)
		}
	}
	if len(state.Branches) == 0 {
		t.Error("a clone has at least one remote-tracking branch")
	}
}

func TestBranchesReportDriftFromTheMainline(t *testing.T) {
	// The wider view: what is everyone else working on, and how much of it
	// would there be to review (ADR 0026).
	origin := newRepo(t)
	gitCmd(t, origin, "commit", "--allow-empty", "-m", "shared history")
	gitCmd(t, origin, "branch", "-M", "main")

	// A colleague's branch with two commits on it.
	gitCmd(t, origin, "checkout", "-q", "-b", "feature-x")
	gitAs(t, origin, "Alice", "commit", "--allow-empty", "-m", "Alice: start the cache")
	gitAs(t, origin, "Alice", "commit", "--allow-empty", "-m", "Alice: finish the cache")
	// ...and one that is already merged into main.
	gitCmd(t, origin, "checkout", "-q", "main")
	gitCmd(t, origin, "branch", "already-done")
	gitCmd(t, origin, "checkout", "-q", "main")

	clone := t.TempDir()
	gitCmd(t, filepath.Dir(clone), "clone", "--quiet", origin, filepath.Base(clone))

	got, err := gitsnap.New(clone, "s").Branches(context.Background(), "origin/main")
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}

	by := map[string]domain.Branch{}
	for _, b := range got {
		by[b.Short] = b
	}

	feature, ok := by["feature-x"]
	if !ok {
		t.Fatalf("feature-x missing from %+v", got)
	}
	if feature.Ahead != 2 {
		t.Errorf("feature-x ahead = %d, want 2", feature.Ahead)
	}
	if feature.Merged {
		t.Error("feature-x has work on it and is not merged")
	}
	if feature.Author != "Alice" {
		t.Errorf("Author = %q, want the person who pushed", feature.Author)
	}
	if feature.Subject != "Alice: finish the cache" {
		t.Errorf("Subject = %q, want the newest commit", feature.Subject)
	}

	if done, ok := by["already-done"]; !ok {
		t.Error("already-done missing")
	} else if !done.Merged {
		t.Errorf("already-done = %+v, want it marked merged", done)
	}
}

func TestBranchesFindTheDefaultWithoutBeingTold(t *testing.T) {
	origin := newRepo(t)
	gitCmd(t, origin, "commit", "--allow-empty", "-m", "one")
	gitCmd(t, origin, "branch", "-M", "main")

	clone := t.TempDir()
	gitCmd(t, filepath.Dir(clone), "clone", "--quiet", origin, filepath.Base(clone))

	got, _ := gitsnap.New(clone, "s").Branches(context.Background(), "")
	if len(got) == 0 {
		t.Fatal("a clone has remote branches")
	}
	if got[0].Base != "origin/main" {
		t.Errorf("Base = %q, want origin/main found on its own", got[0].Base)
	}
}

func TestBranchesSkipTheSymbolicHeadRef(t *testing.T) {
	// refs/remotes/origin/HEAD is a symbolic alias for the default branch, and
	// git's refname:short renders it as bare "origin" — so a suffix check for
	// "/HEAD" does not catch it, and it turns up in the list as a branch called
	// "origin" that nobody created.
	origin := newRepo(t)
	gitCmd(t, origin, "commit", "--allow-empty", "-m", "one")
	gitCmd(t, origin, "branch", "-M", "main")

	clone := t.TempDir()
	gitCmd(t, filepath.Dir(clone), "clone", "--quiet", origin, filepath.Base(clone))

	got, err := gitsnap.New(clone, "s").Branches(context.Background(), "")
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	for _, b := range got {
		if b.Name == "origin" || strings.HasSuffix(b.Name, "/HEAD") {
			t.Errorf("%+v is a symbolic alias, not a branch", b)
		}
	}
	if len(got) == 0 {
		t.Error("the real branch should still be listed")
	}
}

func TestIgnoredAppliesACustomFileToTrackedPaths(t *testing.T) {
	// msr's ignore rules are gitignore rules, applied by git rather than
	// reimplemented: the syntax has enough corners that a second
	// implementation would differ from the one people already know (ADR 0027).
	dir := newRepo(t)
	os.MkdirAll(filepath.Join(dir, "vendor", "x"), 0o755)
	os.MkdirAll(filepath.Join(dir, "api"), 0o755)
	for path, body := range map[string]string{
		"vendor/x/lib.go":     "a\n",
		"api/handler.go":      "b\n",
		"go.sum":              "c\n",
		"api/service.pb.go":   "d\n",
		"api/mock_service.go": "e\n",
	} {
		os.WriteFile(filepath.Join(dir, path), []byte(body), 0o644)
	}
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "everything")

	rules := filepath.Join(dir, ".msrignore")
	os.WriteFile(rules, []byte("vendor/\ngo.sum\n*.pb.go\nmock_*.go\n"), 0o644)

	paths := []string{"vendor/x/lib.go", "api/handler.go", "go.sum",
		"api/service.pb.go", "api/mock_service.go"}

	got, err := gitsnap.New(dir, "s").Ignored(context.Background(), rules, paths)
	if err != nil {
		t.Fatalf("Ignored: %v", err)
	}

	// Tracked files are never "ignored" to git in the ordinary sense, so this
	// has to ask about the rules rather than about the index.
	if _, hidden := got["api/handler.go"]; hidden {
		t.Error("the reviewer's own file must not be hidden")
	}
	for _, want := range []string{"vendor/x/lib.go", "go.sum", "api/service.pb.go", "api/mock_service.go"} {
		if _, hidden := got[want]; !hidden {
			t.Errorf("%s should be hidden by the rules", want)
		}
	}
	// The pattern comes back too, so the page can say *why* something is gone.
	if got["go.sum"] != "go.sum" {
		t.Errorf("go.sum matched %q, want the pattern that hid it", got["go.sum"])
	}
	if got["vendor/x/lib.go"] != "vendor/" {
		t.Errorf("vendor file matched %q", got["vendor/x/lib.go"])
	}
}

func TestNoIgnoreFileHidesNothing(t *testing.T) {
	// Nothing is hidden unless the reviewer asked for it. A review tool that
	// hides files by default is a review tool you cannot trust.
	dir := newRepo(t)
	got, err := gitsnap.New(dir, "s").Ignored(context.Background(),
		filepath.Join(dir, ".msrignore"), []string{"a.go", "go.sum"})
	if err != nil {
		t.Fatalf("Ignored: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want nothing hidden", got)
	}
}

func TestDiffAllReturnsEveryFileFromOneInvocation(t *testing.T) {
	// One git process for the whole range instead of one per file. At six
	// hundred files the difference is twenty-eight seconds a page load
	// (ADR 0029).
	dir := newRepo(t)
	os.MkdirAll(filepath.Join(dir, "pkg"), 0o755)
	for _, f := range []string{"a.go", "pkg/b.go", "pkg/c.go"} {
		os.WriteFile(filepath.Join(dir, f), []byte("package x\n\nfunc F() {}\n"), 0o644)
	}
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "before")
	base := strings.TrimSpace(gitCmd(t, dir, "rev-parse", "HEAD"))

	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\n\nfunc F(i int) {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "pkg/b.go"), []byte("package x\n\nfunc F() {}\nfunc G() {}\n"), 0o644)
	os.Remove(filepath.Join(dir, "pkg/c.go"))
	os.WriteFile(filepath.Join(dir, "pkg/d.go"), []byte("package x\n\nfunc H() {}\n"), 0o644)
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "after")
	head := strings.TrimSpace(gitCmd(t, dir, "rev-parse", "HEAD"))

	got, err := gitsnap.New(dir, "s").DiffAll(context.Background(),
		domain.SnapshotRef{Commit: base}, domain.SnapshotRef{Commit: head})
	if err != nil {
		t.Fatalf("DiffAll: %v", err)
	}

	// Modified, added and deleted alike: a review shows all three.
	for _, want := range []string{"a.go", "pkg/b.go", "pkg/c.go", "pkg/d.go"} {
		d, ok := got[want]
		if !ok {
			t.Errorf("%s missing from %v", want, keysOf(got))
			continue
		}
		if strings.TrimSpace(d.Text) == "" {
			t.Errorf("%s has an empty diff", want)
		}
	}
	if len(got) != 4 {
		t.Errorf("got %d files %v, want 4", len(got), keysOf(got))
	}

	// Each file's diff is its own, not the whole range repeated.
	if strings.Contains(got["a.go"].Text, "pkg/b.go") {
		t.Errorf("a.go carries another file's hunks:\n%s", got["a.go"].Text)
	}
	if !strings.Contains(got["a.go"].Text, "func F(i int)") {
		t.Errorf("a.go is missing its own change:\n%s", got["a.go"].Text)
	}
}

func TestDiffAllAgreesWithDiffingOneFile(t *testing.T) {
	// The batched path replaces the per-file one, so what it produces has to be
	// what the per-file one produced.
	dir := newRepo(t)
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\n\nfunc F() {}\n"), 0o644)
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "before")
	base := strings.TrimSpace(gitCmd(t, dir, "rev-parse", "HEAD"))
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\n\nfunc F(i int) {}\n"), 0o644)
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "after")
	head := strings.TrimSpace(gitCmd(t, dir, "rev-parse", "HEAD"))

	snap := gitsnap.New(dir, "s")
	from, to := domain.SnapshotRef{Commit: base}, domain.SnapshotRef{Commit: head}

	all, err := snap.DiffAll(context.Background(), from, to)
	if err != nil {
		t.Fatalf("DiffAll: %v", err)
	}
	one, err := snap.Diff(context.Background(), from, to, []string{"a.go"})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if all["a.go"].Text != one.Text {
		t.Errorf("batched and per-file disagree:\n--- batched\n%s\n--- per-file\n%s",
			all["a.go"].Text, one.Text)
	}
}

func keysOf(m map[string]domain.Diff) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestCommitsSeparatingCountsEitherWayRound(t *testing.T) {
	// Comparing a newer point with an older one is a real question — "what
	// would I lose going back to this" — and `newer..older` is empty by
	// definition, so the tile read "0 commits" over a diff of sixteen files.
	dir := newRepo(t)
	base := time.Now()
	commitAt(t, dir, base.Add(-4*time.Hour), "a.go", "One")
	gitCmd(t, dir, "tag", "v1.0.0")
	commitAt(t, dir, base.Add(-3*time.Hour), "b.go", "Two")
	commitAt(t, dir, base.Add(-2*time.Hour), "c.go", "Three")
	gitCmd(t, dir, "tag", "v1.1.0")

	s := gitsnap.New(dir, "s")
	older, _ := s.ResolveRef(context.Background(), "v1.0.0")
	newer, _ := s.ResolveRef(context.Background(), "v1.1.0")

	forward, err := s.CommitsSeparating(context.Background(), older, newer)
	if err != nil {
		t.Fatalf("CommitsSeparating: %v", err)
	}
	backward, err := s.CommitsSeparating(context.Background(), newer, older)
	if err != nil {
		t.Fatalf("CommitsSeparating: %v", err)
	}

	if len(forward) != 2 {
		t.Errorf("forward: got %d, want 2", len(forward))
	}
	if len(backward) != len(forward) {
		t.Errorf("backward: got %d, want the same %d — the two points are the same "+
			"distance apart whichever order they were named in", len(backward), len(forward))
	}
}

func TestResolvingATagGivesTheCommitItPointsAt(t *testing.T) {
	// An annotated tag is an object of its own, and `rev-parse v1.0.0` answers
	// with *that* object. Everything downstream then compares a tag hash with
	// commit hashes and finds nothing: the range a reviewer picked could not be
	// marked in the history, and the tag's date came back as its message.
	dir := newRepo(t)
	commitAt(t, dir, time.Now().Add(-time.Hour), "a.go", "One")
	gitCmd(t, dir, "tag", "-a", "v1.0.0", "-m", "the first one")

	s := gitsnap.New(dir, "s")
	ref, err := s.ResolveRef(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}

	head := gitCmd(t, dir, "rev-parse", "HEAD")
	if ref.Commit != head {
		t.Errorf("ResolveRef = %q, want the commit %q it points at", ref.Commit, head)
	}
	// And the label is still what the reviewer typed.
	if ref.Label != "v1.0.0" {
		t.Errorf("Label = %q, want the ref as given", ref.Label)
	}
}
