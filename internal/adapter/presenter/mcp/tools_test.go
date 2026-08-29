package mcp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/mcp"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// space is a workspace held in memory: the tools are about what msr knows, and
// where it is kept is not part of what is being tested.
type space struct {
	open   mcp.Review
	openTo error
	all    []mcp.Review
}

func (s space) Open() (mcp.Review, error)  { return s.open, s.openTo }
func (s space) All() ([]mcp.Review, error) { return s.all, nil }

func note(kind domain.NoteKind, file, text string) domain.Note {
	return domain.Note{
		ID: text, Kind: kind, File: file, Text: text,
		TS: time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC),
	}
}

// tool runs one of the msr tools by name and returns the text it produced.
func tool(t *testing.T, w mcp.Workspace, name string, args map[string]any) string {
	t.Helper()
	for _, candidate := range mcp.Tools(w) {
		if candidate.Name == name {
			text, err := candidate.Call(t.Context(), args)
			if err != nil {
				return "error: " + err.Error()
			}
			return text
		}
	}
	t.Fatalf("no tool named %q; have %v", name, names(mcp.Tools(w)))
	return ""
}

func names(tools []mcp.Tool) []string {
	var out []string
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

func TestFeedbackIsWhatAHumanWroteAndStillWants(t *testing.T) {
	// The whole point of the surface: an agent asking what to fix gets the
	// reviewer's outstanding asks, not their approvals and not the model's
	// guesses (ADR 0031).
	w := space{open: mcp.Review{
		ID: "abc123", Title: "add retries", Ref: "abc123",
		Notes: []domain.Note{
			note(domain.NoteObjection, "http.go", "this retries forever"),
			note(domain.NoteOK, "http.go", "reads fine"),
			note(domain.NoteQuestion, "main.go", "why the extra goroutine?"),
			note(domain.NoteNote, "main.go", "thinking aloud"),
		},
		Analyses: []domain.Analysis{{
			Kind: "security", At: time.Now(), Verdict: "one thing",
			Findings: []domain.Finding{{File: "http.go", Note: "token in a log line"}},
		}},
	}}

	got := tool(t, w, "review_feedback", nil)

	for _, want := range []string{"this retries forever", "why the extra goroutine?"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"reads fine", "thinking aloud", "token in a log line"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("should not carry %q:\n%s", unwanted, got)
		}
	}
}

func TestStatusSaysWhereTheReviewStandsWithoutSpendingTheContext(t *testing.T) {
	// The cheapest question an agent can ask, and the one it should ask first:
	// is anyone still looking at this, and is there anything for me.
	w := space{open: mcp.Review{
		ID: "abc123", Title: "add retries", Ref: "abc123", Repo: "mondspace-reviewer",
		Notes: []domain.Note{
			note(domain.NoteObjection, "http.go", "this retries forever"),
			note(domain.NoteOK, "http.go", "reads fine"),
		},
		Signoff: domain.Signoff{
			TargetID: "abc123", At: time.Now(), Comment: "good apart from the retry loop",
		},
		Analyses: []domain.Analysis{{
			Kind: "security", At: time.Now(), Verdict: "one thing worth a look",
			Findings: []domain.Finding{{File: "http.go", Note: "token in a log line"}},
		}},
	}}

	got := tool(t, w, "review_status", nil)

	for _, want := range []string{
		"add retries",                    // which review
		"1 ",                             // how much is outstanding
		"good apart from the retry loop", // the human's closing word
		"model_findings",                 // where the inferred material lives
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from:\n%s", want, got)
		}
	}
	if strings.Contains(got, "token in a log line") {
		t.Errorf("status should say findings exist, not spend context on them:\n%s", got)
	}
}

