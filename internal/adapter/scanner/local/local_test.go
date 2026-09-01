package local_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/scanner/local"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// A fake analyser, so these tests do not depend on what happens to be installed
// on the machine running them.
func fake(t *testing.T, name string, script string) (usecase.Analyser, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake analysers here are shell scripts")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return usecase.Analyser{
		Name:   name,
		Detect: []string{path, "--version"},
		Run:    []string{path, "{files}"},
		Format: usecase.FormatLines,
	}, dir
}

func TestAnAnalyserThatIsNotThereIsSimplyNotRun(t *testing.T) {
	// No installers, no nagging, no error on the review (ADR 0043).
	missing := usecase.Analyser{
		Name:   "nothing-like-this-exists",
		Detect: []string{"/nonexistent/binary", "--version"},
		Run:    []string{"/nonexistent/binary"},
		Format: usecase.FormatLines,
	}
	s := local.New(t.TempDir(), []usecase.Analyser{missing})

	if got := s.Look(context.Background(), []string{"a.go"}, nil, "HEAD"); len(got) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
	if s.Installed() != 0 {
		t.Error("nothing should have been detected")
	}
	report := s.Report()
	if len(report) != 1 || report[0].Present {
		t.Fatalf("report = %+v", report)
	}
	if report[0].Absent == "" {
		t.Error("the settings page has to be able to say what was not found")
	}
}

func TestAnInstalledAnalysersFindingsAreAttributedToIt(t *testing.T) {
	a, _ := fake(t, "pretend-lint", `
if [ "$1" = "--version" ]; then echo "pretend-lint 1.2.3"; exit 0; fi
echo "api/handler.go:42:2: something is wrong here (X123)"
exit 1
`)
	s := local.New(t.TempDir(), []usecase.Analyser{a})

	got := s.Look(context.Background(), []string{"api/handler.go"}, nil, "HEAD")
	if len(got) != 1 {
		t.Fatalf("got %+v, want one finding", got)
	}
	if got[0].Tool != "pretend-lint" || got[0].Rule != "X123" {
		t.Errorf("finding = %+v, want it attributed", got[0])
	}
	if got[0].File != "api/handler.go" || got[0].Line != 42 {
		t.Errorf("finding = %+v", got[0])
	}
	// Exiting non-zero is what a linter does when it finds something. Treating
	// that as a failure would mean only clean runs were ever reported.
	if r := s.Report(); r[0].Failed != "" {
		t.Errorf("a linter with findings is not a broken linter: %q", r[0].Failed)
	}
}

func TestAnUnchangedFileIsNotAnalysedTwice(t *testing.T) {
	a, dir := fake(t, "counting-lint", `
if [ "$1" = "--version" ]; then echo "counting-lint 1"; exit 0; fi
echo run >> "$(dirname "$0")/runs"
echo "a.go:1:1: something (R1)"
`)
	s := local.New(t.TempDir(), []usecase.Analyser{a})
	prints := map[string]string{"a.go": "print-one"}

	s.Look(context.Background(), []string{"a.go"}, prints, "HEAD")
	s.Look(context.Background(), []string{"a.go"}, prints, "HEAD")

	runs, _ := os.ReadFile(filepath.Join(dir, "runs"))
	if n := strings.Count(string(runs), "run"); n != 1 {
		t.Errorf("ran %d times, want 1 — unchanged file, no run", n)
	}

	// A file that moved is the one reason to ask again.
	s.Look(context.Background(), []string{"a.go"}, map[string]string{"a.go": "print-two"}, "HEAD")
	runs, _ = os.ReadFile(filepath.Join(dir, "runs"))
	if n := strings.Count(string(runs), "run"); n != 2 {
		t.Errorf("ran %d times after the file moved, want 2", n)
	}
}

func TestAnAnalyserThatHangsIsAbandonedNotWaitedFor(t *testing.T) {
	// The same contract the model has: analysis never blocks the review.
	a, _ := fake(t, "slow-lint", `
if [ "$1" = "--version" ]; then echo "slow-lint 1"; exit 0; fi
sleep 30
`)
	s := local.New(t.TempDir(), []usecase.Analyser{a}).WithCap(200 * time.Millisecond)

	start := time.Now()
	got := s.Look(context.Background(), []string{"a.go"}, nil, "HEAD")
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("waited %s for a stuck analyser", took)
	}
	if len(got) != 0 {
		t.Errorf("got %+v from a tool that never answered", got)
	}
	if r := s.Report(); r[0].Failed == "" {
		t.Error("a tool that gave up should be reported on the settings page")
	}
}

func TestACrashingAnalyserIsReportedOnceAndNeverInterrupts(t *testing.T) {
	a, _ := fake(t, "broken-lint", `
if [ "$1" = "--version" ]; then echo "broken-lint 1"; exit 0; fi
exit 2
`)
	s := local.New(t.TempDir(), []usecase.Analyser{a})

	for i := 0; i < 3; i++ {
		if got := s.Look(context.Background(), []string{"a.go"},
			map[string]string{"a.go": "p" + string(rune('a'+i))}, "HEAD"); len(got) != 0 {
			t.Fatalf("a crash is not a finding: %+v", got)
		}
	}
	r := s.Report()
	if r[0].Failed == "" {
		t.Error("the settings page should say it broke")
	}
}
