package usecase_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

const gosecSARIF = `{
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {"name": "gosec"}},
    "results": [
      {
        "ruleId": "G404",
        "level": "warning",
        "message": {"text": "Use of weak random number generator (math/rand instead of crypto/rand)"},
        "locations": [{"physicalLocation": {
          "artifactLocation": {"uri": "file:///repo/internal/api/handler.go"},
          "region": {"startLine": 42}
        }}]
      },
      {
        "ruleId": "G304",
        "level": "error",
        "message": {"text": "Potential file inclusion via variable"},
        "locations": [{"physicalLocation": {
          "artifactLocation": {"uri": "internal/store/read.go"},
          "region": {"startLine": 7}
        }}]
      }
    ]
  }]
}`

func TestSARIFIsReadIntoAttributedFindings(t *testing.T) {
	a := analyserNamed(t, "gosec")
	got := usecase.ReadFindings(a, gosecSARIF, "/repo")

	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2: %+v", len(got), got)
	}
	if got[0].Tool != "gosec" || got[0].Rule != "G404" {
		t.Errorf("first finding = %+v, want it attributed to gosec/G404", got[0])
	}
	// An absolute URI and a relative one must both land on the path git uses,
	// or the finding attaches to no file at all.
	if got[0].File != "internal/api/handler.go" {
		t.Errorf("file = %q, want it relative to the repository", got[0].File)
	}
	if got[1].File != "internal/store/read.go" {
		t.Errorf("file = %q", got[1].File)
	}
	if got[0].Line != 42 {
		t.Errorf("line = %d", got[0].Line)
	}
	if got[0].Severity != domain.SeverityMedium || got[1].Severity != domain.SeverityHigh {
		t.Errorf("severities = %q, %q; want the tool's own words mapped",
			got[0].Severity, got[1].Severity)
	}
}

func TestUnreadableOutputIsNoFindingsRatherThanAFailure(t *testing.T) {
	// A tool that printed a banner, or a usage message, or nothing at all, must
	// not take a review down (ADR 0043).
	a := analyserNamed(t, "gosec")
	for _, output := range []string{"", "gosec version 2.20.0", "{not json", "null"} {
		if got := usecase.ReadFindings(a, output, "/repo"); len(got) != 0 {
			t.Errorf("%q yielded %+v, want nothing", output, got)
		}
	}
}

func TestLineOutputIsReadIncludingTheRuleItNamed(t *testing.T) {
	// staticcheck and ruff put the check in a trailing parenthesis, and a
	// finding that cannot name its rule is not `reported` — it is a sentence.
	a := analyserNamed(t, "staticcheck")
	out := "internal/usecase/audit.go:88:2: this value of err is never used (SA4006)\n" +
		"# github.com/x/y\n" +
		"internal/api/handler.go:12: unreachable code\n"

	got := usecase.ReadFindings(a, out, "/repo")
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2: %+v", len(got), got)
	}
	if got[0].Rule != "SA4006" {
		t.Errorf("rule = %q, want it pulled out of the message", got[0].Rule)
	}
	if got[0].Message != "this value of err is never used" {
		t.Errorf("message = %q, want the rule removed from it", got[0].Message)
	}
	if got[0].Line != 88 || got[1].Line != 12 {
		t.Errorf("lines = %d, %d", got[0].Line, got[1].Line)
	}
	// Nothing said how bad it is, so nothing is claimed beyond "worth checking".
	if got[1].Severity != domain.SeverityMedium {
		t.Errorf("severity = %q", got[1].Severity)
	}
}

func TestAFindingWithNothingToSayIsDropped(t *testing.T) {
	a := analyserNamed(t, "staticcheck")
	if got := usecase.ReadFindings(a, "a.go:1:1:\n", "/repo"); len(got) != 0 {
		t.Errorf("an empty message is not a finding: %+v", got)
	}
}

func TestASARIFDocumentIsFoundAmongWhateverElseWasPrinted(t *testing.T) {
	// golangci-lint prints its own summary after the document; other tools
	// print a banner before it. Neither is a reason to lose the report.
	a := analyserNamed(t, "gosec")
	wrapped := "loading config…\n" + gosecSARIF + "\n14 issues:\n* errcheck: 4\n"

	got := usecase.ReadFindings(a, wrapped, "/repo")
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2", len(got))
	}
}
