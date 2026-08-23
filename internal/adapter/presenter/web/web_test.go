package web_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/web"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

func testSession() web.Session {
	return web.Session{
		ID:     "s",
		Prompt: "add token validation",
		Units: []domain.Unit{
			{ID: "s-f001", SessionID: "s", Files: []string{"auth/token.go"},
				Flags:    []domain.Flag{domain.FlagNoTest},
				Headline: domain.Headline{Text: "edited token.go", WhySrc: domain.WhyInferred}},
			{ID: "s-f002", SessionID: "s", Files: []string{"http/middleware.go"},
				Headline: domain.Headline{Text: "added middleware.go", Why: "guard routes", WhySrc: domain.WhyStated}},
		},
		Diffs: map[string]domain.Diff{
			"s-f001": {Text: "@@ -1 +1 @@\n-old body\n+new body\n+extra\n"},
			"s-f002": {Text: "@@ -0,0 +1 @@\n+package http\n"},
		},
	}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// recordingNotes captures persisted annotations.
type recordingNotes struct{ notes []domain.Note }

func (r *recordingNotes) AppendNote(n domain.Note) error {
	r.notes = append(r.notes, n)
	return nil
}

func TestAnnotatePersistsNoteAndShowsIt(t *testing.T) {
	store := &recordingNotes{}
	h := web.NewServer(testSession(), store)

	form := strings.NewReader("kind=objection&text=wrong+layer")
	req := httptest.NewRequest(http.MethodPost, "/units/s-f001/notes", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 200 or 303", rec.Code)
	}
	if len(store.notes) != 1 {
		t.Fatalf("got %d persisted notes, want 1", len(store.notes))
	}
	n := store.notes[0]
	if n.Kind != domain.NoteObjection || n.UnitID != "s-f001" || n.Text != "wrong layer" {
		t.Errorf("note = %+v, want objection 'wrong layer' on s-f001", n)
	}
	if n.SessionID != "s" || n.ID == "" || n.TS.IsZero() {
		t.Errorf("note should carry session, id and timestamp: %+v", n)
	}

	// The annotation is visible on the page afterwards.
	if body := get(t, h, "/").Body.String(); !strings.Contains(body, "wrong layer") {
		t.Errorf("index should show the new note:\n%s", body)
	}
}

func TestAnnotateRejectsUnknownUnitAndKind(t *testing.T) {
	store := &recordingNotes{}
	h := web.NewServer(testSession(), store)

	post := func(path, body string) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post("/units/nope/notes", "kind=ok"); code != http.StatusNotFound {
		t.Errorf("unknown unit: status = %d, want 404", code)
	}
	if code := post("/units/s-f001/notes", "kind=bogus"); code != http.StatusBadRequest {
		t.Errorf("unknown kind: status = %d, want 400", code)
	}
	if len(store.notes) != 0 {
		t.Errorf("nothing should be persisted for invalid input, got %+v", store.notes)
	}
}

func TestAskKeepsConversationHistory(t *testing.T) {
	var asked []string
	h := web.NewServer(testSession(), nil).WithAsk(
		func(_ context.Context, question string, history []web.Exchange) (string, error) {
			asked = append(asked, question)
			// The assistant sees what was already discussed (issue #12).
			return fmt.Sprintf("answer %d (history %d)", len(asked), len(history)), nil
		})

	ask := func(q string) string {
		req := httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader("question="+q))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK && rec.Code != http.StatusSeeOther {
			t.Fatalf("ask %q: status = %d", q, rec.Code)
		}
		return rec.Body.String()
	}

	ask("what+changed")
	ask("and+why")

	if len(asked) != 2 {
		t.Fatalf("summarizer called %d times, want 2", len(asked))
	}
	// The second question carried the first exchange as context.
	body := get(t, h, "/").Body.String()
	for _, want := range []string{"what changed", "and why", "answer 1", "answer 2"} {
		if !strings.Contains(body, want) {
			t.Errorf("conversation should persist on the page, missing %q", want)
		}
	}
}

