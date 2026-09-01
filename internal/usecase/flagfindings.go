package usecase

import (
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// msr's own flags, absorbed into the reported layer (ADR 0043).
//
// The `no-test` / `swallowed-err` flags were always a hand-rolled static
// analyser: deterministic, rule-named, reproducible — every property that makes
// a finding `reported` rather than `inferred`. They predate the analyser layer
// and they were running beside it, which meant two mechanisms producing the
// same kind of answer with only one of them dismissible, counted, or rolled up
// per file.
//
// So there is one producer, `Flags`, and two renderings of it. The flags that
// warrant stopping become findings, where they can be dismissed and counted
// alongside gosec's. The rest stay chips and a tally, because that is what they
// are: facts about the change, not warnings about it, and painting them as
// findings is the thing ADR 0041 stopped doing (ADR 0041).

// msrTool is what a finding from msr's own rules is attributed to. Named, like
// every other tool, because "which analyser said this" has to have an answer.
const msrTool = "msr"

// stopFlags are the flags that mean stop, and what msr means by each.
//
// The wording matters more here than anywhere else in this file: a flag chip is
// a word a reviewer learns, and a finding is a sentence a reviewer reads once.
var stopFlags = map[domain.Flag]struct {
	Severity domain.Severity
	Message  string
}{
	domain.FlagSwallowedErr: {domain.SeverityMedium,
		"An error is assigned and then discarded on a line this change added. " +
			"Either it cannot happen, and the code should say why, or it can."},
	domain.FlagPublicAPI: {domain.SeverityMedium,
		"An exported declaration was removed or changed. Whatever was calling it " +
			"is outside this change and will not compile."},
	domain.FlagFailed: {domain.SeverityHigh,
		"A tool call the agent made failed here. What it was trying to do may " +
			"not have happened."},
}

// FlagFindings turns msr's own deterministic flags into findings.
//
// File-level, with no line. The flag rules answer "is this true of this file",
// not "where"; claiming a line would be inventing one, and an anchor that is a
// guess is worse than none (ADR 0028).
//
// Marked new, always: every one of them is derived from the change's own diff,
// so there is no version of them that was "already there".
func FlagFindings(units []domain.Unit, diffs map[string]domain.Diff) []domain.Reported {
	var out []domain.Reported
	for _, u := range units {
		for _, flag := range Flags(u, diffs[u.ID]) {
			meaning, stops := stopFlags[flag]
			if !stops {
				continue
			}
			for _, file := range u.Files {
				out = append(out, domain.Reported{
					Tool:     msrTool,
					Rule:     string(flag),
					File:     file,
					Message:  meaning.Message,
					Severity: meaning.Severity,
					New:      true,
				})
			}
		}
	}
	return out
}
