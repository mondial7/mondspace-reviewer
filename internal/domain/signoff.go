package domain

import "time"

// Signoff records that a reviewer finished with a target, and what they wanted
// to say about it as a whole (ADR 0021).
//
// Notes answer "what do I think of this change to this file". Nothing answered
// "am I done with this, and what is my overall view" — which is the question
// you have on reopening something you looked at yesterday.
type Signoff struct {
	TargetID string    `json:"target_id"`
	At       time.Time `json:"at"`
	// Comment is the closing remark on the change as a whole. Optional: being
	// done is worth recording even with nothing to add.
	Comment string `json:"comment,omitempty"`
	// Print and Files are what the review looked like at the moment it was
	// signed off, so reopening can say whether the code has moved underneath
	// the judgement. A count alone is not enough — editing a line in an
	// already-changed file leaves it identical — so the fingerprint decides.
	Print string `json:"print,omitempty"`
	Files int    `json:"files"`
}

// Done reports whether this is a real sign-off rather than a zero value.
func (s Signoff) Done() bool { return !s.At.IsZero() }
