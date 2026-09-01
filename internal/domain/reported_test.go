package domain_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

func TestAReportedFindingNamesWhoSaidItAndWhy(t *testing.T) {
	// This is the whole of what separates `reported` from `inferred`: it can be
	// looked up, suppressed in the tool's own config, and reproduced (ADR 0043).
	r := domain.Reported{
		Tool: "gosec", Rule: "G404", File: "internal/api/handler.go", Line: 42,
		Message: "Use of weak random number generator", Severity: domain.SeverityMedium,
	}
	if got := r.Where(); got != "internal/api/handler.go:42" {
		t.Errorf("Where() = %q", got)
	}
	if got := r.Ref(); got != "gosec/G404" {
		t.Errorf("Ref() = %q; a rule id alone is not something anybody can look up", got)
	}
	if !r.Stands() {
		t.Error("a finding nobody has ruled on still stands")
	}
}

func TestAFindingAboutAWholeFileHasNoLine(t *testing.T) {
	r := domain.Reported{Tool: "gitleaks", Rule: "generic-api-key", File: "config.yaml"}
	if got := r.Where(); got != "config.yaml" {
		t.Errorf("Where() = %q, want no line", got)
	}
	if got := (domain.Reported{Tool: "go vet"}).Ref(); got != "go vet" {
		t.Errorf("Ref() = %q, want the tool alone when it named no rule", got)
	}
}

func TestAFindingKeepsItsIdentityWhenTheDiffGrowsAboveIt(t *testing.T) {
	// A key that included the line number would lose every dismissal on a file
	// the moment anything was added to the top of it.
	at42 := domain.Reported{Tool: "t", Rule: "r", File: "a.go", Line: 42, Message: "m"}
	at99 := domain.Reported{Tool: "t", Rule: "r", File: "a.go", Line: 99, Message: "m"}
	if at42.Key() != at99.Key() {
		t.Error("the same finding on a moved line is still the same finding")
	}

	other := domain.Reported{Tool: "t", Rule: "r", File: "a.go", Line: 42, Message: "something else"}
	if at42.Key() == other.Key() {
		t.Error("a different claim about the same line is a different finding")
	}
}

func TestADismissedFindingStopsStanding(t *testing.T) {
	r := domain.Reported{Tool: "t", Rule: "r", File: "a.go", Verdict: domain.VerdictDismissed}
	if r.Stands() {
		t.Error("a dismissal that does not stick is not a dismissal")
	}
}
