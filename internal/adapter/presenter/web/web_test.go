package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
