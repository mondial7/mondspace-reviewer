package usecase

import (
	"fmt"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// LiveRef names the target that always means "HEAD against the working tree".
// Unlike every other ref it does not identify a point in history — it follows
// one, which is what makes it the thing to watch while an agent is working.
const LiveRef = "live"

// maxTagPulses bounds how much of a tag burst is spelled out. Fetching a
// repository can land dozens at once, and thirty toasts is a denial of service
// on the reader.
const maxTagPulses = 3

// Pulses says what moved in a repository between two observations, in the words
// the reviewer will read.
//
// It is pure, and it is deliberately conservative: silence is the right answer
// far more often than not, because this interrupts someone who is reading.
func Pulses(prev, next domain.RepoState) []domain.Pulse {
	// The first observation is the baseline, not news. Without this every page
	// load would greet the reviewer with a toast about commits that were
	// already there when they arrived.
	if prev.Head == "" {
		return nil
	}

	var out []domain.Pulse

	committed := next.Head != prev.Head
	if committed {
		out = append(out, domain.Pulse{
			Kind: domain.PulseCommit,
			Text: commitText(next),
			Ref:  shortHash(next.Head),
		})
	}

	out = append(out, tagPulses(prev.Tags, next.Tags)...)

	// A commit necessarily empties the working tree, so reporting both would be
	// two pieces of news about one event. The commit is the one that matters.
	if !committed && next.DirtyPrint != prev.DirtyPrint && next.DirtyFiles > 0 {
		out = append(out, domain.Pulse{
			Kind: domain.PulseFiles,
			Text: fmt.Sprintf("%s changed since HEAD", count(next.DirtyFiles, "file")),
			Ref:  LiveRef,
		})
	}

	return out
}

func commitText(next domain.RepoState) string {
	head := "New commit"
	if next.Commits > 1 {
		head = fmt.Sprintf("%d new commits", next.Commits)
	}
	if next.Subject == "" {
		return head
	}
	return head + " · " + Brief(next.Subject, 60)
}

// tagPulses names the tags that were not there before. Order follows the new
// list, which git gives newest first, so the most interesting one is the one
// that survives the cap.
func tagPulses(before, after []string) []domain.Pulse {
	had := make(map[string]bool, len(before))
	for _, t := range before {
		had[t] = true
	}

	var fresh []string
	for _, t := range after {
		if !had[t] {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) == 0 {
		return nil
	}

	shown := fresh
	var overflow int
	if len(fresh) > maxTagPulses {
		shown, overflow = fresh[:maxTagPulses-1], len(fresh)-(maxTagPulses-1)
	}

	out := make([]domain.Pulse, 0, maxTagPulses)
	for _, t := range shown {
		out = append(out, domain.Pulse{Kind: domain.PulseTag, Text: "New tag " + t, Ref: t})
	}
	if overflow > 0 {
		// No ref: "6 more tags" names no single thing to open.
		out = append(out, domain.Pulse{
			Kind: domain.PulseTag,
			Text: fmt.Sprintf("…and %s", count(overflow, "more tag")),
		})
	}
	return out
}

// count writes "1 file" or "4 files" — the number and its noun agreeing, which
// is the difference between a sentence and a debug print.
func count(n int, noun string) string {
	return fmt.Sprintf("%d %s", n, plural(noun, n))
}

// ResolveLive points a live target at wherever HEAD is now.
//
// Every other target names a fixed range and is resolved once, when it is
// discovered. The live target is the exception by design: it follows HEAD, so
// its baseline has to be re-read at the moment it is reviewed rather than at
// the moment it was listed. Its id is untouched — that stability is the whole
// reason the live target exists (ADR 0018).
func ResolveLive(t domain.Target, head domain.SnapshotRef) domain.Target {
	if t.Kind != domain.TargetLive || head.Commit == "" {
		return t
	}
	t.From = head
	return t
}

// InStore reports whether a path is msr's own store rather than the reviewer's
// work. Without it a review contains msr's bookkeeping, and the live watcher
// announces its own writes as though the agent had made them.
//
// storeRel is the store's path relative to the repository root, and is empty
// when the store lives outside the repository — in which case nothing inside it
// is the store, and matching on the empty string would hide everything.
func InStore(storeRel string) func(string) bool {
	if storeRel == "" {
		return func(string) bool { return false }
	}
	return func(f string) bool {
		return f == storeRel || strings.HasPrefix(f, storeRel+"/")
	}
}
