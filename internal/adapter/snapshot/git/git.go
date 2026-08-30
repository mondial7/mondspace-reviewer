// Package git snapshots the working tree into throwaway commits under
// refs/mondspace/review/<session>, without touching the user's HEAD, index, or
// working tree (SPEC §7).
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	// Peeled to the commit. An annotated tag is an object of its own, and
	// `rev-parse v1.0.0` answers with *that* — after which everything
	// downstream compares a tag hash against commit hashes and finds nothing:
	// the range a reviewer picked could not be marked in the history, and the
	// tag's date came back as its message.
	out, err := s.run(ctx, os.Environ(), "rev-parse", ref+"^{commit}")
	if err != nil {
		// Not everything peels — the empty tree, for one — so a ref that will
		// not is still worth resolving as itself.
		out, err = s.run(ctx, os.Environ(), "rev-parse", ref)
		if err != nil {
			return domain.SnapshotRef{}, fmt.Errorf("resolving ref %q: %w", ref, err)
		}
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

// commitFieldSep separates the fields of one `git log` record. It is a unit
// separator rather than anything printable, because a commit subject may
// legitimately contain any punctuation a naive delimiter would use.
const commitFieldSep = "\x1f"

// The reported timestamp is the committer date, the same clock `--since`
// filters on, so what is shown and what was selected can never disagree.
//
// CommitsSince lists the commits made from `since` onwards, newest first, so
// the cockpit can report what a session actually landed. Commits older than the
// session belong to somebody else and are excluded.
//
// A repository with no commits yet returns nothing rather than an error: `git
// log` fails outright with no HEAD, but an empty history is an ordinary state
// for a fresh project and must not break the page.
func (s *Snapshotter) CommitsSince(ctx context.Context, since time.Time) ([]domain.Commit, error) {
	if since.IsZero() {
		return nil, nil
	}
	out, err := s.run(ctx, os.Environ(), "log",
		"--since="+since.UTC().Format(time.RFC3339),
		"--pretty=format:%H"+commitFieldSep+"%an"+commitFieldSep+"%cI"+commitFieldSep+"%s")
	if err != nil {
		return nil, nil // no HEAD yet, or no history: not a failure
	}

	var commits []domain.Commit
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, commitFieldSep)
		if len(fields) != 4 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			continue
		}
		commits = append(commits, domain.Commit{
			Hash: fields[0], Author: fields[1], TS: ts, Subject: fields[3],
		})
	}
	return commits, nil
}

// Numstat reports per-file churn against a baseline in one call, which is what
// makes it cheap enough for the cockpit to poll. Untracked files are included:
// a brand-new file is exactly what an agent creates, and `git diff` alone cannot
// see one.
//
// An empty `to` compares against the working tree, matching ChangedFiles.
func (s *Snapshotter) Numstat(ctx context.Context, from, to domain.SnapshotRef) ([]domain.FileStat, error) {
	args := []string{"diff", "--numstat", from.Commit}
	if to.Commit != "" {
		args = append(args, to.Commit)
	}
	out, err := s.run(ctx, os.Environ(), args...)
	if err != nil {
		return nil, err
	}

	var stats []domain.FileStat
	for _, line := range strings.Split(out, "\n") {
		if f, ok := parseNumstat(line); ok {
			stats = append(stats, f)
		}
	}

	// Untracked files never appear in `git diff`. They only exist against the
	// working tree, so there is nothing to add when `to` names a commit.
	if to.Commit == "" {
		others, err := s.run(ctx, os.Environ(), "ls-files", "--others", "--exclude-standard")
		if err == nil {
			for _, path := range strings.Split(others, "\n") {
				path = strings.TrimSpace(path)
				if path == "" {
					continue
				}
				stats = append(stats, domain.FileStat{Path: path, Added: countLines(filepath.Join(s.repoDir, path))})
			}
		}
	}
	return stats, nil
}

