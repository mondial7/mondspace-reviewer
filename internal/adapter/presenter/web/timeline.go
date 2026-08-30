package web

import (
	"sort"
	"strings"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// Checkpoint is one row of the history: a commit, a tag on nothing here, or a
// recorded run. They are one list because a reviewer looking for "the one from
// Tuesday" should not have to know which kind it was.
type Checkpoint struct {
	usecase.LogEntry
	// Kind is what this row is, for the label and the colour. Empty means a
	// commit, which needs neither.
	Kind domain.TargetKind
	// Tags are the names pointing at this commit. A tag is not a row of its
	// own when the thing it names is already one.
	Tags []string
	// Title is what to show. For a commit it is the subject.
	Title string
	// Open is where the row leads.
	Open string
}

// Timeline merges the commit log with everything else worth reviewing, newest
// first.
//
// Tags and recorded runs used to live in a control beside this card, which
// asked the same question the card answers and answered it in a different
// vocabulary. Here they are rows.
func Timeline(entries []usecase.LogEntry, targets []TargetSummary) []Checkpoint {
	out := make([]Checkpoint, 0, len(entries)+len(targets))
	at := map[string]int{}

	for _, e := range entries {
		at[e.Hash] = len(out)
		at[e.Ref] = len(out)
		out = append(out, Checkpoint{LogEntry: e, Title: e.Subject, Open: "/?target=" + e.Ref})
	}

	for _, t := range targets {
		switch t.Kind {
		case domain.TargetLive, domain.TargetWorktree:
			// It has a row already, at the top, and it is not a point in
			// history — it is where history has not happened yet.
			continue
		case domain.TargetTag:
			if i, ok := commitRow(at, t.Commit); ok {
				out[i].Tags = append(out[i].Tags, t.Ref)
				continue
			}
		case domain.TargetCommit:
			// The log is where commits come from; a second copy of one is a
			// second row saying the same thing.
			if _, ok := commitRow(at, t.Commit); ok {
				continue
			}
			if _, ok := at[t.Ref]; ok {
				continue
			}
		}

		out = append(out, Checkpoint{
			LogEntry: usecase.LogEntry{
				Commit:    domain.Commit{TS: t.TS, Subject: t.Title},
				Ref:       t.Ref,
				Ago:       usecase.Ago(time.Since(t.TS)),
				SignedOff: t.Reviewed,
			},
			Kind: t.Kind, Title: t.Title, Open: "/?target=" + t.Ref,
		})
	}

	// Newest first, which is the order the log already arrives in and the order
	// a reviewer thinks in.
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.After(out[j].TS) })
	return out
}

// commitRow finds the row for a commit hash, long or short.
func commitRow(at map[string]int, hash string) (int, bool) {
	if hash == "" {
		return 0, false
	}
	if i, ok := at[hash]; ok {
		return i, true
	}
	for ref, i := range at {
		if len(ref) >= 7 && strings.HasPrefix(hash, ref) {
			return i, true
		}
	}
	return 0, false
}
