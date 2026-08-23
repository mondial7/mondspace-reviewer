// Package git snapshots the working tree into throwaway commits under
// refs/mondspace/review/<session>, without touching the user's HEAD, index, or
// working tree (SPEC §7).
package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

type Snapshotter struct {
	repoDir   string
	sessionID string
	prev      string // previous snapshot commit, chained as the next parent
	n         int
}

func New(repoDir, sessionID string) *Snapshotter {
	return &Snapshotter{repoDir: repoDir, sessionID: sessionID}
}

func (s *Snapshotter) ref() string {
	return "refs/mondspace/review/" + s.sessionID
}

// Snapshot builds a throwaway index (honouring .gitignore via `git add -A`),
// writes a tree, and commits it under the session's review ref.
func (s *Snapshotter) Snapshot(ctx context.Context, label string) (domain.SnapshotRef, error) {
	tmpIndex, err := os.CreateTemp("", "msr-index-*")
	if err != nil {
		return domain.SnapshotRef{}, err
	}
	indexPath := tmpIndex.Name()
	tmpIndex.Close()
	// git must build the throwaway index from scratch: an empty pre-existing
	// file is rejected as a malformed index.
	os.Remove(indexPath)
	defer os.Remove(indexPath)
	// Supply a fixed identity for these throwaway review commits so snapshots
	// work even where git has no configured user (fresh CI runners, containers).
	env := append(os.Environ(),
		"GIT_INDEX_FILE="+indexPath,
		"GIT_AUTHOR_NAME=mondspace-reviewer", "GIT_AUTHOR_EMAIL=msr@localhost",
		"GIT_COMMITTER_NAME=mondspace-reviewer", "GIT_COMMITTER_EMAIL=msr@localhost",
	)

	if _, err := s.run(ctx, env, "add", "-A"); err != nil {
		return domain.SnapshotRef{}, err
	}
	tree, err := s.run(ctx, env, "write-tree")
	if err != nil {
		return domain.SnapshotRef{}, err
	}

	args := []string{"commit-tree", tree, "-m", fmt.Sprintf("msr snapshot %d", s.n+1)}
	if s.prev != "" {
		args = append(args, "-p", s.prev)
	}
	commit, err := s.run(ctx, env, args...)
	if err != nil {
		return domain.SnapshotRef{}, err
	}

	if _, err := s.run(ctx, env, "update-ref", s.ref(), commit); err != nil {
		return domain.SnapshotRef{}, err
	}

	s.prev = commit
	s.n++
	return domain.SnapshotRef{Commit: commit, Label: label}, nil
}

// emptyTree is git's well-known hash of the empty tree, used as the baseline
// when there is no commit before a session (a brand-new repo).
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Diff returns the change between two snapshots, restricted to paths. An empty
// `to` diffs against the current working tree (the net change since `from`).
func (s *Snapshotter) Diff(ctx context.Context, from, to domain.SnapshotRef, paths []string) (domain.Diff, error) {
	args := []string{"diff", from.Commit}
	if to.Commit != "" {
		args = append(args, to.Commit)
	}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	text, err := s.run(ctx, os.Environ(), args...)
	if err != nil {
		return domain.Diff{}, err
	}
	// A new, untracked file produces no `git diff` output; show it as all-added.
	if text == "" && to.Commit == "" && len(paths) == 1 {
		if alt, ok := s.untrackedDiff(ctx, paths[0]); ok {
			text = alt
		}
	}
	return domain.Diff{Text: text, Files: paths}, nil
}

// Baseline is the commit at or before `before` on HEAD's first-parent history —
// the repo state just before a session began. With no such commit it is the
// empty tree.
func (s *Snapshotter) Baseline(ctx context.Context, before time.Time) (domain.SnapshotRef, error) {
	out, err := s.run(ctx, os.Environ(), "rev-list", "-1", "--first-parent",
		"--before="+before.UTC().Format(time.RFC3339), "HEAD")
	if err != nil || out == "" {
		return domain.SnapshotRef{Commit: emptyTree, Label: "baseline"}, nil
	}
	return domain.SnapshotRef{Commit: out, Label: "baseline"}, nil
}

// ResolveRef resolves any commit-ish (a commit hash, branch, or tag) to a
// SnapshotRef, so a caller-supplied `--since`/`--until` can be diffed the same
// way as any other snapshot.
func (s *Snapshotter) ResolveRef(ctx context.Context, ref string) (domain.SnapshotRef, error) {
	out, err := s.run(ctx, os.Environ(), "rev-parse", ref)
	if err != nil {
		return domain.SnapshotRef{}, fmt.Errorf("resolving ref %q: %w", ref, err)
	}
	return domain.SnapshotRef{Commit: out, Label: ref}, nil
}

// ChangedFiles lists the files whose net content differs from `from` to `to`.
// An empty `to` diffs against the current working tree (as today), including
// new untracked files; a supplied `to` bounds the far end to that commit,
// which excludes untracked files and any working-tree changes made since.
func (s *Snapshotter) ChangedFiles(ctx context.Context, from, to domain.SnapshotRef) ([]string, error) {
	set := map[string]bool{}

	args := []string{"diff", "--name-only", from.Commit}
	if to.Commit != "" {
		args = append(args, to.Commit)
	}
	args = append(args, "--")
	tracked, err := s.run(ctx, os.Environ(), args...)
	if err != nil {
		return nil, err
	}
	for _, f := range nonEmptyLines(tracked) {
		set[f] = true
	}
	if to.Commit == "" {
		if untracked, err := s.run(ctx, os.Environ(), "ls-files", "--others", "--exclude-standard"); err == nil {
			for _, f := range nonEmptyLines(untracked) {
				set[f] = true
			}
		}
	}

	files := make([]string, 0, len(set))
	for f := range set {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}

// untrackedDiff renders a not-yet-tracked file as an all-added diff. git returns
// exit 1 when there are differences, which is expected here, so its status is
// ignored.
func (s *Snapshotter) untrackedDiff(ctx context.Context, path string) (string, bool) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--no-index", "--", os.DevNull, path)
	cmd.Dir = s.repoDir
	out, _ := cmd.Output()
	if len(out) == 0 {
		return "", false
	}
	return string(out), true
}

// reviewRefPrefix is the namespace every session's throwaway review ref lives
// under (SPEC §7).
const reviewRefPrefix = "refs/mondspace/review/"

// ReviewRefs lists the session IDs that currently have a review ref, sorted
// for deterministic output. It is repo-wide: the receiver's own sessionID is
// irrelevant to this call.
func (s *Snapshotter) ReviewRefs(ctx context.Context) ([]string, error) {
	out, err := s.run(ctx, os.Environ(), "for-each-ref", "--format=%(refname)", reviewRefPrefix)
	if err != nil {
		return nil, err
	}
	var sessions []string
	for _, line := range nonEmptyLines(out) {
		sessions = append(sessions, strings.TrimPrefix(line, reviewRefPrefix))
	}
	sort.Strings(sessions)
	return sessions, nil
}

// DeleteReviewRef deletes the review ref for one session. Deleting an
// already-absent ref is not an error: git update-ref -d succeeds either way,
// and a caller may race with another gc run.
func (s *Snapshotter) DeleteReviewRef(ctx context.Context, sessionID string) error {
	_, err := s.run(ctx, os.Environ(), "update-ref", "-d", reviewRefPrefix+sessionID)
	return err
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func (s *Snapshotter) run(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = s.repoDir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}