// NumstatSince is what has changed between a snapshot and the working tree.
//
// It exists because Numstat cannot answer this correctly. A snapshot records
// untracked files — that is where an agent's new work lives — so diffing one
// against the working tree has to compare like with like. `git diff` alone
// reports every still-untracked file as *deleted*, because the real index never
// had it, and Numstat's untracked scan then reports the same file as added: one
// unchanged file, arriving twice with opposite signs.
//
// So the working tree is written to a tree object of its own and the two trees
// are compared. That costs a `git add -A` against a throwaway index, which is
// why callers should ask only when something has actually moved.
func (s *Snapshotter) NumstatSince(ctx context.Context, from domain.SnapshotRef) ([]domain.FileStat, error) {
	tree, err := s.worktreeTree(ctx)
	if err != nil {
		return nil, err
	}

	out, err := s.run(ctx, os.Environ(), "diff", "--numstat", from.Commit, tree)
	if err != nil {
		return nil, err
	}

	var stats []domain.FileStat
	for _, line := range strings.Split(out, "\n") {
		if f, ok := parseNumstat(line); ok {
			stats = append(stats, f)
		}
	}
	return stats, nil
}

// worktreeTree writes the working tree — tracked and untracked alike — to a
// tree object, without touching the user's HEAD, index or files.
func (s *Snapshotter) worktreeTree(ctx context.Context) (string, error) {
	tmpIndex, err := os.CreateTemp("", "msr-index-*")
	if err != nil {
		return "", err
	}
	indexPath := tmpIndex.Name()
	tmpIndex.Close()
	// git builds the throwaway index from scratch: an empty pre-existing file
	// is rejected as a malformed index.
	os.Remove(indexPath)
	defer os.Remove(indexPath)

	env := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	if _, err := s.run(ctx, env, "add", "-A"); err != nil {
		return "", err
	}
	return s.run(ctx, env, "write-tree")
}

// parseCommitLog reads the pretty-format above into commits.
func parseCommitLog(out string) []domain.Commit {
	var commits []domain.Commit
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, commitFieldSep)
		if len(fields) != 5 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			continue
		}
		c := domain.Commit{Hash: fields[0], Author: fields[1], TS: ts, Subject: fields[3]}
		// %P lists every parent; the first is the one a merge came from, which is
		// the range a reviewer means by "what did this commit do".
		if parents := strings.Fields(fields[4]); len(parents) > 0 {
			c.Parent = parents[0]
		}
		commits = append(commits, c)
	}
	return commits
}

// parseNumstat reads one `added\tremoved\tpath` record. A binary file reports
// "-" for both counts and is recorded as changed with no line count.
func parseNumstat(line string) (domain.FileStat, bool) {
	fields := strings.SplitN(strings.TrimSpace(line), "\t", 3)
	if len(fields) != 3 || fields[2] == "" {
		return domain.FileStat{}, false
	}
	added, _ := strconv.Atoi(fields[0])
	removed, _ := strconv.Atoi(fields[1])
	return domain.FileStat{Path: fields[2], Added: added, Removed: removed}, true
}

// countLines is how many lines an untracked file adds. It is read rather than
// diffed because git has nothing to diff it against.
func countLines(path string) int {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	if len(body) == 0 {
		return 0
	}
	return bytes.Count(body, []byte("\n")) + boolToInt(!bytes.HasSuffix(body, []byte("\n")))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// FileVersions lists the commits that touched one file, newest first, so a
// reviewer can step back through how it got here. `--follow` is deliberately not
// used: it guesses at renames, and a guess presented as history is worse than a
// short history.
func (s *Snapshotter) FileVersions(ctx context.Context, path string, limit int) ([]domain.Commit, error) {
	if limit <= 0 {
		limit = 20
	}
	out, err := s.run(ctx, os.Environ(), "log", fmt.Sprintf("-%d", limit),
		"--pretty=format:%H"+commitFieldSep+"%an"+commitFieldSep+"%cI"+commitFieldSep+"%s",
		"--", path)
	if err != nil {
		return nil, nil // no HEAD, or a file git has never seen
	}

	var versions []domain.Commit
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, commitFieldSep)
		if len(fields) != 4 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			continue
		}
		versions = append(versions, domain.Commit{
			Hash: fields[0], Author: fields[1], TS: ts, Subject: fields[3],
		})
	}
	return versions, nil
}

// DiffAt is what one commit did to one file — the view the overlay shows when
// stepping through a file's history.
func (s *Snapshotter) DiffAt(ctx context.Context, commit, path string) (domain.Diff, error) {
	out, err := s.run(ctx, os.Environ(), "show", "--format=", "--patch", commit, "--", path)
	if err != nil {
		return domain.Diff{}, err
	}
	return domain.Diff{Text: out}, nil
}

