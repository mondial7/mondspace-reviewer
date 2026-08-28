package domain

import "time"

// AnalysisKind is one way of reading a change. The story — what happened and
// why — is one reading; a security pass and a breaking-change pass are others
// (ADR 0024).
//
// They are deliberately separate readings rather than one longer answer. A
// model asked three questions at once answers the first well and the others as
// an afterthought, and the reviewer cannot tell which is which.
type AnalysisKind string

// Finding is one thing worth a second look: where, and one line about why.
//
// There is no severity. A small local model assigning "critical" or "medium" is
// false precision dressed as a verdict, and this project is careful about the
// difference between what was stated and what was guessed (ADR 0003). The card
// says plainly that these are inferred; the reviewer decides what they weigh.
type Finding struct {
	File string `json:"file,omitempty"`
	Note string `json:"note"`
}

// Analysis is the result of running one audit over one target.
type Analysis struct {
	TargetID string       `json:"target_id"`
	Kind     AnalysisKind `json:"kind"`
	At       time.Time    `json:"at"`
	Model    string       `json:"model,omitempty"`
	// Verdict is the one-line answer, and it is required. Findings are usually
	// empty: "nothing here worth a second look" is the common result, and it has
	// to read as a result rather than as something that failed to run.
	Verdict  string    `json:"verdict"`
	Findings []Finding `json:"findings,omitempty"`
	// Print is what the review looked like when this ran, so a later visit can
	// say the code has moved rather than presenting a stale reading as current
	// (ADR 0021).
	Print string `json:"print,omitempty"`
}

// Done reports whether this audit has actually run.
func (a Analysis) Done() bool { return !a.At.IsZero() }

// Clean reports that it ran and found nothing.
func (a Analysis) Clean() bool { return a.Done() && len(a.Findings) == 0 }
