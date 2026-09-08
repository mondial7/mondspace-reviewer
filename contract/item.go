// Package contract is the shape msr writes and another tool reads.
//
// It is outside `internal/` for exactly one reason: the planner compiles
// against this declaration rather than against a copy of it that agrees today
// (ADR 0044). It imports the standard library and nothing else, so that the day
// it needs its own `go.mod` there is nothing to untangle — and the import path
// is the same either way.
package contract

import (
	"strconv"
	"time"
)

// Source is which reading produced an item.
//
// A model's finding, an analyser's finding and a reviewer's note are the same
// shape; they differ by who said it, and that is this field. The distinction
// between `stated`, `inferred` and `reported` (ADR 0003, ADR 0043) is the
// reason it is recorded on every item and never inferred from the other fields.
type Source string

const (
	// SourceAnalyser is a deterministic tool: reproducible, rule-named, and the
	// only class a reviewer can act on without checking it first.
	//
	// Spelled as the rest of msr spells it. The wire value is part of the
	// contract, so it is worth saying that this is deliberate rather than a typo
	// somebody should helpfully correct later.
	SourceAnalyser Source = "analyser"
	// SourceLLM is a model's reading — the security pass, the breaking-change
	// pass. Always a suggestion, never a fact.
	SourceLLM Source = "llm"
	// SourceHuman is what the reviewer said, typed in a lens.
	SourceHuman Source = "human"
	// SourcePlanner is an item authored in the planner and handed the other way.
	SourcePlanner Source = "planner"
)

// Severity is how much an item should interrupt the reviewer.
//
// Three levels, not five, and defined by what the reviewer should *do* rather
// than by a score nobody computed (ADR 0024, ADR 0044). An analyser's own
// vocabulary is mapped onto these by Normalise; the tool's word for it is not
// lost, because the rule id travels verbatim on the item.
type Severity string

const (
	// SeverityHigh: I would not merge this without dealing with it.
	SeverityHigh Severity = "high"
	// SeverityMedium: worth checking before merging.
	SeverityMedium Severity = "medium"
	// SeverityLow: worth knowing about, not worth blocking on.
	SeverityLow Severity = "low"
)

// Severities is the three levels, worst first — the order they are read in.
var Severities = []Severity{SeverityHigh, SeverityMedium, SeverityLow}

// Rank orders severities, worst lowest, for sorting. An unrecognised level
// ranks with medium, which is where it is normalised to anyway.
func (s Severity) Rank() int {
	switch s {
	case SeverityHigh:
		return 0
	case SeverityLow:
		return 2
	default:
		return 1
	}
}

// Normalise maps whatever came back to one of the three.
//
// An endpoint that ignored the schema, or a linter with its own scale, can
// return anything. Dropping the item would hide it and calling it high would
// cry wolf, so an unusable level becomes "worth checking" — the honest answer
// when nobody said.
func (s Severity) Normalise() Severity {
	for _, known := range Severities {
		if s == known {
			return known
		}
	}
	return SeverityMedium
}

// AtLeast reports whether s is as severe as floor. It is the comparison every
// filter wants and the one that is easy to write backwards, since worse is a
// lower rank.
func (s Severity) AtLeast(floor Severity) bool {
	return s.Normalise().Rank() <= floor.Normalise().Rank()
}

// Verdict is what the reviewer decided about an item (ADR 0030).
//
// Only "dismissed" changes anything: an item nobody has ruled on and an item
// somebody confirmed are both still to deal with.
type Verdict string

const (
	// VerdictDismissed: looked at, not a problem. The item stays, greyed,
	// because deleting it would invite the next pass to raise it as new.
	VerdictDismissed Verdict = "dismissed"
	// VerdictConfirmed: looked at, and it is real.
	VerdictConfirmed Verdict = "confirmed"
)

// State is what has happened to an item, as opposed to what anybody thinks of
// it (ADR 0044).
//
// Verdict is an opinion; this is a fact about the world. An item was handed to
// an agent at a time, in a batch, and either survived the next pass or did not.
type State string

const (
	// StateOpen is every item that has not been handed anywhere. The zero value
	// is deliberately not this — see Item.CurrentState.
	StateOpen State = "open"
	// StateAccepted is acknowledged and queued for work, but not yet sent.
	StateAccepted State = "accepted"
	// StatePushed is in flight: delivered to an implementation agent in a batch
	// that has not been verified yet.
	StatePushed State = "pushed"
	// StateFixed is verified closed by a later pass — the fingerprint no longer
	// resolves in the code. Nobody sets this by hand.
	StateFixed State = "fixed"
)

// Location is where in the tree an item is.
//
// Path and line alone are not identity: the working tree is live and lines move
// under a reviewer while they read. What makes an item findable again is on the
// Item itself — the fingerprint, the anchor, the unit id.
type Location struct {
	// Path is repository-relative with forward slashes, so it matches what git
	// reports on every platform.
	Path string `json:"path"`
	// StartLine is 1-indexed. Zero means the item is about the file as a whole,
	// which some tools do report and plenty of human notes are.
	StartLine int `json:"start_line,omitempty"`
	// EndLine is inclusive, and equals StartLine for a single-line item.
	EndLine int `json:"end_line,omitempty"`
	// Commit is the revision the location was resolved against, when there is
	// one. A live review has none: the tree is uncommitted by definition.
	Commit string `json:"commit,omitempty"`
}

