package web_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/web"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
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

func TestCockpitReadsAsChaptersWithProse(t *testing.T) {
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

	rec := get(t, h, "/")

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
	// Each chapter is anchored to the first unit it covers, which is what lets
	// the two columns scroll in register.
	if !strings.Contains(body, `data-anchor="s-f001"`) {
		t.Errorf("chapters should anchor to their units:\n%s", body)
	}
	// Zen mode is reachable from every page.
	if !strings.Contains(body, "data-zen-toggle") {
		t.Errorf("every page should offer zen mode:\n%s", body)
	}
}

func TestCockpitLabelsMechanicalFallback(t *testing.T) {
	narrative := domain.Narrative{
		SessionID: "s", Title: "Session s", Intro: "2 files changed.",
		Source:   domain.NarrativeMechanical,
		Chapters: []domain.Chapter{{Title: "auth", Prose: "1 file changed under auth.", UnitIDs: []string{"s-f001"}}},
	}
	h := web.NewServer(testSession(), nil).WithNarrative(narrative)

	body := get(t, h, "/").Body.String()

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

func TestCockpitOffersRetryOnlyWhenTheModelHasNotNarrated(t *testing.T) {
	mechanical := domain.Narrative{SessionID: "s", Title: "T", Source: domain.NarrativeMechanical,
		Chapters: []domain.Chapter{{Title: "auth", Prose: "1 file changed under auth.", UnitIDs: []string{"s-f001"}}}}

	// Fell back to mechanical grouping: offer the reviewer a way to try again.
	h := web.NewServer(testSession(), nil).WithNarrative(mechanical).
		WithNarrate(func(context.Context, string) {})
	if !strings.Contains(get(t, h, "/").Body.String(), "/story/narrate") {
		t.Error("a mechanical story should offer a retry")
	}

	// Already narrated by the model: re-running costs calls for no gain, so the
	// page must not invite it.
	narrated := mechanical
	narrated.Source = domain.NarrativeModel
	h = web.NewServer(testSession(), nil).WithNarrative(narrated).
		WithNarrate(func(context.Context, string) {})
	if strings.Contains(get(t, h, "/").Body.String(), "/story/narrate") {
		t.Error("a model-narrated story should not offer a retry")
	}

	// No narrator wired at all: no button, because pressing it could do nothing.
	h = web.NewServer(testSession(), nil).WithNarrative(mechanical)
	if strings.Contains(get(t, h, "/").Body.String(), "/story/narrate") {
		t.Error("without a narrator there is nothing to retry")
	}
}

func TestNarrateRunsInTheBackgroundAndNeverTwiceAtOnce(t *testing.T) {
	// The whole point of this control: a reviewer who clicks twice, or two open
	// tabs, must not start two narrations. Model calls are the scarce resource.
	release := make(chan struct{})
	var mu sync.Mutex
	started := 0

	h := web.NewServer(testSession(), nil).WithNarrate(func(context.Context, string) {
		mu.Lock()
		started++
		mu.Unlock()
		<-release
	})

	post := func() int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/story/narrate", nil))
		return rec.Code
	}

	if code := post(); code != http.StatusSeeOther {
		t.Fatalf("first POST = %d, want 303 (it must not block on the model)", code)
	}
	// Wait for the first narration to be under way, then ask again.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := started
		mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	post()
	post()

	mu.Lock()
	got := started
	mu.Unlock()
	if got != 1 {
		t.Errorf("started %d narrations, want exactly 1 while one is running", got)
	}

	close(release)
}