func TestAskingAboutOneFileGetsEverythingAHumanWroteThere(t *testing.T) {
	// Narrower than review_feedback and therefore more generous: someone who
	// names a file wants the whole human record of it, approvals included —
	// "I already looked at this and it was fine" is worth an agent knowing.
	superseded := note(domain.NoteQuestion, "http.go", "an earlier wording")
	superseded.SupersededBy = "later"

	w := space{open: mcp.Review{
		ID: "abc123", Title: "add retries",
		Notes: []domain.Note{
			note(domain.NoteOK, "http.go", "the backoff reads fine"),
			note(domain.NoteObjection, "http.go", "this retries forever"),
			superseded,
			note(domain.NoteQuestion, "main.go", "why the extra goroutine?"),
		},
		Analyses: []domain.Analysis{{
			Kind: "security", At: time.Now(), Verdict: "one thing",
			Findings: []domain.Finding{{File: "http.go", Note: "token in a log line"}},
		}},
	}}

	got := tool(t, w, "review_file", map[string]any{"path": "http.go"})

	for _, want := range []string{"the backoff reads fine", "this retries forever"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		"why the extra goroutine?", // another file
		"an earlier wording",       // replaced by a later note
		"token in a log line",      // the model, not a human
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("should not carry %q:\n%s", unwanted, got)
		}
	}
}

func TestAToolThatNeedsAPathSaysSoRatherThanAnsweringAboutNothing(t *testing.T) {
	w := space{open: mcp.Review{ID: "abc123"}}

	got := tool(t, w, "review_file", nil)

	if !strings.Contains(got, "path") {
		t.Errorf("want the missing argument named, got:\n%s", got)
	}
}

func findings(kind domain.AnalysisKind, model string, fs ...domain.Finding) domain.Analysis {
	return domain.Analysis{
		Kind: kind, At: time.Date(2026, 8, 29, 11, 0, 0, 0, time.UTC),
		Model: model, Verdict: "two things worth a look", Findings: fs,
	}
}

func TestModelFindingsArriveLabelledAsGuessesToCheck(t *testing.T) {
	// The judge is a small local model. Its output reaching an agent as though
	// it were a reviewer's instruction is exactly the loop ADR 0003 exists to
	// prevent, so the labelling is part of the payload, not just the docs.
	w := space{open: mcp.Review{
		ID: "abc123", Title: "add retries",
		Analyses: []domain.Analysis{findings("security", "qwen3.5-9b",
			domain.Finding{File: "http.go", Note: "token in a log line", Severity: domain.SeverityHigh},
		)},
	}}

	got := tool(t, w, "model_findings", nil)

	for _, want := range []string{
		"token in a log line", // the finding itself
		"security",            // which reading produced it
		"high",                // how much it claims to matter
		"qwen3.5-9b",          // and by what
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from:\n%s", want, got)
		}
	}
	if !strings.Contains(strings.ToLower(got), "verify") {
		t.Errorf("the payload itself must ask for verification:\n%s", got)
	}
	if !strings.Contains(strings.ToLower(got), "not written by a human") {
		t.Errorf("the payload itself must disown human authorship:\n%s", got)
	}
}

func TestAFindingAHumanDismissedIsNotHandedOverAsWork(t *testing.T) {
	// It is still shown — an agent that cannot see the dismissal will raise the
	// same thing again — but shown as settled, not as something to do.
	w := space{open: mcp.Review{
		ID: "abc123", Title: "add retries",
		Analyses: []domain.Analysis{findings("security", "qwen3.5-9b",
			domain.Finding{File: "http.go", Note: "token in a log line", Severity: domain.SeverityHigh},
			domain.Finding{File: "http.go", Note: "the sleep is unbounded",
				Severity: domain.SeverityLow, Verdict: domain.VerdictDismissed},
		)},
	}}

	got := tool(t, w, "model_findings", nil)

	if !strings.Contains(got, "the sleep is unbounded") {
		t.Errorf("a dismissal is worth knowing about:\n%s", got)
	}
	settled := strings.Index(got, "the sleep is unbounded")
	if !strings.Contains(got[max(0, settled-200):settled+80], "dismissed") {
		t.Errorf("want it marked dismissed:\n%s", got)
	}
}

func TestModelFindingsNarrowToAFileWhenAsked(t *testing.T) {
	w := space{open: mcp.Review{
		ID: "abc123",
		Analyses: []domain.Analysis{findings("security", "qwen3.5-9b",
			domain.Finding{File: "http.go", Note: "token in a log line"},
			domain.Finding{File: "main.go", Note: "flag parsed twice"},
		)},
	}}

	got := tool(t, w, "model_findings", map[string]any{"path": "http.go"})

	if !strings.Contains(got, "token in a log line") || strings.Contains(got, "flag parsed twice") {
		t.Errorf("want only http.go:\n%s", got)
	}
}
