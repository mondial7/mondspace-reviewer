package usecase

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// EmptyTree is git's canonical empty tree. A root commit has no parent, so the
// only honest baseline for it is nothing at all.
const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// BuildTargets turns what git knows about a repository into the list of things
// worth reviewing, newest first (ADR 0017).
//
// It is pure: the caller gathers the commits, tags and sessions, and this only
// decides what they add up to. Every target is a range, because reviewing one is
// exactly what the engine already did.
func BuildTargets(repo string, commits []domain.Commit, tags []domain.Tag, sessions []domain.Target, dirty bool) []domain.Target {
	var targets []domain.Target

	// Work in progress leads: it is the most likely thing a reviewer wants, and
	// it is the only target that disappears the moment it is committed.
	if dirty {
		var head domain.SnapshotRef
		if len(commits) > 0 {
			head = domain.SnapshotRef{Commit: commits[0].Hash, Label: "HEAD"}
		}
		targets = append(targets, withID(repo, domain.Target{
			Repo: repo, Kind: domain.TargetWorktree,
			Title: "Uncommitted work", Subtitle: "the working tree against HEAD",
			From: head,
		}))
	}

	for _, tag := range tagTargets(repo, tags, commits) {
		targets = append(targets, tag)
	}
	for _, pr := range pullRequestTargets(repo, commits) {
		targets = append(targets, pr)
	}
	for _, c := range commits {
		targets = append(targets, withID(repo, domain.Target{
			Repo: repo, Kind: domain.TargetCommit,
			Title: c.Subject, Subtitle: shortHash(c.Hash) + " · " + c.Author,
			From: parentRef(c), To: domain.SnapshotRef{Commit: c.Hash, Label: shortHash(c.Hash)},
			TS: c.TS, Commits: 1,
		}))
	}
	for _, s := range sessions {
		s.Repo = repo
		if s.ID == "" {
			s = withID(repo, s)
		}
		targets = append(targets, s)
	}

	attachSessions(targets, sessions)
	return targets
}

// parentRef is the baseline for one commit. A root commit diffs against the
// empty tree, which is the only thing that means "everything here is new".
func parentRef(c domain.Commit) domain.SnapshotRef {
	if c.Parent == "" {
		return domain.SnapshotRef{Commit: EmptyTree, Label: "the beginning"}
	}
	return domain.SnapshotRef{Commit: c.Parent, Label: shortHash(c.Parent)}
}

// tagTargets makes each tag the range since the tag before it — "what shipped in
// v3.1.0" — falling back to the beginning of history for the oldest.
func tagTargets(repo string, tags []domain.Tag, commits []domain.Commit) []domain.Target {
	out := make([]domain.Target, 0, len(tags))
	for i, tag := range tags {
		from := domain.SnapshotRef{Commit: EmptyTree, Label: "the beginning"}
		if i+1 < len(tags) {
			from = domain.SnapshotRef{Commit: tags[i+1].Hash, Label: tags[i+1].Name}
		}
		out = append(out, withID(repo, domain.Target{
			Repo: repo, Kind: domain.TargetTag,
			Title:    tag.Name,
			Subtitle: "everything since " + from.Label,
			From:     from,
			To:       domain.SnapshotRef{Commit: tag.Hash, Label: tag.Name},
			TS:       tag.TS,
			Commits:  countBetween(commits, from.Commit, tag.Hash),
		}))
	}
	return out
}

