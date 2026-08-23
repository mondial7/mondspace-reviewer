package usecase_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestExportJSONMarshalsReport(t *testing.T) {
	sess := reportSession()
	sess.Notes = append(sess.Notes,
		domain.Note{ID: "n4", UnitID: "s-u003", Kind: domain.NoteDebt, Text: "add a test"},
	)

	data, err := usecase.ExportJSON(usecase.BuildReport(sess))
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	var got domain.Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if got.SessionID != "s" || got.Prompt != "add token validation" {
		t.Errorf("session/prompt not marshalled: %+v", got)
	}
	if len(got.Groups) == 0 || len(got.Debt) != 1 {
		t.Errorf("groups/debt not marshalled: %+v", got)
	}
	if got.Debt[0].NoteText != "add a test" {
		t.Errorf("debt note text lost: %+v", got.Debt)
	}
}

func TestExportMarkdownOpenAgenda(t *testing.T) {
	sess := reportSession()
	sess.Notes = append(sess.Notes,
		domain.Note{ID: "n6", UnitID: "s-u001", Kind: domain.NoteQuestion, Text: "why an interface?"},
	)

	md := usecase.ExportMarkdown(usecase.BuildReport(sess))

	if !strings.Contains(md, "## Open Agenda") {
		t.Errorf("missing Open Agenda heading:\n%s", md)
	}
	// Objection on s-u002 and question on s-u001, phrased as directives.
	if !strings.Contains(md, "Address") || !strings.Contains(md, "wrong layer") {
		t.Errorf("objection not phrased as a directive:\n%s", md)
	}
	if !strings.Contains(md, "Answer") || !strings.Contains(md, "why an interface?") {
		t.Errorf("question not phrased as a directive:\n%s", md)
	}
}

func TestExportMarkdownPreservesWhySource(t *testing.T) {
	md := usecase.ExportMarkdown(usecase.BuildReport(reportSession()))

	// s-u001 stated its intent; s-u002 did not.
	if !strings.Contains(md, "stated: swap lib") {
		t.Errorf("stated rationale not preserved:\n%s", md)
	}
	// The inferred unit must never be shown as stated.
	lines := strings.Split(md, "\n")
	for _, l := range lines {
		if strings.Contains(l, "s-u002") && strings.Contains(l, "stated:") {
			t.Errorf("inferred unit shown as stated: %q", l)
		}
	}
}

func TestExportMarkdownSupersededAndUnreviewed(t *testing.T) {
	sess := reportSession()
	sess.Units = append(sess.Units,
		domain.Unit{ID: "s-u004", Files: []string{"auth/token.go"}},
		domain.Unit{ID: "s-u009", Headline: domain.Headline{Text: "untouched"}},
	)
	sess.Notes = append(sess.Notes,
		domain.Note{ID: "n7", UnitID: "s-u001", Kind: domain.NoteObjection, Text: "bad choice"},
	)

	md := usecase.ExportMarkdown(usecase.BuildReport(sess))

	if !strings.Contains(md, "## Superseded") || !strings.Contains(md, "superseded by s-u004") {
		t.Errorf("superseded section missing or unmarked:\n%s", md)
	}
	if !strings.Contains(md, "## Unreviewed") || !strings.Contains(md, "s-u009") {
		t.Errorf("unreviewed section missing:\n%s", md)
	}
}

func TestExportMarkdownDebtTaskList(t *testing.T) {
	sess := reportSession()
	sess.Notes = append(sess.Notes,
		domain.Note{ID: "n4", UnitID: "s-u003", Kind: domain.NoteDebt, Text: "add a test for the retry"},
	)

	md := usecase.ExportMarkdown(usecase.BuildReport(sess))

	if !strings.Contains(md, "## Debt") {
		t.Errorf("missing Debt heading:\n%s", md)
	}
	if !strings.Contains(md, "- [ ] ") || !strings.Contains(md, "add a test for the retry") {
		t.Errorf("debt should be a checkbox task list:\n%s", md)
	}
}

func TestExportSlackHeadlineCounts(t *testing.T) {
	sess := reportSession()
	for i := range sess.Units {
		if sess.Units[i].ID == "s-u002" {
			sess.Units[i].Flags = []domain.Flag{domain.FlagNoTest}
		}
	}
	sess.Notes = append(sess.Notes,
		domain.Note{ID: "n6", UnitID: "s-u001", Kind: domain.NoteQuestion, Text: "why an interface?"},
		domain.Note{ID: "n7", UnitID: "s-u003", Kind: domain.NoteDebt, Text: "add a test"},
	)

	msg := usecase.ExportSlack(usecase.BuildReport(sess))

	// 3 units reviewed (all annotated), 1 flagged (s-u002), 1 question + 1
	// objection open (s-u001, s-u002), 1 debt item (s-u003).
	for _, want := range []string{"3", "reviewed", "1 flagged", "1 question", "1 objection", "1 debt"} {
		if !strings.Contains(msg, want) {
			t.Errorf("headline missing %q:\n%s", want, msg)
		}
	}
	if !strings.Contains(msg, sess.ID) {
		t.Errorf("headline missing session id:\n%s", msg)
	}
	// First line is the headline; must be plain mrkdwn, not a markdown heading.
	firstLine := strings.SplitN(msg, "\n", 2)[0]
	if strings.HasPrefix(firstLine, "#") {
		t.Errorf("headline must not be a markdown heading: %q", firstLine)
	}
}

