package usecase_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func exported(id, path string, line int, rule string, sev contract.Severity) contract.Item {
	return contract.Item{
		ID:          id,
		Fingerprint: "fp-" + id,
		Source:      contract.SourceAnalyser,
		Producer:    "gosec",
		RuleID:      rule,
		Location:    contract.Location{Path: path, StartLine: line, EndLine: line},
		Severity:    sev,
		Title:       "gosec " + rule + " — weak random",
		Message:     "Use of weak random number generator.",
		Directive:   "use crypto/rand at " + path,
		Snippet:     "token := rand.Int()",
	}
}

func TestExportAgentIsFlatAndOrderedByFile(t *testing.T) {
	got := usecase.ExportAgent([]contract.Item{
		exported("b", "internal/b.go", 4, "G404", contract.SeverityLow),
		exported("a", "internal/a.go", 9, "G404", contract.SeverityHigh),
	})

	if strings.Index(got, "internal/a.go") > strings.Index(got, "internal/b.go") {
		t.Errorf("files are out of order:\n%s", got)
	}
	for _, want := range []string{"internal/a.go:9", "use crypto/rand", "token := rand.Int()", "`a`"} {
		if !strings.Contains(got, want) {
			t.Errorf("the agent export is missing %q:\n%s", want, got)
		}
	}
}

func TestExportAgentWithNothingOutstanding(t *testing.T) {
	if got := usecase.ExportAgent(nil); !strings.Contains(got, "Nothing outstanding") {
		t.Errorf("an empty export reads as %q", got)
	}
}

// Twelve instances of one rule are one piece of work, and that is what a
// planner should be handed.
func TestExportPlanClustersByRule(t *testing.T) {
	got := usecase.ExportPlan([]contract.Item{
		exported("a", "internal/a.go", 1, "G404", contract.SeverityHigh),
		exported("b", "internal/b.go", 2, "G404", contract.SeverityHigh),
		exported("c", "internal/c.go", 3, "G401", contract.SeverityLow),
	})

	if !strings.Contains(got, "## gosec:G404 — 2 item(s), high") {
		t.Errorf("no G404 cluster:\n%s", got)
	}
	if !strings.Contains(got, "Across 2 file(s)") {
		t.Errorf("the summary line does not say how far it spreads:\n%s", got)
	}
	if strings.Index(got, "gosec:G404") > strings.Index(got, "gosec:G401") {
		t.Errorf("the worse cluster is not first:\n%s", got)
	}
	// The members are named, so a planner can promote a cluster and still know
	// which findings it just took on.
	for _, id := range []string{"`a`", "`b`", "`c`"} {
		if !strings.Contains(got, id) {
			t.Errorf("cluster members do not include %s:\n%s", id, got)
		}
	}
}

// Anything without a rule clusters by where it is, which is the closest thing
// to a module msr can know without a language server.
func TestExportPlanClustersHumanNotesByDirectory(t *testing.T) {
	note := contract.Item{
		ID: "n1", Source: contract.SourceHuman, Producer: "human",
		Location: contract.Location{Path: "internal/api/handler.go"},
		Severity: contract.SeverityMedium, Title: "objection", Directive: "this swallows the error",
	}

	got := usecase.ExportPlan([]contract.Item{note})

	if !strings.Contains(got, "human in internal/api") {
		t.Errorf("no directory cluster:\n%s", got)
	}
}

func TestExportGitHubIssuesDedupesByFingerprint(t *testing.T) {
	onMain := exported("a", "internal/a.go", 1, "G404", contract.SeverityHigh)
	onBranch := onMain
	onBranch.ID = "b"
	onBranch.Branch = "feature/x"

	body, err := usecase.ExportGitHubIssues([]contract.Item{onMain, onBranch})
	if err != nil {
		t.Fatal(err)
	}

	var issues []struct {
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal(body, &issues); err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("one finding on two branches made %d issues", len(issues))
	}
	if !strings.Contains(issues[0].Body, "internal/a.go:1") {
		t.Errorf("the issue does not say where it is:\n%s", issues[0].Body)
	}
	if len(issues[0].Labels) != 3 {
		t.Errorf("labels = %v, want msr, the severity and the rule", issues[0].Labels)
	}
}

func TestExportItemsJSONLIsOneLinePerItem(t *testing.T) {
	body, err := usecase.ExportItemsJSONL([]contract.Item{exported("a", "a.go", 1, "G404", contract.SeverityLow), exported("b", "b.go", 1, "G404", contract.SeverityLow)})
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	var back contract.Item
	if err := json.Unmarshal([]byte(lines[0]), &back); err != nil {
		t.Fatal(err)
	}
	if back.ID != "a" {
		t.Errorf("the first line round-tripped to %q", back.ID)
	}
}

func TestExportItemsMarkdownSaysWhoSaidItAndWhatWasDecided(t *testing.T) {
	item := exported("a", "internal/a.go", 12, "G404", contract.SeverityHigh)
	item.Verdict = contract.VerdictDismissed

	got := usecase.ExportItemsMarkdown([]contract.Item{item})

	for _, want := range []string{"internal/a.go:12", "gosec G404", "dismissed", "high"} {
		if !strings.Contains(got, want) {
			t.Errorf("the markdown export is missing %q:\n%s", want, got)
		}
	}
}
