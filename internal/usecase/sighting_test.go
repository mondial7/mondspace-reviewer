package usecase_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

const body = `package p

func f() {
	token := rand.Int()
	return token
}
`

func sighter(t *testing.T) usecase.Sighting {
	t.Helper()
	n := 0
	return usecase.Sighting{
		Branch:    "main",
		SessionID: "sess-1",
		At:        time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC),
		Mint: func() string {
			n++
			return "id-" + string(rune('0'+n))
		},
		Body: func(path string) string {
			if path == "internal/p/p.go" {
				return body
			}
			return ""
		},
	}
}

func TestSeen(t *testing.T) {
	found := []contract.Item{contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "./internal/p/p.go", StartLine: 4, EndLine: 4}, Producer: "gosec", RuleID: "G404", Message: "Use of weak random number generator. Prefer crypto/rand.", Severity: domain.SeverityHigh, New: true}}

	got := sighter(t).Seen(found)

	if len(got) != 1 {
		t.Fatalf("converted %d findings, want 1", len(got))
	}
	item := got[0]
	if item.Location.Path != "internal/p/p.go" {
		t.Errorf("path = %q, want it normalised", item.Location.Path)
	}
	if item.Source != contract.SourceAnalyser || item.Producer != "gosec" || item.RuleID != "G404" {
		t.Errorf("provenance = %q/%q/%q", item.Source, item.Producer, item.RuleID)
	}
	if item.Fingerprint == "" {
		t.Error("no fingerprint")
	}
	// A finding an agent cannot act on is not a factory artifact.
	if item.Directive == "" {
		t.Error("no directive")
	}
	if !strings.Contains(item.Snippet, "rand.Int()") {
		t.Errorf("snippet = %q, want the line it is about", item.Snippet)
	}
	if !item.New || item.Severity != contract.SeverityHigh {
		t.Errorf("new = %v, severity = %q", item.New, item.Severity)
	}
	if item.Title != "gosec/G404 — Use of weak random number generator" {
		t.Errorf("title = %q", item.Title)
	}
}

// The same finding on the same code fingerprints the same on the next run, and
// a finding of a different rule on the same line does not.
func TestSeenFingerprintsByRuleAndPlace(t *testing.T) {
	one := contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "internal/p/p.go", StartLine: 4, EndLine: 4}, Producer: "gosec", RuleID: "G404"}
	two := contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "internal/p/p.go", StartLine: 4, EndLine: 4}, Producer: "gosec", RuleID: "G401"}

	got := sighter(t).Seen([]contract.Item{one, two})
	again := sighter(t).Seen([]contract.Item{one})

	if got[0].Fingerprint != again[0].Fingerprint {
		t.Error("the same finding fingerprinted differently on a second pass")
	}
	if got[0].Fingerprint == got[1].Fingerprint {
		t.Error("two rules on one line share a fingerprint")
	}
	if got[0].ID == got[1].ID {
		t.Error("two findings share an id")
	}
}

// A file that has gone since the tool ran is ordinary. The finding is still
// worth recording; it simply has no window and no snippet.
func TestSeenSurvivesAnUnreadableFile(t *testing.T) {
	got := sighter(t).Seen([]contract.Item{contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "gone.go", StartLine: 4, EndLine: 4}, Producer: "gosec", RuleID: "G404", Message: "x"}})

	if len(got) != 1 {
		t.Fatalf("converted %d findings, want 1", len(got))
	}
	if got[0].Fingerprint == "" {
		t.Error("no fingerprint for a finding whose file could not be read")
	}
	if got[0].Snippet != "" {
		t.Errorf("snippet = %q, want none", got[0].Snippet)
	}
}

func TestFromNote(t *testing.T) {
	note := domain.Note{
		ID: "note-1", SessionID: "sess-1", UnitID: "unit-3",
		Kind: domain.NoteObjection, Text: "this swallows the error",
		File: "internal/p/p.go", Anchor: "	token := rand.Int()", AnchorNth: 1,
		TS: time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC),
	}

	got := sighter(t).FromNote(note)

	if got.ID != "note-1" {
		t.Errorf("id = %q, want the note's own", got.ID)
	}
	if got.Source != contract.SourceHuman {
		t.Errorf("source = %q", got.Source)
	}
	if got.Severity != contract.SeverityHigh {
		t.Errorf("an objection came out as %q, want high", got.Severity)
	}
	if got.UnitID != "unit-3" || got.Anchor != note.Anchor || got.AnchorNth != 1 {
		t.Error("the note lost its anchoring on the way to an item")
	}
	if got.Directive != note.Text {
		t.Errorf("directive = %q, want what the reviewer wrote", got.Directive)
	}
}

// Two notes on one file are two items, even when neither is anchored to a line.
func TestFromNoteSeparatesUnanchoredNotes(t *testing.T) {
	s := sighter(t)
	first := s.FromNote(domain.Note{ID: "note-1", Kind: domain.NoteDebt, Text: "a", File: "internal/p/p.go"})
	second := s.FromNote(domain.Note{ID: "note-2", Kind: domain.NoteDebt, Text: "b", File: "internal/p/p.go"})

	if first.Fingerprint == second.Fingerprint {
		t.Error("two file-wide notes share a fingerprint")
	}
}

// A whole-project source has to be cut down to the change, or a server holding
// two years of issues drowns a two-file review.
func TestOnlyIn(t *testing.T) {
	found := []contract.Item{
		contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "./internal/p/p.go", StartLine: 4, EndLine: 4}, Producer: "sonar"},
		contract.Item{Source: contract.SourceAnalyser, Location: contract.Location{Path: "internal/elsewhere.go", StartLine: 9, EndLine: 9}, Producer: "sonar"},
	}

	got := usecase.OnlyIn(found, map[string]bool{"internal/p/p.go": true})

	if len(got) != 1 || got[0].Location.Path != "./internal/p/p.go" {
		t.Errorf("OnlyIn = %+v, want the finding in the changed file", got)
	}
}

func TestFromAnalysis(t *testing.T) {
	analysis := domain.Analysis{
		Kind:  domain.AnalysisKind("security"),
		Model: "qwen3-4b",
		Findings: []domain.Finding{
			{File: "internal/p/p.go", Note: "The token is generated with math/rand.", Severity: domain.SeverityHigh},
			{File: "internal/p/p.go", Note: "The error is discarded.", Severity: domain.SeverityMedium},
		},
	}

	got := sighter(t).FromAnalysis(analysis)

	if len(got) != 2 {
		t.Fatalf("converted %d findings, want 2", len(got))
	}
	if got[0].Source != contract.SourceLLM || got[0].Producer != "qwen3-4b" {
		t.Errorf("provenance = %q/%q", got[0].Source, got[0].Producer)
	}
	// Two sentences about one file are two findings, not one.
	if got[0].Fingerprint == got[1].Fingerprint {
		t.Error("two findings in one file share a fingerprint")
	}
	if got[0].Directive == "" {
		t.Error("a model's finding arrived with nothing to do about it")
	}
}

// The same objection, rephrased in whitespace or capitals, is the same
// objection.
func TestFromAnalysisIsStableAcrossWording(t *testing.T) {
	one := domain.Analysis{Kind: "security", Findings: []domain.Finding{{File: "a.go", Note: "The token is weak."}}}
	two := domain.Analysis{Kind: "security", Findings: []domain.Finding{{File: "a.go", Note: "the   token  is weak."}}}

	first := sighter(t).FromAnalysis(one)
	second := sighter(t).FromAnalysis(two)

	if first[0].Fingerprint != second[0].Fingerprint {
		t.Error("the same sentence, spaced differently, became a different finding")
	}
}
