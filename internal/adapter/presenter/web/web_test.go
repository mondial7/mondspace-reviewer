package web_test

import (
	"bufio"
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

func TestStoryPageReadsAsChaptersWithProse(t *testing.T) {
	narrative := domain.Narrative{
		SessionID: "s",
		Title:     "Locking down authentication",
		Intro:     "The session added token validation and wired it into the request path.",
		Source:    domain.NarrativeModel,
		Chapters: []domain.Chapter{
			{Title: "Token validation", Prose: "A TokenValidator interface was extracted and tested.",
				UnitIDs: []string{"s-f001"}},
			{Title: "Request path", Prose: "Middleware now guards every route.",
				UnitIDs: []string{"s-f002"}},
		},
	}
	h := web.NewServer(testSession(), nil).WithNarrative(narrative)

	rec := get(t, h, "/story")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Locking down authentication",      // the title
		"wired it into the request path",   // the intro
		"Token validation", "Request path", // chapter titles
		"A TokenValidator interface was extracted", // the prose
		"auth/token.go", // real files beside the prose
		"+2",            // real stats, from git not the model
	} {
		if !strings.Contains(body, want) {
			t.Errorf("story missing %q", want)
		}
	}
	// Model-written prose must be labelled as inferred (ADR 0003 / 0013).
	if !strings.Contains(body, "inferred") {
		t.Errorf("model-written narrative must be labelled inferred:\n%s", body)
	}
	// Each chapter links into the real review of its units.
	if !strings.Contains(body, `href="/#unit-s-f001"`) {
		t.Errorf("chapters should link into the diff review:\n%s", body)
	}
	// Focus mode is available here too.
	if !strings.Contains(body, "focus-toggle") {
		t.Errorf("story page should offer focus mode:\n%s", body)
	}
}

func TestStoryPageLabelsMechanicalFallback(t *testing.T) {
	narrative := domain.Narrative{
		SessionID: "s", Title: "Session s", Intro: "2 files changed.",
		Source:   domain.NarrativeMechanical,
		Chapters: []domain.Chapter{{Title: "auth", Prose: "1 file changed under auth.", UnitIDs: []string{"s-f001"}}},
	}
	h := web.NewServer(testSession(), nil).WithNarrative(narrative)

	body := get(t, h, "/story").Body.String()

	// The reviewer must be able to tell a model story from a mechanical one.
	if !strings.Contains(body, "mechanical") {
		t.Errorf("a fallback narrative should say so:\n%s", body)
	}
}

func TestReanalyseReplacesHeadlineAndRecordsModel(t *testing.T) {
	var called []string
	h := web.NewServer(testSession(), nil).WithReanalyse(
		func(_ context.Context, u domain.Unit) (domain.Headline, string, error) {
			called = append(called, u.ID)
			return domain.Headline{Text: "a sharper summary", WhySrc: domain.WhyInferred}, "qwen/qwen3.5-9b", nil
		})

	req := httptest.NewRequest(http.MethodPost, "/units/s-f001/reanalyse", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(called) != 1 || called[0] != "s-f001" {
		t.Fatalf("re-analysed %v, want just s-f001", called)
	}

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "a sharper summary") {
		t.Errorf("the re-analysed headline should replace the old one:\n%s", body)
	}
	// Which model produced a headline must be visible — attribution matters when
	// re-running with a better model (issue #10).
	if !strings.Contains(body, "qwen/qwen3.5-9b") {
		t.Errorf("the producing model should be attributed:\n%s", body)
	}
	// Only the requested unit changes.
	if strings.Contains(body, "a sharper summary</span> <span") {
		t.Errorf("re-analysis leaked to other units")
	}
}

