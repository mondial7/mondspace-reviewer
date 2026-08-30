package web_test

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/web"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// settingsSections are the panes the sidebar offers, in the order it offers
// them. Overview is first because it is the one you open without a reason.
var settingsSections = []struct {
	slug, heading string
}{
	{"overview", "overview"},
	{"model", "reviewer agent"},
	{"remote", "watching the remote"},
	{"repositories", "repositories"},
	{"reviews", "reviews"},
	{"usage", "agent usage"},
}

func TestSettingsOpensOnTheOverview(t *testing.T) {
	// Settings is a place you arrive at without a question in mind as often as
	// with one, so the door has to open onto the answer to "is this working".
	rec := get(t, wiredServer(t), "/settings")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings = %d", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{"overview", "reviewer model", "repositories"} {
		if !strings.Contains(strings.ToLower(body), want) {
			t.Errorf("the overview should say %q:\n%s", want, firstLines(body, 4))
		}
	}
	// The things you would go looking for: is it answering, is it fetching.
	for _, want := range []string{"online", "fetching"} {
		if !strings.Contains(strings.ToLower(body), want) {
			t.Errorf("the overview should report %q at a glance", want)
		}
	}
}

func TestEverySectionIsReachableAndRendersItsOwn(t *testing.T) {
	h := wiredServer(t)
	for _, sec := range settingsSections {
		rec := get(t, h, "/settings?s="+sec.slug)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /settings?s=%s = %d", sec.slug, rec.Code)
			continue
		}
		if !strings.Contains(strings.ToLower(rec.Body.String()), sec.heading) {
			t.Errorf("%s does not render %q", sec.slug, sec.heading)
		}
	}
}

func TestASectionYouAreNotLookingAtIsNotBuilt(t *testing.T) {
	// The point of the split: a pane you are not on costs nothing to render and
	// nothing to keep current. Sessions is the expensive one — it is every
	// review in the workspace.
	h := wiredServer(t).WithConfigure(
		func(domain.AgentConfig) error { return nil })
	rec := get(t, h, "/settings?s=model")
	body := rec.Body.String()

	if !strings.Contains(body, "agent-endpoint") {
		t.Fatalf("the model pane should carry the endpoint field:\n%s", firstLines(body, 4))
	}
	if strings.Contains(strings.ToLower(body), "agent usage") {
		t.Error("the usage pane should not be rendered underneath the model pane")
	}
}

func TestAnUnknownSectionLandsOnTheOverview(t *testing.T) {
	// A stale bookmark or a typo is not an error worth a page of its own.
	rec := get(t, wiredServer(t), "/settings?s=nonsense")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings?s=nonsense = %d", rec.Code)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "overview") {
		t.Error("want the overview")
	}
}

func TestTheSidebarSaysWhichSectionYouAreIn(t *testing.T) {
	rec := get(t, wiredServer(t), "/settings?s=remote")
	if !strings.Contains(rec.Body.String(), `aria-current="page"`) {
		t.Error("no section is marked as the one you are on")
	}
}

func TestTheOldStatusAddressStillLeadsSomewhere(t *testing.T) {
	// It is in the tutorial, in the README, on the branches page and in anyone's
	// bookmarks. Renaming a page is not a reason to break a link.
	rec := get(t, wiredServer(t), "/status")
	if rec.Code != http.StatusMovedPermanently && rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /status = %d, want a redirect", rec.Code)
	}
	if got := rec.Header().Get("Location"); !strings.HasPrefix(got, "/settings") {
		t.Errorf("Location = %q, want /settings", got)
	}
}

func TestAnIntervalIsShownTheWayYouWouldTypeIt(t *testing.T) {
	// Go's duration format put "1m0s" in a box a person has to edit. It is not
	// wrong, it is just not what anybody writes — and the value has to survive
	// a round trip through the form that shows it.
	for _, c := range []struct{ in, want string }{
		{"1m0s", "1m"},
		{"2m0s", "2m"},
		{"1h0m0s", "1h"},
		{"1m30s", "1m30s"},
		{"45s", "45s"},
		{"1h30m0s", "1h30m"},
	} {
		if got := web.TidyInterval(c.in); got != c.want {
			t.Errorf("TidyInterval(%q) = %q, want %q", c.in, got, c.want)
		}
		if _, err := time.ParseDuration(web.TidyInterval(c.in)); err != nil {
			t.Errorf("TidyInterval(%q) = %q, which the form cannot parse back: %v",
				c.in, web.TidyInterval(c.in), err)
		}
	}
}

func TestASettingSaysWhetherItIsOnInWords(t *testing.T) {
	// A tick in a box is the control, not the answer. Reading a checkbox to
	// find out whether msr is talking to your remote is the wrong way round.
	h := wiredServer(t)

	body := get(t, h, "/settings?s=remote").Body.String()

	if !strings.Contains(body, "data-state=\"on\"") {
		t.Errorf("the pane should say in markup that it is on:\n%s", firstLines(body, 6))
	}
	if !strings.Contains(strings.ToLower(body), "every 1m") {
		t.Errorf("and say what 'on' currently means:\n%s", firstLines(body, 6))
	}
}

func TestEveryInputOnTheSettingsPageHasALabel(t *testing.T) {
	// A box with a word floating above it is a label only to someone who can
	// see the layout. `for`/`id` is what makes it one to everything else — and
	// what makes the word itself clickable.
	body, err := os.ReadFile("templates/settings.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)

	ids := regexp.MustCompile(`<input[^>]*\bid="([^"]+)"`).FindAllStringSubmatch(page, -1)
	if len(ids) < 3 {
		t.Fatalf("expected the settings forms to carry inputs, found %d", len(ids))
	}
	for _, m := range ids {
		if !strings.Contains(page, `for="`+m[1]+`"`) {
			t.Errorf("input %q has no label pointing at it", m[1])
		}
	}

	// And nothing unlabelled: every text box needs an id to be pointed at.
	all := regexp.MustCompile(`<input[^>]*type="(text|search)"[^>]*>`).FindAllString(page, -1)
	for _, tag := range all {
		if !strings.Contains(tag, "id=") && !strings.Contains(tag, "aria-label") {
			t.Errorf("unlabelled input: %s", tag)
		}
	}
}
