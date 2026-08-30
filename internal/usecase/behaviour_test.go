package usecase_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// narrativeDiffs is what those units actually did, which is the thing a chapter
// is supposed to be about.
func narrativeDiffs() map[string]domain.Diff {
	return map[string]domain.Diff{
		"s-f001": {Text: "@@ -12,3 +12,3 @@\n-\tconst ttl = 24 * time.Hour\n+\tconst ttl = time.Hour\n"},
		"s-f002": {Text: "@@ -0,0 +1,4 @@\n+func TestTokenExpiresInAnHour(t *testing.T) {\n"},
		"s-f003": {Text: "@@ -30,2 +30,4 @@\n+\tif token.Expired() {\n+\t\treturn errUnauthorised\n"},
		"s-f004": {Text: "@@ -1,2 +1,2 @@\n-Tokens last a day.\n+Tokens last an hour.\n"},
	}
}

func TestAChapterIsAChangeInBehaviourNotAFolder(t *testing.T) {
	// A chapter titled "auth" tells a reviewer where to look and nothing about
	// what happened. With the diffs in front of it the model can name the change
	// itself, and files from three directories can belong to one chapter.
	n := &narrator{reply: `{"title":"Tokens expire in an hour","intro":"I",
		"chapters":[{"title":"Tokens expire after an hour, not a day",
		"prose":"The lifetime drops and the middleware now rejects an expired one.",
		"files":["auth/token.go","http/middleware.go","README.md"]},
		{"title":"A test pins the new lifetime","prose":"p","files":["auth/token_test.go"]}]}`}

	got, err := usecase.Narrate(context.Background(), n,
		domain.Session{ID: "s", Prompt: "shorten the token lifetime"},
		narrativeUnits(), narrativeDiffs())
	if err != nil {
		t.Fatalf("Narrate: %v", err)
	}

	if len(got.Chapters) != 2 {
		t.Fatalf("got %d chapters: %+v", len(got.Chapters), got.Chapters)
	}
	if got.Chapters[0].Title != "Tokens expire after an hour, not a day" {
		t.Errorf("the chapter should be named after the change: %q", got.Chapters[0].Title)
	}
	// One behaviour, three directories: a chapter is not a folder.
	if len(got.Chapters[0].UnitIDs) != 3 {
		t.Errorf("files from three directories belong to one chapter: %+v", got.Chapters[0])
	}
}

func TestTheModelIsShownWhatChanged(t *testing.T) {
	// It cannot describe a change it was never shown. The prompt used to carry
	// area names and filenames only, which is exactly why every chapter came
	// back as the name of a directory.
	n := &narrator{reply: `{"title":"T","intro":"I","chapters":[{"title":"C","prose":"p","files":["auth/token.go"]}]}`}

	_, _ = usecase.Narrate(context.Background(), n, domain.Session{ID: "s"},
		narrativeUnits(), narrativeDiffs())

	for _, want := range []string{"auth/token.go", "const ttl = time.Hour"} {
		if !strings.Contains(n.asked, want) {
			t.Errorf("the prompt should show %q:\n%s", want, n.asked)
		}
	}
}

func TestWithNoDiffsItStillNarratesTheAreas(t *testing.T) {
	// Nothing to read means the older shape, which asks about areas — worse,
	// and better than nothing.
	n := &narrator{reply: `{"title":"T","intro":"I","chapters":[{"title":"Auth","prose":"p","groups":["auth"]}]}`}

	got, err := usecase.Narrate(context.Background(), n, domain.Session{ID: "s"},
		narrativeUnits(), nil)
	if err != nil {
		t.Fatalf("Narrate: %v", err)
	}
	if got.Source != domain.NarrativeModel || len(got.Chapters) == 0 {
		t.Errorf("got %+v", got)
	}
}
