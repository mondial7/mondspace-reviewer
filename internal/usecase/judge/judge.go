// Package judge is the decision layer over a push path that already works by
// hand (ADR 0047).
//
// It selects, orders and phrases findings that already exist. It cannot author
// one, and the type system is the reason: everything it returns came out of
// what it was given.
package judge

import (
	"sort"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
)

// Policy is what auto-mode is allowed to do. Every field is a bound, and the
// defaults are the conservative end of each.
type Policy struct {
	// Enabled is off unless somebody turned it on.
	Enabled bool
	// MaxPushes is how many times auto-mode may push in one session.
	MaxPushes int
	// Cooldown is how long it waits after a push before considering another.
	Cooldown time.Duration
	// MinSeverity is the floor. Auto-mode steering an agent towards a low
	// finding is an interruption that costs more than the finding.
	MinSeverity contract.Severity
	// MaxPerBatch is how many items may go in one push. An agent handed
	// fifteen directives mid-task does none of them well.
	MaxPerBatch int
}

// DefaultPolicy is what auto-mode does when nobody has said otherwise: high
// findings only, three at a time, five minutes apart, five pushes a session.
func DefaultPolicy() Policy {
	return Policy{
		Enabled:     false,
		MaxPushes:   5,
		Cooldown:    5 * time.Minute,
		MinSeverity: contract.SeverityHigh,
		MaxPerBatch: 3,
	}
}

// State is what auto-mode has already done this session.
type State struct {
	// Session is which run this is about. A new session starts the counts
	// again, which is what makes MaxPushes mean anything.
	Session string `json:"session,omitempty"`
	// Pushes is how many batches have gone out this session.
	Pushes int `json:"pushes"`
	// LastPush is when the last one went, for the cooldown.
	LastPush time.Time `json:"last_push,omitempty"`
	// Suspended says a human pushed by hand, which stands auto-mode down for
	// the rest of the session (ADR 0047).
	Suspended bool `json:"suspended,omitempty"`
}

// Decision is one finding considered, and what became of it.
//
// Rejections are recorded as carefully as selections: if you cannot see why a
// finding was held back, you cannot trust the ones it let through.
type Decision struct {
	At          time.Time `json:"at"`
	ItemID      string    `json:"item_id"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	Chosen      bool      `json:"chosen"`
	Reason      string    `json:"reason"`
	Batch       string    `json:"batch,omitempty"`
	Session     string    `json:"session,omitempty"`
}

// Verdict is one run of the judge: what it would push, and every reason it had.
type Verdict struct {
	Chosen    []contract.Item
	Decisions []Decision
	// Held is why nothing was chosen, when nothing was. It is separate from the
	// per-item reasons because "the cooldown has not elapsed" is a fact about
	// the run rather than about any finding.
	Held string
}

// Decide picks what to push now.
//
// It may only select, order and phrase what it was given. There is no path
// through this function that invents an item, and that is deliberate: a judge
// that could author findings would be a second, unaccountable planner, and the
// audit trail would stop meaning anything.
func Decide(open []contract.Item, state State, policy Policy, at time.Time) Verdict {
	if !policy.Enabled {
		return Verdict{Held: "auto-mode is off"}
	}
	if state.Suspended {
		return Verdict{Held: "suspended for this session by a manual push"}
	}
	if policy.MaxPushes > 0 && state.Pushes >= policy.MaxPushes {
		return Verdict{Held: "this session's push budget is spent"}
	}
	if !state.LastPush.IsZero() && at.Sub(state.LastPush) < policy.Cooldown {
		return Verdict{Held: "still inside the cooldown since the last push"}
	}

	ordered := make([]contract.Item, len(open))
	copy(ordered, open)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if ra, rb := a.Severity.Normalise().Rank(), b.Severity.Normalise().Rank(); ra != rb {
			return ra < rb
		}
		// Then by place, because a batch an agent can work down one file at a
		// time is worth more than one ordered by when things were found.
		if a.Location.Path != b.Location.Path {
			return a.Location.Path < b.Location.Path
		}
		if a.Location.StartLine != b.Location.StartLine {
			return a.Location.StartLine < b.Location.StartLine
		}
		return a.ID < b.ID
	})

	verdict := Verdict{}
	for _, item := range ordered {
		decision := Decision{At: at, ItemID: item.ID, Fingerprint: item.Fingerprint, Session: state.Session}
		switch {
		case !item.Pushable():
			decision.Reason = whyNotPushable(item)
		case policy.MinSeverity != "" && !item.Severity.AtLeast(policy.MinSeverity):
			decision.Reason = "below the severity floor (" + string(policy.MinSeverity) + ")"
		case item.Directive == "":
			// The judge may phrase a directive, not write one. A finding that
			// arrived without any instruction has nothing to phrase.
			decision.Reason = "no directive to act on"
		case policy.MaxPerBatch > 0 && len(verdict.Chosen) >= policy.MaxPerBatch:
			decision.Reason = "batch is full"
		default:
			decision.Chosen = true
			decision.Reason = "highest-severity outstanding finding with a directive"
			verdict.Chosen = append(verdict.Chosen, item)
		}
		verdict.Decisions = append(verdict.Decisions, decision)
	}

	if len(verdict.Chosen) == 0 {
		verdict.Held = "nothing outstanding that clears the policy"
	}
	return verdict
}

// whyNotPushable puts the item's own rule into words, so the log says which one
// stopped it rather than only that something did.
func whyNotPushable(item contract.Item) string {
	if item.Verdict == contract.VerdictDismissed {
		return "dismissed"
	}
	switch item.CurrentState() {
	case contract.StatePushed:
		return "already in flight"
	case contract.StateFixed:
		return "already fixed"
	default:
		return "not pushable"
	}
}

// Stamp records a push against the state: one more, at this time.
func Stamp(state State, at time.Time) State {
	state.Pushes++
	state.LastPush = at
	return state
}

// Suspend stands auto-mode down for the rest of the session, which is what a
// manual push does (ADR 0047).
func Suspend(state State) State {
	state.Suspended = true
	return state
}
