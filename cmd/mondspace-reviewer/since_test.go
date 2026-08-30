package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewSinceCommandPlainListsNetChangeWithNoSession(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "a.go")
	runGit(t, repo, "commit", "-qm", "init")
	runGit(t, repo, "tag", "before-work")

	// Work happens with no session recorded at all: change a.go, add b.go.
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "b.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	args := []string{
		"review", "--plain",
		"--since=before-work",
		"--repo=" + repo,
		"--out=" + t.TempDir(),
	}
	if err := run(context.Background(), args, nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "a.go") || !strings.Contains(got, "b.go") {
		t.Errorf("output missing changed files:\n%s", got)
	}
	if !strings.Contains(got, "since-before-work") {
		t.Errorf("unit ids should be seeded from a synthesized since-<ref> session id, got:\n%s", got)
	}
}

func TestReviewSinceCommandUntilBoundsTheFarEnd(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "a.go")
	runGit(t, repo, "commit", "-qm", "init")
	runGit(t, repo, "tag", "start")

	if err := os.WriteFile(filepath.Join(repo, "b.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "b.go")
	runGit(t, repo, "commit", "-qm", "mid")
	runGit(t, repo, "tag", "mid")

	if err := os.WriteFile(filepath.Join(repo, "c.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "c.go")
	runGit(t, repo, "commit", "-qm", "late")

	var out bytes.Buffer
	args := []string{
		"review", "--plain",
		"--since=start", "--until=mid",
		"--repo=" + repo,
		"--out=" + t.TempDir(),
	}
	if err := run(context.Background(), args, nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "b.go") {
		t.Errorf("output missing b.go (changed within --since/--until):\n%s", got)
	}
	if strings.Contains(got, "c.go") {
		t.Errorf("output must not include c.go (committed after --until):\n%s", got)
	}
}

func TestReviewSinceCommandUsesGivenSessionID(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "a.go")
	runGit(t, repo, "commit", "-qm", "init")
	runGit(t, repo, "tag", "start")

	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	args := []string{
		"review", "--plain",
		"--since=start",
		"--session=my-sess",
		"--repo=" + repo,
		"--out=" + t.TempDir(),
	}
	if err := run(context.Background(), args, nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[my-sess-f001]") {
		t.Errorf("unit id should use the given --session, got:\n%s", got)
	}
}

func TestReviewSinceCommandRequiresPlainOrTUI(t *testing.T) {
	if err := run(context.Background(), []string{"review", "--since=HEAD"}, nil, &bytes.Buffer{}); err == nil {
		t.Error("--since with neither --plain nor --tui should error")
	}
}

func TestReviewSinceCommandUnknownRefErrors(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "a.go")
	runGit(t, repo, "commit", "-qm", "init")

	err := run(context.Background(), []string{
		"review", "--plain", "--since=does-not-exist", "--repo=" + repo, "--out=" + t.TempDir(),
	}, nil, &bytes.Buffer{})
	if err == nil {
		t.Error("an unresolvable --since ref should error, not crash silently")
	}
}

func TestTheCommandLineHonoursMsrignoreToo(t *testing.T) {
	// .msrignore keeps generated files out of a review (ADR 0027). It has been
	// doing that in the app since v6 and not on the command line at all, so the
	// same repository read two ways gave two different reviews.
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	write(t, repo, "a.go", "package a\n")
	write(t, repo, ".msrignore", "*.pb.go\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "init")
	runGit(t, repo, "tag", "before")

	write(t, repo, "a.go", "package a\n\nfunc F() {}\n")
	write(t, repo, "api.pb.go", "// generated\npackage a\n")

	var out bytes.Buffer
	args := []string{"review", "--plain", "--since=before", "--repo=" + repo, "--out=" + t.TempDir()}
	if err := run(context.Background(), args, nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "a.go") {
		t.Errorf("the real change should still be listed:\n%s", got)
	}
	// Not as a unit to review...
	if strings.Contains(got, "added api.pb.go") {
		t.Errorf("an ignored file should not be a unit in the review:\n%s", got)
	}
	// ...but never silently either: what is hidden is always named, with the
	// rule that hid it. That is why .msrignore has no defaults.
	for _, want := range []string{"1 file", "api.pb.go", "*.pb.go", "--all"} {
		if !strings.Contains(got, want) {
			t.Errorf("it should say what it hid and why — missing %q:\n%s", want, got)
		}
	}
}

func TestShowingEverythingBypassesTheIgnoreRules(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	write(t, repo, "a.go", "package a\n")
	write(t, repo, ".msrignore", "*.pb.go\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "init")
	runGit(t, repo, "tag", "before")
	write(t, repo, "api.pb.go", "// generated\npackage a\n")

	var out bytes.Buffer
	args := []string{"review", "--plain", "--all", "--since=before", "--repo=" + repo, "--out=" + t.TempDir()}
	if err := run(context.Background(), args, nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "api.pb.go") {
		t.Errorf("--all should show everything:\n%s", out.String())
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
