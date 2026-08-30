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
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
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
		func(context.Context, string, string, []web.Exchange) (string, error) { return "an answer", nil })

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

func TestReviewCardAlwaysOffersAReadWhenOneIsPossible(t *testing.T) {
	mechanical := domain.Narrative{SessionID: "s", Title: "T", Source: domain.NarrativeMechanical,
		Chapters: []domain.Chapter{{Title: "auth", Prose: "1 file changed under auth.", UnitIDs: []string{"s-f001"}}}}

	// Fell back to mechanical grouping: offer the reviewer a way to try again.
	h := web.NewServer(testSession(), nil).WithNarrative(mechanical).
		WithNarrate(func(context.Context, string) {})
	if !strings.Contains(get(t, h, "/").Body.String(), "/story/narrate") {
		t.Error("a mechanical story should offer a retry")
	}

	// Already read: re-reading is still offered, because a review goes stale as
	// soon as the code moves. It is a button, not something automatic — the cost
	// is still the reviewer's to spend (ADR 0014).
	narrated := mechanical
	narrated.Source = domain.NarrativeModel
	h = web.NewServer(testSession(), nil).WithNarrative(narrated).
		WithNarrate(func(context.Context, string) {})
	if !strings.Contains(get(t, h, "/").Body.String(), "/story/narrate") {
		t.Error("a read review should still offer a re-read")
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
		func(_ context.Context, _, question string, history []web.Exchange) (string, error) {
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
		func(context.Context, string, string, []web.Exchange) (string, error) {
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

func TestSettingsListReviewsAcrossReposAndAgents(t *testing.T) {
	sessions := []web.SessionSummary{
		{ID: "s1", Repo: "mondspace-reviewer", Agent: "claude-code", Prompt: "add token validation",
			Files: 12, Flags: 3, Open: 1, Started: time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)},
		{ID: "s2", Repo: "other-project", Agent: "opencode", Prompt: "port the parser",
			Files: 4, Flags: 0, Open: 0, Started: time.Date(2026, 8, 23, 11, 30, 0, 0, time.UTC)},
	}
	h := web.NewServer(testSession(), nil).WithWorkspace(sessions)

	rec := get(t, h, "/settings?s=reviews")

	if rec.Code != http.StatusOK {
		t.Fatalf("reviews = %d, want 200", rec.Code)
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

func TestSettingsReportTheReviewerAgentAndItsReviews(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithWorkspace([]web.SessionSummary{
			{ID: "s", Repo: "mondspace-reviewer", Agent: "claude-code", Prompt: "add token validation", Files: 2},
			{ID: "other", Repo: "mondspace-reviewer", Agent: "opencode", Prompt: "fix the retry", Files: 5},
		}).
		WithAgent(web.AgentStatus{
			Model: "qwen/qwen3.5-9b", Endpoint: "http://localhost:1234/v1", Online: true,
			Usage: port.TokenUsage{Calls: 12, Failures: 1, Prompt: 4000, Completion: 900, Reasoning: 700, Millis: 36000},
		})

	rec := get(t, h, "/settings?s=model")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the model pane = %d, want 200", rec.Code)
	}

	// The reviewer's own agent: which model, where, and is it up right now.
	for _, want := range []string{"qwen/qwen3.5-9b", "http://localhost:1234/v1", "online"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the model pane is missing %q", want)
		}
	}
	// What it has spent, which is its own pane. Reasoning is broken out: on a
	// thinking model it is the number that decides whether the context window
	// is big enough.
	spend := get(t, h, "/settings?s=usage").Body.String()
	for _, want := range []string{"12", "4,000", "700"} {
		if !strings.Contains(spend, want) {
			t.Errorf("the usage pane is missing the figure %q", want)
		}
	}
	// Every review known for this project, likewise.
	reviews := get(t, h, "/settings?s=reviews").Body.String()
	if !strings.Contains(reviews, "claude-code") || !strings.Contains(reviews, "fix the retry") {
		t.Errorf("the reviews pane should list the project's reviews:\n%s", reviews)
	}
}

func TestSettingsSayWhenTheModelIsOffline(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithAgent(web.AgentStatus{Model: "m", Endpoint: "http://127.0.0.1:1/v1", Online: false})

	body := get(t, h, "/settings?s=model").Body.String()

	if !strings.Contains(body, "offline") {
		t.Errorf("an unreachable endpoint must be reported as offline:\n%s", body)
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

func TestSettingsListOpenRepositoriesAndAbsorbTheSessionsPage(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithWorkspace([]web.SessionSummary{
			{ID: "s", Repo: "api", Prompt: "add token validation"},
			{ID: "other", Repo: "web", Prompt: "port the parser"},
		}).
		WithRepos([]web.RepoStatus{
			{Name: "api", Path: "/w/api", Sessions: 1},
			{Name: "web", Path: "/w/web", Sessions: 1},
		}, nil)

	body := get(t, h, "/settings?s=repositories").Body.String()

	// Repositories, with where they are and how much they hold.
	for _, want := range []string{"/w/api", "/w/web", "api", "web"} {
		if !strings.Contains(body, want) {
			t.Errorf("the repositories pane is missing %q", want)
		}
	}
	// The old sessions page folded into settings, so its content must be there.
	if !strings.Contains(get(t, h, "/settings?s=reviews").Body.String(), "port the parser") {
		t.Errorf("the reviews pane should list the workspace's reviews")
	}
	// And the old address still resolves.
	if rec := get(t, h, "/sessions"); rec.Code != http.StatusMovedPermanently {
		t.Errorf("GET /sessions = %d, want a permanent redirect", rec.Code)
	}
}

func TestAddingARepositoryWithoutRestarting(t *testing.T) {
	var asked string
	h := web.NewServer(testSession(), nil).WithRepos(nil,
		func(path string) ([]web.TargetSummary, []web.RepoStatus, error) {
			asked = path
			return []web.TargetSummary{{ID: "new", Repo: "worker", Title: "retry the queue"}},
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
	// The new repository's targets are immediately reachable.
	if !strings.Contains(get(t, h, "/").Body.String(), "retry the queue") {
		t.Error("a newly opened repository's targets should appear at once")
	}
}

func TestAddingARepositoryReportsWhyItFailed(t *testing.T) {
	// A typo'd path must say so, not fail silently and leave the reviewer
	// wondering whether it worked.
	h := web.NewServer(testSession(), nil).WithRepos(nil,
		func(string) ([]web.TargetSummary, []web.RepoStatus, error) {
			return nil, nil, errors.New("/w/nope is not a git repository")
		})

	req := httptest.NewRequest(http.MethodPost, "/repos", strings.NewReader("path=/w/nope"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !strings.Contains(get(t, h, "/settings?s=repositories").Body.String(), "not a git repository") {
		t.Error("the failure should be shown on the page")
	}
}

func TestDescribingOneGroupOnDemand(t *testing.T) {
	// The automatic pass is bounded, so most groups in a large session are left
	// "not yet described". Without a way to ask, that is a dead end.
	var asked string
	h := web.NewServer(testSession(), nil).
		WithNarrative(domain.Narrative{SessionID: "s", Source: domain.NarrativeModel}).
		WithDescribe(func(_ context.Context, _ string, unitIDs []string) (string, error) {
			asked = strings.Join(unitIDs, ",")
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
	// The command is told which files, not a group id to look up again.
	if !strings.Contains(asked, "s-f") {
		t.Errorf("described %q, want the unit ids the page rendered", asked)
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
		func(context.Context, string, []string) (string, error) { return "x", nil })
	if !strings.Contains(get(t, h, "/").Body.String(), "/describe") {
		t.Error("a wired describer should offer the control")
	}
}

func TestWorkInFlightIsVisibleWhileItRuns(t *testing.T) {
	// Every model call takes seconds to minutes on a local model. A page that
	// shows nothing while one runs is indistinguishable from a broken one.
	h := web.NewServer(testSession(), nil)

	done := h.BeginWork("narrate", "t1", "reading v3.1.0")
	if h.InFlight() != 1 {
		t.Fatalf("InFlight = %d, want 1 while the call runs", h.InFlight())
	}
	if body := get(t, h, "/").Body.String(); !strings.Contains(body, "reading v3.1.0") {
		t.Errorf("work in flight should be visible on the page:\n%s", body[:600])
	}

	done(nil)
	if h.InFlight() != 0 {
		t.Errorf("InFlight = %d, want 0 once it finished", h.InFlight())
	}
	// Finished work stays on the record, so a reviewer can see what was asked.
	if !strings.Contains(get(t, h, "/settings?s=usage").Body.String(), "reading v3.1.0") {
		t.Error("finished work should still be listed under usage")
	}
}

func TestFailedWorkSaysSoAndOffersARetry(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithNarrate(func(context.Context, string) {})

	done := h.BeginWork("narrate", "t1", "reading v3.1.0")
	done(errors.New("the model spent its whole budget on reasoning"))

	body := get(t, h, "/settings?s=usage").Body.String()
	if !strings.Contains(body, "budget on reasoning") {
		t.Errorf("a failure should say why:\n%s", body)
	}
	if !strings.Contains(body, "/story/narrate?target=t1") {
		t.Errorf("failed work should be retriggerable:\n%s", body)
	}
}

func TestWorkKeepsOnlyRecentHistory(t *testing.T) {
	// A long session must not grow an unbounded list in memory, or a status page
	// that takes a second to render.
	h := web.NewServer(testSession(), nil)
	for i := 0; i < 100; i++ {
		h.BeginWork("ask", "t", fmt.Sprintf("question %d", i))(nil)
	}

	if n := len(h.Work()); n > 30 {
		t.Errorf("kept %d work entries, want a bounded recent history", n)
	}
	// The newest is kept, not the oldest.
	if !strings.Contains(fmt.Sprint(h.Work()), "question 99") {
		t.Error("the most recent work should survive the bound")
	}
}

func TestNarrationWorkAlwaysFinishes(t *testing.T) {
	// A spinner that never stops is worse than none: it says the assistant is
	// still thinking when it is not, and there is no way to tell from the page.
	h := web.NewServer(testSession(), nil).
		WithNarrate(func(context.Context, string) { /* returns immediately */ })

	req := httptest.NewRequest(http.MethodPost, "/story/narrate", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	waitIdle(t, h)
	if n := h.InFlight(); n != 0 {
		t.Errorf("InFlight = %d after the narrator returned, want 0", n)
	}
}

// waitIdle waits for the work registered by a request to finish.
func waitIdle(t *testing.T, h *web.Server) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.Work()) > 0 && h.InFlight() == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("work did not finish: %+v", h.Work())
}

func TestNarrationWorkFinishesEvenWhenTheNarratorPanics(t *testing.T) {
	// A panic in a background goroutine takes the process with it, and on the
	// way out would leave the page claiming to be thinking. Neither is
	// acceptable for something a model can trigger.
	h := web.NewServer(testSession(), nil).
		WithNarrate(func(context.Context, string) { panic("the model adapter blew up") })

	req := httptest.NewRequest(http.MethodPost, "/story/narrate", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	waitIdle(t, h)
	if n := h.InFlight(); n != 0 {
		t.Errorf("InFlight = %d after a panic, want 0", n)
	}
	// And it must say what happened rather than showing a silent success.
	var failed bool
	for _, w := range h.Work() {
		if w.Failed() {
			failed = true
		}
	}
	if !failed {
		t.Error("a panicking narrator should be recorded as a failure")
	}
}

func TestReviewCardSaysWhetherTheAssistantHasReadThis(t *testing.T) {
	// Switching target is exactly when a reviewer needs to know whether what
	// they are looking at has been read yet, and how long ago.
	read := domain.Narrative{
		SessionID: "s", Title: "T", Source: domain.NarrativeModel,
		Model: "qwen/qwen3.5-9b", WrittenAt: time.Now().Add(-90 * time.Minute),
		Chapters: []domain.Chapter{{Title: "auth", UnitIDs: []string{"s-f001"}}},
	}
	h := web.NewServer(testSession(), nil).WithNarrative(read).
		WithNarrate(func(context.Context, string) {})

	body := get(t, h, "/").Body.String()

	for _, want := range []string{"reviewcard", "qwen/qwen3.5-9b", "1h 30m ago"} {
		if !strings.Contains(body, want) {
			t.Errorf("the review card is missing %q", want)
		}
	}
	// Re-reading is always offered, because a review can go stale.
	if !strings.Contains(body, "/story/narrate") {
		t.Errorf("a read review should still offer a re-read:\n%s", body)
	}
}

func TestReviewCardOffersAFirstReadWhenThereHasBeenNone(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithNarrative(domain.Narrative{SessionID: "s", Source: domain.NarrativeMechanical}).
		WithNarrate(func(context.Context, string) {})

	body := get(t, h, "/").Body.String()

	if !strings.Contains(body, "not read yet") {
		t.Errorf("an unread target should say so:\n%s", body)
	}
	if !strings.Contains(body, "/story/narrate") {
		t.Error("an unread target must offer a read")
	}
}

func TestReviewCardShowsProgressWhileReading(t *testing.T) {
	// A local model takes minutes. Knowing it is working — and roughly how far
	// along — is the difference between waiting and giving up.
	h := web.NewServer(testSession(), nil).
		WithNarrative(domain.Narrative{
			SessionID: "s", Source: domain.NarrativeModel,
			Chapters: []domain.Chapter{{Title: "auth", UnitIDs: []string{"s-f001"}}},
			Meanings: map[string]string{"g1": "one group described"},
		}).
		WithNarrate(func(context.Context, string) { time.Sleep(time.Second) })

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/story/narrate", nil))

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "reading") {
		t.Errorf("a read in progress should say so:\n%s", body)
	}
	// While it runs, asking again would just queue a second call.
	if strings.Contains(body, `action="/story/narrate"`) {
		t.Error("a read already running should not offer another")
	}
}

func TestConfiguringTheModelFromTheStatusPage(t *testing.T) {
	// Pointing msr at a different machine or a different model should not mean
	// editing a file and restarting.
	var got domain.AgentConfig
	h := web.NewServer(testSession(), nil).
		WithAgent(web.AgentStatus{Model: "old", Endpoint: "http://old:1234/v1"}).
		WithConfigure(func(c domain.AgentConfig) error {
			got = c
			return nil
		})

	if !strings.Contains(get(t, h, "/settings?s=model").Body.String(), `action="/agent"`) {
		t.Fatal("the status page should offer the model settings")
	}

	req := httptest.NewRequest(http.MethodPost, "/agent",
		strings.NewReader("endpoint=http://new:1234/v1&model=llama-3&no_thinking=on"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /agent = %d, want 303", rec.Code)
	}
	if got.Endpoint != "http://new:1234/v1" || got.Model != "llama-3" || !got.NoThinking {
		t.Errorf("configured %+v, want the submitted settings", got)
	}
	// And the page reflects it at once, rather than after a restart.
	if !strings.Contains(get(t, h, "/settings?s=model").Body.String(), "llama-3") {
		t.Error("the new model should show immediately")
	}
}

func TestARejectedConfigurationSaysWhy(t *testing.T) {
	h := web.NewServer(testSession(), nil).WithConfigure(
		func(domain.AgentConfig) error { return errors.New("that endpoint did not answer") })

	req := httptest.NewRequest(http.MethodPost, "/agent",
		strings.NewReader("endpoint=http://nope:1234/v1&model=m"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !strings.Contains(get(t, h, "/settings?s=model").Body.String(), "did not answer") {
		t.Error("a rejected change should be explained on the page")
	}
}

func TestSettingsAreNotOfferedWithoutSomewhereToPutThem(t *testing.T) {
	if strings.Contains(get(t, web.NewServer(testSession(), nil), "/settings?s=model").Body.String(), `action="/agent"`) {
		t.Error("without a configure hook there is nothing behind the form")
	}
}

func TestActingOnASwitchedTargetReachesTheRightReview(t *testing.T) {
	// Every action posts to a path with a unit or group id in it. Without the
	// target alongside, the handler looked the id up in the review the server
	// started with — so describing, annotating and re-analysing all failed the
	// moment the reviewer switched to something else.
	other := web.Session{
		ID: "other", Prompt: "port the parser", Repo: "api",
		Units: []domain.Unit{{ID: "other-f001", SessionID: "other",
			Files: []string{"parser/lex.go"}, Headline: domain.Headline{Text: "rewrote the lexer"}}},
		Diffs: map[string]domain.Diff{"other-f001": {Text: "@@ -1 +1 @@\n+lex\n"}},
	}
	notes := &recordingNotes{}
	h := web.NewServer(testSession(), notes).
		WithLoader(func(context.Context, string) (web.Session, error) { return other, nil }).
		WithDescribe(func(context.Context, string, []string) (string, error) { return "meaning", nil }).
		WithDescribeFile(func(context.Context, string, string) (string, []string, error) { return "meaning", nil, nil })

	// Every action form on that page must carry the target it is acting on.
	body := get(t, h, "/?target=other").Body.String()
	for _, want := range []string{
		`/units/other-f001/notes?target=other`,
		`/units/other-f001/describe?target=other`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("an action form is missing its target: want %q", want)
		}
	}

	// And annotating one really does land against that review.
	req := httptest.NewRequest(http.MethodPost,
		"/units/other-f001/notes?target=other", strings.NewReader("kind=ok&text=fine"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("annotating a switched target = %d, want 303", rec.Code)
	}
	if len(notes.notes) != 1 || notes.notes[0].UnitID != "other-f001" {
		t.Errorf("stored %+v, want a note on the switched review's unit", notes.notes)
	}
}

func TestDescribingAGroupOnASwitchedTarget(t *testing.T) {
	// The reported symptom: re-describe answered "no such group in this review"
	// because the group id came from one review and was looked up in another.
	other := web.Session{
		ID: "other", Repo: "api",
		Units: []domain.Unit{{ID: "other-f001", SessionID: "other", Files: []string{"parser/lex.go"}}},
		Diffs: map[string]domain.Diff{"other-f001": {Text: "@@\n+lex\n"}},
	}
	var asked []string
	h := web.NewServer(testSession(), nil).
		WithLoader(func(context.Context, string) (web.Session, error) { return other, nil }).
		WithDescribe(func(_ context.Context, _ string, units []string) (string, error) {
			asked = units
			return "what it is for", nil
		})

	body := get(t, h, "/?target=other").Body.String()
	group := regexp.MustCompile(`id="group-([0-9a-f]+)"`).FindStringSubmatch(body)
	if group == nil {
		t.Fatalf("no group rendered:\n%s", body[:400])
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/groups/"+group[1]+"/describe?target=other", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("describe on a switched target = %d, want 303 (not 'no such group')", rec.Code)
	}
	if len(asked) != 1 || asked[0] != "other-f001" {
		t.Errorf("asked to describe %v, want the switched review's unit", asked)
	}
}

func TestStatusReportsWhetherSkippingReasoningActuallyWorked(t *testing.T) {
	// "only some chat templates honour this" left the reviewer to guess. msr
	// always sends the request; whether the model's chat template reads it is
	// decided inside the server. That is measurable, so it should be measured.
	h := web.NewServer(testSession(), nil).WithAgent(web.AgentStatus{
		Model: "qwen/qwen3.5-9b", NoThinking: true,
		Usage: port.TokenUsage{Calls: 4, Completion: 400, Reasoning: 380},
	})

	body := get(t, h, "/settings?s=usage").Body.String()

	if !strings.Contains(body, "ignoring") {
		t.Errorf("a model still reasoning with the setting on should say so:\n%s", body)
	}
	// And say how much, because "most of it" and "a trace" are different problems.
	if !strings.Contains(body, "95%") {
		t.Errorf("the reasoning share should be shown:\n%s", body)
	}
}

func TestSettingsSayNothingWhenSkippingIsWorking(t *testing.T) {
	h := web.NewServer(testSession(), nil).WithAgent(web.AgentStatus{
		Model: "some/model", NoThinking: true,
		Usage: port.TokenUsage{Calls: 4, Completion: 400, Reasoning: 0},
	})

	if strings.Contains(get(t, h, "/settings?s=usage").Body.String(), "ignoring") {
		t.Error("a model that stopped reasoning should not be accused of ignoring the setting")
	}
}

func TestStatusDoesNotAccuseAModelBeforeItHasBeenAsked(t *testing.T) {
	// With no calls yet there is no evidence either way, and guessing would be
	// exactly the vagueness this replaced.
	h := web.NewServer(testSession(), nil).WithAgent(web.AgentStatus{
		Model: "some/model", NoThinking: true,
	})

	if strings.Contains(get(t, h, "/settings?s=usage").Body.String(), "ignoring") {
		t.Error("nothing has been asked yet, so nothing can be concluded")
	}
}

func TestReasoningShareIsShownEvenWhenNotSkipping(t *testing.T) {
	// It is the number that decides whether a context window is big enough, so
	// it is worth seeing whatever the setting says.
	h := web.NewServer(testSession(), nil).WithAgent(web.AgentStatus{
		Usage: port.TokenUsage{Calls: 2, Completion: 200, Reasoning: 50},
	})

	if !strings.Contains(get(t, h, "/settings?s=usage").Body.String(), "25%") {
		t.Error("the reasoning share should be shown regardless of the setting")
	}
}

func TestStatsSuitWhatIsBeingReviewed(t *testing.T) {
	// A single commit has one commit in it by definition, and no pull request.
	// Showing "1 commit · 0 PRs" is answering questions nobody asked, and it
	// crowds out the ones they did.
	sess := testSession()
	sess.Target = domain.Target{Kind: domain.TargetCommit, Subtitle: "a1b2c3d4 · Marco"}
	sess.Stats = domain.SessionStats{Files: 2, Added: 12, Removed: 4, Commits: 1}
	h := web.NewServer(sess, nil)

	body := get(t, h, "/").Body.String()

	for _, unwanted := range []string{`stat__label">commits`, `stat__label">PRs`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("a single commit should not show %q", unwanted)
		}
	}
	for _, want := range []string{`stat__label">files`, `stat__label">lines`} {
		if !strings.Contains(body, want) {
			t.Errorf("a commit should still show %q", want)
		}
	}
}

func TestATagShowsWhatShippedInIt(t *testing.T) {
	// "What went into this release" is the question a tag answers, so the commits
	// and the pull requests are the point.
	sess := testSession()
	sess.Target = domain.Target{Kind: domain.TargetTag}
	sess.Stats = domain.SessionStats{Files: 25, Added: 900, Removed: 120, Commits: 14, PullRequests: 3}
	h := web.NewServer(sess, nil)

	body := get(t, h, "/").Body.String()

	for _, want := range []string{`stat__label">commits`, `stat__label">PRs`} {
		if !strings.Contains(body, want) {
			t.Errorf("a tag should show %q", want)
		}
	}
}

func TestASessionShowsHowLongItRan(t *testing.T) {
	sess := testSession()
	sess.Target = domain.Target{Kind: domain.TargetSession}
	sess.Stats = domain.SessionStats{Files: 3, Open: 90 * time.Minute, Commits: 2}
	h := web.NewServer(sess, nil)

	if !strings.Contains(get(t, h, "/").Body.String(), `stat__label">open`) {
		t.Error("how long a run went on is the thing only a session has")
	}
}

func TestTheWorkingTreeDoesNotPretendToHaveCommits(t *testing.T) {
	sess := testSession()
	sess.Target = domain.Target{Kind: domain.TargetWorktree}
	sess.Stats = domain.SessionStats{Files: 6, Added: 40, Removed: 3}
	h := web.NewServer(sess, nil)

	body := get(t, h, "/").Body.String()
	if strings.Contains(body, `stat__label">commits`) {
		t.Error("uncommitted work has no commits, which is the whole point of it")
	}
	if !strings.Contains(body, "uncommitted") {
		t.Errorf("it should say what it is:\n%s", body)
	}
}

func TestTokensAreNotShownBesideReviewFacts(t *testing.T) {
	// The token count is what the assistant has spent since msr started, across
	// every review. Sitting it beside this review's file and line counts read as
	// though it belonged to this review. It lives on /status, explained.
	sess := testSession()
	sess.Target = domain.Target{Kind: domain.TargetCommit}
	sess.Stats = domain.SessionStats{Files: 1}
	h := web.NewServer(sess, nil).
		WithAgent(web.AgentStatus{Usage: port.TokenUsage{Calls: 3, Prompt: 1200, Completion: 800}})

	if strings.Contains(get(t, h, "/").Body.String(), `stat__label">tokens`) {
		t.Error("a process-wide token total does not belong among this review's facts")
	}
}

func TestATargetCanBeOpenedByItsRefNotOnlyItsID(t *testing.T) {
	// The picker and the two comparison fields become one kind of input, so they
	// need one vocabulary. A tag is "v5.1.0" to a person; a hex id is not.
	other := web.Session{ID: "abc123", Prompt: "v5.1.0", Repo: "api",
		Units: []domain.Unit{{ID: "abc123-f001", Files: []string{"a.go"}}}}
	var asked []string
	h := web.NewServer(testSession(), nil).
		WithTargets([]web.TargetSummary{
			{ID: "abc123", Ref: "v5.1.0", Kind: domain.TargetTag, Title: "v5.1.0", Repo: "api"},
		}).
		WithLoader(func(_ context.Context, id string) (web.Session, error) {
			asked = append(asked, id)
			if id == "abc123" {
				return other, nil
			}
			return web.Session{}, errors.New("no such target")
		})

	body := get(t, h, "/?target=v5.1.0").Body.String()

	if !strings.Contains(body, "a.go") {
		t.Errorf("a target should open by its ref:\n%s", body[:400])
	}
	// It is resolved to the id before the loader is asked, so the loader keeps
	// one way of being addressed.
	if len(asked) == 0 || asked[0] != "abc123" {
		t.Errorf("loader was asked for %v, want the resolved id", asked)
	}
}

func TestAllThreeInputsShareOneList(t *testing.T) {
	// One element, one vocabulary, searchable. Three different widgets for three
	// versions of "which point in history" was the confusing part.
	h := web.NewServer(testSession(), nil).
		WithTargets([]web.TargetSummary{
			{ID: "t1", Ref: "v5.1.0", Kind: domain.TargetTag, Title: "v5.1.0", Repo: "api"},
			{ID: "t2", Ref: "a1b2c3d4", Kind: domain.TargetCommit, Title: "Fix the retry", Repo: "api"},
		}).
		WithCompare(func(context.Context, string, string, string) (string, error) { return "x", nil })

	body := get(t, h, "/").Body.String()

	if n := strings.Count(body, `list="refs"`); n != 3 {
		t.Errorf("found %d inputs bound to the shared list, want 3 (target, from, to)", n)
	}
	if !strings.Contains(body, `<datalist id="refs"`) {
		t.Error("the shared list of points in history is missing")
	}
	// Every known point is offered, whichever field you are filling in.
	for _, want := range []string{"v5.1.0", "a1b2c3d4"} {
		if !strings.Contains(body, want) {
			t.Errorf("the list is missing %q", want)
		}
	}
}

func TestPulseReachesAnOpenPageWithItsWords(t *testing.T) {
	// A pulse is the only event that carries content: every other one just says
	// "something changed, re-fetch". A toast has to be able to say what
	// happened without a round trip, or it cannot appear promptly.
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

	go func() {
		time.Sleep(50 * time.Millisecond)
		h.Pulse([]domain.Pulse{
			{Kind: domain.PulseCommit, Text: "New commit · Fix the parser", Ref: "abc12345"},
		})
	}()

	var event, data string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		switch line := sc.Text(); {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: ") && event == "pulse":
			data = strings.TrimPrefix(line, "data: ")
		}
		if data != "" {
			break
		}
	}

	if event != "pulse" {
		t.Fatalf("event = %q, want pulse", event)
	}
	if !strings.Contains(data, "Fix the parser") || !strings.Contains(data, "abc12345") {
		t.Errorf("the pulse should carry its words and its ref, got %s", data)
	}
	// One line of data, or EventSource will not parse it.
	if strings.Contains(data, "\n") {
		t.Error("data must be a single line")
	}
}

func TestAnEmptyPulseIsNotBroadcast(t *testing.T) {
	// Silence is the common case: the watcher looks every couple of seconds and
	// almost always finds nothing. Waking every open page for that would undo
	// the point of pushing rather than polling.
	h := web.NewServer(testSession(), nil)
	before := h.PulsesSent()
	h.Pulse(nil)
	h.Pulse([]domain.Pulse{})
	if h.PulsesSent() != before {
		t.Errorf("nothing to say should send nothing, sent %d", h.PulsesSent()-before)
	}
}

func TestTheLiveTargetIsNeverServedFromCache(t *testing.T) {
	// Caching a loaded target is right for a commit or a tag: the range is
	// fixed, so rebuilding it would produce the same answer. The live target is
	// the exception — its whole purpose is that it changed since you last
	// looked, and a cache would make it the one thing on the page that lies.
	loads := 0
	files := "before.go"

	h := web.NewServer(testSession(), nil).
		WithTargets([]web.TargetSummary{{
			ID: "live-1", Ref: "live", Kind: domain.TargetLive, Title: "Live",
		}}).
		WithLoader(func(_ context.Context, id string) (web.Session, error) {
			loads++
			return web.Session{
				ID: "live-1",
				Units: []domain.Unit{{
					ID: "live-1-f001", Files: []string{files},
					Headline: domain.Headline{Text: "working"},
				}},
			}, nil
		})

	if body := get(t, h, "/?target=live").Body.String(); !strings.Contains(body, "before.go") {
		t.Fatalf("the live target should render:\n%s", body)
	}

	files = "after.go"
	body := get(t, h, "/?target=live").Body.String()

	if loads != 2 {
		t.Errorf("loaded %d times, want 2 — the live target is rebuilt each look", loads)
	}
	if !strings.Contains(body, "after.go") {
		t.Errorf("the live target served a stale working tree:\n%s", body)
	}
}

func TestOrdinaryTargetsAreStillCached(t *testing.T) {
	// The exception above must stay an exception: rebuilding a fixed range on
	// every request would make every pulse cost a full diff of history.
	loads := 0
	h := web.NewServer(testSession(), nil).
		WithTargets([]web.TargetSummary{{
			ID: "c1", Ref: "abc12345", Kind: domain.TargetCommit, Title: "A commit",
		}}).
		WithLoader(func(_ context.Context, id string) (web.Session, error) {
			loads++
			return web.Session{ID: "c1", Units: []domain.Unit{{
				ID: "c1-f001", Files: []string{"fixed.go"},
			}}}, nil
		})

	get(t, h, "/?target=abc12345")
	get(t, h, "/?target=abc12345")
	if loads != 1 {
		t.Errorf("loaded %d times, want once", loads)
	}
}

func TestTheLiveTargetIsRebuiltEvenWhenItIsTheOpenOne(t *testing.T) {
	// msr opens on the live target by default, which makes it the current
	// session — and the current session is served straight from memory. That
	// short-circuit is right for every fixed range and wrong for this one: it
	// meant the live review showed whatever the working tree held at start-up
	// and never moved again.
	loads := 0
	files := "before.go"

	live := testSession()
	live.ID = "live-1"
	h := web.NewServer(live, nil).
		WithTargets([]web.TargetSummary{{
			ID: "live-1", Ref: "live", Kind: domain.TargetLive, Title: "Live",
		}}).
		WithLoader(func(_ context.Context, id string) (web.Session, error) {
			loads++
			return web.Session{ID: "live-1", Units: []domain.Unit{{
				ID: "live-1-f001", Files: []string{files},
				Headline: domain.Headline{Text: "working"},
			}}}, nil
		})

	files = "after.go"
	for _, path := range []string{"/", "/?target=live", "/?target=live-1"} {
		body := get(t, h, path).Body.String()
		if !strings.Contains(body, "after.go") {
			t.Errorf("%s served a frozen working tree:\n%s", path, body)
		}
	}
	if loads != 3 {
		t.Errorf("rebuilt %d times, want one per look", loads)
	}
}

func TestSendingAWorkloadToItsOwnModelFromTheStatusPage(t *testing.T) {
	// The two-server split has to be adjustable where everything else is, or it
	// becomes the one setting that needs a config file and a restart.
	var got domain.AgentConfig
	h := web.NewServer(testSession(), nil).
		WithAgent(web.AgentStatus{Model: "small", Endpoint: "http://127.0.0.1:8081/v1"}).
		WithConfigure(func(c domain.AgentConfig) error { got = c; return nil })

	req := httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(
		"endpoint=http://127.0.0.1:8081/v1&model=small"+
			"&endpoint_narration=http://127.0.0.1:8082/v1&model_narration=big"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if ref := got.For(domain.Narration); ref.Endpoint != "http://127.0.0.1:8082/v1" || ref.Model != "big" {
		t.Errorf("narration = %+v, want the 9B on 8082", ref)
	}
	for _, w := range []domain.Workload{domain.Describe, domain.Ask} {
		if ref := got.For(w); ref.Model != "small" {
			t.Errorf("%s = %+v, want the shared model", w, ref)
		}
	}
}

func TestClearingAWorkloadFieldPutsItBackOnTheSharedModel(t *testing.T) {
	// Emptying the box is how a reviewer collapses back to one server. Treating
	// blank as "an override to an empty model" would leave that workload
	// pointing at nothing.
	var got domain.AgentConfig
	h := web.NewServer(testSession(), nil).
		WithConfigure(func(c domain.AgentConfig) error { got = c; return nil })

	req := httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(
		"endpoint=http://a/v1&model=m&endpoint_narration=&model_narration=  "))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got.Split() {
		t.Errorf("got %+v, want no override left", got)
	}
}

func TestTheStatusPageShowsWhichModelAnswersWhat(t *testing.T) {
	// With two models answering, "the model" is no longer a single fact, and a
	// page that shows one of them is actively misleading about the other.
	h := web.NewServer(testSession(), nil).WithAgent(web.AgentStatus{
		Model: "small", Endpoint: "http://127.0.0.1:8081/v1",
		Workloads: []web.WorkloadModel{
			{Workload: "narration", Endpoint: "http://127.0.0.1:8082/v1", Model: "big"},
			{Workload: "describe", Endpoint: "http://127.0.0.1:8081/v1", Model: "small"},
		},
	})

	body := get(t, h, "/settings?s=model").Body.String()
	for _, want := range []string{"narration", "big", "describe", "8082"} {
		if !strings.Contains(body, want) {
			t.Errorf("the status page should name %q:\n%s", want, body)
		}
	}
}

func TestWorkThatArrivedMidReviewIsOfferedAsAChoice(t *testing.T) {
	// The reviewer is reading. The agent keeps working. Silently folding the
	// new work into the page would change what they are judging underneath
	// them; silently withholding it would let them finish a review of
	// something that no longer exists. So it is shown, and they choose
	// (ADR 0020).
	h := web.NewServer(testSession(), nil)
	h.SetPending([]domain.FileStat{
		{Path: "auth/token.go", Added: 6, Removed: 1},
		{Path: "docs/new.md", Added: 3},
	}, domain.SnapshotRef{Commit: "pin"}, domain.SnapshotRef{Commit: "now"}, time.Now())

	body := get(t, h, "/").Body.String()

	if !strings.Contains(body, "2 files changed since you opened this review") {
		t.Errorf("the banner should say what is waiting:\n%s", body)
	}
	// The three ways out, which are the whole point of showing it.
	for _, want := range []string{"/live/include", "/live/split", "data-pending-dismiss"} {
		if !strings.Contains(body, want) {
			t.Errorf("the banner should offer %q", want)
		}
	}
	// And it must name the files, or "2 files" is not a basis for deciding.
	if !strings.Contains(body, "auth/token.go") || !strings.Contains(body, "docs/new.md") {
		t.Error("the banner should name the files that are waiting")
	}
}

func TestAFileAlreadyJudgedIsCalledOutInTheBanner(t *testing.T) {
	// A reviewer who marked a file ok formed that view against a version that
	// no longer exists. Nothing else on the page would tell them.
	sess := testSession()
	sess.Notes = []domain.Note{{ID: "n1", UnitID: "s-f001", Kind: domain.NoteOK, Text: "fine"}}
	h := web.NewServer(sess, nil)

	h.SetPending([]domain.FileStat{{Path: "auth/token.go", Added: 2}},
		domain.SnapshotRef{Commit: "pin"}, domain.SnapshotRef{Commit: "now"}, time.Now())

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "1 you had already annotated") {
		t.Errorf("the banner should say a judgement is now stale:\n%s", body)
	}
}

func TestNothingWaitingShowsNoBanner(t *testing.T) {
	// It is checked every couple of seconds and almost always finds nothing.
	// A banner that is always there is furniture, not a notification.
	h := web.NewServer(testSession(), nil)
	if strings.Contains(get(t, h, "/").Body.String(), "since you opened this review") {
		t.Error("no pending work should mean no banner")
	}
}

func TestPendingIsClearedWhenTheWorkIsTakenIn(t *testing.T) {
	h := web.NewServer(testSession(), nil)
	h.SetPending([]domain.FileStat{{Path: "a.go", Added: 1}},
		domain.SnapshotRef{}, domain.SnapshotRef{}, time.Now())
	if h.Pending().Empty() {
		t.Fatal("precondition: something should be waiting")
	}

	h.ClearPending()

	if !h.Pending().Empty() {
		t.Error("taking the work in should leave nothing waiting")
	}
	if strings.Contains(get(t, h, "/").Body.String(), "since you opened this review") {
		t.Error("the banner should be gone")
	}
}

func TestIncludingTheWaitingWorkExtendsTheReview(t *testing.T) {
	var asked string
	h := web.NewServer(testSession(), nil).
		WithLiveActions(
			func(_ context.Context, id string) error { asked = id; return nil },
			nil)
	h.SetPending([]domain.FileStat{{Path: "a.go", Added: 1}},
		domain.SnapshotRef{}, domain.SnapshotRef{}, time.Now())

	req := httptest.NewRequest(http.MethodPost, "/live/include", strings.NewReader("target=s"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /live/include = %d, want 303", rec.Code)
	}
	if asked != "s" {
		t.Errorf("extended %q, want the open review", asked)
	}
	// The work is no longer waiting: it is what you are now reading.
	if !h.Pending().Empty() {
		t.Error("including the work should stop it waiting")
	}
}

func TestReviewingOnlyTheNewWorkOpensThatRange(t *testing.T) {
	// The third choice: leave this review as it stands and go look at just what
	// arrived. That is an ordinary range target, which is why it can be opened
	// like anything else.
	h := web.NewServer(testSession(), nil).
		WithLiveActions(nil,
			func(context.Context, string) (string, error) { return "since-you-looked", nil })
	h.SetPending([]domain.FileStat{{Path: "a.go", Added: 1}},
		domain.SnapshotRef{}, domain.SnapshotRef{}, time.Now())

	req := httptest.NewRequest(http.MethodPost, "/live/split", strings.NewReader("target=s"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /live/split = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); !strings.Contains(got, "since-you-looked") {
		t.Errorf("Location = %q, want the new range", got)
	}
	if !h.Pending().Empty() {
		t.Error("moving to the new work should stop it waiting")
	}
}

func TestAFailedIncludeLeavesTheWorkWaiting(t *testing.T) {
	// If it could not be done, the reviewer must still be able to see the
	// choice — silently clearing it would lose the notification entirely.
	h := web.NewServer(testSession(), nil).
		WithLiveActions(func(context.Context, string) error {
			return errors.New("git is busy")
		}, nil)
	h.SetPending([]domain.FileStat{{Path: "a.go", Added: 1}},
		domain.SnapshotRef{}, domain.SnapshotRef{}, time.Now())

	req := httptest.NewRequest(http.MethodPost, "/live/include", strings.NewReader("target=s"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if h.Pending().Empty() {
		t.Error("a failed include must leave the choice on the page")
	}
}

func TestFinishingAReviewRecordsItWithAComment(t *testing.T) {
	// Notes answer "what do I think of this file". Nothing answered "am I done
	// with this, and what is my overall view" — which is the question you have
	// on reopening something you looked at yesterday (ADR 0021).
	var got domain.Signoff
	h := web.NewServer(testSession(), nil).
		WithSignoff(func(_ context.Context, v domain.Signoff) error { got = v; return nil },
			func(string) domain.Signoff { return domain.Signoff{} })

	req := httptest.NewRequest(http.MethodPost, "/review/signoff",
		strings.NewReader("target=s&comment=happy+with+this%3B+retry+loop+needs+a+follow-up"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /review/signoff = %d, want 303", rec.Code)
	}
	if got.TargetID != "s" {
		t.Errorf("signed off %q, want the open review", got.TargetID)
	}
	if got.Comment != "happy with this; retry loop needs a follow-up" {
		t.Errorf("comment = %q", got.Comment)
	}
	if got.At.IsZero() {
		t.Error("a sign-off has to say when")
	}
	// It must capture what the review looked like, or reopening cannot tell
	// whether the code moved underneath the judgement.
	if got.Files != 2 {
		t.Errorf("files = %d, want the 2 in this review", got.Files)
	}
	if got.Print == "" {
		t.Error("a sign-off should fingerprint what was reviewed")
	}
}

func TestBeingDoneIsWorthRecordingWithNothingToAdd(t *testing.T) {
	var got domain.Signoff
	h := web.NewServer(testSession(), nil).
		WithSignoff(func(_ context.Context, v domain.Signoff) error { got = v; return nil },
			func(string) domain.Signoff { return domain.Signoff{} })

	req := httptest.NewRequest(http.MethodPost, "/review/signoff", strings.NewReader("target=s&comment="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !got.Done() {
		t.Error("an empty comment is still a sign-off")
	}
}

func TestAReviewedTargetSaysSoWhenReopened(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithSignoff(nil, func(string) domain.Signoff {
			return domain.Signoff{
				TargetID: "s", At: time.Now().Add(-2 * time.Hour),
				Comment: "shipped it", Print: "same", Files: 2,
			}
		})

	body := get(t, h, "/").Body.String()

	if !strings.Contains(body, "reviewed 2h ago") {
		t.Errorf("a finished review should say so:\n%s", body)
	}
	if !strings.Contains(body, "shipped it") {
		t.Error("the closing comment should come back with it")
	}
}

func TestAReviewSignedOffBeforeTheCodeMovedIsQualified(t *testing.T) {
	// The same discipline as the pending banner, one level up: "reviewed" about
	// something that has changed since would be a lie.
	h := web.NewServer(testSession(), nil).
		WithSignoff(nil, func(string) domain.Signoff {
			return domain.Signoff{
				TargetID: "s", At: time.Now().Add(-time.Hour),
				Print: "an-old-print", Files: 1,
			}
		})

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "but it has changed since") {
		t.Errorf("a stale sign-off must be qualified:\n%s", body)
	}
}

func TestAnUnfinishedReviewOffersTheButton(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithSignoff(func(context.Context, domain.Signoff) error { return nil },
			func(string) domain.Signoff { return domain.Signoff{} })

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "/review/signoff") {
		t.Errorf("there should be a way to finish a review:\n%s", body)
	}
}

func TestThePickerMarksWhatHasAlreadyBeenReviewed(t *testing.T) {
	// What is left to look at should be readable without opening anything.
	h := web.NewServer(testSession(), nil).WithTargets([]web.TargetSummary{
		{ID: "a", Ref: "v1.0.0", Kind: domain.TargetTag, Title: "v1.0.0", Reviewed: true},
		{ID: "b", Ref: "abc12345", Kind: domain.TargetCommit, Title: "still open"},
	})

	body := get(t, h, "/").Body.String()

	if !strings.Contains(body, "✓ tag · v1.0.0") {
		t.Errorf("a signed-off target should be ticked in the picker:\n%s", body)
	}
	if strings.Contains(body, "✓ commit · still open") {
		t.Error("an unreviewed target must not be ticked")
	}
}

func TestKeyboardNavigationIsServedAndWired(t *testing.T) {
	// Reading is the common case in the cockpit and was the one thing that
	// needed a mouse (ADR 0022).
	h := web.NewServer(testSession(), nil)

	page := get(t, h, "/").Body.String()
	if !strings.Contains(page, "/assets/keys.js") {
		t.Error("the cockpit should load the keyboard bindings")
	}

	asset := get(t, h, "/assets/keys.js")
	if asset.Code != http.StatusOK {
		t.Fatalf("GET /assets/keys.js = %d, want 200", asset.Code)
	}
	body := asset.Body.String()
	// The bindings that move between reviews and repositories are the ones that
	// cannot be discovered by clicking, so they are the ones worth pinning.
	for _, want := range []string{"stepReview", "stepRepo", "keysheet"} {
		if !strings.Contains(body, want) {
			t.Errorf("keys.js should define %q", want)
		}
	}
}

func TestThePickerCarriesWhatNavigationNeeds(t *testing.T) {
	// Switching repository walks the picker's own option list, so there is one
	// source of truth for what exists rather than two that can drift.
	h := web.NewServer(testSession(), nil).WithTargets([]web.TargetSummary{
		{ID: "a", Ref: "v1", Kind: domain.TargetTag, Title: "v1", Repo: "api"},
		{ID: "b", Ref: "v2", Kind: domain.TargetTag, Title: "v2", Repo: "web"},
	})

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, `data-repo="api"`) || !strings.Contains(body, `data-repo="web"`) {
		t.Errorf("each option should name its repository:\n%s", body)
	}
}

func TestDescribedCountsGroupsAndNotEveryMeaningStored(t *testing.T) {
	// Group descriptions and per-file descriptions live in the same map, keyed
	// differently. Counting the map counted both, so describing files pushed
	// the card to "8/4 described" — a ratio that cannot be true.
	sess := testSession()
	sess.Narrative = domain.Narrative{
		Source: domain.NarrativeModel,
		Chapters: []domain.Chapter{
			{Title: "auth", UnitIDs: []string{"s-f001"}},
			{Title: "http", UnitIDs: []string{"s-f002"}},
		},
		Meanings: map[string]string{
			// two groups...
			"auth": "what the auth change is for",
			"http": "what the middleware change is for",
			// ...and per-file descriptions, which are not groups.
			"file:auth/token.go":      "what this file does",
			"file:http/middleware.go": "what this one does",
		},
	}
	h := web.NewServer(sess, nil)

	body := get(t, h, "/").Body.String()

	if strings.Contains(body, "4/2 described") {
		t.Error("per-file descriptions must not be counted as groups")
	}
	if !strings.Contains(body, "described") {
		t.Fatalf("the card should report progress:\n%s", body)
	}
}

func TestTheRepositoryRootIsNamedNotPunctuated(t *testing.T) {
	// Files at the repository root group under ".", which renders as a lone
	// full stop beside real directory names — it reads as a rendering fault
	// rather than as a place.
	sess := testSession()
	sess.Units = []domain.Unit{
		{ID: "s-f001", Files: []string{"go.mod"},
			Headline: domain.Headline{Text: "added a dependency"}},
	}
	sess.Diffs = map[string]domain.Diff{"s-f001": {Text: "@@\n+require x\n"}}

	body := get(t, web.NewServer(sess, nil), "/").Body.String()

	if !strings.Contains(body, `class="group__dir">root<`) {
		t.Errorf("the repository root should be named:\n%s",
			body[max(0, strings.Index(body, "group__dir")-100):])
	}
}

func TestALongCommitSubtitleDoesNotOverflowItsTile(t *testing.T) {
	// The tiles are small and fixed; a commit subtitle is a hash plus an author
	// name of unbounded length, and it was wrapping to three lines and spilling.
	sess := testSession()
	sess.Target = domain.Target{Kind: domain.TargetCommit,
		Subtitle: "01ce10d0 · A Developer With A Very Long Name Indeed"}
	h := web.NewServer(sess, nil).WithStats(domain.SessionStats{Files: 1})

	body := get(t, h, "/").Body.String()

	// The panel may show the subtitle in full; the tile shows the hash alone.
	tile := body[strings.Index(body, "cockpit__stats"):]
	if strings.Contains(tile, "A Developer With A Very Long Name Indeed") {
		t.Errorf("the author does not belong in the tile:\n%s", tile[:400])
	}
	if !strings.Contains(tile, "01ce10d0") {
		t.Error("the tile should still identify the commit")
	}
}

func TestTheTutorialExplainsThePageToSomeoneNew(t *testing.T) {
	// msr opens on a page with three columns, a picker, flags and a keyboard
	// nobody has been told about. Everything it does is discoverable by
	// clicking around for twenty minutes; the tour is for the first five.
	h := web.NewServer(testSession(), nil)

	rec := get(t, h, "/tutorial")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tutorial = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// The four things someone has to do, in order.
	for _, want := range []string{
		"Pick what to review",
		"ask it to read",
		"security pass",
		"then annotate",
		"Mark it reviewed",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the tour should cover %q", want)
		}
	}
	// And the keys, which are the least discoverable part.
	for _, want := range []string{">j<", ">k<", "⌘K"} {
		if !strings.Contains(body, want) {
			t.Errorf("the tour should list %q", want)
		}
	}
	// It has to lead back to work rather than being a dead end.
	if !strings.Contains(body, `href="/"`) {
		t.Error("the tour should offer a way back to the cockpit")
	}
}

func TestEveryPageOffersTheTour(t *testing.T) {
	// A tour nobody can find is not a tour.
	h := web.NewServer(testSession(), nil)
	for _, page := range []string{"/", "/settings", "/activity"} {
		if !strings.Contains(get(t, h, page).Body.String(), "/tutorial") {
			t.Errorf("%s should link to the tour", page)
		}
	}
}

func TestTheAnalysisCardsAreOfferedAndRunOnDemand(t *testing.T) {
	// Three readings of the same change, each run when the reviewer asks for it
	// and never before: a model call is slow and costs something, so nothing
	// here happens by itself (ADR 0024).
	h := web.NewServer(testSession(), nil).
		WithAnalyses(
			func(context.Context, string, domain.AnalysisKind) error { return nil },
			func(string, domain.AnalysisKind) domain.Analysis { return domain.Analysis{} })

	body := get(t, h, "/").Body.String()

	for _, want := range []string{"Security pass", "Breaking changes"} {
		if !strings.Contains(body, want) {
			t.Errorf("the cards should offer %q:\n%s", want, body)
		}
	}
	for _, want := range []string{"/analysis/security", "/analysis/breaking"} {
		if !strings.Contains(body, want) {
			t.Errorf("there should be a way to run %q", want)
		}
	}
	// Never run is a state the card has to show, or a reviewer cannot tell it
	// apart from one that found nothing.
	if !strings.Contains(body, "not run yet") {
		t.Error("an audit nobody has run should say so")
	}
}

func TestRunningOneAuditAsksForThatOneOnly(t *testing.T) {
	// The request returns at once and the model is asked in the background: a
	// local audit takes seconds, and a browser must not sit on it.
	ran := make(chan domain.AnalysisKind, 4)
	h := web.NewServer(testSession(), nil).
		WithAnalyses(
			func(_ context.Context, _ string, k domain.AnalysisKind) error {
				ran <- k
				return nil
			},
			func(string, domain.AnalysisKind) domain.Analysis { return domain.Analysis{} })

	req := httptest.NewRequest(http.MethodPost, "/analysis/security", strings.NewReader("target=s"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /analysis/security = %d, want 303", rec.Code)
	}

	select {
	case got := <-ran:
		if got != usecase.AuditSecurity {
			t.Errorf("ran %q, want the security audit", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the audit was never run")
	}
	// ...and nothing else was set going with it.
	select {
	case also := <-ran:
		t.Errorf("running one audit also ran %q", also)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAnUnknownAuditIsNotFound(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithAnalyses(
			func(context.Context, string, domain.AnalysisKind) error {
				t.Error("an audit that does not exist must not be run")
				return nil
			},
			func(string, domain.AnalysisKind) domain.Analysis { return domain.Analysis{} })

	req := httptest.NewRequest(http.MethodPost, "/analysis/astrology", strings.NewReader("target=s"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /analysis/astrology = %d, want 404", rec.Code)
	}
}

func TestACleanAuditReadsAsAnAnswer(t *testing.T) {
	// The common result. It has to look like the audit ran and found nothing,
	// not like it failed to run.
	h := web.NewServer(testSession(), nil).
		WithAnalyses(nil, func(_ string, k domain.AnalysisKind) domain.Analysis {
			if k != usecase.AuditSecurity {
				return domain.Analysis{}
			}
			return domain.Analysis{
				TargetID: "s", Kind: k, At: time.Now().Add(-3 * time.Minute),
				Verdict: "Nothing here worth a second look.",
				// The fingerprint of what is actually on screen, so this reads
				// as current rather than as a stale run.
				Print: usecase.Fingerprint(testSession().Units),
			}
		})

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "Nothing here worth a second look.") {
		t.Errorf("a clean verdict should be shown:\n%s", body)
	}
	if !strings.Contains(body, "clean") {
		t.Error("a clean audit should be marked as such")
	}
}

func TestFindingsAreShownWithTheirFile(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithAnalyses(nil, func(_ string, k domain.AnalysisKind) domain.Analysis {
			if k != usecase.AuditBreaking {
				return domain.Analysis{}
			}
			return domain.Analysis{
				TargetID: "s", Kind: k, At: time.Now(),
				Verdict: "One exported signature changed.",
				Findings: []domain.Finding{
					{File: "api/handler.go", Note: "Routes now requires a Validator; existing callers will not compile."},
				},
				Print: usecase.Fingerprint(testSession().Units),
			}
		})

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "api/handler.go") {
		t.Error("a finding should name its file")
	}
	if !strings.Contains(body, "existing callers will not compile") {
		t.Error("a finding should say what it found")
	}
	// The honesty label: a model reading a diff is suggesting, not ruling.
	if !strings.Contains(body, "inferred") {
		t.Error("findings are model-inferred and should say so")
	}
}

func TestAnAuditRunBeforeTheCodeMovedSaysSo(t *testing.T) {
	// Same discipline as a sign-off: a reading of a version that no longer
	// exists must not present itself as current (ADR 0021).
	h := web.NewServer(testSession(), nil).
		WithAnalyses(nil, func(_ string, k domain.AnalysisKind) domain.Analysis {
			return domain.Analysis{
				TargetID: "s", Kind: k, At: time.Now().Add(-time.Hour),
				Verdict: "Nothing found.", Print: "a-stale-print",
			}
		})

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "changed since") {
		t.Errorf("a stale audit must be qualified:\n%s", body)
	}
}

func TestAFailedAuditSaysSoRatherThanLookingUnrun(t *testing.T) {
	// An audit that could not run must never look like one nobody has started,
	// and above all must never look clean.
	h := web.NewServer(testSession(), nil).
		WithAnalyses(
			func(context.Context, string, domain.AnalysisKind) error {
				return errors.New("the model refused")
			},
			func(string, domain.AnalysisKind) domain.Analysis { return domain.Analysis{} })

	req := httptest.NewRequest(http.MethodPost, "/analysis/security", strings.NewReader("target=s"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	deadline := time.Now().Add(5 * time.Second)
	for {
		body := get(t, h, "/").Body.String()
		if strings.Contains(body, "the model refused") {
			return // it said what went wrong
		}
		if time.Now().After(deadline) {
			t.Fatalf("a failed audit should say why:\n%s", body)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func TestAnAuditCanBeAskedForByRefNotOnlyByID(t *testing.T) {
	// Every link on the page carries a ref — /?target=v5.4.0 — so the audit
	// endpoint has to speak the same language as the rest of the app.
	ran := make(chan string, 2)
	sess := testSession()
	h := web.NewServer(sess, nil).
		WithTargets([]web.TargetSummary{
			{ID: "s", Ref: "abc12345", Kind: domain.TargetCommit, Title: "a commit"},
		}).
		WithAnalyses(
			func(_ context.Context, target string, _ domain.AnalysisKind) error {
				ran <- target
				return nil
			},
			func(string, domain.AnalysisKind) domain.Analysis { return domain.Analysis{} })

	req := httptest.NewRequest(http.MethodPost, "/analysis/security?target=abc12345",
		strings.NewReader("target=abc12345"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	select {
	case got := <-ran:
		if got != "s" {
			t.Errorf("audited %q, want the ref resolved to its id", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the audit never ran")
	}
}

func TestAnActionOnAnUnknownTargetRefusesRatherThanGuessing(t *testing.T) {
	// Falling back to whatever is open is right when *rendering* a stale link:
	// it beats a dead end. It is wrong for an action. Auditing, or signing off,
	// something other than what was named is worse than doing nothing, and it
	// is invisible — the result lands under a review nobody was looking at.
	ran := make(chan string, 2)
	h := web.NewServer(testSession(), nil).
		WithTargets([]web.TargetSummary{
			{ID: "s", Ref: "abc12345", Kind: domain.TargetCommit, Title: "a commit"},
		}).
		WithAnalyses(
			func(_ context.Context, target string, _ domain.AnalysisKind) error {
				ran <- target
				return nil
			},
			func(string, domain.AnalysisKind) domain.Analysis { return domain.Analysis{} })

	req := httptest.NewRequest(http.MethodPost, "/analysis/security?target=deadbeef",
		strings.NewReader("target=deadbeef"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("POST with an unknown target = %d, want 404", rec.Code)
	}
	select {
	case got := <-ran:
		t.Errorf("it audited %q instead of refusing", got)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSigningOffAnUnknownTargetAlsoRefuses(t *testing.T) {
	signed := make(chan string, 2)
	h := web.NewServer(testSession(), nil).
		WithTargets([]web.TargetSummary{
			{ID: "s", Ref: "abc12345", Kind: domain.TargetCommit, Title: "a commit"},
		}).
		WithSignoff(func(_ context.Context, v domain.Signoff) error {
			signed <- v.TargetID
			return nil
		}, func(string) domain.Signoff { return domain.Signoff{} })

	req := httptest.NewRequest(http.MethodPost, "/review/signoff?target=deadbeef",
		strings.NewReader("target=deadbeef&comment=fine"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("POST with an unknown target = %d, want 404", rec.Code)
	}
	select {
	case got := <-signed:
		t.Errorf("it signed off %q instead of refusing", got)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestACardIsColouredByItsWorstFinding(t *testing.T) {
	// A row of cards should be readable at a glance: which one has the thing
	// that would stop a merge, without opening any of them.
	h := web.NewServer(testSession(), nil).
		WithAnalyses(nil, func(_ string, k domain.AnalysisKind) domain.Analysis {
			if k != usecase.AuditSecurity {
				return domain.Analysis{}
			}
			return domain.Analysis{
				TargetID: "s", Kind: k, At: time.Now(), Verdict: "Two things.",
				Findings: []domain.Finding{
					{File: "a.go", Note: "worth knowing", Severity: domain.SeverityLow},
					{File: "b.go", Note: "secret committed", Severity: domain.SeverityHigh},
				},
				Print: usecase.Fingerprint(testSession().Units),
			}
		})

	body := get(t, h, "/").Body.String()

	if !strings.Contains(body, "acard--worst-high") {
		t.Error("the card should take the colour of its worst finding")
	}
	// The counts say more than a bare total in the same space.
	if !strings.Contains(body, "1 high · 1 low") {
		t.Errorf("the card should tally by severity:\n%s", body)
	}
	// And the severity is labelled as inferred like everything else the model
	// says, or three tidy labels read as a judgement nobody made.
	if !strings.Contains(body, "including how bad") {
		t.Error("the severity should be marked as inferred too")
	}
}

func TestTheLogCardShowsHistoryAndWhereYouAre(t *testing.T) {
	// "Where am I against everything that has landed" (issue #18).
	now := time.Now()
	h := web.NewServer(testSession(), nil).WithLog(func(_, _ string) web.LogView {
		return web.LogView{
			Entries: []usecase.LogEntry{
				{Commit: domain.Commit{Hash: "ccccccccccccc", Subject: "Alice: add the cache", Author: "Alice", TS: now},
					Ref: "cccccccc", Ago: "2m ago", OnRemote: true},
				{Commit: domain.Commit{Hash: "bbbbbbbbbbbbb", Subject: "Fix the parser", Author: "You", TS: now},
					Ref: "bbbbbbbb", Ago: "1h ago", Reviewing: true},
				{Commit: domain.Commit{Hash: "aaaaaaaaaaaaa", Subject: "Scaffold", Author: "You", TS: now},
					Ref: "aaaaaaaa", Ago: "2h ago", SignedOff: true, OnRemote: true},
			},
			Remote: domain.RemoteState{Branch: "main", Upstream: "origin/main", Behind: 2, Ahead: 1},
		}
	})

	body := get(t, h, "/").Body.String()

	for _, want := range []string{"Alice: add the cache", "Fix the parser", "Scaffold"} {
		if !strings.Contains(body, want) {
			t.Errorf("the log should list %q", want)
		}
	}
	// Every row opens that commit.
	if !strings.Contains(body, "/?target=cccccccc") {
		t.Error("a log row should link to its commit")
	}
	// The three markers that make it more than a list.
	if !strings.Contains(body, "log__row--here") {
		t.Error("the log should mark where the reviewer is")
	}
	if !strings.Contains(body, "log__row--signed") {
		t.Error("the log should mark what has been reviewed")
	}
	if !strings.Contains(body, "log__local") {
		t.Error("the log should mark what has not left this machine")
	}
	// And where the branch stands.
	if !strings.Contains(body, "origin/main") || !strings.Contains(body, "2 behind") {
		t.Errorf("the log should say where the branch sits:\n%s", body)
	}
}

func TestNoLogCardWithoutAWayToBuildIt(t *testing.T) {
	if strings.Contains(get(t, web.NewServer(testSession(), nil), "/").Body.String(), "log__row") {
		t.Error("no log source should mean no log card")
	}
}

func TestTheBranchesPageShowsWhatTheTeamIsWorkingOn(t *testing.T) {
	now := time.Now()
	h := web.NewServer(testSession(), nil).WithBranches(func(string) web.BranchView {
		return web.BranchView{
			Base: "origin/main",
			Branches: []domain.Branch{
				{Name: "origin/feature-x", Short: "feature-x", Subject: "Alice: finish the cache",
					Author: "Alice", TS: now.Add(-30 * time.Minute), Ahead: 2, Behind: 1, Base: "origin/main"},
				{Name: "origin/already-done", Short: "already-done", Subject: "old work",
					Author: "Bob", TS: now.Add(-72 * time.Hour), Merged: true, Base: "origin/main"},
			},
		}
	})

	rec := get(t, h, "/branches")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /branches = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	if !strings.Contains(body, "feature-x") || !strings.Contains(body, "Alice: finish the cache") {
		t.Errorf("the page should list the branches:\n%s", body)
	}
	// Reviewing a colleague's branch is the range base..branch, which is an
	// ordinary comparison — so the row opens a real review.
	// The range is base..branch. A slash is legal unencoded in a query value,
	// so this checks the link, not one particular escaping of it.
	link := regexp.MustCompile(`(?i)href="/compare\?[^"]*from=origin(/|%2f)main[^"]*to=origin(/|%2f)feature-x`)
	if !link.MatchString(body) {
		t.Errorf("a branch row should open a review of what is on it:\n%s", body)
	}
	// Merged branches are there but not competing for attention.
	if !strings.Contains(body, "branch--merged") {
		t.Error("a merged branch should be marked as having nothing left to review")
	}
	if !strings.Contains(body, "2 ahead") {
		t.Error("the page should say how much there would be to review")
	}
}

func TestNoBranchesPageWithoutARemote(t *testing.T) {
	h := web.NewServer(testSession(), nil)
	if rec := get(t, h, "/branches"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /branches = %d with no source, want 404", rec.Code)
	}
}

func TestWatchingTheRemoteIsToggledFromTheStatusPage(t *testing.T) {
	// A setting you can only change by restarting is not a setting on a status
	// page (ADR 0026).
	var got struct {
		on    bool
		every time.Duration
	}
	h := web.NewServer(testSession(), nil).
		WithRemoteWatch(
			func() (bool, time.Duration) { return false, 2 * time.Minute },
			func(on bool, every time.Duration) error {
				got.on, got.every = on, every
				return nil
			})

	if !strings.Contains(get(t, h, "/settings?s=remote").Body.String(), `action="/remote"`) {
		t.Fatal("the status page should offer the remote watch")
	}

	req := httptest.NewRequest(http.MethodPost, "/remote",
		strings.NewReader("watch=on&every=45s"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /remote = %d, want 303", rec.Code)
	}
	if !got.on || got.every != 45*time.Second {
		t.Errorf("set %+v, want it on every 45s", got)
	}
}

func TestTurningTheRemoteWatchOff(t *testing.T) {
	// An unchecked checkbox sends nothing at all, so "absent" has to mean off
	// rather than "leave it as it was" — otherwise it can never be turned off.
	on := true
	h := web.NewServer(testSession(), nil).
		WithRemoteWatch(
			func() (bool, time.Duration) { return true, time.Minute },
			func(want bool, _ time.Duration) error { on = want; return nil })

	req := httptest.NewRequest(http.MethodPost, "/remote", strings.NewReader("every=1m"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if on {
		t.Error("submitting the form with the box unchecked should turn it off")
	}
}

func TestTheStatusPageSaysWhetherItIsFetching(t *testing.T) {
	// Fetching writes to the repository and talks to the network. Whether it is
	// happening must be visible, not buried in how the process was started.
	h := web.NewServer(testSession(), nil).
		WithRemoteWatch(func() (bool, time.Duration) { return true, 90 * time.Second }, nil)

	body := get(t, h, "/settings?s=remote").Body.String()
	if !strings.Contains(body, "every 1m 30s") && !strings.Contains(body, "1m30s") {
		t.Errorf("the page should say how often it fetches:\n%s", body)
	}
}

func TestTheLogFollowsTheRepositoryBeingReviewed(t *testing.T) {
	// A workspace spans repositories, and the picker moves between them. A
	// history card that keeps showing the repository msr started in would be
	// showing somebody else's commits under this one's name.
	asked := make(chan string, 4)
	other := web.Session{ID: "other", Repo: "api",
		Units: []domain.Unit{{ID: "other-f001", Files: []string{"api/x.go"}}}}

	h := web.NewServer(testSession(), nil).
		WithLoader(func(context.Context, string) (web.Session, error) { return other, nil }).
		WithTargets([]web.TargetSummary{
			{ID: "other", Ref: "otherref", Kind: domain.TargetCommit, Title: "in the api repo"},
		}).
		WithLog(func(targetID, ref string) web.LogView {
			asked <- targetID
			return web.LogView{}
		})

	get(t, h, "/?target=otherref")

	select {
	case got := <-asked:
		if got != "other" {
			t.Errorf("the log was built for %q, want the review being read", got)
		}
	default:
		t.Fatal("the log was never built")
	}
}

func TestAConversationBelongsToTheReviewItWasAbout(t *testing.T) {
	// The invariant written above AskFunc: asking a question while looking at
	// one target and being answered about another is worse than not answering.
	// The thread was one flat list, so questions about one review appeared
	// under another — and were handed to the model as history for it.
	var gotHistory []web.Exchange
	other := web.Session{ID: "other", Prompt: "the other review"}

	h := web.NewServer(testSession(), nil).
		WithLoader(func(context.Context, string) (web.Session, error) { return other, nil }).
		WithExchanges(nil, []domain.Exchange{
			{ID: "e1", SessionID: "s", Question: "why is this retried?", Answer: "because 503s"},
			{ID: "e2", SessionID: "other", Question: "what is this for?", Answer: "the api"},
		}).
		WithAsk(func(_ context.Context, _, _ string, history []web.Exchange) (string, error) {
			gotHistory = history
			return "an answer", nil
		})

	// The review it was asked about shows its own conversation, and not the
	// other one's.
	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "why is this retried?") {
		t.Error("a review should show the questions asked about it")
	}
	if strings.Contains(body, "what is this for?") {
		t.Error("a review must not show another review's conversation")
	}

	// ...and neither does the model.
	req := httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader("question=and+now"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	for _, e := range gotHistory {
		if e.SessionID != "s" {
			t.Errorf("the model was given %q from another review as history", e.Question)
		}
	}
	if len(gotHistory) != 1 {
		t.Errorf("history = %d exchanges, want only this review's", len(gotHistory))
	}
}

func TestAnAnswerIsFiledUnderTheReviewItWasAsked(t *testing.T) {
	h := web.NewServer(testSession(), nil).
		WithExchanges(nil, nil).
		WithAsk(func(context.Context, string, string, []web.Exchange) (string, error) {
			return "an answer", nil
		})

	req := httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader("question=why"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !strings.Contains(get(t, h, "/").Body.String(), "why") {
		t.Error("the review it was asked about should show it")
	}
}

func TestAConversationComesBackWhenYouReturnToItsReview(t *testing.T) {
	// Exchanges were persisted and then only ever shown for whichever review
	// was open at start-up. Coming back to a review you asked questions about
	// showed an empty box, which reads as though nothing was ever asked.
	loaded := 0
	h := web.NewServer(testSession(), nil).
		WithLoader(func(context.Context, string) (web.Session, error) {
			return web.Session{ID: "older", Prompt: "a review from yesterday"}, nil
		}).
		WithTargets([]web.TargetSummary{
			{ID: "older", Ref: "olderref", Kind: domain.TargetCommit, Title: "yesterday"},
		}).
		WithConversations(func(targetID string) []domain.Exchange {
			loaded++
			if targetID != "older" {
				return nil
			}
			return []domain.Exchange{
				{ID: "e9", SessionID: "older", Question: "why the retry loop?", Answer: "503s"},
			}
		})

	body := get(t, h, "/?target=olderref").Body.String()

	if !strings.Contains(body, "why the retry loop?") {
		t.Errorf("returning to a review should bring its conversation back:\n%s", body)
	}

	// ...and it is not re-read from the store on every page load.
	before := loaded
	get(t, h, "/?target=olderref")
	if loaded != before {
		t.Errorf("the conversation was loaded again; it should be remembered")
	}
}

func TestHiddenFilesAreAnnouncedNotJustGone(t *testing.T) {
	// The whole safety argument for this feature: a review tool may leave files
	// out only if it says so, says why, and can show them (ADR 0027).
	sess := testSession()
	sess.Hidden = []usecase.Hidden{
		{Path: "go.sum", Pattern: "go.sum"},
		{Path: "vendor/x/lib.go", Pattern: "vendor/"},
	}
	h := web.NewServer(sess, nil)

	body := get(t, h, "/").Body.String()

	if !strings.Contains(body, "2 files hidden") {
		t.Errorf("the page should say how many were left out:\n%s", body)
	}
	for _, want := range []string{"go.sum", "vendor/", ".msrignore"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page should name %q", want)
		}
	}
	// ...and offer to show them.
	if !strings.Contains(body, "all=1") {
		t.Error("there should be a way to see them anyway")
	}
}

func TestAskingForEverythingShowsTheHiddenFiles(t *testing.T) {
	var wantedAll bool
	sess := testSession()
	h := web.NewServer(sess, nil).WithShowAll(func(all bool) { wantedAll = all })

	get(t, h, "/?all=1")

	if !wantedAll {
		t.Error("?all=1 should ask for the hidden files back")
	}
}

func TestNoBannerWhenNothingIsHidden(t *testing.T) {
	// The common case. A banner that is always there is furniture.
	if strings.Contains(get(t, web.NewServer(testSession(), nil), "/").Body.String(), "files hidden") {
		t.Error("nothing hidden should mean no banner")
	}
}

func TestTheReviewLogCanBeTakenOutOfTheApp(t *testing.T) {
	// "The review log is the product" is stated in three places, and the only
	// way to get it out was a separate CLI invocation against a session id
	// (issue #19).
	sess := testSession()
	sess.Notes = []domain.Note{
		{ID: "n1", UnitID: "s-f001", Kind: domain.NoteObjection, Text: "this retries forever"},
	}
	h := web.NewServer(sess, nil)

	if !strings.Contains(get(t, h, "/").Body.String(), "/export") {
		t.Error("the cockpit should offer the review log")
	}

	for _, tc := range []struct{ format, contentType, want string }{
		{"md", "text/markdown", "this retries forever"},
		{"json", "application/json", `"objection"`},
		{"slack", "text/plain", "this retries forever"},
	} {
		rec := get(t, h, "/export?format="+tc.format)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /export?format=%s = %d", tc.format, rec.Code)
			continue
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, tc.contentType) {
			t.Errorf("%s Content-Type = %q, want %q", tc.format, got, tc.contentType)
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s export missing %q:\n%s", tc.format, tc.want, rec.Body.String())
		}
		// It is a file you keep, so it arrives as one with a name.
		if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
			t.Errorf("%s should download, got Content-Disposition %q", tc.format, cd)
		}
	}
}

func TestExportDefaultsToMarkdownAndRefusesNonsense(t *testing.T) {
	h := web.NewServer(testSession(), nil)

	if got := get(t, h, "/export").Header().Get("Content-Type"); !strings.HasPrefix(got, "text/markdown") {
		t.Errorf("no format should mean markdown, got %q", got)
	}
	if rec := get(t, h, "/export?format=parquet"); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown format = %d, want 404", rec.Code)
	}
}

func TestExportIsOfTheReviewYouAreLookingAt(t *testing.T) {
	// Exporting one review while looking at another is the same class of
	// mistake as auditing the wrong one.
	other := testSession()
	other.ID = "other"
	other.Notes = []domain.Note{
		{ID: "n2", UnitID: "s-f001", Kind: domain.NoteOK, Text: "a note from the other review"},
	}
	h := web.NewServer(testSession(), nil).
		WithLoader(func(context.Context, string) (web.Session, error) { return other, nil }).
		WithTargets([]web.TargetSummary{
			{ID: "other", Ref: "otherref", Kind: domain.TargetCommit, Title: "other"},
		})

	body := get(t, h, "/export?target=otherref&format=md").Body.String()
	if !strings.Contains(body, "a note from the other review") {
		t.Errorf("the export should be of the review asked for:\n%s", body)
	}
}

func TestAnnotatingAReviewOtherThanTheOpenOneShowsThere(t *testing.T) {
	// Notes appended to whichever review msr started with, whatever was being
	// annotated: so a note on a commit vanished on the redirect, and turned up
	// on an unrelated review instead. Annotations are the product.
	other := web.Session{
		ID: "other", Prompt: "another review",
		Units: []domain.Unit{{ID: "other-f001", SessionID: "other", Files: []string{"api/x.go"}}},
		Diffs: map[string]domain.Diff{"other-f001": {Text: "@@\n+x\n"}},
	}
	kept := &recordingNotes{}
	h := web.NewServer(testSession(), kept).
		WithLoader(func(context.Context, string) (web.Session, error) { return other, nil }).
		WithTargets([]web.TargetSummary{
			{ID: "other", Ref: "otherref", Kind: domain.TargetCommit, Title: "other"},
		})

	req := httptest.NewRequest(http.MethodPost, "/units/other-f001/notes?target=otherref",
		strings.NewReader("kind=objection&text=this+changes+the+signature"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	// It is written against the review it was made on...
	if len(kept.notes) != 1 || kept.notes[0].SessionID != "other" {
		t.Fatalf("stored %+v, want one note against \"other\"", kept.notes)
	}
	// ...shows on that review...
	if !strings.Contains(get(t, h, "/?target=otherref").Body.String(), "this changes the signature") {
		t.Error("the note should show on the review it was made against")
	}
	// ...and not on the one msr happened to start with.
	if strings.Contains(get(t, h, "/").Body.String(), "this changes the signature") {
		t.Error("the note must not appear on an unrelated review")
	}
}

type recordingNotes struct{ notes []domain.Note }

func (r *recordingNotes) AppendNote(n domain.Note) error {
	r.notes = append(r.notes, n)
	return nil
}

func TestALineCanBeAnnotatedAndTheNoteShowsUnderIt(t *testing.T) {
	// Real review happens on lines. This was the widest gap between msr and
	// what a reviewer expects (ADR 0028).
	sess := testSession()
	sess.Notes = []domain.Note{
		{ID: "n1", UnitID: "s-f001", Kind: domain.NoteQuestion,
			Anchor: "+new body", Text: "why replace the whole body?"},
	}
	h := web.NewServer(sess, nil)

	body := get(t, h, "/").Body.String()

	// The note renders against its line rather than in the file's note list.
	if !strings.Contains(body, "diff__note") {
		t.Errorf("a line note should render on its line:\n%s", body)
	}
	if !strings.Contains(body, "why replace the whole body?") {
		t.Error("the note text should be shown")
	}
	// Every annotatable line carries what it takes to write one.
	if !strings.Contains(body, "data-anchor") {
		t.Error("diff lines should be annotatable")
	}
}

func TestAnnotatingALineRecordsWhichLine(t *testing.T) {
	kept := &recordingNotes{}
	h := web.NewServer(testSession(), kept)

	req := httptest.NewRequest(http.MethodPost, "/units/s-f001/notes",
		strings.NewReader("kind=question&text=why+this&anchor=%2Bnew+body&nth=0"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if len(kept.notes) != 1 {
		t.Fatalf("stored %+v", kept.notes)
	}
	if kept.notes[0].Anchor != "+new body" {
		t.Errorf("Anchor = %q, want the line it was written on", kept.notes[0].Anchor)
	}
}

func TestANoteWhoseLineWentIsShownAsSuch(t *testing.T) {
	// It must not vanish, and must not be shown as though it still applies.
	sess := testSession()
	sess.Notes = []domain.Note{
		{ID: "n1", UnitID: "s-f001", Kind: domain.NoteObjection,
			Anchor: "+a line that is no longer in this diff", Text: "this was wrong"},
	}
	h := web.NewServer(sess, nil)

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, "this was wrong") {
		t.Error("a note whose line went must still be shown")
	}
	if !strings.Contains(body, "note--orphaned") {
		t.Errorf("it should be marked as no longer anchored:\n%s", body)
	}
}

func TestANoteRecordsTheFileItWasAbout(t *testing.T) {
	// A note carries a unit id, and a unit is derived from git rather than
	// stored — so for a commit target nothing on disk could say which file a
	// note was about. "I remember the file" is at least as common as
	// remembering the wording (ADR 0030).
	kept := &recordingNotes{}
	h := web.NewServer(testSession(), kept)

	req := httptest.NewRequest(http.MethodPost, "/units/s-f001/notes",
		strings.NewReader("kind=objection&text=this+retries+forever"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if len(kept.notes) != 1 {
		t.Fatalf("stored %+v", kept.notes)
	}
	if kept.notes[0].File != "auth/token.go" {
		t.Errorf("File = %q, want the file the unit covers", kept.notes[0].File)
	}
}
