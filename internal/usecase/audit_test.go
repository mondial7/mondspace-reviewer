package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func auditReview() ([]domain.Unit, map[string]domain.Diff) {
	units := []domain.Unit{
		{ID: "u1", Files: []string{"auth/token.go"}},
		{ID: "u2", Files: []string{"api/handler.go"}},
	}
	diffs := map[string]domain.Diff{
		"u1": {Text: "@@\n+func Valid(t string) bool {\n+\treturn hmac.Equal(sum, sig)\n"},
		"u2": {Text: "@@\n-func Routes() *http.ServeMux {\n+func Routes(v auth.Validator) *http.ServeMux {\n"},
	}
	return units, diffs
}

func TestEveryAuditIsOfferedWithSomethingToSayAboutItself(t *testing.T) {
	// The cards are chosen from this list, so an audit with no title or purpose
	// would render as a blank button.
	got := usecase.Audits()
	if len(got) < 2 {
		t.Fatalf("want at least a security and a breaking-change audit, got %d", len(got))
	}

	seen := map[domain.AnalysisKind]bool{}
	for _, a := range got {
		if a.Kind == "" || a.Title == "" || a.Purpose == "" {
			t.Errorf("%+v: every audit needs a kind, a title and a purpose", a)
		}
		if seen[a.Kind] {
			t.Errorf("%s offered twice", a.Kind)
		}
		seen[a.Kind] = true
	}
	for _, want := range []domain.AnalysisKind{usecase.AuditSecurity, usecase.AuditBreaking} {
		if !seen[want] {
			t.Errorf("%s should be offered", want)
		}
	}
}

func TestAnAuditThatFindsNothingIsAResultNotAFailure(t *testing.T) {
	// The common case, and the one that has to read well: a reviewer glancing at
	// a clean card should learn something in one line, not wonder whether it ran.
	d := &describer{reply: `{"verdict":"Nothing here worth a second look.","findings":[]}`}
	units, diffs := auditReview()

	got, err := usecase.RunAudit(context.Background(), d, mustAudit(t, usecase.AuditSecurity),
		"t1", units, diffs)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}

	if !got.Done() {
		t.Error("it ran, so it is done")
	}
	if !got.Clean() {
		t.Errorf("no findings means clean, got %+v", got.Findings)
	}
	if got.Verdict == "" {
		t.Error("a clean audit still has to say something")
	}
}

