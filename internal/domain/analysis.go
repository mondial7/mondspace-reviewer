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

// Severity is how much a finding should interrupt the reviewer.
//
// Three levels, not five, and defined by what the reviewer should *do* rather
// than by a score nobody computed. A finer scale would invite a small local
// model to express confidence it does not have, and the whole set is still
// labelled inferred: it is the model's suggestion of weight, not a rating
// (ADR 0003, ADR 0024).
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
// An endpoint that ignored the schema can return anything. Dropping the finding
// would hide it and calling it high would cry wolf, so an unusable level
// becomes "worth checking" — the honest answer when the model did not say.
func (s Severity) Normalise() Severity {
	for _, known := range Severities {
		if s == known {
			return known
		}
	}
	return SeverityMedium
}

// Verdict is what the reviewer decided about a finding (ADR 0030).
//
// Only "dismissed" exists as a state that changes anything: a finding nobody
// has ruled on and a finding somebody confirmed are both things still to deal
// with, and inventing a third colour for the difference would be decoration.
type Verdict string

const (
	// VerdictDismissed: looked at, not a problem. It stays on the card, greyed,
	// because deleting it would invite the next audit to raise it again as
	// though it were new.
	VerdictDismissed Verdict = "dismissed"
	// VerdictConfirmed: looked at, and it is real. Recorded because "I have
	// read this" is worth distinguishing from "nobody has looked".
	VerdictConfirmed Verdict = "confirmed"
)

// Finding is one thing worth a second look: where, one line about why, how much
// it should interrupt, and what the reviewer made of it.
type Finding struct {
	File     string   `json:"file,omitempty"`
	Note     string   `json:"note"`
	Severity Severity `json:"severity,omitempty"`
	Verdict  Verdict  `json:"verdict,omitempty"`
}

// Stands reports whether this finding is still something to deal with.
func (f Finding) Stands() bool { return f.Verdict != VerdictDismissed }

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
	// (ADR 0021, ADR 0037).
	Print string `json:"print,omitempty"`
	// Prints is what each file looked like when this ran, keyed by path. It is
	// the finer version of the same question: not "has anything moved" but
	// "which of these findings is this still about" (ADR 0038).
	Prints map[string]string `json:"prints,omitempty"`
	// Read is how many files the last run actually put in front of the model,
	// and Of how many there were. Equal means it read the whole change; fewer
	// means the rest was carried forward from an earlier run of this same audit,
	// and the card says so rather than implying a fresh whole-change reading.
	Read int `json:"read,omitempty"`
	Of   int `json:"of,omitempty"`
	// Engine is what actually answered, and Fallback says it was not the engine
	// this reading is routed to. A verdict from a 4B model shown with the same
	// confidence as one from the CLI is worse than no verdict, because the
	// reviewer has no way to know how much of it to believe (ADR 0039).
	Engine   Engine `json:"engine,omitempty"`
	Fallback bool   `json:"fallback,omitempty"`
}

// Partial reports that the last run of this audit re-read only part of the
// change and carried the rest forward.
func (a Analysis) Partial() bool { return a.Of > 0 && a.Read > 0 && a.Read < a.Of }

// Done reports whether this audit has actually run.
func (a Analysis) Done() bool { return !a.At.IsZero() }

// Clean reports that it ran and found nothing.
func (a Analysis) Clean() bool { return a.Done() && len(a.Standing()) == 0 }

// Standing is the findings the reviewer has not dismissed. Everything that
// counts or colours anything is measured on these: a card still reporting
// "2 high" after both were dismissed has not listened.
func (a Analysis) Standing() []Finding {
	var out []Finding
	for _, f := range a.Findings {
		if f.Stands() {
			out = append(out, f)
		}
	}
	return out
}

// Worst is the highest severity among the findings, or empty when there are
// none. The card is coloured from it, so a row of cards can be read at a glance
// without opening any of them.
func (a Analysis) Worst() Severity {
	worst := Severity("")
	for _, f := range a.Standing() {
		if worst == "" || f.Severity.Rank() < worst.Rank() {
			worst = f.Severity.Normalise()
		}
	}
	return worst
}

// Tally counts findings by severity. "1 high · 2 medium" says more in the same
// space than "3 to look at".
func (a Analysis) Tally() map[Severity]int {
	out := map[Severity]int{}
	for _, f := range a.Standing() {
		out[f.Severity.Normalise()]++
	}
	return out
}
