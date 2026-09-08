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

func TestFromReported(t *testing.T) {
	found := []domain.Reported{{
		Tool: "gosec", Rule: "G404", File: "./internal/p/p.go", Line: 4,
		Message:  "Use of weak random number generator. Prefer crypto/rand.",
		Severity: domain.SeverityHigh, New: true,
	}}

	got := sighter(t).FromReported(found)

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
	if item.Title != "gosec G404 — Use of weak random number generator" {
		t.Errorf("title = %q", item.Title)
	}
}

// The same finding on the same code fingerprints the same on the next run, and
// a finding of a different rule on the same line does not.
func TestFromReportedFingerprintsByRuleAndPlace(t *testing.T) {
	one := domain.Reported{Tool: "gosec", Rule: "G404", File: "internal/p/p.go", Line: 4}
	two := domain.Reported{Tool: "gosec", Rule: "G401", File: "internal/p/p.go", Line: 4}

	got := sighter(t).FromReported([]domain.Reported{one, two})
	again := sighter(t).FromReported([]domain.Reported{one})

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
func TestFromReportedSurvivesAnUnreadableFile(t *testing.T) {
	got := sighter(t).FromReported([]domain.Reported{{
		Tool: "gosec", Rule: "G404", File: "gone.go", Line: 4, Message: "x",
	}})

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
