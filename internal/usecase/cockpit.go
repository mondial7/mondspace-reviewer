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

// liveWindow is how long after the last event a session still counts as alive.
// It is generous: an agent thinking, running a test suite, or waiting on a build
// is still working, and a cockpit that goes dark every time it pauses is worse
// than one that is slightly optimistic.
const liveWindow = 5 * time.Minute

// pullRequestRef matches the two ways a merged pull request shows up in a git
// log: GitHub's merge commit, and its squash-merge subject suffix.
var pullRequestRef = regexp.MustCompile(`(?:[Mm]erge pull request #|\(#)(\d+)`)

// ComputeStats reduces a session to the numbers the cockpit shows. It is pure:
// the caller gathers the facts, this only counts them.
func ComputeStats(sess domain.Session, units []domain.Unit, diffs map[string]domain.Diff, commits []domain.Commit, now time.Time) domain.SessionStats {
	stats := domain.SessionStats{
		Files:   len(units),
		Commits: len(commits),
	}

	for _, u := range units {
		added, removed := countChangedLines(diffs[u.ID])
		stats.Added += added
		stats.Removed += removed
	}

	// Two commits can land one pull request, so count distinct references.
	seen := map[string]bool{}
	for _, c := range commits {
		if m := pullRequestRef.FindStringSubmatch(c.Subject); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			stats.PullRequests++
		}
	}

	if first, last, ok := eventSpan(sess); ok {
		stats.Started = first
		stats.Open = now.Sub(first)
		if stats.Open < 0 {
			stats.Open = 0
		}
		stats.Live = now.Sub(last) <= liveWindow
	}
	return stats
}

// eventSpan is the first and last event time, or false for a session that has
// recorded nothing — whose duration is zero, not "since the zero time".
func eventSpan(sess domain.Session) (first, last time.Time, ok bool) {
	for _, e := range sess.Events {
		if e.TS.IsZero() {
			continue
		}
		if first.IsZero() || e.TS.Before(first) {
			first = e.TS
		}
		if e.TS.After(last) {
			last = e.TS
		}
	}
	return first, last, !first.IsZero()
}

// countChangedLines counts added and removed lines, ignoring the +++/--- file
// headers that would otherwise each be miscounted as a changed line.
func countChangedLines(d domain.Diff) (added, removed int) {
	for _, line := range strings.Split(d.Text, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

// gitFileHeader matches the plumbing git puts above each file's hunks. A feed
// names the file itself, so these lines carry nothing — and on a small budget
// they were consuming five of every fourteen lines shown.
func gitFileHeader(line string) bool {
	switch {
	case strings.HasPrefix(line, "diff --git "),
		strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "--- "),
		strings.HasPrefix(line, "+++ "),
		strings.HasPrefix(line, "new file mode "),
		strings.HasPrefix(line, "deleted file mode "),
		strings.HasPrefix(line, "old mode "),
		strings.HasPrefix(line, "new mode "),
		strings.HasPrefix(line, "similarity index "),
		strings.HasPrefix(line, "rename from "),
		strings.HasPrefix(line, "rename to "):
		return true
	}
	return false
}

// CompactDiff shortens a diff to about maxLines, keeping every hunk header so
// the shape of the change survives, and says how much it left out. A feed of
// changes is unreadable if one 2,000-line diff pushes everything else off the
// screen — but silently truncating a diff in a review tool is worse than not
// showing it, so the elision is always visible.
//
// Git's per-file plumbing is dropped: the caller shows the filename, so those
// lines add nothing and cost several of a small budget.
//
// It reports whether it compacted anything; a diff already short enough is
// returned untouched.
func CompactDiff(d domain.Diff, maxLines int) (domain.Diff, bool) {
	if maxLines <= 0 {
		maxLines = 12
	}
	all := strings.Split(strings.TrimRight(d.Text, "\n"), "\n")

	lines := make([]string, 0, len(all))
	for _, line := range all {
		if !gitFileHeader(line) {
			lines = append(lines, line)
		}
	}
	if len(lines) <= maxLines {
		if len(lines) == len(all) {
			return d, false
		}
		return domain.Diff{Text: strings.Join(lines, "\n") + "\n"}, true
	}

	// Hunk headers first: they are the map of the change. Then fill the rest of
	// the budget with the earliest content lines, which is where a reviewer
	// looks first.
	var kept []string
	budget := maxLines
	for _, line := range lines {
		if budget == 0 {
			break
		}
		if strings.HasPrefix(line, "@@") || len(kept) < maxLines-countHunks(lines) {
			kept = append(kept, line)
			budget--
		}
	}

	dropped := len(lines) - len(kept)
	if dropped > 0 {
		kept = append(kept, fmt.Sprintf("… %d more %s", dropped, plural("line", dropped)))
	}
	return domain.Diff{Text: strings.Join(kept, "\n") + "\n"}, true
}

func countHunks(lines []string) int {
	n := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			n++
		}
	}
	return n
}

// ReviewFingerprint identifies what a review currently contains, cheaply enough
// to poll every few seconds.
//
// It fingerprints churn per file rather than snapshot refs. A live review diffs
// against the working tree, so a unit's "to" ref is empty and never moves —
// fingerprinting units would miss every edit that did not add or remove a file,
// which is most of them.
//
// Order does not affect it: git may list files in any order.
func ReviewFingerprint(files []domain.FileStat) string {
	lines := make([]string, 0, len(files))
	for _, f := range files {
		lines = append(lines, fmt.Sprintf("%s\x00%d\x00%d", f.Path, f.Added, f.Removed))
	}
	sort.Strings(lines)

	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:16])
}

// WithChanges adds a bounded digest of what actually changed to an ask context.
//
// Without it a session-scoped question carries only file names and note text,
// and the assistant correctly answers that it cannot say anything: metadata is
// not a change. The digest is capped because the alternative — every diff — is a
// prompt no local context window can hold, and it always states what it omitted
// rather than presenting a truncation as the whole picture.
func WithChanges(ctx domain.AskContext, units []domain.Unit, diffs map[string]domain.Diff, maxLines int) domain.AskContext {
	if maxLines <= 0 {
		maxLines = 200
	}

	var b strings.Builder
	used, covered := 0, 0
	for _, u := range units {
		if used >= maxLines {
			break
		}
		d := diffs[u.ID]
		if strings.TrimSpace(d.Text) == "" {
			continue
		}
		// A slice each, so one enormous file cannot consume the whole budget and
		// hide every other change.
		compact, _ := CompactDiff(d, perFileDigestLines)
		b.WriteString("--- " + strings.Join(u.Files, ", ") + "\n" + compact.Text)
		used += strings.Count(compact.Text, "\n") + 1
		covered++
	}

	if left := len(units) - covered; left > 0 {
		b.WriteString(fmt.Sprintf("… and %d more %s not shown here\n", left, plural("file", left)))
	}

	ctx.Diff = domain.Diff{Text: b.String()}
	return ctx
}

// perFileDigestLines is how much of any one file reaches the digest.
const perFileDigestLines = 10