func TestNarrateIsRefusedWhenNoNarratorIsWired(t *testing.T) {
	rec := httptest.NewRecorder()
	web.NewServer(testSession(), nil).ServeHTTP(rec,
		httptest.NewRequest(http.MethodPost, "/story/narrate", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /story/narrate = %d, want 404 when there is no narrator", rec.Code)
	}
}

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

func TestStatusListsSessionsAcrossReposAndAgents(t *testing.T) {
	sessions := []web.SessionSummary{
		{ID: "s1", Repo: "mondspace-reviewer", Agent: "claude-code", Prompt: "add token validation",
			Files: 12, Flags: 3, Open: 1, Started: time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)},
		{ID: "s2", Repo: "other-project", Agent: "opencode", Prompt: "port the parser",
			Files: 4, Flags: 0, Open: 0, Started: time.Date(2026, 8, 23, 11, 30, 0, 0, time.UTC)},
	}
	h := web.NewServer(testSession(), nil).WithWorkspace(sessions)

	rec := get(t, h, "/status")

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
	for _, want := range []string{"auth/token.go", "no-test"} {
		if !strings.Contains(body, want) {
			t.Errorf("review content %q must be server-rendered, not canvas-only", want)
		}
	}
	// Zen mode is reachable without JavaScript deciding it for us.
	if !strings.Contains(body, "data-zen-toggle") {
		t.Errorf("page should expose zen mode:\n%s", body)
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
		"auth/token.go", // the file is the anchor
		"auth/token.go", // the file itself
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

func TestCockpitShowsStatsFeedAndLiveness(t *testing.T) {
	h := web.NewServer(testSession(), nil).WithStats(domain.SessionStats{
		Open: 90 * time.Minute, Live: true,
		Files: 2, Added: 12, Removed: 4, Commits: 3, PullRequests: 1,
	})

	rec := get(t, h, "/cockpit")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /cockpit = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// The four numbers the reviewer asked for, all from git or the event log.
	for _, want := range []string{"1h 30m", "3", "1", "+12", "-4"} {
		if !strings.Contains(body, want) {
			t.Errorf("cockpit is missing the stat %q", want)
		}
	}
	// The feed: one line per change, with its diff.
	if !strings.Contains(body, "auth/token.go") || !strings.Contains(body, "http/middleware.go") {
		t.Errorf("cockpit feed should list the changes:\n%s", body)
	}
	// Liveness is exposed to the page so the animation can react to it.
	if !strings.Contains(body, `data-live="true"`) {
		t.Errorf("a live session should say so in the DOM:\n%s", body)
	}
}

func TestCockpitGoesCalmWhenTheSessionIsFinished(t *testing.T) {
	h := web.NewServer(testSession(), nil).WithStats(domain.SessionStats{Live: false, Files: 2})

	if !strings.Contains(get(t, h, "/cockpit").Body.String(), `data-live="false"`) {
		t.Error("an idle session should report itself as not live")
	}
}

func TestCockpitCompactsAnEnormousDiff(t *testing.T) {
	// One 2,000-line diff must not push every other change off the screen.
	var huge strings.Builder
	huge.WriteString("@@ -1,900 +1,900 @@\n")
	for i := 0; i < 900; i++ {
		huge.WriteString("+generated\n")
	}
	sess := testSession()
	sess.Diffs["s-f001"] = domain.Diff{Text: huge.String()}

	body := get(t, web.NewServer(sess, nil), "/cockpit").Body.String()

	if strings.Count(body, "generated") > 40 {
		t.Errorf("the feed should compact a huge diff, found %d lines of it", strings.Count(body, "generated"))
	}
	// And say that it did, rather than truncating in silence.
	if !strings.Contains(body, "more line") {
		t.Errorf("a compacted diff must say how much it left out:\n%s", body)
	}
}

func TestCockpitFeedIsNewestFirst(t *testing.T) {
	// A cockpit answers "what just happened", so the newest change leads.
	body := get(t, web.NewServer(testSession(), nil), "/cockpit").Body.String()

	if strings.Index(body, "http/middleware.go") > strings.Index(body, "auth/token.go") {
		t.Error("the feed should lead with the most recent change")
	}
}

func TestCockpitIsTheOnlyReviewPage(t *testing.T) {
	h := web.NewServer(testSession(), nil)

	// One page carries the story, the changes, and the ability to annotate.
	body := get(t, h, "/").Body.String()
	for _, want := range []string{"cockpit__story", "cockpit__changes", "annotate"} {
		if !strings.Contains(body, want) {
			t.Errorf("the cockpit should carry %q", want)
		}
	}

	// The old addresses still resolve, so links and bookmarks do not rot.
	for _, old := range []string{"/review", "/story"} {
		rec := get(t, h, old)
		if rec.Code != http.StatusMovedPermanently {
			t.Errorf("GET %s = %d, want a permanent redirect", old, rec.Code)
		}
		if got := rec.Header().Get("Location"); got != "/" {
			t.Errorf("GET %s redirects to %q, want /", old, got)
		}
	}
}

func TestAnnotatingReturnsToTheReviewQueueNotTheCockpit(t *testing.T) {
	// A reviewer annotating a unit is working through the queue; sending them to
	// the cockpit would lose their place.
	h := web.NewServer(testSession(), &recordingNotes{})

	req := httptest.NewRequest(http.MethodPost, "/units/s-f001/notes",
		strings.NewReader("kind=objection&text=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Location"); got != "/#unit-s-f001" {
		t.Errorf("Location = %q, want /#unit-s-f001", got)
	}
}

func TestStatusPageReportsTheReviewerAgentAndOpenSessions(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithWorkspace([]web.SessionSummary{
			{ID: "s", Repo: "mondspace-reviewer", Agent: "claude-code", Prompt: "add token validation", Files: 2},
			{ID: "other", Repo: "mondspace-reviewer", Agent: "opencode", Prompt: "fix the retry", Files: 5},
		}).
		WithAgent(web.AgentStatus{
			Model: "qwen/qwen3.5-9b", Endpoint: "http://localhost:1234/v1", Online: true,
			Usage: port.TokenUsage{Calls: 12, Failures: 1, Prompt: 4000, Completion: 900, Reasoning: 700, Millis: 36000},
		})

	rec := get(t, h, "/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// The reviewer's own agent: which model, where, and is it up right now.
	for _, want := range []string{"qwen/qwen3.5-9b", "http://localhost:1234/v1", "online"} {
		if !strings.Contains(body, want) {
			t.Errorf("status page is missing %q", want)
		}
	}
	// What it has spent. Reasoning is broken out: on a thinking model it is the
	// number that decides whether the context window is big enough.
	for _, want := range []string{"12", "4,000", "700"} {
		if !strings.Contains(body, want) {
			t.Errorf("status page is missing the usage figure %q", want)
		}
	}
	// Every session known for this project.
	if !strings.Contains(body, "claude-code") || !strings.Contains(body, "fix the retry") {
		t.Errorf("status page should list the project's sessions:\n%s", body)
	}
}

func TestStatusPageSaysWhenTheModelIsOffline(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithAgent(web.AgentStatus{Model: "m", Endpoint: "http://127.0.0.1:1/v1", Online: false})

	body := get(t, h, "/status").Body.String()

	if !strings.Contains(body, "offline") {
		t.Errorf("an unreachable endpoint must be reported as offline:\n%s", body)
	}
}

func TestCockpitShowsTokensWhenThereAreSome(t *testing.T) {
	// "if we have them" — a session that has made no model call shows no token
	// tile rather than a misleading zero.
	quiet := get(t, web.NewServer(testSession(), nil), "/")
	if strings.Contains(quiet.Body.String(), "tokens") {
		t.Error("with no model calls there is no token stat to show")
	}

	h := web.NewServer(testSession(), nil).
		WithAgent(web.AgentStatus{Usage: port.TokenUsage{Calls: 3, Prompt: 1200, Completion: 800}})

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "tokens") || !strings.Contains(body, "2,000") {
		t.Errorf("the cockpit should total the tokens spent:\n%s", body)
	}
}

func TestSwitchingSessionLoadsItOnDemand(t *testing.T) {
	// A workspace may span several repositories and many sessions. Materialising
	// every one at start-up would mean a git diff per file per session, so they
	// are loaded when first asked for — and only once.
	loads := map[string]int{}
	other := web.Session{
		ID: "other", Prompt: "port the parser", Repo: "otherrepo",
		Units: []domain.Unit{{ID: "other-f001", SessionID: "other",
			Files:    []string{"parser/lex.go"},
			Headline: domain.Headline{Text: "rewrote the lexer"}}},
		Diffs: map[string]domain.Diff{"other-f001": {Text: "@@ -1 +1 @@\n+lex\n"}},
	}

	h := web.NewServer(testSession(), nil).WithLoader(
		func(_ context.Context, id string) (web.Session, error) {
			loads[id]++
			if id == "other" {
				return other, nil
			}
			return web.Session{}, errors.New("no such session")
		})

	body := get(t, h, "/?session=other").Body.String()
	if !strings.Contains(body, "parser/lex.go") {
		t.Errorf("switching should render the other session:\n%s", body)
	}

	// Asked for again, it comes from the cache rather than being rebuilt.
	get(t, h, "/?session=other")
	if loads["other"] != 1 {
		t.Errorf("loaded %d times, want once — a loaded session is cached", loads["other"])
	}

	// The session it started with is still there, not replaced.
	if !strings.Contains(get(t, h, "/").Body.String(), "auth/token.go") {
		t.Error("switching away must not discard the original session")
	}
}

func TestUnknownSessionFallsBackRatherThanFailing(t *testing.T) {
	// A stale link or a deleted session must not leave the reviewer at an error
	// page with no way back.
	h := web.NewServer(testSession(), nil).WithLoader(
		func(context.Context, string) (web.Session, error) {
			return web.Session{}, errors.New("gone")
		})

	rec := get(t, h, "/?session=vanished")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /?session=vanished = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auth/token.go") {
		t.Error("an unknown session should fall back to the one already open")
	}
}

func TestCockpitOffersATargetPicker(t *testing.T) {
	h := web.NewServer(testSession(), nil).WithTargets([]web.TargetSummary{
		{ID: "t1", Repo: "mondspace-reviewer", Kind: domain.TargetCommit, Title: "Fix the retry"},
		{ID: "t2", Repo: "api", Kind: domain.TargetTag, Title: "v2.0.0"},
		{ID: "t3", Repo: "api", Kind: domain.TargetSession, Title: "port the parser"},
	})

	body := get(t, h, "/").Body.String()

	// Navigable without JavaScript: a plain form the browser can submit.
	if !strings.Contains(body, `name="target"`) {
		t.Errorf("the cockpit needs a target picker:\n%s", body)
	}
	// Git supplies most of what is reviewable; a session is one kind among them.
	for _, want := range []string{"Fix the retry", "v2.0.0", "port the parser", "commit", "tag"} {
		if !strings.Contains(body, want) {
			t.Errorf("the picker is missing %q", want)
		}
	}
	// Repositories are still distinguished, since a workspace spans them.
	if !strings.Contains(body, "api") {
		t.Errorf("the picker should name the repository:\n%s", body)
	}
}

func TestStatusListsOpenRepositoriesAndAbsorbsTheSessionsPage(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithWorkspace([]web.SessionSummary{
			{ID: "s", Repo: "api", Prompt: "add token validation"},
			{ID: "other", Repo: "web", Prompt: "port the parser"},
		}).
		WithRepos([]web.RepoStatus{
			{Name: "api", Path: "/w/api", Sessions: 1},
			{Name: "web", Path: "/w/web", Sessions: 1},
		}, nil)

	body := get(t, h, "/status").Body.String()

	// Repositories, with where they are and how much they hold.
	for _, want := range []string{"/w/api", "/w/web", "api", "web"} {
		if !strings.Contains(body, want) {
			t.Errorf("status is missing %q", want)
		}
	}
	// The sessions page folded in here, so its content must be present.
	if !strings.Contains(body, "port the parser") {
		t.Errorf("status should list the workspace's sessions:\n%s", body)
	}
	// And the old address still resolves.
	if rec := get(t, h, "/sessions"); rec.Code != http.StatusMovedPermanently {
		t.Errorf("GET /sessions = %d, want a permanent redirect", rec.Code)
	}
}

func TestAddingARepositoryWithoutRestarting(t *testing.T) {
	var asked string
	h := web.NewServer(testSession(), nil).WithRepos(nil,
		func(path string) ([]web.SessionSummary, []web.RepoStatus, error) {
			asked = path
			return []web.SessionSummary{{ID: "new", Repo: "worker", Prompt: "retry the queue"}},
				[]web.RepoStatus{{Name: "worker", Path: "/w/worker", Sessions: 1}}, nil
		})

	req := httptest.NewRequest(http.MethodPost, "/repos", strings.NewReader("path=/w/worker"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /repos = %d, want 303", rec.Code)
	}
	if asked != "/w/worker" {
		t.Errorf("asked to open %q", asked)
	}
	// The new repository's sessions are immediately reachable.
	if !strings.Contains(get(t, h, "/status").Body.String(), "retry the queue") {
		t.Error("a newly opened repository's sessions should appear at once")
	}
}

func TestAddingARepositoryReportsWhyItFailed(t *testing.T) {
	// A typo'd path must say so, not fail silently and leave the reviewer
	// wondering whether it worked.
	h := web.NewServer(testSession(), nil).WithRepos(nil,
		func(string) ([]web.SessionSummary, []web.RepoStatus, error) {
			return nil, nil, errors.New("/w/nope is not a git repository")
		})

	req := httptest.NewRequest(http.MethodPost, "/repos", strings.NewReader("path=/w/nope"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !strings.Contains(get(t, h, "/status").Body.String(), "not a git repository") {
		t.Error("the failure should be shown on the page")
	}
}

func TestDescribingOneGroupOnDemand(t *testing.T) {
	// The automatic pass is bounded, so most groups in a large session are left
	// "not yet described". Without a way to ask, that is a dead end.
	var asked string
	h := web.NewServer(testSession(), nil).
		WithNarrative(domain.Narrative{SessionID: "s", Source: domain.NarrativeModel}).
		WithDescribe(func(_ context.Context, _, groupID string) (string, error) {
			asked = groupID
			return "Persists the story so a restart costs nothing.", nil
		})

	// The group id is rendered on the page; take it from there rather than
	// recomputing it, so the test exercises the same identity the button uses.
	body := get(t, h, "/").Body.String()
	id := regexp.MustCompile(`id="group-([0-9a-f]+)"`).FindStringSubmatch(body)
	if id == nil {
		t.Fatalf("no group rendered:\n%s", body[:400])
	}

	req := httptest.NewRequest(http.MethodPost, "/groups/"+id[1]+"/describe", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST describe = %d, want 303", rec.Code)
	}
	if asked != id[1] {
		t.Errorf("described %q, want %q", asked, id[1])
	}
	if !strings.Contains(get(t, h, "/").Body.String(), "Persists the story") {
		t.Error("the new description should appear on the page")
	}
}

func TestDescribeButtonIsOfferedOnlyWhenItCanDoSomething(t *testing.T) {
	// Without a describer wired there is nothing behind the button, and offering
	// it would be a promise the page cannot keep.
	plain := get(t, web.NewServer(testSession(), nil), "/").Body.String()
	if strings.Contains(plain, "/describe") {
		t.Error("no describer wired, so no describe control")
	}

	h := web.NewServer(testSession(), nil).WithDescribe(
		func(context.Context, string, string) (string, error) { return "x", nil })
	if !strings.Contains(get(t, h, "/").Body.String(), "/describe") {
		t.Error("a wired describer should offer the control")
	}
}
