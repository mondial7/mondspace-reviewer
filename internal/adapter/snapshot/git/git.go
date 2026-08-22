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
	"strings"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
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
	env := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)

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

// Diff returns the change between two snapshots, restricted to paths.
func (s *Snapshotter) Diff(ctx context.Context, from, to domain.SnapshotRef, paths []string) (domain.Diff, error) {
	args := []string{"diff", from.Commit, to.Commit}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	text, err := s.run(ctx, os.Environ(), args...)
	if err != nil {
		return domain.Diff{}, err
	}
	return domain.Diff{Text: text, Files: paths}, nil
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
