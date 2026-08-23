package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestSelectReposAsksOnlyWhenThereAreMany(t *testing.T) {
	few := []string{"/w/a", "/w/b", "/w/c"}
	var out bytes.Buffer

	got, err := selectRepos(few, strings.NewReader(""), &out, true)

	if err != nil {
		t.Fatalf("selectRepos: %v", err)
	}
	// A handful is not a decision worth interrupting a launch for.
	if len(got) != 3 {
		t.Errorf("got %v, want all three opened without asking", got)
	}
	if out.Len() != 0 {
		t.Errorf("nothing should have been printed:\n%s", out.String())
	}
}

func TestSelectReposRequiresAChoiceWhenThereAreTooMany(t *testing.T) {
	many := []string{"/w/a", "/w/b", "/w/c", "/w/d", "/w/e", "/w/f", "/w/g"}
	var out bytes.Buffer

	got, err := selectRepos(many, strings.NewReader("1,3,5\n"), &out, true)

	if err != nil {
		t.Fatalf("selectRepos: %v", err)
	}
	want := []string{"/w/a", "/w/c", "/w/e"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("got %v, want %v", got, want)
	}
	// The list has to be readable: numbered, one per line.
	if !strings.Contains(out.String(), "1) ") || !strings.Contains(out.String(), "7) ") {
		t.Errorf("the prompt should number every repository:\n%s", out.String())
	}
}

func TestSelectReposAcceptsRangesAndAll(t *testing.T) {
	many := []string{"/w/a", "/w/b", "/w/c", "/w/d", "/w/e", "/w/f"}

	got, err := selectRepos(many, strings.NewReader("2-4\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	if len(got) != 3 || got[0] != "/w/b" || got[2] != "/w/d" {
		t.Errorf("range 2-4 = %v, want b c d", got)
	}

	all, err := selectRepos(many, strings.NewReader("all\n"), &bytes.Buffer{}, true)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 6 {
		t.Errorf("all = %v, want every repository", all)
	}
}

func TestSelectReposRefusesToGuessWithoutATerminal(t *testing.T) {
	// In a script or CI there is nobody to ask, and silently opening 40
	// repositories would be worse than stopping.
	many := []string{"/a", "/b", "/c", "/d", "/e", "/f"}

	_, err := selectRepos(many, strings.NewReader(""), &bytes.Buffer{}, false)

	if err == nil {
		t.Fatal("expected an error when there is no terminal to ask")
	}
	if !strings.Contains(err.Error(), "--repo") {
		t.Errorf("the error should say how to fix it: %v", err)
	}
}

func TestSelectReposRejectsAnEmptyOrImpossibleChoice(t *testing.T) {
	many := []string{"/a", "/b", "/c", "/d", "/e", "/f"}

	if _, err := selectRepos(many, strings.NewReader("\n"), &bytes.Buffer{}, true); err == nil {
		t.Error("an empty choice should not silently open everything")
	}
	if _, err := selectRepos(many, strings.NewReader("99\n"), &bytes.Buffer{}, true); err == nil {
		t.Error("an out-of-range choice should be refused")
	}
}

func TestIsTerminalRejectsDevNull(t *testing.T) {
	// /dev/null is a character device, so the naive check calls it a terminal —
	// and then a script redirecting from it gets a prompt nobody can answer.
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip("no /dev/null on this platform")
	}
	defer null.Close()

	if isTerminal(null) {
		t.Error("/dev/null must not count as a terminal")
	}
}

func TestIsTerminalRejectsAPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	if isTerminal(r) {
		t.Error("a pipe must not count as a terminal")
	}
}
