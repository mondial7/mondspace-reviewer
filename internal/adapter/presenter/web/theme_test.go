package web_test

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func stylesheet(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("assets/app.css")
	if err != nil {
		t.Fatalf("reading the stylesheet: %v", err)
	}
	return string(body)
}

var rootBlock = regexp.MustCompile(`(?m)^\s*(:root[^{]*)\{`)

// themeBlocks pulls every `:root…{ … }` block out of the stylesheet, keyed by
// the selector that opened it. These blocks are the only place a theme's
// colours are allowed to live.
func themeBlocks(t *testing.T) map[string]map[string]bool {
	t.Helper()
	css := stylesheet(t)
	token := regexp.MustCompile(`(--[a-z0-9-]+)\s*:`)

	blocks := map[string]map[string]bool{}
	for _, m := range rootBlock.FindAllStringSubmatchIndex(css, -1) {
		selector := strings.TrimSpace(css[m[2]:m[3]])
		end := strings.Index(css[m[1]:], "}")
		if end < 0 {
			t.Fatalf("unclosed block for %q", selector)
		}
		names := map[string]bool{}
		for _, tok := range token.FindAllStringSubmatch(css[m[1]:m[1]+end], -1) {
			names[tok[1]] = true
		}
		blocks[selector] = names
	}
	return blocks
}

func TestEveryThemeAnswersForEveryColourTheOthersOverride(t *testing.T) {
	// A theme that forgets a token silently inherits the one above it, and the
	// page is then half in one palette and half in another — with nothing in
	// the theme's own block looking wrong.
	blocks := themeBlocks(t)

	base, ok := blocks[":root"]
	if !ok {
		t.Fatal("no bare :root block — the base every theme overrides")
	}
	if len(blocks) < 4 {
		t.Fatalf("expected a base and at least three themes, found %v", names(blocks))
	}

	shared := map[string]bool{}
	for selector, tokens := range blocks {
		if selector != ":root" {
			for n := range tokens {
				shared[n] = true
			}
		}
	}

	for _, selector := range names(blocks) {
		if selector == ":root" {
			continue
		}
		tokens := blocks[selector]
		for _, n := range names(shared) {
			if !tokens[n] {
				t.Errorf("%s does not define %s — it will inherit whatever was set above it",
					selector, n)
			}
		}
		for _, n := range names(tokens) {
			if !base[n] {
				t.Errorf("%s defines %s, which :root never does — nothing sets it by default",
					selector, n)
			}
		}
	}
}

func TestNoColourIsWrittenOutsideATheme(t *testing.T) {
	// The one rule that makes the rule above worth anything. A colour hardcoded
	// in a component belongs to no theme, so no theme can override it: two
	// hardcoded nebulae stayed dark on the light page precisely because nothing
	// in the light block was wrong.
	css := stylesheet(t)

	var outside strings.Builder
	last := 0
	for _, m := range rootBlock.FindAllStringSubmatchIndex(css, -1) {
		end := strings.Index(css[m[1]:], "}")
		outside.WriteString(css[last:m[0]])
		last = m[1] + end + 1
	}
	outside.WriteString(css[last:])

	literal := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	for _, line := range strings.Split(outside.String(), "\n") {
		if hit := literal.FindString(line); hit != "" {
			t.Errorf("%s is written into a rule rather than into a theme: %s",
				hit, strings.TrimSpace(line))
		}
	}
}

// names is the sorted keys of a map, so failures read in a stable order.
func names[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
