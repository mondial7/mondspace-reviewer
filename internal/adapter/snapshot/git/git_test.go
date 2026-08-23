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
	files, err := s.ChangedFiles(ctx, baseline)
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