// Item is one thing to be done: a place in the code, a sentence about it, a
// weight, and a state (ADR 0044).
//
// It is wider than any of the three types it replaces and most items leave most
// of it empty. That is the cost of one table instead of three, and it is
// visible in every line written.
type Item struct {
	// ID is unique to this record and never reused. It is what a push refers to.
	ID string `json:"id"`
	// Fingerprint is identity across runs: the same code analysed twice gives
	// the same fingerprint, and reformatting does not change it. Two records
	// sharing one fingerprint are the same finding seen twice, which is what
	// makes a dismissal stick and a fix detectable.
	Fingerprint string `json:"fingerprint"`

	Source   Source `json:"source"`
	Producer string `json:"producer,omitempty"`
	// RuleID is the tool's own identifier, verbatim, so it can be searched for
	// and suppressed in the tool's own config. An item that cannot name its rule
	// is not reproducible and is not `reported` (ADR 0043).
	RuleID string `json:"rule_id,omitempty"`

	Location Location `json:"location"`
	Severity Severity `json:"severity,omitempty"`
	// Title is the one line a list shows.
	Title string `json:"title"`
	// Message is what the producer actually said, in its own words.
	Message string `json:"message,omitempty"`
	// Directive is what a downstream agent should DO about it, and it is stored
	// rather than generated at export time (ADR 0044): a reviewer who rewrites
	// it has done the most valuable work in the review, and regenerating would
	// throw that away on the next run.
	Directive string `json:"directive,omitempty"`
	// Snippet is enough of the code to act on without opening the file, for an
	// agent that has no repository context loaded.
	Snippet string `json:"snippet,omitempty"`

	// New says this item is about a line the change actually touched. Showing
	// the ones that were already there turns "3 things to look at" into "412
	// things to look at", which is the same as none (ADR 0043).
	New bool `json:"new,omitempty"`

	Verdict Verdict `json:"verdict,omitempty"`
	State   State   `json:"state,omitempty"`

	// Branch is what the item is about, and what it is stored against. Not a
	// directory: branch names contain slashes, and a rename would orphan them
	// (ADR 0045).
	Branch string `json:"branch,omitempty"`
	// SessionID attributes the item to the run that raised it. It is worth
	// showing and it decides nothing (ADR 0045).
	SessionID string `json:"session_id,omitempty"`
	// UnitID is the unit a human note was written against. Unit ids are
	// immutable history while the working tree is live (ADR 0030).
	UnitID string `json:"unit_id,omitempty"`
	// Anchor is the diff line this item sits on, verbatim, and AnchorNth is
	// which occurrence of that exact text it is. A line number drifts onto
	// something else without ever looking wrong (ADR 0028).
	Anchor    string `json:"anchor,omitempty"`
	AnchorNth int    `json:"anchor_nth,omitempty"`

	FirstSeen time.Time  `json:"first_seen"`
	LastSeen  time.Time  `json:"last_seen"`
	PushedAt  *time.Time `json:"pushed_at,omitempty"`
	// PushBatch is the batch this item went out in. It is what makes a push
	// idempotent: a batch that has been sent is not sent again.
	PushBatch string `json:"push_batch,omitempty"`
	// PushedBy is "human" or "judge". Auto-mode records itself identically to a
	// human push so the loop stays inspectable through the same store.
	PushedBy string `json:"pushed_by,omitempty"`
	// FixedAt is when a later pass found the fingerprint gone.
	FixedAt *time.Time `json:"fixed_at,omitempty"`

	Related []string `json:"related,omitempty"`
}

// CurrentState is the item's state with the zero value read as open.
//
// Every item ever written by anything is open until something says otherwise,
// and a store full of records with an empty state field would otherwise need
// every caller to remember that.
func (i Item) CurrentState() State {
	if i.State == "" {
		return StateOpen
	}
	return i.State
}

// Stands reports whether this item is still something to deal with.
//
// Dismissed is a decision and fixed is a fact; both mean nobody needs to look
// again. Pushed does not: an item handed to an agent is still open until a
// later pass finds it gone (ADR 0044).
func (i Item) Stands() bool {
	return i.Verdict != VerdictDismissed && i.CurrentState() != StateFixed
}

// Pushable reports whether this item may be handed to an implementation agent.
//
// A dismissed item can never be pushed, and one already in flight is not sent
// twice — those are the two rules that keep a push idempotent.
func (i Item) Pushable() bool {
	if i.Verdict == VerdictDismissed {
		return false
	}
	switch i.CurrentState() {
	case StatePushed, StateFixed:
		return false
	default:
		return true
	}
}

// Where names the item's location for a human: "internal/api/handler.go:42".
func (i Item) Where() string {
	if i.Location.StartLine == 0 {
		return i.Location.Path
	}
	return i.Location.Path + ":" + strconv.Itoa(i.Location.StartLine)
}
