package usecase

import (
	"reflect"
	"sort"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
)

// Reconciliation is what makes a store of findings different from a log of
// them (ADR 0044, ADR 0045).
//
// The same code analysed twice produces the same fingerprints, so the second
// run is not a set of new findings: it is the same set, seen again. What is
// worth recording is only the difference — what appeared, what was already
// ruled on, and what has gone.

// Pass is what one analysis run actually looked at.
//
// It is here because "this finding is gone" and "nobody looked for it this
// time" are the same absence, and closing an item on the second one would
// quietly mark half a repository fixed the first time somebody scopes a run to
// one directory.
type Pass struct {
	// At is the run's clock. One time for the whole pass, so every item it
	// touches agrees about when it was seen.
	At time.Time
	// Producers is which tools ran, by name. Empty means all of them — a full
	// pass, where an absence really is an absence.
	Producers map[string]bool
	// Paths is which files they were pointed at, repository-relative. Empty
	// means the whole tree.
	Paths map[string]bool
}

// Covered reports whether this pass was in a position to see an item at all.
func (p Pass) Covered(item contract.Item) bool {
	if len(p.Producers) > 0 && item.Producer != "" && !p.Producers[item.Producer] {
		return false
	}
	if len(p.Paths) > 0 && !p.Paths[contract.NormalisePath(item.Location.Path)] {
		return false
	}
	return true
}

// identity is what makes two sightings the same record: the fingerprint, on one
// branch. The same finding on two branches is two records, because one of them
// may be fixed while the other is not.
type identity struct {
	fingerprint string
	branch      string
}

// Reconcile folds a pass's sightings into what is already stored and returns
// the records to write.
//
// Three things happen. A sighting that matches a stored item carries that
// item's id and everything the reviewer decided about it — this is what stops a
// second run reporting the same finding as new. A sighting nobody has seen
// before becomes a record, born dismissed if that fingerprint was ever
// dismissed anywhere, because a dismissal that has to be repeated is not a
// dismissal (ADR 0030). And a stored item this pass could have seen and did not
// is fixed: the code it was about is gone.
//
// Only the records that changed come back. The store is append-only with
// last-write-wins per id, so writing the ones that did not change would grow
// the file for nothing.
func Reconcile(stored, seen []contract.Item, pass Pass) []contract.Item {
	latest := map[identity]contract.Item{}
	dismissed := map[string]bool{}
	for _, item := range stored {
		latest[identity{item.Fingerprint, item.Branch}] = item
		if item.Verdict == contract.VerdictDismissed {
			dismissed[item.Fingerprint] = true
		}
	}

	var out []contract.Item
	live := map[identity]bool{}
	for _, sighting := range seen {
		id := identity{sighting.Fingerprint, sighting.Branch}
		if live[id] {
			// One tool reporting the same fingerprint twice in one pass is one
			// finding. Nothing downstream would know which of the two to push.
			continue
		}
		live[id] = true

		if prior, ok := latest[id]; ok {
			if changed, ok := carry(prior, sighting, pass.At); ok {
				out = append(out, changed)
			}
			continue
		}

		fresh := sighting
		fresh.FirstSeen = pass.At
		fresh.LastSeen = pass.At
		if fresh.State == "" {
			fresh.State = contract.StateOpen
		}
		if dismissed[fresh.Fingerprint] {
			fresh.Verdict = contract.VerdictDismissed
		}
		out = append(out, fresh)
	}

	for id, prior := range latest {
		if live[id] || !prior.Stands() || !pass.Covered(prior) {
			continue
		}
		fixed := prior
		fixed.State = contract.StateFixed
		at := pass.At
		fixed.FixedAt = &at
		out = append(out, fixed)
	}

	// Map iteration is unordered and this is written to a file somebody diffs.
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// carry moves a stored item forward onto where the code is now, and reports
// whether anything actually changed.
//
// What the reviewer decided outlives what the tool said: the verdict, the
// state, the push, and a directive somebody rewrote by hand (ADR 0044). What
// the tool said this time replaces what it said last time, because the lines
// have moved and the message may have been sharpened.
func carry(prior, sighting contract.Item, at time.Time) (contract.Item, bool) {
	next := prior
	next.LastSeen = at
	next.Location = sighting.Location
	next.Severity = sighting.Severity
	next.Title = sighting.Title
	next.Message = sighting.Message
	next.Snippet = sighting.Snippet
	next.Anchor = sighting.Anchor
	next.AnchorNth = sighting.AnchorNth
	next.New = sighting.New
	if next.Directive == "" {
		next.Directive = sighting.Directive
	}
	// An item that was fixed and is back was not fixed. It is the same finding
	// as before — same fingerprint, same file — so it keeps its history rather
	// than arriving as something new.
	if next.CurrentState() == contract.StateFixed {
		next.State = contract.StateOpen
		next.FixedAt = nil
	}

	return next, !sameSighting(prior, next)
}

// sameSighting reports whether two records differ in anything but when they
// were last seen. A pass over an untouched tree must write nothing.
func sameSighting(a, b contract.Item) bool {
	a.LastSeen, b.LastSeen = time.Time{}, time.Time{}
	return reflect.DeepEqual(a, b)
}
