package web_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// navOf is the destinations one page's rail offers, and which one it marks as
// the page you are on.
type navOf struct {
	links   []string
	current string
}

var (
	railBlock  = regexp.MustCompile(`(?s)<nav class="storynav">(.*?)</nav>`)
	railLink   = regexp.MustCompile(`<a class="storynav__link" href="([^"]+)"([^>]*)>`)
	railMarked = regexp.MustCompile(`aria-current="page"`)
)

// rails reads the nav out of every template that has one.
func rails(t *testing.T) map[string]navOf {
	t.Helper()
	files, err := filepath.Glob("templates/*.html")
	if err != nil || len(files) == 0 {
		t.Fatalf("no templates found: %v", err)
	}

	out := map[string]navOf{}
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		block := railBlock.FindSubmatch(body)
		if block == nil {
			continue // a partial, with no shell of its own
		}
		var nav navOf
		for _, m := range railLink.FindAllStringSubmatch(string(block[1]), -1) {
			nav.links = append(nav.links, m[1])
			if railMarked.MatchString(m[2]) {
				nav.current = m[1]
			}
		}
		out[filepath.Base(path)] = nav
	}
	return out
}

func TestEveryPageOffersTheSameWayOut(t *testing.T) {
	// Six templates each carry their own copy of the rail, and three of them
	// had drifted: /search offered no way to search and /activity offered
	// neither branches nor search. A destination you cannot reach from where
	// you are is a destination that does not exist.
	all := rails(t)
	if len(all) < 5 {
		t.Fatalf("expected most pages to carry a rail, found %d", len(all))
	}

	var reference string
	var want []string
	for _, name := range names(all) {
		if reference == "" {
			reference, want = name, all[name].links
			continue
		}
		got := all[name].links
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s offers %v; %s offers %v", name, got, reference, want)
		}
	}
}

// destinations are the templates the rail itself leads to. A page reached from
// inside one of them — one file's whole diff — offers the same way out and
// marks nothing, because it is not somewhere the rail can take you.
var destinations = map[string]bool{
	"cockpit.html": true, "branches.html": true, "search.html": true,
	"activity.html": true, "settings.html": true, "tutorial.html": true,
}

func TestEachDestinationSaysWhichOneYouAreOn(t *testing.T) {
	// Without it the rail is six identical links and the reader has to
	// remember where they clicked.
	all := rails(t)
	for _, name := range names(all) {
		switch {
		case destinations[name] && all[name].current == "":
			t.Errorf("%s marks no link as the current page", name)
		case !destinations[name] && all[name].current != "":
			t.Errorf("%s marks %q as current, but the rail cannot take you there",
				name, all[name].current)
		}
	}
}
