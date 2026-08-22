package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReviewHooksSourceValidatesFlags(t *testing.T) {
	if err := run(context.Background(), []string{"review", "--source=hooks", "--plain"}, nil, &syncBuf{}); err == nil {
		t.Error("hooks source without --session should error")
	}
	if err := run(context.Background(), []string{"review", "--source=bogus", "--plain", "--file=x"}, nil, &syncBuf{}); err == nil {
		t.Error("unknown source should error")
	}
}

func TestReviewHooksSourceDrivesLivePath(t *testing.T) {
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
		`{"id":"e1","session_id":"s","kind":"edit","files":["a.go"]}`,
		`{"id":"e2","session_id":"s","kind":"batch_end"}`,
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
		_ = run(ctx, []string{"review", "--source=hooks", "--plain",
			"--repo=" + repo, "--out=" + out, "--session=s"}, nil, buf)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(buf.String(), "WHAT") {
		if time.Now().After(deadline) {
			cancel()
			wg.Wait()
			t.Fatalf("no unit rendered within 5s:\n%s", buf.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	if !strings.Contains(buf.String(), "a.go") {
		t.Errorf("output missing the edited file:\n%s", buf.String())
	}
}

// syncBuf is a goroutine-safe writer for the following review path.
type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, o)
	}
}
