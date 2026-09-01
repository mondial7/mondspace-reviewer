package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

// The audits msr can run over a change, besides telling its story (ADR 0024).
const (
	AuditSecurity domain.AnalysisKind = "security"
	AuditBreaking domain.AnalysisKind = "breaking"
)

// What keeps an audit card readable. These are caps, not targets: most audits
// should come back with nothing, and the ones that do not are a prompt to look
// rather than a report to file.
const (
	maxFindings  = 5
	findingChars = 140
	verdictChars = 110
	// auditDiffLines is how much of each file's change an audit is shown. Enough
	// to judge, bounded so a large review still fits a modest context window.
	auditDiffLines = 40
	auditMaxFiles  = 24
)

// Audit is one reading of a change: what it looks for, and how it asks.
type Audit struct {
	Kind  domain.AnalysisKind
	Title string
	// Purpose is the one line under the title on the card, so a reviewer knows
	// what they are about to spend a model call on.
	Purpose string
	// ask builds the question. It takes the review and nothing else — no other
	// audit's result can reach it, which is what makes these independent
	// readings rather than one conversation.
	ask func(target string, units []domain.Unit, diffs map[string]domain.Diff) string
}

// Audits is every audit on offer, in the order the cards appear.
func Audits() []Audit {
	return []Audit{
		{
			Kind:    AuditSecurity,
			Title:   "Security pass",
			Purpose: "what in this change is worth a second look",
			ask:     securityPrompt,
		},
		{
			Kind:    AuditBreaking,
			Title:   "Breaking changes",
			Purpose: "what this could break for existing callers",
			ask:     breakingPrompt,
		},
	}
}

// AuditFor finds an audit by kind.
func AuditFor(kind domain.AnalysisKind) (Audit, bool) {
	for _, a := range Audits() {
		if a.Kind == kind {
			return a, true
		}
	}
	return Audit{}, false
}

