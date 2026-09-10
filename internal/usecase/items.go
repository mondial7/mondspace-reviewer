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
	// Branch is the branch the pass ran on.
	//
	// A pass sees one checkout. What it does not find on that branch is gone
	// from that branch; on any other branch it has looked at nothing at all,
	// and closing a finding there would say the other branch was fixed by work
	// that never touched it (ADR 0045).
	Branch string
	// Tentative says this pass's silence is not evidence.
	//
	// A deterministic tool that does not report a finding it reported an hour
	// ago has told you something: the code changed. A model asked the same
	// question twice has not — it may simply have answered differently. So a
	// model's pass never closes anything, and a finding it raised is closed by
	// the reviewer or by the code it names going away.
	Tentative bool
}

// Covered reports whether this pass was in a position to see an item at all.
func (p Pass) Covered(item contract.Item) bool {
	if item.Branch != p.Branch {
		return false
	}
	// An item with no producer was raised by something this pass cannot speak
	// for — a note, a reading — and a list of tools that ran says nothing about
	// it either way.
	if len(p.Producers) > 0 && (item.Producer == "" || !p.Producers[item.Producer]) {
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
		if pass.Tentative || live[id] || !prior.Stands() || !pass.Covered(prior) {
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

// Caused splits what this change is responsible for from what was already
// there, and says how many were already there (ADR 0043, ADR 0053).
//
// Every repository of any age has hundreds of findings nobody is going to act
// on today. The cockpit has always folded those away and counted them; the
// store did not, so `msr findings` listed them as work and `msr push --batch`
// would have handed an agent a pile of code its change never touched.
//
// The flag only means anything for a deterministic analyser: a note a reviewer
// typed and a reading a model produced are about this change by construction,
// and neither carries `new`.
func Caused(items []contract.Item) (caused []contract.Item, alreadyThere int) {
	for _, item := range items {
		if item.Source == contract.SourceAnalyser && !item.New {
			alreadyThere++
			continue
		}
		caused = append(caused, item)
	}
	return caused, alreadyThere
}

// SurfaceCap is how many items a run puts in front of a reviewer.
//
// The overflow is stored, not dropped: silently having none and silently
// hiding four hundred look identical on a page, and one of them means the tool
// is broken (ADR 0043).
const SurfaceCap = 25

// Surface picks what a run actually shows, worst first, and says how much it
// held back.
//
// Ordered by severity and then by how recently it appeared, because a reviewer
// reading a capped list is reading the top of it and the top should be the
// things they have not already decided to live with.
func Surface(items []contract.Item, cap int, floor contract.Severity) (shown []contract.Item, held int) {
	var standing []contract.Item
	for _, item := range items {
		if !item.Stands() {
			continue
		}
		if floor != "" && !item.Severity.AtLeast(floor) {
			continue
		}
		standing = append(standing, item)
	}

	sort.SliceStable(standing, func(i, j int) bool {
		a, b := standing[i], standing[j]
		if ra, rb := a.Severity.Normalise().Rank(), b.Severity.Normalise().Rank(); ra != rb {
			return ra < rb
		}
		if !a.LastSeen.Equal(b.LastSeen) {
			return a.LastSeen.After(b.LastSeen)
		}
		return a.ID < b.ID
	})

	if cap <= 0 || len(standing) <= cap {
		return standing, 0
	}
	return standing[:cap], len(standing) - cap
}

// DismissalsBeforeSuppressing is how many times one rule has to be waved away
// before msr suggests turning it off.
const DismissalsBeforeSuppressing = 3

// RulesWorthSuppressing names the rules a reviewer keeps dismissing.
//
// Three distinct fingerprints, not three dismissals: dismissing the same
// finding twice is one opinion held twice, and a rule that fires three times in
// three places and is waved away every time is a rule this repository does not
// want. The suggestion is made to the reviewer; nothing is suppressed by msr
// deciding it on its own.
func RulesWorthSuppressing(items []contract.Item) []string {
	seen := map[string]map[string]bool{}
	for _, item := range items {
		if item.Verdict != contract.VerdictDismissed || item.RuleID == "" {
			continue
		}
		rule := item.Producer + ":" + item.RuleID
		if seen[rule] == nil {
			seen[rule] = map[string]bool{}
		}
		seen[rule][item.Fingerprint] = true
	}

	var out []string
	for rule, fingerprints := range seen {
		if len(fingerprints) >= DismissalsBeforeSuppressing {
			out = append(out, rule)
		}
	}
	sort.Strings(out)
	return out
}