func TestAskSurfacesErrorWithoutCrashing(t *testing.T) {
	h := web.NewServer(testSession(), nil).WithAsk(
		func(context.Context, string, []web.Exchange) (string, error) {
			return "", errors.New("summarizer offline")
		})

	req := httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader("question=hi"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code >= 500 {
		t.Errorf("an offline summarizer must not 500: status = %d", rec.Code)
	}
	if body := get(t, h, "/").Body.String(); !strings.Contains(body, "offline") {
		t.Errorf("the offline notice should be shown to the reviewer:\n%s", body)
	}
}

func TestWorkspaceListsSessionsAcrossReposAndAgents(t *testing.T) {
	sessions := []web.SessionSummary{
		{ID: "s1", Repo: "mondspace-reviewer", Agent: "claude-code", Prompt: "add token validation",
			Files: 12, Flags: 3, Open: 1, Started: time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)},
		{ID: "s2", Repo: "other-project", Agent: "opencode", Prompt: "port the parser",
			Files: 4, Flags: 0, Open: 0, Started: time.Date(2026, 8, 23, 11, 30, 0, 0, time.UTC)},
	}
	h := web.NewServer(testSession(), nil).WithWorkspace(sessions)

	rec := get(t, h, "/sessions")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"s1", "s2",
		"mondspace-reviewer", "other-project", // repos
		"claude-code", "opencode", // agents
		"add token validation", "port the parser", // the storyline across sessions
	} {
		if !strings.Contains(body, want) {
			t.Errorf("workspace missing %q", want)
		}
	}
	// Each row links into its own review.
	if !strings.Contains(body, `href="/?session=s1"`) {
		t.Errorf("workspace rows should link to the session review:\n%s", body)
	}
}

func TestReviewContentIsInTheDOMNotOnlyTheCanvas(t *testing.T) {
	h := web.NewServer(testSession(), nil)
	body := get(t, h, "/").Body.String()

	// The cinematic scene reads from the DOM; nothing may be canvas-only, so the
	// review stays usable, selectable and searchable without WebGL (ADR 0012).
	for _, want := range []string{"auth/token.go", "edited token.go", "no-test"} {
		if !strings.Contains(body, want) {
			t.Errorf("review content %q must be server-rendered, not canvas-only", want)
		}
	}
	// Focus mode is reachable without JavaScript deciding it for us.
	if !strings.Contains(body, "focus") {
		t.Errorf("page should expose focus mode:\n%s", body)
	}
	// The scene is progressive enhancement: a vendored module, not a CDN.
	if strings.Contains(body, "//unpkg.com") || strings.Contains(body, "//cdn.") {
		t.Errorf("assets must be served locally, not from a CDN")
	}
}

func TestVendoredThreeIsServedLocally(t *testing.T) {
	h := web.NewServer(testSession(), nil)

	rec := get(t, h, "/assets/vendor/three.module.min.js")

	if rec.Code != http.StatusOK {
		t.Fatalf("three.js should be served locally: status = %d", rec.Code)
	}
	if rec.Body.Len() < 100_000 {
		t.Errorf("three.js body looks truncated: %d bytes", rec.Body.Len())
	}
}

func TestIndexListsUnits(t *testing.T) {
	h := web.NewServer(testSession(), nil)

	rec := get(t, h, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"auth/token.go",   // the file is the anchor
		"edited token.go", // the storyline
		"http/middleware.go",
		"no-test",              // flags surface
		"add token validation", // the task prompt gives context
		"+2",                   // net change stats
		"-1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
	// stated vs inferred must be distinguishable in the markup, not just by colour.
	if !strings.Contains(body, "stated") || !strings.Contains(body, "inferred") {
		t.Errorf("index should label rationale source:\n%s", body)
	}
}
