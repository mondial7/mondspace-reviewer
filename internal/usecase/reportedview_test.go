package usecase_test

import (
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func threeFiles() []domain.Unit {
	return []domain.Unit{
		{ID: "u1", Files: []string{"api/handler.go"}},
		{ID: "u2", Files: []string{"api/routes.go"}},
		{ID: "u3", Files: []string{"README.md"}},
	}
}

func TestFindingsRollUpPerFileAndIntoOneSentence(t *testing.T) {
	view := usecase.GroupReported([]contract.Item{
		contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "api/handler.go", StartLine: 42, EndLine: 42}, Producer: "gosec", RuleID: "G404", Message: "weak rng", Severity: domain.SeverityMedium, New: true},
		contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "api/handler.go", StartLine: 9, EndLine: 9}, Producer: "gosec", RuleID: "G304", Message: "file inclusion", Severity: domain.SeverityHigh, New: true},
		contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "api/routes.go", StartLine: 3, EndLine: 3}, Producer: "staticcheck", RuleID: "SA4006", Message: "unused", Severity: domain.SeverityLow},
	}, threeFiles())

	if view.Files != 1 || view.Of != 3 {
		t.Errorf("got %d of %d, want 1 of 3", view.Files, view.Of)
	}
	if view.New != 2 || view.Standing != 1 {
		t.Errorf("got %d new and %d standing, want 2 and 1", view.New, view.Standing)
	}
	if got := view.Summary(); !strings.Contains(got, "1 of 3 files has findings") {
		t.Errorf("Summary() = %q", got)
	}
	// Worst first, so reading the top of a file's list is reading the thing
	// most worth looking at.
	at := view.FindingsFor("api/handler.go")
	if len(at.New) != 2 || at.New[0].RuleID != "G304" {
		t.Errorf("findings = %+v, want the high one first", at.New)
	}
	if at.Worst != domain.SeverityHigh {
		t.Errorf("worst = %q", at.Worst)
	}
	if strings.Join(view.Tools, ",") != "gosec,staticcheck" {
		t.Errorf("tools = %v; a reviewer has to be able to tell 'found nothing' from 'never ran'", view.Tools)
	}
}

func TestNothingNewStillSaysWhatIsAlreadyThere(t *testing.T) {
	// Silently having none and silently hiding four hundred look the same.
	view := usecase.GroupReported([]contract.Item{
		contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "api/routes.go", StartLine: 3, EndLine: 3}, Producer: "staticcheck", RuleID: "SA1", Message: "old"},
		contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "api/routes.go", StartLine: 9, EndLine: 9}, Producer: "staticcheck", RuleID: "SA2", Message: "older"},
	}, threeFiles())

	if view.Any() {
		t.Error("nothing new is nothing new")
	}
	if got := view.Summary(); !strings.Contains(got, "2 findings already here") {
		t.Errorf("Summary() = %q", got)
	}
}

func TestACleanReviewSaysSo(t *testing.T) {
	view := usecase.GroupReported(nil, threeFiles())
	if got := view.Summary(); !strings.Contains(got, "Nothing to report in 3 files") {
		t.Errorf("Summary() = %q", got)
	}
	if view.Summary() == "" {
		t.Error("a clean result has to read as a result, not as something that failed to run")
	}
}

func TestAReviewWithNoFilesSaysNothingAtAll(t *testing.T) {
	if got := usecase.GroupReported(nil, nil).Summary(); got != "" {
		t.Errorf("Summary() = %q, want silence", got)
	}
}

func TestADismissedFindingStopsCounting(t *testing.T) {
	// A layer still saying "3 findings" after all three were dismissed has not
	// listened (ADR 0030).
	view := usecase.GroupReported([]contract.Item{
		contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "api/handler.go", StartLine: 42, EndLine: 42}, Producer: "gosec", RuleID: "G404", Message: "weak rng", New: true, Verdict: domain.VerdictDismissed},
	}, threeFiles())

	if view.Any() || view.Files != 0 {
		t.Errorf("view = %+v, want nothing standing", view)
	}
}

func TestStoredRulingsAreStampedOntoAFreshRun(t *testing.T) {
	fresh := []contract.Item{
		contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "a.go", StartLine: 42, EndLine: 42}, Producer: "gosec", RuleID: "G404", Message: "weak rng"},
	}
	got := usecase.ApplyDismissals(fresh, map[string]domain.Verdict{
		fresh[0].Key(): domain.VerdictDismissed,
	})
	if got[0].Stands() {
		t.Error("the stored ruling was not applied")
	}
}

func TestAFindingOnAFileTheReviewDoesNotListIsCountedNotDropped(t *testing.T) {
	// A finding msr cannot place is still a finding, and pretending otherwise
	// is how a count comes to disagree with itself.
	view := usecase.GroupReported([]contract.Item{
		contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "somewhere/else.go", StartLine: 1, EndLine: 1}, Producer: "gosec", RuleID: "G1", Message: "x", New: true},
	}, threeFiles())

	if view.Files != 0 {
		t.Errorf("it belongs to no file in this review: %+v", view.ByFile)
	}
	if view.Standing != 1 {
		t.Errorf("standing = %d, want it counted", view.Standing)
	}
}
