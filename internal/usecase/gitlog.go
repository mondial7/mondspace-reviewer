package usecase

import (
	"fmt"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// maxBranchPulses bounds how much of a burst of new branches is spelled out.
// The same reasoning as tags: a fetch after a busy morning can bring a dozen.
const maxBranchPulses = 3

// LogEntry is one commit as the log card shows it: what it was, and where the
// reviewer stands relative to it (issue #18).
type LogEntry struct {
	domain.Commit
	// Ref is the short hash, which is what the picker and every link speak.
	Ref string
	Ago string
	// Reviewing marks the commit currently open, which is the question the card
	// exists to answer: where am I against everything that has landed.
	Reviewing bool
	// SignedOff marks one already finished with (ADR 0021), so the card answers
	// "what is left" as well as "where am I".
	SignedOff bool
	// OnRemote marks a commit colleagues can already see. A commit that is only
	// local is a different thing, and the card should not make anyone guess
	// which they are looking at.
	OnRemote bool
	// Incoming marks somebody else's work: on the upstream, not yet here. It is
	// the reason to watch a remote at all — seeing it an hour later is seeing
	// it too late (issue #18).
	Incoming bool
}

// BuildLog turns recent history into the rows of the log card.
//
// Pure: the caller supplies what it learned from git — which commits the
// upstream can reach, which have been signed off — and this only decides how
// they read.
func BuildLog(commits []domain.Commit, reviewingRef string,
	onRemote, reviewed map[string]bool, now time.Time) []LogEntry {
	return BuildLogAcross(commits, reviewingRef, onRemote, reviewed, nil, now)
}

// BuildLogAcross is BuildLog over a history that spans the local branch and its
// upstream, marking the commits that are not here yet.
//
// local is what HEAD can reach. An empty map means there is no upstream to
// compare against, and nothing is incoming — a repository with no remote must
// not paint its whole history as somebody else's work.
func BuildLogAcross(commits []domain.Commit, reviewingRef string,
	onRemote, reviewed, local map[string]bool, now time.Time) []LogEntry {

	out := make([]LogEntry, 0, len(commits))
	for _, c := range commits {
		short := shortHash(c.Hash)
		out = append(out, LogEntry{
			Commit:    c,
			Ref:       short,
			Ago:       Ago(now.Sub(c.TS)),
			Reviewing: reviewingRef != "" && (reviewingRef == short || reviewingRef == c.Hash),
			SignedOff: reviewed[c.Hash],
			OnRemote:  onRemote[c.Hash],
			Incoming:  len(local) > 0 && !local[c.Hash],
		})
	}
	return out
}

// RemoteNews says what somebody else did, between two looks at the remote.
//
// This is the point of the whole feature: a reviewer reading a diff has no way
// to know a colleague just pushed, and finding out an hour later is finding out
// too late (issue #18).
func RemoteNews(prev, next domain.RemoteState) []domain.Pulse {
	// The first look is the baseline, not news — the same discipline as the
	// repository watcher, or opening a page would announce everything that was
	// already there.
	if prev.UpstreamHash == "" {
		return nil
	}

	var out []domain.Pulse

	if next.UpstreamHash != prev.UpstreamHash && next.Upstream != "" {
		out = append(out, domain.Pulse{
			Kind: domain.PulseRemote,
			Text: upstreamText(next),
			Ref:  next.Upstream,
		})
	}

	out = append(out, branchPulses(prev.Branches, next.Branches)...)
	return out
}

// upstreamText names how much arrived and who it came from. "Alice pushed"
// answers a different question from "you are 3 behind", and the reviewer wants
// both in one line.
func upstreamText(next domain.RemoteState) string {
	head := fmt.Sprintf("%s on %s", count(max(next.Behind, 1), "new commit"), next.Upstream)
	if next.LastAuthor != "" {
		return head + " · " + Brief(next.LastAuthor, 40)
	}
	return head
}

// branchPulses names branches that were not there before.
//
// Only new ones. Branches are deleted constantly after merging, and a toast for
// each would be noise about work that is already finished.
func branchPulses(before, after []string) []domain.Pulse {
	had := make(map[string]bool, len(before))
	for _, b := range before {
		had[b] = true
	}

	var fresh []string
	for _, b := range after {
		if !had[b] {
			fresh = append(fresh, b)
		}
	}
	if len(fresh) == 0 {
		return nil
	}

	shown := fresh
	var overflow int
	if len(fresh) > maxBranchPulses {
		shown, overflow = fresh[:maxBranchPulses-1], len(fresh)-(maxBranchPulses-1)
	}

	out := make([]domain.Pulse, 0, maxBranchPulses)
	for _, b := range shown {
		out = append(out, domain.Pulse{
			Kind: domain.PulseRemote, Text: "New branch " + b, Ref: b,
		})
	}
	if overflow > 0 {
		out = append(out, domain.Pulse{
			Kind: domain.PulseRemote,
			Text: fmt.Sprintf("…and %s", count(overflow, "more branch")),
		})
	}
	return out
}