// RunAudit puts one audit's question to the model and returns what came back,
// trimmed to what a card can hold.
//
// It is one call. Each audit is a fresh question about the same diff, sharing
// nothing with the others: a model asked three things at once answers the first
// well and the rest as an afterthought, and the reviewer cannot tell which is
// which.
func RunAudit(ctx context.Context, n Narrator, a Audit, targetID string,
	units []domain.Unit, diffs map[string]domain.Diff) (domain.Analysis, error) {

	reply, err := ask(ctx, n, a.ask(targetID, units, diffs), auditSchema())
	if err != nil {
		// Silence would be indistinguishable from "nothing found", which is the
		// one answer a security card must never give by accident.
		return domain.Analysis{}, fmt.Errorf("%s audit: %w", a.Kind, err)
	}

	var out struct {
		Verdict  string `json:"verdict"`
		Findings []struct {
			File     string `json:"file"`
			Note     string `json:"note"`
			Severity string `json:"severity"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(extractJSON(reply)), &out); err != nil {
		return domain.Analysis{}, fmt.Errorf("%s audit: unreadable reply: %w", a.Kind, err)
	}

	result := domain.Analysis{
		TargetID: targetID,
		Kind:     a.Kind,
		At:       time.Now(),
		// Stored whole. The card has room for a hundred characters and cuts
		// what it shows to fit; storing the cut version made the ellipsis on
		// the card a dead end, because there was no longer anything for the
		// report to expand it to (ADR 0041). The schema already bounds this.
		Verdict: Brief(out.Verdict, verdictChars+60),
		// Fingerprinted over the diff text, not the unit list. A live review
		// diffs against the working tree, so its units' refs never move: an
		// audit keyed on those would call itself current for the rest of the
		// afternoon however much the agent rewrote (ADR 0037).
		Print:  ChangeFingerprint(units, diffs),
		Prints: FilePrints(units, diffs),
	}
	for _, f := range out.Findings {
		note := Brief(strings.TrimSpace(f.Note), findingChars+60)
		if note == "" {
			continue
		}
		result.Findings = append(result.Findings, domain.Finding{
			File: strings.TrimSpace(f.File),
			Note: note,
			// Normalised here rather than trusted: an endpoint that ignored the
			// schema can return any string, and a level nobody recognises must
			// not reach the page.
			Severity: domain.Severity(strings.ToLower(strings.TrimSpace(f.Severity))).Normalise(),
		})
		if len(result.Findings) == maxFindings {
			break
		}
	}

	// Worst first. A reviewer reads the top of a card and stops, so what they
	// read first has to be the thing most worth their attention — and the cap
	// above must drop the least important, not whatever came last.
	sort.SliceStable(result.Findings, func(i, j int) bool {
		return result.Findings[i].Severity.Rank() < result.Findings[j].Severity.Rank()
	})
	if result.Verdict == "" {
		result.Verdict = defaultVerdict(len(result.Findings))
	}
	return result, nil
}

// RunAuditIncremental re-reads only the part of a change that moved, and folds
// the answer into what the same audit already found (ADR 0038).
//
// A live review moves two files at a time. Re-reading fourteen to learn about
// two costs seven times what it needs to, takes seven times as long on a local
// model, and — because a rerun produces fresh findings that no longer match the
// old ones by text — quietly loses whatever the reviewer had already dismissed.
//
// It falls back to a whole-change reading whenever it cannot do better: no
// earlier result, an earlier result from before per-file prints existed, or a
// change where everything moved anyway.
func RunAuditIncremental(ctx context.Context, n Narrator, a Audit, targetID string,
	units []domain.Unit, diffs map[string]domain.Diff, earlier domain.Analysis) (domain.Analysis, error) {

	prints := FilePrints(units, diffs)
	if !earlier.Done() || len(earlier.Prints) == 0 {
		return RunAudit(ctx, n, a, targetID, units, diffs)
	}

	moved, gone := MovedFiles(earlier.Prints, prints)
	if len(moved) == 0 {
		// Nothing to ask about. Re-stamping is not the same as re-running: the
		// findings, the verdict and the reviewer's judgements are all still
		// exactly what this audit last said, and the only thing out of date is
		// the record of which files that was about.
		out := earlier
		out.Print, out.Prints = ChangeFingerprint(units, diffs), prints
		out.Read, out.Of = 0, len(prints)
		if len(gone) > 0 {
			out.Findings = keepFindings(out.Findings, Touched(gone))
		}
		return out, nil
	}
	if len(moved) >= len(prints) {
		// Everything moved. There is nothing to carry, so this is a full run
		// with extra steps.
		return RunAudit(ctx, n, a, targetID, units, diffs)
	}

	changed := UnitsTouching(units, Touched(moved))
	fresh, err := RunAudit(ctx, n, a, targetID, changed, DiffsOf(changed, diffs))
	if err != nil {
		return domain.Analysis{}, err
	}

	// What the fresh reading was not shown, it cannot speak for. Findings about
	// files it did not see are carried across exactly as they were, dismissals
	// and all; findings about files that moved are gone, because the reading
	// that just ran is the current answer for those.
	merged := fresh
	merged.Findings = append(
		CarryJudgements(fresh, earlier).Findings,
		keepFindings(earlier.Findings, Touched(moved, gone))...,
	)
	merged.Findings = rankFindings(merged.Findings)

	merged.Print, merged.Prints = ChangeFingerprint(units, diffs), prints
	merged.Read, merged.Of = len(moved), len(prints)
	return merged, nil
}

// keepFindings drops the findings about files in the given set.
//
// A finding that names no file at all is kept. It was a claim about the change
// as a whole, and a reading that saw two files out of fourteen is in no position
// to say it has stopped being true — dropping it would be a partial reading
// silently clearing a security finding, which is the one thing this must never
// do. A whole-change run is what clears it.
func keepFindings(findings []domain.Finding, drop map[string]bool) []domain.Finding {
	var out []domain.Finding
	for _, f := range findings {
		if f.File != "" && drop[f.File] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// rankFindings puts a merged set in the order a card reads it, and bounds it.
//
// Worst first, as everywhere else. The cap counts only what still stands: a
// dismissal costs nothing to keep and dropping one would invite the next run to
// raise the same thing as though nobody had ever looked at it.
func rankFindings(findings []domain.Finding) []domain.Finding {
	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].Severity.Rank() < findings[j].Severity.Rank()
	})

	out := make([]domain.Finding, 0, len(findings))
	standing := 0
	for _, f := range findings {
		if f.Stands() {
			if standing == maxFindings {
				continue
			}
			standing++
		}
		out = append(out, f)
		if len(out) == maxCarried {
			break
		}
	}
	return out
}

// maxCarried bounds a merged card overall, dismissals included. Without it a
// review worked on all afternoon accumulates a judgement history nobody reads
// on a card that has room for five lines.
const maxCarried = 4 * maxFindings

// defaultVerdict covers a model that filled in the findings and forgot the
// sentence. A card with an empty headline looks broken.
func defaultVerdict(findings int) string {
	if findings == 0 {
		return "Nothing here worth a second look."
	}
	return fmt.Sprintf("%s worth a look.", count(findings, "thing"))
}

// auditSchema is the shape every audit replies in. One sentence and a short
// list: the cap is in the schema so brevity is enforced by the grammar rather
// than requested politely in the prompt.
func auditSchema() port.JSONSchema {
	return port.JSONSchema{
		Name: "audit",
		Schema: object(map[string]any{
			"verdict": map[string]any{"type": "string", "maxLength": verdictChars + 60},
			"findings": map[string]any{
				"type":     "array",
				"maxItems": maxFindings,
				"items": object(map[string]any{
					"file": map[string]any{"type": "string", "maxLength": 120},
					"note": map[string]any{"type": "string", "maxLength": findingChars + 60},
					// An enum, so the model cannot invent a fourth level or
					// reach for "critical" when it means "have a look".
					"severity": map[string]any{"type": "string", "enum": severityNames()},
				}, "file", "note", "severity"),
			},
		}, "verdict", "findings"),
	}
}

// severityNames is the enum the schema constrains severity to.
func severityNames() []string {
	out := make([]string, 0, len(domain.Severities))
	for _, s := range domain.Severities {
		out = append(out, string(s))
	}
	return out
}

// changeDigest is the change itself, bounded. Every audit is shown exactly this
// and nothing else.
func changeDigest(units []domain.Unit, diffs map[string]domain.Diff) string {
	var b strings.Builder
	shown := 0
	for _, u := range units {
		if shown == auditMaxFiles {
			b.WriteString(fmt.Sprintf("\n…and %d more files\n", len(units)-shown))
			break
		}
		for _, f := range u.Files {
			b.WriteString("\n--- " + f + "\n")
		}
		b.WriteString(headLines(diffs[u.ID].Text, auditDiffLines))
		shown++
	}
	return b.String()
}

// headLines is the first n lines of a diff, so one enormous file cannot crowd
// out every other.
func headLines(text string, n int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= n {
		return text
	}
	return strings.Join(lines[:n], "\n") + "\n…\n"
}

// The two prompts below are deliberately written to make *silence* the easy
// answer. A model asked to "find security issues" will find them whether or not
// they are there; asked to report only what it can see, and told that finding
// nothing is a good answer, it mostly behaves.

func securityPrompt(target string, units []domain.Unit, diffs map[string]domain.Diff) string {
	return `You are helping a developer review a change for security.

Look only at the change below. Report only what you can actually see in it —
never anything you assume might exist elsewhere in the codebase.

Worth reporting: secrets or credentials added, authentication or authorization
that can be bypassed, unvalidated input reaching a query, command or path,
crypto used incorrectly, permissions widened, an error that hides a failure.

Not worth reporting: style, naming, missing tests, code that might mishandle an
edge case but has no security consequence, or anything you would have to guess
about code you cannot see. "Could be more robust" is not a security finding.

If there is nothing, say so and return an empty list — that is a good answer and
the usual one. Do not pad the list to look thorough: a verdict of "nothing
found" alongside a finding is a contradiction, and the empty list is what makes
the clean answer trustworthy.

If there is something, list *every* distinct problem you can see, not only the
most serious. Two obvious ones do not excuse skipping a third.

Reply as JSON:
  verdict  — one sentence, at most ` + fmt.Sprint(verdictChars) + ` characters.
  findings — at most ` + fmt.Sprint(maxFindings) + `. Each is the file, one
             sentence naming the concrete thing you saw, and a severity:
               high   — exploitable as written; do not merge without fixing it
               medium — probably wrong, or wrong in some contexts; check it
               low    — worth knowing, not worth blocking on
             Empty when there is nothing. Reserve "high" for what you can
             actually see going wrong, not for what sounds serious.

The change:
` + changeDigest(units, diffs)
}

func breakingPrompt(target string, units []domain.Unit, diffs map[string]domain.Diff) string {
	return `You are helping a developer review a change for breaking changes.

Look only at the change below, and ask one question of it: would existing
callers of this code still work after it?

Worth reporting: an exported function, method, type or field whose signature,
name or behaviour changed or was removed; a struct field removed or renamed; a
route, flag, environment variable or config key changed or dropped; a stored or
serialised format that older data no longer fits; a default that changed.

Not worth reporting: anything unexported or internal that callers cannot reach,
or refactors that keep the same surface.

A newly added function, type or field breaks nobody: it did not exist before, so
nothing was calling it. Never report an addition. Only something that was there
and is now different or gone can break a caller.

If nothing here breaks a caller, say so and return an empty list — that is a
good answer and the usual one. Do not pad the list to look thorough: a verdict
of "nothing breaks" alongside a finding is a contradiction, and the empty list
is what makes the clean answer trustworthy.

Reply as JSON:
  verdict  — one sentence, at most ` + fmt.Sprint(verdictChars) + ` characters.
  findings — at most ` + fmt.Sprint(maxFindings) + `. Each is the file, one
             sentence naming what changed and who it breaks, and a severity:
               high   — existing callers will fail to compile or run
               medium — behaviour changed in a way a caller could notice
               low    — a caller might care, but nothing breaks
             Empty when nothing breaks.

The change:
` + changeDigest(units, diffs)
}

// extractJSON pulls the object out of a reply that may be wrapped in prose or a
// code fence. A schema-constrained endpoint returns bare JSON and this is a
// no-op; one that ignored the schema usually returns the right object with
// something friendly around it.
func extractJSON(reply string) string {
	start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if start < 0 || end <= start {
		return reply
	}
	return reply[start : end+1]
}

// Judge records what a reviewer made of one finding (ADR 0030).
//
// The finding stays. Removing it would invite the next run of the audit to
// raise the same thing as though it were new, which is how a dismissal becomes
// pointless.
func Judge(a domain.Analysis, file, note string, verdict domain.Verdict) domain.Analysis {
	out := a
	out.Findings = append([]domain.Finding(nil), a.Findings...)
	for i := range out.Findings {
		if out.Findings[i].File == file && out.Findings[i].Note == note {
			out.Findings[i].Verdict = verdict
		}
	}
	return out
}

// CarryJudgements moves what the reviewer already decided onto a fresh run of
// the same audit.
//
// A rerun produces the same findings from the same diff, so without this a
// dismissal would last exactly until the next click. Matched on the file *and*
// the claim: if the model now says something different about the same file that
// is a new claim, and nobody has ruled on it.
func CarryJudgements(fresh, earlier domain.Analysis) domain.Analysis {
	if len(earlier.Findings) == 0 {
		return fresh
	}
	judged := make(map[string]domain.Verdict, len(earlier.Findings))
	for _, f := range earlier.Findings {
		if f.Verdict != "" {
			judged[f.File+"\x00"+f.Note] = f.Verdict
		}
	}

	out := fresh
	out.Findings = append([]domain.Finding(nil), fresh.Findings...)
	for i := range out.Findings {
		if v, ok := judged[out.Findings[i].File+"\x00"+out.Findings[i].Note]; ok {
			out.Findings[i].Verdict = v
		}
	}
	return out
}
