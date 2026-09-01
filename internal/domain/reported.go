package domain

// WhyReported is a third class beside stated and inferred (ADR 0003, ADR 0043).
//
// `stated` is the agent's own words. `inferred` is a model's guess. `reported`
// is what a deterministic analyser said: reproducible, attributable to a named
// tool and a named rule, and the same answer every time it is asked.
//
// It is the only one of the three a reviewer can act on without checking it
// first, and the interface should say so by how it looks rather than by
// explaining it.
const WhyReported = "reported"

// Reported is one finding from a deterministic analyser.
//
// It carries where it is, what was said, and — this is the part that makes it
// `reported` rather than `inferred` — exactly who said it and under which rule.
// A finding that cannot name its rule is not reproducible and does not belong
// in this class.
type Reported struct {
	// Tool is the analyser's name as msr knows it: "golangci-lint", "gosec".
	Tool string `json:"tool"`
	// Rule is the tool's own identifier for what was violated: "errcheck",
	// "G404", "no-unused-vars". Verbatim, so it can be searched for, suppressed
	// in the tool's own config, or read about.
	Rule string `json:"rule"`
	// File is repository-relative, with forward slashes, so it matches the
	// paths git reports and the units built from them.
	File string `json:"file"`
	// Line is 1-indexed. Zero means the finding is about the file as a whole,
	// which some tools do report.
	Line int `json:"line,omitempty"`
	// Message is the tool's sentence, as it wrote it.
	Message  string   `json:"message"`
	Severity Severity `json:"severity,omitempty"`
	// Verdict is what the reviewer decided, exactly as for a model's finding: a
	// dismissal has to survive the next run or it is not a dismissal (ADR 0030).
	Verdict Verdict `json:"verdict,omitempty"`
	// New says this finding is about a line the change actually touched. The
	// alternative is a finding that was already there before anybody opened
	// this review, and showing every one of those would make the whole layer
	// worthless (ADR 0043).
	New bool `json:"new,omitempty"`
	// Anchor is the diff line this finding sits on, verbatim, so it survives
	// the diff growing above it — the same anchoring an annotation uses
	// (ADR 0028).
	Anchor string `json:"anchor,omitempty"`
}

// Stands reports whether this finding is still something to deal with.
func (r Reported) Stands() bool { return r.Verdict != VerdictDismissed }

// Where names the finding's location for a human: "internal/api/handler.go:42".
func (r Reported) Where() string {
	if r.Line == 0 {
		return r.File
	}
	return r.File + ":" + itoa(r.Line)
}

// Ref names the rule for a human: "gosec/G404". Both halves, because rule ids
// are only unique within a tool and "G404" alone is not something anybody can
// look up.
func (r Reported) Ref() string {
	if r.Rule == "" {
		return r.Tool
	}
	return r.Tool + "/" + r.Rule
}

// Key identifies a finding across runs, for carrying a dismissal onto the next
// one.
//
// Deliberately not the line number: a diff grows above a finding constantly,
// and a key that moved would lose every dismissal on the file every time
// anything was added to the top of it. Tool, rule, file and the tool's own
// message are stable across exactly the runs where it is the same finding.
func (r Reported) Key() string {
	return r.Tool + "\x00" + r.Rule + "\x00" + r.File + "\x00" + r.Message
}

// itoa is strconv.Itoa without the import. This package deliberately depends on
// nothing.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