// DiscoverRepos finds the repositories to review under a path. If the path is
// itself a checkout, that is the answer — running inside a project should not
// offer up its vendored dependencies. Otherwise its immediate children are
// scanned, which is the common workspace shape: ~/work/{api,web,worker}.
//
// Only one level down: a deep walk of a home directory is slow and turns up
// checkouts nobody meant to review. Hidden directories are skipped.
//
// Results are sorted, so a launch prompt lists them the same way every time.
func DiscoverRepos(root string) []string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	if isRepo(abs) {
		return []string{abs}
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil
	}
	var repos []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if child := filepath.Join(abs, e.Name()); isRepo(child) {
			repos = append(repos, child)
		}
	}
	sort.Strings(repos)
	return repos
}

// isRepo reports whether a directory is a git checkout. A .git entry may be a
// directory or, in a worktree or submodule, a file pointing elsewhere.
func isRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// RecentCommits walks history newest first, with each commit's first parent, so
// a commit can be reviewed on its own as the range parent..commit. A root commit
// has no parent and diffs against the empty tree.
func (s *Snapshotter) RecentCommits(ctx context.Context, limit int) ([]domain.Commit, error) {
	if limit <= 0 {
		limit = 50
	}
	out, err := s.run(ctx, os.Environ(), "log", fmt.Sprintf("-%d", limit),
		"--pretty=format:%H"+commitFieldSep+"%an"+commitFieldSep+"%cI"+
			commitFieldSep+"%s"+commitFieldSep+"%P")
	if err != nil {
		return nil, nil // no HEAD yet: an empty history, not a failure
	}

	commits := parseCommitLog(out)
	return commits, nil
}

// Tags lists the repository's tags newest first, with the commit each points at.
// A tag is how a reviewer asks "what shipped in v3.1.0".
func (s *Snapshotter) Tags(ctx context.Context, limit int) ([]domain.Tag, error) {
	if limit <= 0 {
		limit = 50
	}
	out, err := s.run(ctx, os.Environ(), "for-each-ref",
		"--sort=-creatordate", fmt.Sprintf("--count=%d", limit),
		"--format=%(refname:short)"+commitFieldSep+"%(objectname)"+commitFieldSep+"%(creatordate:iso-strict)",
		"refs/tags")
	if err != nil || out == "" {
		return nil, nil // no tags is an ordinary state
	}

	var tags []domain.Tag
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, commitFieldSep)
		if len(fields) != 3 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			continue
		}
		tags = append(tags, domain.Tag{Name: fields[0], Hash: fields[1], TS: ts})
	}
	return tags, nil
}

// IsDirty reports whether the working tree differs from HEAD, which is what
// makes "the work in progress" worth offering as something to review.
func (s *Snapshotter) IsDirty(ctx context.Context) (bool, error) {
	out, err := s.run(ctx, os.Environ(), "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// CommitsBetween lists the commits in a range, newest first.
//
// This is not CommitsSince: a range target asks "what is *in* this", and asking
// for commits since its date answers the opposite — everything after it — which
// is how a tagged release came to report zero commits in it.
//
// An empty far end means the working tree, so everything after `from` counts.
// CommitTime is when a commit was made. Empty for the working tree, which has
// no time of its own.
func (s *Snapshotter) CommitTime(ctx context.Context, ref domain.SnapshotRef) (time.Time, error) {
	if ref.Commit == "" || ref.Commit == emptyTree {
		return time.Time{}, nil
	}
	// `log -1`, not `show -s`: an annotated tag resolves to the tag object, and
	// asking `show` for one prints the tag message rather than a date.
	out, err := s.run(ctx, os.Environ(), "log", "-1", "--format=%cI", ref.Commit)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(out))
}

// CommitsSeparating is how many commits lie between two points, whichever order
// they were named in.
//
// Comparing a newer point with an older one is a real question — "what would I
// lose going back to this" — and `newer..older` is empty by definition, so the
// tile read "0 commits" over a diff of sixteen files. Two points are the same
// distance apart whichever way round you say them.
func (s *Snapshotter) CommitsSeparating(ctx context.Context, from, to domain.SnapshotRef) ([]domain.Commit, error) {
	forward, err := s.CommitsBetween(ctx, from, to)
	if err != nil || len(forward) > 0 {
		return forward, err
	}
	// Only when the near end is a real commit: an empty `from` already means
	// "all of history up to the far end", which is not a range to reverse.
	if from.Commit == "" || from.Commit == emptyTree || to.Commit == "" {
		return forward, nil
	}
	return s.CommitsBetween(ctx, to, from)
}

func (s *Snapshotter) CommitsBetween(ctx context.Context, from, to domain.SnapshotRef) ([]domain.Commit, error) {
	spec := from.Commit + ".."
	if to.Commit != "" {
		spec += to.Commit
	} else {
		spec += "HEAD"
	}
	if from.Commit == "" || from.Commit == emptyTree {
		// Nothing before it: the range is all of history up to the far end.
		spec = "HEAD"
		if to.Commit != "" {
			spec = to.Commit
		}
	}

	out, err := s.run(ctx, os.Environ(), "log", "-200",
		"--pretty=format:%H"+commitFieldSep+"%an"+commitFieldSep+"%cI"+commitFieldSep+"%s",
		spec)
	if err != nil {
		return nil, nil // an unresolvable range is empty, not fatal
	}

	var commits []domain.Commit
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, commitFieldSep)
		if len(fields) != 4 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			continue
		}
		commits = append(commits, domain.Commit{
			Hash: fields[0], Author: fields[1], TS: ts, Subject: fields[3],
		})
	}
	return commits, nil
}