func TestExportSlackListsFlaggedItems(t *testing.T) {
	sess := reportSession()
	for i := range sess.Units {
		if sess.Units[i].ID == "s-u002" {
			sess.Units[i].Flags = []domain.Flag{domain.FlagNoTest}
		}
	}

	msg := usecase.ExportSlack(usecase.BuildReport(sess))

	if !strings.Contains(msg, "*Flagged*") {
		t.Errorf("missing Flagged section:\n%s", msg)
	}
	if !strings.Contains(msg, "• s-u002") || !strings.Contains(msg, "no-test") {
		t.Errorf("flagged item not bulleted with its flag:\n%s", msg)
	}
	// A unit with no flags must not show up in the Flagged section.
	lines := strings.Split(msg, "\n")
	inFlagged := false
	for _, l := range lines {
		if strings.HasPrefix(l, "*") {
			inFlagged = strings.Contains(l, "*Flagged*")
			continue
		}
		if inFlagged && strings.Contains(l, "s-u001") {
			t.Errorf("unflagged unit leaked into Flagged section: %q", l)
		}
	}
}

func TestExportSlackOmitsFlaggedSectionWhenNoneFlagged(t *testing.T) {
	msg := usecase.ExportSlack(usecase.BuildReport(reportSession()))
	if strings.Contains(msg, "*Flagged*") {
		t.Errorf("Flagged section should be absent with no flagged units:\n%s", msg)
	}
}

func TestExportSlackOpenAgendaAsDirectives(t *testing.T) {
	sess := reportSession()
	sess.Notes = append(sess.Notes,
		domain.Note{ID: "n6", UnitID: "s-u001", Kind: domain.NoteQuestion, Text: "why an interface?"},
	)

	msg := usecase.ExportSlack(usecase.BuildReport(sess))

	if !strings.Contains(msg, "*Open agenda*") {
		t.Errorf("missing Open agenda section:\n%s", msg)
	}
	if !strings.Contains(msg, "Address") || !strings.Contains(msg, "wrong layer") {
		t.Errorf("objection not phrased as a directive:\n%s", msg)
	}
	if !strings.Contains(msg, "Answer") || !strings.Contains(msg, "why an interface?") {
		t.Errorf("question not phrased as a directive:\n%s", msg)
	}
}

func TestExportSlackTruncatesLongListsWithCount(t *testing.T) {
	sess := domain.Session{ID: "big"}
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("big-u%03d", i)
		sess.Units = append(sess.Units, domain.Unit{
			ID: id, Files: []string{fmt.Sprintf("f%d.go", i)},
			Headline: domain.Headline{Text: "change"},
			Flags:    []domain.Flag{domain.FlagLarge},
		})
		sess.Notes = append(sess.Notes, domain.Note{ID: "n" + id, UnitID: id, Kind: domain.NoteObjection, Text: "objection"})
	}

	msg := usecase.ExportSlack(usecase.BuildReport(sess))

	flaggedBullets := strings.Count(msg[:strings.Index(msg, "*Open agenda*")], "• ")
	if flaggedBullets != 5 {
		t.Errorf("flagged bullets = %d, want 5 (capped)", flaggedBullets)
	}
	if !strings.Contains(msg, "…and 3 more") {
		t.Errorf("truncation must not be silent, want an explicit count:\n%s", msg)
	}
}

func TestExportSlackCapsMessageLength(t *testing.T) {
	sess := domain.Session{ID: "huge"}
	longText := strings.Repeat("a very long objection note that pads the message out ", 30)
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("huge-u%03d", i)
		sess.Units = append(sess.Units, domain.Unit{ID: id, Files: []string{fmt.Sprintf("f%d.go", i)}})
		sess.Notes = append(sess.Notes, domain.Note{ID: "n" + id, UnitID: id, Kind: domain.NoteObjection, Text: longText})
	}

	msg := usecase.ExportSlack(usecase.BuildReport(sess))

	if len(msg) > 3000 {
		t.Errorf("message length = %d, want <= 3000", len(msg))
	}
	if !strings.Contains(msg, "truncated") {
		t.Errorf("a capped message must say so, not truncate silently:\n%s", msg[len(msg)-200:])
	}
}

func TestExportMarkdownReviewReport(t *testing.T) {
	r := usecase.BuildReport(reportSession())

	md := usecase.ExportMarkdown(r)

	for _, want := range []string{
		"# Review",             // title
		"add token validation", // task prompt
		"## Review Report",
		"### ok",
		"s-u001",
		"extracted validator",
		"### objection",
		"s-u002",
		"wrong layer", // the note text
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
}
