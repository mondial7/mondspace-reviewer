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