// CommitsAcross is recent history over more than one ref, newest first.
//
// The log card walks HEAD *and* the upstream, so a colleague's commit appears
// above your own work rather than only as a number saying you are behind
// (issue #18). A ref that does not resolve is skipped rather than fatal: a
// branch with no upstream is ordinary.
func (s *Snapshotter) CommitsAcross(ctx context.Context, limit int, refs ...string) ([]domain.Commit, error) {
	if limit <= 0 {
		limit = 50
	}
	args := []string{"log", fmt.Sprintf("-%d", limit), "--date-order",
		"--pretty=format:%H" + commitFieldSep + "%an" + commitFieldSep + "%cI" +
			commitFieldSep + "%s" + commitFieldSep + "%P"}
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		if _, err := s.run(ctx, os.Environ(), "rev-parse", "--verify", "--quiet", ref); err != nil {
			continue
		}
		args = append(args, ref)
	}
	out, err := s.run(ctx, os.Environ(), args...)
	if err != nil {
		return nil, nil // no history yet: empty, not a failure
	}
	return parseCommitLog(out), nil
}

// Remote reads where the current branch sits against its upstream, and what
// remote-tracking branches exist (issue #18).
//
// It reads only what is already in the repository. Whatever the last `git
// fetch` brought is what this sees — msr does not fetch unless asked to
// (ADR 0025).
func (s *Snapshotter) Remote(ctx context.Context) (domain.RemoteState, error) {
	var state domain.RemoteState

	branch, err := s.run(ctx, os.Environ(), "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return state, nil // no HEAD yet: not a failure, just nothing to say
	}
	state.Branch = strings.TrimSpace(branch)

	// A branch with no upstream is the normal state of a scratch repository,
	// and not something to report as an error.
	upstream, err := s.run(ctx, os.Environ(), "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		state.Branches = s.remoteBranches(ctx)
		return state, nil
	}
	state.Upstream = strings.TrimSpace(upstream)

	if hash, err := s.run(ctx, os.Environ(), "rev-parse", state.Upstream); err == nil {
		state.UpstreamHash = strings.TrimSpace(hash)
	}

	// One command for both counts: behind is what a colleague pushed, ahead is
	// what has not left this machine.
	if counts, err := s.run(ctx, os.Environ(), "rev-list", "--left-right", "--count",
		state.Upstream+"..."+"HEAD"); err == nil {
		if fields := strings.Fields(counts); len(fields) == 2 {
			state.Behind, _ = strconv.Atoi(fields[0])
			state.Ahead, _ = strconv.Atoi(fields[1])
		}
	}

	if state.Behind > 0 {
		if out, err := s.run(ctx, os.Environ(), "log", "-1",
			"--pretty=format:%an"+commitFieldSep+"%s", state.Upstream); err == nil {
			if fields := strings.SplitN(strings.TrimSpace(out), commitFieldSep, 2); len(fields) == 2 {
				state.LastAuthor, state.LastSubject = fields[0], fields[1]
			}
		}
	}

	state.Branches = s.remoteBranches(ctx)
	return state, nil
}