func TestFindingsAreCappedSoTheCardStaysReadable(t *testing.T) {
	// A card that lists twenty things is a wall, and a reviewer skims a wall.
	// The cap is what keeps this a prompt to look rather than a report to file.
	var b strings.Builder
	b.WriteString(`{"verdict":"Several things to check.","findings":[`)
	for i := 0; i < 12; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"file":"a.go","note":"something worth checking"}`)
	}
	b.WriteString(`]}`)

	d := &describer{reply: b.String()}
	units, diffs := auditReview()

	got, err := usecase.RunAudit(context.Background(), d, mustAudit(t, usecase.AuditSecurity),
		"t1", units, diffs)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}
	if len(got.Findings) > 5 {
		t.Errorf("got %d findings, want at most 5", len(got.Findings))
	}
}

func TestALongFindingIsTrimmedRatherThanShown(t *testing.T) {
	long := strings.Repeat("this is a very long explanation that nobody reads ", 12)
	d := &describer{reply: `{"verdict":"one thing","findings":[{"file":"a.go","note":"` + long + `"}]}`}
	units, diffs := auditReview()

	got, _ := usecase.RunAudit(context.Background(), d, mustAudit(t, usecase.AuditBreaking),
		"t1", units, diffs)

	if len(got.Findings) != 1 {
		t.Fatalf("got %+v", got.Findings)
	}
	if n := len([]rune(got.Findings[0].Note)); n > 160 {
		t.Errorf("note is %d characters; the card cannot hold that", n)
	}
}

func TestAnAuditSeesTheReviewAndNothingElse(t *testing.T) {
	// The whole point of running these separately. Whatever the security audit
	// concluded must never reach the breaking-change audit: two independent
	// readings of the same diff are worth more than one reading twice.
	units, diffs := auditReview()

	first := &describer{reply: `{"verdict":"SECRET-SECURITY-VERDICT","findings":[{"file":"a.go","note":"SECRET-SECURITY-FINDING"}]}`}
	if _, err := usecase.RunAudit(context.Background(), first,
		mustAudit(t, usecase.AuditSecurity), "t1", units, diffs); err != nil {
		t.Fatalf("first audit: %v", err)
	}

	second := &describer{reply: `{"verdict":"fine","findings":[]}`}
	if _, err := usecase.RunAudit(context.Background(), second,
		mustAudit(t, usecase.AuditBreaking), "t1", units, diffs); err != nil {
		t.Fatalf("second audit: %v", err)
	}

	for _, asked := range second.asked {
		for _, leaked := range []string{"SECRET-SECURITY-VERDICT", "SECRET-SECURITY-FINDING"} {
			if strings.Contains(asked, leaked) {
				t.Errorf("the breaking audit was shown the security audit's output:\n%s", asked)
			}
		}
	}
	// ...and it did see the actual change, or it is auditing nothing.
	if !strings.Contains(strings.Join(second.asked, "\n"), "auth/token.go") {
		t.Error("the audit should be shown the files it is judging")
	}
}

func TestEachAuditAsksItsOwnQuestion(t *testing.T) {
	// Two audits differing only in name would be one audit run twice.
	units, diffs := auditReview()

	sec := &describer{reply: `{"verdict":"x","findings":[]}`}
	usecase.RunAudit(context.Background(), sec, mustAudit(t, usecase.AuditSecurity), "t1", units, diffs)

	brk := &describer{reply: `{"verdict":"x","findings":[]}`}
	usecase.RunAudit(context.Background(), brk, mustAudit(t, usecase.AuditBreaking), "t1", units, diffs)

	if strings.Join(sec.asked, "") == strings.Join(brk.asked, "") {
		t.Fatal("the two audits asked the same question")
	}
	if !strings.Contains(strings.ToLower(strings.Join(sec.asked, " ")), "security") {
		t.Error("the security audit should say what it is looking for")
	}
	lower := strings.ToLower(strings.Join(brk.asked, " "))
	if !strings.Contains(lower, "break") && !strings.Contains(lower, "caller") {
		t.Error("the breaking audit should say what it is looking for")
	}
}

func TestAnAuditIsSchemaConstrained(t *testing.T) {
	// Same discipline as narration: the shape is guaranteed by the server, not
	// parsed hopefully out of prose (ADR 0014).
	d := &describer{reply: `{"verdict":"x","findings":[]}`}
	units, diffs := auditReview()

	usecase.RunAudit(context.Background(), d, mustAudit(t, usecase.AuditSecurity), "t1", units, diffs)

	if len(d.schema) != 1 {
		t.Fatalf("want one schema-constrained call, got %d", len(d.schema))
	}
	props, ok := d.schema[0].Schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("the schema should describe an object")
	}
	for _, want := range []string{"verdict", "findings"} {
		if _, has := props[want]; !has {
			t.Errorf("the schema should require %q", want)
		}
	}
}

func TestAModelThatCannotAnswerIsReportedNotInvented(t *testing.T) {
	// Silence would be indistinguishable from "nothing found", which is the one
	// answer a security card must never give by accident.
	d := &describer{err: errors.New("summarizer offline")}
	units, diffs := auditReview()

	_, err := usecase.RunAudit(context.Background(), d, mustAudit(t, usecase.AuditSecurity),
		"t1", units, diffs)

	if err == nil {
		t.Fatal("a failed audit must be reported, not read as clean")
	}
}

func TestAnAuditRecordsWhatItLookedAt(t *testing.T) {
	// So a card can say "run against an older version of this" rather than
	// reading as current, the same discipline as a sign-off (ADR 0021).
	d := &describer{reply: `{"verdict":"x","findings":[]}`}
	units, diffs := auditReview()

	got, _ := usecase.RunAudit(context.Background(), d, mustAudit(t, usecase.AuditSecurity),
		"t1", units, diffs)

	if got.Print == "" {
		t.Error("an audit should fingerprint what it read")
	}
	if got.Kind != usecase.AuditSecurity || got.TargetID != "t1" {
		t.Errorf("got %+v, want it to know what it audited", got)
	}
}

func mustAudit(t *testing.T, kind domain.AnalysisKind) usecase.Audit {
	t.Helper()
	a, ok := usecase.AuditFor(kind)
	if !ok {
		t.Fatalf("no audit %q", kind)
	}
	return a
}

func TestFindingsCarryASeverityTheModelChoseFromAFixedSet(t *testing.T) {
	// Three levels, not five, and named for what the reviewer should do rather
	// than for a score nobody computed (ADR 0024).
	d := &describer{reply: `{"verdict":"Two things.","findings":[
		{"file":"a.go","note":"secret committed","severity":"high"},
		{"file":"b.go","note":"worth checking","severity":"low"}]}`}
	units, diffs := auditReview()

	got, err := usecase.RunAudit(context.Background(), d, mustAudit(t, usecase.AuditSecurity),
		"t1", units, diffs)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}

	if len(got.Findings) != 2 {
		t.Fatalf("got %+v", got.Findings)
	}
	if got.Findings[0].Severity != domain.SeverityHigh {
		t.Errorf("first = %+v, want high", got.Findings[0])
	}

	// The model cannot invent a level: the schema names the three.
	props := d.schema[0].Schema["properties"].(map[string]any)
	items := props["findings"].(map[string]any)["items"].(map[string]any)
	sev := items["properties"].(map[string]any)["severity"].(map[string]any)
	enum, ok := sev["enum"].([]string)
	if !ok || len(enum) != 3 {
		t.Errorf("severity should be an enum of three, got %v", sev["enum"])
	}
}

func TestTheWorstFindingIsListedFirst(t *testing.T) {
	// A reviewer reads the top of a card and stops. What they read first has to
	// be the thing most worth their attention.
	d := &describer{reply: `{"verdict":"Several.","findings":[
		{"file":"a.go","note":"minor","severity":"low"},
		{"file":"b.go","note":"serious","severity":"high"},
		{"file":"c.go","note":"middling","severity":"medium"}]}`}
	units, diffs := auditReview()

	got, _ := usecase.RunAudit(context.Background(), d, mustAudit(t, usecase.AuditSecurity),
		"t1", units, diffs)

	want := []domain.Severity{domain.SeverityHigh, domain.SeverityMedium, domain.SeverityLow}
	for i, w := range want {
		if got.Findings[i].Severity != w {
			t.Errorf("finding %d = %s, want %s", i, got.Findings[i].Severity, w)
		}
	}
}

func TestAMissingOrInventedSeverityBecomesTheMiddleOne(t *testing.T) {
	// An endpoint that ignored the schema can return anything. Dropping the
	// finding would hide it; calling it high would cry wolf. "Worth checking"
	// is the honest answer when the model did not say.
	d := &describer{reply: `{"verdict":"x","findings":[
		{"file":"a.go","note":"no severity given"},
		{"file":"b.go","note":"nonsense level","severity":"CATASTROPHIC"}]}`}
	units, diffs := auditReview()

	got, _ := usecase.RunAudit(context.Background(), d, mustAudit(t, usecase.AuditSecurity),
		"t1", units, diffs)

	for _, f := range got.Findings {
		if f.Severity != domain.SeverityMedium {
			t.Errorf("%+v: an unusable level should become medium", f)
		}
	}
}

func TestAnAnalysisKnowsItsWorstFinding(t *testing.T) {
	// The card is coloured from this, so a page of cards can be read at a
	// glance without opening any of them.
	a := domain.Analysis{At: time.Now(), Findings: []domain.Finding{
		{Note: "a", Severity: domain.SeverityLow},
		{Note: "b", Severity: domain.SeverityHigh},
		{Note: "c", Severity: domain.SeverityMedium},
	}}
	if got := a.Worst(); got != domain.SeverityHigh {
		t.Errorf("Worst = %s, want high", got)
	}

	clean := domain.Analysis{At: time.Now()}
	if got := clean.Worst(); got != "" {
		t.Errorf("a clean audit has no worst finding, got %q", got)
	}
}

func TestAnAnalysisCountsBySeverity(t *testing.T) {
	// "1 high · 2 medium" says more in the same space than "3 to look at".
	a := domain.Analysis{At: time.Now(), Findings: []domain.Finding{
		{Severity: domain.SeverityHigh}, {Severity: domain.SeverityMedium},
		{Severity: domain.SeverityMedium}, {Severity: domain.SeverityLow},
	}}

	got := a.Tally()

	if got[domain.SeverityHigh] != 1 || got[domain.SeverityMedium] != 2 || got[domain.SeverityLow] != 1 {
		t.Errorf("Tally = %v", got)
	}
}

func TestAFindingCanBeDismissedAndStaysDismissed(t *testing.T) {
	// A finding you have judged not-real comes back identically on every rerun.
	// Without somewhere to put that judgement, the only way to stop seeing it is
	// to stop running the audit (ADR 0030).
	before := domain.Analysis{
		TargetID: "t1", Kind: usecase.AuditSecurity, At: time.Now(),
		Findings: []domain.Finding{
			{File: "a.go", Note: "hardcoded secret", Severity: domain.SeverityHigh},
			{File: "b.go", Note: "unvalidated input", Severity: domain.SeverityMedium},
		},
	}

	judged := usecase.Judge(before, "a.go", "hardcoded secret", domain.VerdictDismissed)

	if len(judged.Findings) != 2 {
		t.Fatalf("dismissing must not remove it: %+v", judged.Findings)
	}
	var dismissed, standing int
	for _, f := range judged.Findings {
		switch f.Verdict {
		case domain.VerdictDismissed:
			dismissed++
		default:
			standing++
		}
	}
	if dismissed != 1 || standing != 1 {
		t.Errorf("got %d dismissed and %d standing, want one each", dismissed, standing)
	}
}

func TestADismissalSurvivesTheAuditBeingRunAgain(t *testing.T) {
	// The point. A rerun produces the same findings from the same diff, and the
	// judgement has to be carried onto them or it was pointless.
	judged := domain.Analysis{
		TargetID: "t1", Kind: usecase.AuditSecurity, At: time.Now(),
		Findings: []domain.Finding{
			{File: "a.go", Note: "hardcoded secret", Verdict: domain.VerdictDismissed},
			{File: "b.go", Note: "unvalidated input"},
		},
	}
	rerun := domain.Analysis{
		TargetID: "t1", Kind: usecase.AuditSecurity, At: time.Now(),
		Findings: []domain.Finding{
			{File: "a.go", Note: "hardcoded secret", Severity: domain.SeverityHigh},
			{File: "b.go", Note: "unvalidated input", Severity: domain.SeverityMedium},
			{File: "c.go", Note: "something new", Severity: domain.SeverityLow},
		},
	}

	got := usecase.CarryJudgements(rerun, judged)

	by := map[string]domain.Verdict{}
	for _, f := range got.Findings {
		by[f.File] = f.Verdict
	}
	if by["a.go"] != domain.VerdictDismissed {
		t.Errorf("a.go = %q, want the dismissal carried over", by["a.go"])
	}
	if by["b.go"] != "" || by["c.go"] != "" {
		t.Errorf("nothing else should be judged: %+v", by)
	}
}

func TestAFindingThatChangedIsNotSilentlyStillDismissed(t *testing.T) {
	// A dismissal is about a specific claim. If the model now says something
	// different about the same file, that is a new claim and has not been ruled
	// on — carrying the dismissal across would hide it.
	judged := domain.Analysis{Findings: []domain.Finding{
		{File: "a.go", Note: "hardcoded secret", Verdict: domain.VerdictDismissed},
	}}
	rerun := domain.Analysis{Findings: []domain.Finding{
		{File: "a.go", Note: "the key is read from the environment now, but logged"},
	}}

	got := usecase.CarryJudgements(rerun, judged)

	if got.Findings[0].Verdict != "" {
		t.Errorf("a different claim about the same file must not inherit a dismissal")
	}
}

func TestStandingFindingsAreWhatTheCardCountsAndColours(t *testing.T) {
	// A card that still says "2 high" after both were dismissed has not
	// listened.
	a := domain.Analysis{At: time.Now(), Findings: []domain.Finding{
		{File: "a.go", Note: "x", Severity: domain.SeverityHigh, Verdict: domain.VerdictDismissed},
		{File: "b.go", Note: "y", Severity: domain.SeverityLow},
	}}

	if got := a.Worst(); got != domain.SeverityLow {
		t.Errorf("Worst = %q, want the worst *standing* finding", got)
	}
	if tally := a.Tally(); tally[domain.SeverityHigh] != 0 || tally[domain.SeverityLow] != 1 {
		t.Errorf("Tally = %v, want only what still stands", tally)
	}
	if a.Clean() {
		t.Error("something still stands, so it is not clean")
	}
}

func TestDismissingEverythingLeavesACleanCard(t *testing.T) {
	a := domain.Analysis{At: time.Now(), Findings: []domain.Finding{
		{File: "a.go", Note: "x", Severity: domain.SeverityHigh, Verdict: domain.VerdictDismissed},
	}}
	if !a.Clean() {
		t.Error("with everything dismissed, nothing stands")
	}
}
