package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReviewOpencodeSourceValidatesFlags(t *testing.T) {
	if err := run(context.Background(), []string{"review", "--source=opencode", "--plain"}, nil, &syncBuf{}); err == nil {
		t.Error("opencode source without --session should error")
	}
}

func TestReviewOpencodeSourceDrivesLivePath(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "a.go")
	runGit(t, repo, "commit", "-qm", "init")

	out := t.TempDir()
	sessDir := filepath.Join(out, "s")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "events.jsonl"), []byte(strings.Join([]string{
		`{"id":"e1","sessionId":"s","type":"tool.edit","files":["a.go"]}`,
		`{"id":"e2","sessionId":"s","type":"step.finish"}`,
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	buf := &syncBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = run(ctx, []string{"review", "--source=opencode", "--plain",
			"--repo=" + repo, "--out=" + out, "--session=s"}, nil, buf)
	}()

	deadline := time.Now().Add(30 * time.Second)
	for !strings.Contains(buf.String(), "WHAT") {
		if time.Now().After(deadline) {
			cancel()
			wg.Wait()
			t.Fatalf("no unit rendered before deadline:\n%s", buf.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	if !strings.Contains(buf.String(), "a.go") {
		t.Errorf("output missing the edited file:\n%s", buf.String())
	}
}

// TestReviewOpencodeSourceReadsRealisticFixture drives the full live path
// against the checked-in testdata/sessions/opencode.jsonl fixture, so the
// documented OpenCode payload shape is exercised end to end, not just in the
// package's own unit tests.
func TestReviewOpencodeSourceReadsRealisticFixture(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	for _, f := range []string{
		"auth/token.go", "auth/port.go", "auth/token_test.go",
		"http/middleware.go", "http/routes.go", "go.mod", "go.sum",
	} {
		if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, f), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-qm", "init")

	out := t.TempDir()
	sessDir := filepath.Join(out, "ocfix")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sessions", "opencode.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "events.jsonl"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	buf := &syncBuf{}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = run(ctx, []string{"review", "--source=opencode", "--plain",
			"--repo=" + repo, "--out=" + out, "--session=ocfix"}, nil, buf)
	}()

	deadline := time.Now().Add(30 * time.Second)
	for !strings.Contains(buf.String(), "go.sum") {
		if time.Now().After(deadline) {
			cancel()
			wg.Wait()
			t.Fatalf("fixture-driven session did not reach the last file before deadline:\n%s", buf.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	got := buf.String()
	for _, want := range []string{"auth/token.go", "http/middleware.go", "go.mod"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing file %q from the fixture:\n%s", want, got)
		}
	}
}
