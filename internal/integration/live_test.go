package integration_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/plain"
	gitsnap "github.com/mondial7/mondspace-reviewer/internal/adapter/snapshot/git"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/source/hooks"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// syncBuffer lets the presenter write from the review goroutine while the test
// reads after shutdown.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}
func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestLiveReviewReconstructsSessionFromLog(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "a.go")
	git(t, repo, "commit", "-qm", "init")

	// The log the hooks source tails lives in the store's own session dir, as
	// the agent's ingest hooks write it: two units, two batch_ends.
	storeRoot := t.TempDir()
	sessDir := filepath.Join(storeRoot, "s")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(sessDir, "events.jsonl")
	if err := os.WriteFile(events, []byte(strings.Join([]string{
		`{"id":"e1","session_id":"s","kind":"edit","files":["a.go"]}`,
		`{"id":"e2","session_id":"s","kind":"batch_end"}`,
		`{"id":"e3","session_id":"s","kind":"edit","files":["a.go"]}`,
		`{"id":"e4","session_id":"s","kind":"batch_end"}`,
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	store := jsonl.New(storeRoot)
	out := &syncBuffer{}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = usecase.ReviewLive(ctx, hooks.New(events), gitsnap.New(repo, "s"), store, plain.New(out))
	}()

	// Wait until both units are persisted, then stop the follower.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if sess, err := store.Load("s"); err == nil && len(sess.Units) == 2 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			wg.Wait()
			t.Fatalf("timed out waiting for 2 units")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	// Reconstruct from the stored log alone.
	sess, err := store.Load("s")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(sess.Events) != 4 {
		t.Errorf("reconstructed %d events, want 4", len(sess.Events))
	}
	if len(sess.Units) != 2 {
		t.Fatalf("reconstructed %d units, want 2", len(sess.Units))
	}

	snap := gitsnap.New(repo, "s")
	for i, u := range sess.Units {
		if u.From.Commit == "" || u.To.Commit == "" {
			t.Errorf("unit %d missing snapshot brackets: %+v", i, u)
			continue
		}
		// Both refs must be real objects, and a diff must resolve.
		for _, c := range []string{u.From.Commit, u.To.Commit} {
			check := exec.Command("git", "cat-file", "-e", c)
			check.Dir = repo
			if err := check.Run(); err != nil {
				t.Errorf("unit %d ref %s is not a real object", i, c)
			}
		}
		if _, err := snap.Diff(context.Background(), u.From, u.To, u.Files); err != nil {
			t.Errorf("unit %d diff did not resolve: %v", i, err)
		}
	}

	printed := out.String()
	if !strings.Contains(printed, "WHAT") || strings.Count(printed, "WHAT") != 2 {
		t.Errorf("presenter did not render both units:\n%s", printed)
	}
}
