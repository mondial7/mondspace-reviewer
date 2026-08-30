package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func TestTheWordmarkLinesUp(t *testing.T) {
	// It is four lines of art with text hung off the side of it. A wordmark
	// whose rows are different widths is not a wordmark, and nothing else in
	// the program will ever notice.
	width := len([]rune(wordmark[0]))
	for i, row := range wordmark {
		if got := len([]rune(row)); got != width {
			t.Errorf("row %d is %d wide, the first is %d:\n%s",
				i, got, width, strings.Join(wordmark[:], "\n"))
		}
	}

	// And every row has something hung off it, or nothing — never a leftover
	// formatting verb, which is what happens when a line without one is run
	// through Sprintf anyway.
	var b bytes.Buffer
	banner(&b, "6.1.0", false)
	if strings.Contains(b.String(), "%!") {
		t.Errorf("a formatting verb leaked into the greeting:\n%s", b.String())
	}
}

func TestTheBannerSaysWhichVersionYouAreRunning(t *testing.T) {
	// The whole reason to print it: you are looking at a page and wondering
	// whether it is the build you just made.
	var b bytes.Buffer
	banner(&b, "6.1.0", false)

	for _, want := range []string{"mondspace-reviewer", "6.1.0"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q:\n%s", want, b.String())
		}
	}
}

func TestNoColourWhenNothingCanShowIt(t *testing.T) {
	var plain, tinted bytes.Buffer
	banner(&plain, "6.1.0", false)
	banner(&tinted, "6.1.0", true)

	if strings.Contains(plain.String(), "\x1b[") {
		t.Errorf("escape codes in the plain banner:\n%q", plain.String())
	}
	if !strings.Contains(tinted.String(), "\x1b[") {
		t.Error("the tinted banner should carry colour")
	}
}

func TestOnlyTheCommandsAPersonWatchesGetABanner(t *testing.T) {
	// `ingest` is a hook that must stay silent, `mcp` speaks a protocol, and
	// `export` and `version` are answers someone is piping somewhere. Four
	// pieces of ASCII art in the middle of any of those is a bug.
	for _, cmd := range []string{"web", "review", "ask", "gc", "install-hooks", "help"} {
		if !greets(cmd) {
			t.Errorf("%s should greet", cmd)
		}
	}
	for _, cmd := range []string{"ingest", "mcp", "export", "version", "--version"} {
		if greets(cmd) {
			t.Errorf("%s must not greet — its output is read by something else", cmd)
		}
	}
}

func TestTheBannerCanBeTurnedOff(t *testing.T) {
	// Someone recording a terminal session, or running msr from a script that
	// happens to keep a terminal attached.
	t.Setenv("MSR_NO_BANNER", "1")
	if wantsBanner("web", true) {
		t.Error("MSR_NO_BANNER should silence it")
	}
}

func TestNothingIsDrawnIntoAPipe(t *testing.T) {
	// Box-drawing and escape codes belong on a terminal. Anywhere else they are
	// noise in a log file.
	t.Setenv("MSR_NO_BANNER", "")
	if wantsBanner("web", false) {
		t.Error("a banner should not be written when stderr is not a terminal")
	}
}

func TestADevelopmentBuildSaysWhichCommitItIs(t *testing.T) {
	// The banner exists to answer "is this the build I just made". A module
	// path with no /v6 on it means Go's own version for a local build is a
	// v1 pseudo-version — 1.0.1-0.20260830083531-f2fafe9fda48+dirty — which
	// names the right commit in the least readable way available and claims a
	// major version six behind the truth.
	info := &debug.BuildInfo{Main: debug.Module{Version: "v1.0.1-0.20260830083531-f2fafe9fda48+dirty"}}
	info.Settings = []debug.BuildSetting{
		{Key: "vcs.revision", Value: "f2fafe9fda4812ab"},
		{Key: "vcs.modified", Value: "true"},
	}

	got := describeVersion("", info)

	if strings.Contains(got, "1.0.1") || strings.Contains(got, "20260830") {
		t.Errorf("a pseudo-version reached the user: %q", got)
	}
	if !strings.Contains(got, "f2fafe9") {
		t.Errorf("a development build should name its commit, got %q", got)
	}
	if !strings.Contains(got, "dirty") {
		t.Errorf("uncommitted changes should be admitted, got %q", got)
	}
}

func TestAReleasedBuildSaysItsTag(t *testing.T) {
	if got := describeVersion("v6.1.0", nil); got != "v6.1.0" {
		t.Errorf("describeVersion = %q, want the tag goreleaser stamped", got)
	}
}

func TestWithNothingToGoOnItSaysDev(t *testing.T) {
	if got := describeVersion("", nil); got != "dev" {
		t.Errorf("describeVersion = %q, want \"dev\"", got)
	}
}