// remoteBranches lists remote-tracking branches, so a new one can be noticed.
func (s *Snapshotter) remoteBranches(ctx context.Context) []string {
	out, err := s.run(ctx, os.Environ(), "for-each-ref", "--format=%(refname:short)", "refs/remotes")
	if err != nil {
		return nil
	}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		// origin/HEAD is a symbolic alias for the default branch, not a branch
		// anyone pushed.
		if name == "" || strings.HasSuffix(name, "/HEAD") {
			continue
		}
		branches = append(branches, name)
	}
	sort.Strings(branches)
	return branches
}

// ReachableFrom is the set of commits an upstream ref can reach, so the log can
// say which of them a colleague can already see.
func (s *Snapshotter) ReachableFrom(ctx context.Context, ref string, limit int) map[string]bool {
	if ref == "" {
		return nil
	}
	if limit <= 0 {
		limit = 200
	}
	out, err := s.run(ctx, os.Environ(), "rev-list", fmt.Sprintf("-%d", limit), ref)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if h := strings.TrimSpace(line); h != "" {
			seen[h] = true
		}
	}
	return seen
}

// Fetch updates remote-tracking refs. It is the one thing msr does that talks
// to the network and writes to the repository, so nothing calls it unless the
// reviewer asked for it (ADR 0025).
//
// --no-write-fetch-head keeps it out of .git/FETCH_HEAD, and --prune keeps
// deleted branches from lingering as news that never arrives. It never touches
// HEAD, the index or the working tree.
func (s *Snapshotter) Fetch(ctx context.Context) error {
	_, err := s.run(ctx, os.Environ(), "fetch", "--quiet", "--prune", "--no-write-fetch-head", "--no-tags")
	return err
}

// branchLimit bounds how many branches are inspected. Divergence costs one
// command each, and a list longer than this is not a view anybody reads.
const branchLimit = 40

// Branches lists remote branches with how far each has drifted from base.
//
// base is usually the default branch. Everything is read from refs already in
// the repository: whatever the last fetch brought is what this sees (ADR 0025).
func (s *Snapshotter) Branches(ctx context.Context, base string) ([]domain.Branch, error) {
	if base == "" {
		base = s.defaultBranch(ctx)
	}

	const sep = "\x1f"
	out, err := s.run(ctx, os.Environ(), "for-each-ref",
		fmt.Sprintf("--count=%d", branchLimit), "--sort=-committerdate",
		"--format=%(refname:short)"+sep+"%(objectname)"+sep+"%(authorname)"+sep+
			"%(committerdate:iso-strict)"+sep+"%(subject)"+sep+"%(symref)",
		"refs/remotes")
	if err != nil {
		return nil, nil // no remotes: an empty list, not a failure
	}

	current := ""
	if b, err := s.run(ctx, os.Environ(), "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		current = strings.TrimSpace(b)
	}

	var branches []domain.Branch
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimSpace(line), sep)
		if len(fields) != 6 || fields[0] == "" {
			continue
		}
		// origin/HEAD is a symbolic alias for the default branch, not a branch
		// anyone pushed. Asking whether the ref is symbolic is exact; matching
		// the name is not, because git renders refs/remotes/origin/HEAD as the
		// bare remote name "origin".
		if strings.TrimSpace(fields[5]) != "" || strings.HasSuffix(fields[0], "/HEAD") {
			continue
		}
		ts, err := time.Parse(time.RFC3339, fields[3])
		if err != nil {
			continue
		}

		b := domain.Branch{
			Name: fields[0], Short: shortBranch(fields[0]), Hash: fields[1],
			Author: fields[2], TS: ts, Subject: fields[4], Base: base,
		}
		if base != "" && b.Name != base {
			b.Behind, b.Ahead = s.divergence(ctx, base, b.Name)
			b.Merged = b.Ahead == 0
		}
		b.Mine = current != "" && shortBranch(b.Name) == current
		branches = append(branches, b)
	}
	return branches, nil
}

// divergence counts how far two refs have drifted, in one command.
func (s *Snapshotter) divergence(ctx context.Context, base, ref string) (behind, ahead int) {
	out, err := s.run(ctx, os.Environ(), "rev-list", "--left-right", "--count", base+"..."+ref)
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0
	}
	behind, _ = strconv.Atoi(fields[0])
	ahead, _ = strconv.Atoi(fields[1])
	return behind, ahead
}