// pullRequestTargets groups the commits that reference the same pull request.
// Two commits landing #42 are one piece of work; reviewing them apart is the
// fragmentation this exists to fix.
//
// The reference comes from commit subjects and nothing else: msr talks to no
// forge (ADR 0015), so a pull request it cannot see in the log does not exist.
func pullRequestTargets(repo string, commits []domain.Commit) []domain.Target {
	type span struct {
		newest, oldest domain.Commit
		count          int
	}
	order := []string{}
	spans := map[string]*span{}

	// commits arrive newest first, so the first sighting is the newest.
	for _, c := range commits {
		m := pullRequestRef.FindStringSubmatch(c.Subject)
		if m == nil {
			continue
		}
		num := m[1]
		if s, seen := spans[num]; seen {
			s.oldest = c
			s.count++
			continue
		}
		order = append(order, num)
		spans[num] = &span{newest: c, oldest: c, count: 1}
	}

	out := make([]domain.Target, 0, len(order))
	for _, num := range order {
		s := spans[num]
		if s.count < 2 {
			// One commit is already reviewable as itself; a pull-request target
			// holding exactly that commit would be the same review twice.
			continue
		}
		out = append(out, withID(repo, domain.Target{
			Repo: repo, Kind: domain.TargetPR,
			Title:    "#" + num + " · " + stripPullRequestRef(s.newest.Subject),
			Subtitle: fmt.Sprintf("%d commits", s.count),
			From:     parentRef(s.oldest),
			To:       domain.SnapshotRef{Commit: s.newest.Hash, Label: shortHash(s.newest.Hash)},
			TS:       s.newest.TS, Commits: s.count,
		}))
	}
	return out
}

// attachSessions records, on every target, which recorded runs overlap it. That
// is the grouped view: a commit made during a run carries the run, so the intent
// behind a change is one click away.
func attachSessions(targets []domain.Target, sessions []domain.Target) {
	for i := range targets {
		if targets[i].Kind == domain.TargetSession {
			continue
		}
		for _, s := range sessions {
			// A session covers everything from when it started onwards; a commit
			// from before it began is somebody else's work.
			if !targets[i].TS.IsZero() && !s.TS.IsZero() && !targets[i].TS.Before(s.TS) {
				targets[i].Sessions = append(targets[i].Sessions, s.ID)
			}
		}
	}
}

// countBetween is how many of these commits fall in a range. It is a count over
// what the caller already fetched rather than another git call.
func countBetween(commits []domain.Commit, from, to string) int {
	n, counting := 0, false
	for _, c := range commits {
		if c.Hash == to {
			counting = true
		}
		if !counting {
			continue
		}
		if c.Hash == from {
			break
		}
		n++
	}
	return n
}

// withID derives a target's identity from what it reviews, so the same range
// always reviews to the same id — across restarts, machines and clones — and
// nothing has to be migrated when a session is deleted (ADR 0017).
func withID(repo string, t domain.Target) domain.Target {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		repo, string(t.Kind), t.From.Commit, t.To.Commit, t.Title,
	}, "\x00")))
	t.ID = hex.EncodeToString(sum[:8])
	return t
}

func shortHash(h string) string {
	if len(h) <= 8 {
		return h
	}
	return h[:8]
}

// stripPullRequestRef removes the reference the title states already. It matches
// the whole "(#42)" rather than reusing pullRequestRef, which stops at the
// digits and would leave the closing bracket behind.
var pullRequestSuffix = regexp.MustCompile(`\s*(?:\(#\d+\)|[Mm]erge pull request #\d+(?: from \S+)?)\s*`)

func stripPullRequestRef(subject string) string {
	return strings.TrimSpace(pullRequestSuffix.ReplaceAllString(subject, " "))
}

// SortTargets orders targets newest first, which is how a reviewer reads them.
func SortTargets(targets []domain.Target) {
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].TS.After(targets[j].TS) })
}

// RangeTarget is an arbitrary range a reviewer asked for, rather than one git
// offered. It is built exactly like the others, so it opens, narrates and is
// annotated the same way — the refs simply came from somewhere else.
func RangeTarget(repo, title string, from, to domain.SnapshotRef) domain.Target {
	return withID(repo, domain.Target{
		Repo: repo, Kind: domain.TargetRange,
		Title:    title,
		Subtitle: "a range you chose",
		From:     from, To: to,
		TS: time.Now(),
	})
}