func TestAuditLogRecordsInteractions(t *testing.T) {
	audit := &recordingAudit{}
	h := web.NewServer(testSession(), nil).WithAudit(audit).WithAsk(
		func(context.Context, string, []web.Exchange) (string, error) { return "an answer", nil })

	post := func(path, body string) {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	post("/units/s-f001/notes", "kind=objection&text=wrong")
	post("/ask", "question=why")

	if len(audit.entries) != 2 {
		t.Fatalf("audit recorded %d entries, want 2 (annotate + ask)", len(audit.entries))
	}
	if audit.entries[0].Action != "annotate" || audit.entries[0].UnitID != "s-f001" {
		t.Errorf("first entry = %+v, want an annotate on s-f001", audit.entries[0])
	}
	if audit.entries[1].Action != "ask" {
		t.Errorf("second entry = %+v, want an ask", audit.entries[1])
	}
	for _, e := range audit.entries {
		if e.TS.IsZero() || e.SessionID == "" {
			t.Errorf("every audit entry needs a session and a timestamp: %+v", e)
		}
	}
}

type recordingAudit struct{ entries []web.AuditEntry }

func (a *recordingAudit) Append(e web.AuditEntry) error {
	a.entries = append(a.entries, e)
	return nil
}

// readableAudit can also be read back, so it can drive the activity page.
type readableAudit struct{ recordingAudit }

func (a *readableAudit) Entries() ([]web.AuditEntry, error) { return a.entries, nil }

func TestActivityPageShowsWhatWasRecordedNewestFirst(t *testing.T) {
	audit := &readableAudit{}
	audit.entries = []web.AuditEntry{
		{TS: time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC), SessionID: "s", UnitID: "s-f001",
			Action: "annotate", Detail: "objection: wrong"},
		{TS: time.Date(2026, 8, 23, 9, 5, 0, 0, time.UTC), SessionID: "s",
			Action: "narrate", Detail: "3 chapters", Model: "qwen/qwen3.5-9b", Millis: 31000},
	}
	h := web.NewServer(testSession(), nil).WithAudit(audit)

	rec := get(t, h, "/activity")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /activity = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{"narrate", "annotate", "qwen/qwen3.5-9b", "objection: wrong"} {
		if !strings.Contains(body, want) {
			t.Errorf("activity page is missing %q", want)
		}
	}
	// Newest first: the reviewer wants what just happened, not the beginning.
	if strings.Index(body, "narrate") > strings.Index(body, "annotate") {
		t.Errorf("entries should be newest first")
	}
	// A model call reports what it cost, which is the point of the page.
	if !strings.Contains(body, "31.0s") && !strings.Contains(body, "31s") {
		t.Errorf("a model call should show how long it took: %s", body)
	}
}

func TestActivityPageExplainsItselfWhenNothingIsRecorded(t *testing.T) {
	// No audit log wired at all: the page must still render rather than 404,
	// because a link to it is always in the nav.
	rec := get(t, web.NewServer(testSession(), nil), "/activity")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /activity = %d, want 200 even with no audit log", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Nothing recorded yet") {
		t.Errorf("an empty activity page should say so: %s", rec.Body.String())
	}
}

func TestRecordMakesAModelCallVisibleAndLive(t *testing.T) {
	audit := &readableAudit{}
	h := web.NewServer(testSession(), nil).WithAudit(audit)

	h.Record(web.AuditEntry{SessionID: "s", Action: "narrate",
		Detail: "2 chapters", Model: "qwen/qwen3.5-9b", Millis: 1200})

	if len(audit.entries) != 1 {
		t.Fatalf("recorded %d entries, want 1", len(audit.entries))
	}
	if got := audit.entries[0]; got.Action != "narrate" || got.Model != "qwen/qwen3.5-9b" || got.TS.IsZero() {
		t.Errorf("entry = %+v, want a timestamped narrate entry naming the model", got)
	}
	if !strings.Contains(get(t, h, "/activity").Body.String(), "2 chapters") {
		t.Error("a recorded call should appear on the activity page")
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

func TestSSEStreamsUpdatesAndSetsHeaders(t *testing.T) {
	h := web.NewServer(testSession(), nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("subscribing: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, "no-cache") {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}

	// A change on the server reaches the subscriber without polling.
	go func() {
		time.Sleep(50 * time.Millisecond)
		h.SetNarrative(domain.Narrative{SessionID: "s", Title: "new story"})
	}()

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "event: ") {
			if got := strings.TrimPrefix(sc.Text(), "event: "); got != "narrative" {
				t.Errorf("event = %q, want narrative", got)
			}
			return
		}
	}
	t.Fatal("no event received before the stream ended")
}

func TestSSEStopsWhenTheClientGoesAway(t *testing.T) {
	h := web.NewServer(testSession(), nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("subscribing: %v", err)
	}
	if n := h.Subscribers(); n != 1 {
		t.Fatalf("subscribers = %d, want 1", n)
	}

	cancel()
	resp.Body.Close()

	// The server must drop the subscriber rather than leak the goroutine.
	deadline := time.Now().Add(5 * time.Second)
	for h.Subscribers() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("subscriber still registered after disconnect: %d", h.Subscribers())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