// defaultBranch is what the remote calls its mainline, falling back to the
// usual names when the remote has not said.
func (s *Snapshotter) defaultBranch(ctx context.Context) string {
	if out, err := s.run(ctx, os.Environ(), "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name := strings.TrimSpace(out); name != "" {
			return name
		}
	}
	for _, guess := range []string{"origin/main", "origin/master"} {
		if _, err := s.run(ctx, os.Environ(), "rev-parse", "--verify", "--quiet", guess); err == nil {
			return guess
		}
	}
	return ""
}

// shortBranch drops the remote from a remote-tracking name: origin/feature-x
// is called feature-x by the person who pushed it.
func shortBranch(name string) string {
	if _, rest, found := strings.Cut(name, "/"); found {
		return rest
	}
	return name
}

// IgnoreFile is what msr's own ignore rules are called, at the repository root.
const IgnoreFile = ".msrignore"

// Ignored reports which of the given paths the rules in ignoreFile exclude, and
// which pattern excluded each.
//
// The rules are gitignore rules and git applies them, rather than msr
// reimplementing the syntax: it has enough corners — anchoring, directory-only
// matches, negation, `**` — that a second implementation would quietly differ
// from the one people already know (ADR 0027).
//
// --no-index is what makes this work at all. To git a *tracked* file is never
// "ignored", so without it every path in a review would come back clean.
//
// A missing rules file hides nothing. Nothing is hidden from a review unless
// the reviewer asked for it.
func (s *Snapshotter) Ignored(ctx context.Context, ignoreFile string, paths []string) (map[string]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if _, err := os.Stat(ignoreFile); err != nil {
		return nil, nil
	}

	cmd := exec.CommandContext(ctx, "git",
		"-c", "core.excludesFile="+ignoreFile,
		"check-ignore", "--no-index", "--stdin", "--verbose")
	cmd.Dir = s.repoDir
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\n") + "\n")

	out, err := cmd.Output()
	if err != nil {
		// Exit status 1 means "nothing matched", which is an answer rather than
		// a failure. Anything else and the rules are not applied at all — which
		// shows every file, the safe way to be wrong.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, nil
	}

	hidden := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		// source:linenum:pattern\tpath
		rule, path, found := strings.Cut(strings.TrimSpace(line), "\t")
		if !found || path == "" {
			continue
		}
		pattern := rule
		if i := strings.LastIndex(rule, ":"); i >= 0 {
			pattern = rule[i+1:]
		}
		hidden[path] = pattern
	}
	return hidden, nil
}

// DiffAll diffs a whole range in one invocation, returning each file's own
// diff (ADR 0029).
//
// BuildFileUnits used to call Diff once per changed file. That is one git
// process per file, and at six hundred files it was twenty-eight seconds per
// page load — every load, because it happens before anything can be cached.
//
// The output is split on git's own `diff --git` boundaries, so each file gets
// exactly the text Diff would have produced for it alone.
func (s *Snapshotter) DiffAll(ctx context.Context, from, to domain.SnapshotRef) (map[string]domain.Diff, error) {
	args := []string{"diff", from.Commit}
	if to.Commit != "" {
		args = append(args, to.Commit)
	}
	text, err := s.run(ctx, os.Environ(), args...)
	if err != nil {
		return nil, err
	}
	return splitByFile(text), nil
}

// diffHeader matches the line git puts above every file's hunks. The b-side
// path is the file's name, except for a deletion, where only the a-side exists.
var diffHeader = regexp.MustCompile(`(?m)^diff --git a/(.+?) b/(.+)$`)

// splitByFile cuts a multi-file diff into one diff per file.
func splitByFile(text string) map[string]domain.Diff {
	out := map[string]domain.Diff{}
	if strings.TrimSpace(text) == "" {
		return out
	}

	heads := diffHeader.FindAllStringSubmatchIndex(text, -1)
	for i, h := range heads {
		end := len(text)
		if i+1 < len(heads) {
			end = heads[i+1][0]
		}
		body := text[h[0]:end]

		// The b-side name, which is what the file is called after the change.
		// A deletion has /dev/null on the b-side of the ---/+++ lines but still
		// names the real path here, so this is right for all three cases.
		path := text[h[4]:h[5]]
		out[path] = domain.Diff{
			Text:  strings.TrimSuffix(body, "\n"),
			Files: []string{path},
		}
	}
	return out
}
