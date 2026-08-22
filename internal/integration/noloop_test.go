package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/presenter/plain"
	"github.com/marcomondini/mondspace-reviewer/internal/adapter/source/hooks"
	"github.com/marcomondini/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

// countingSnapshotter needs no repo; it just hands out sequential refs.
type countingSnapshotter struct {
	mu sync.Mutex
	n  int
}

func (s *countingSnapshotter) Snapshot(context.Context, string) (domain.SnapshotRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return domain.SnapshotRef{Commit: fmt.Sprintf("c%d", s.n)}, nil
}
func (s *countingSnapshotter) Diff(context.Context, domain.SnapshotRef, domain.SnapshotRef, []string) (domain.Diff, error) {
	return domain.Diff{}, nil
}

// When the tailed events.jsonl lives in the store's own session directory (as it
// does in the real CLI), the live review must not re-append events into it —
// doing so feeds the tail its own output in an endless loop.
func TestLiveReviewDoesNotReAppendEvents(t *testing.T) {
	storeRoot := t.TempDir()
	sessDir := filepath.Join(storeRoot, "s")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	eventsPath := filepath.Join(sessDir, "events.jsonl")
	if err := os.WriteFile(eventsPath, []byte(strings.Join([]string{
		`{"id":"e1","session_id":"s","kind":"edit","files":["a.go"]}`,
		`{"id":"e2","session_id":"s","kind":"batch_end"}`,
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	store := jsonl.New(storeRoot)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = usecase.ReviewLive(ctx, hooks.New(eventsPath), &countingSnapshotter{}, store, plain.New(&syncBuffer{}))
	}()

	// Give it time to loop if it is going to, then stop.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sess, err := store.Load("s"); err == nil && len(sess.Units) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	cancel()
	wg.Wait()

	// events.jsonl must be untouched: still exactly two lines.
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n"); len(lines) != 2 {
		t.Errorf("events.jsonl grew to %d lines; review re-appended into the tailed log", len(lines))
	}

	sess, err := store.Load("s")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Units) != 1 {
		t.Errorf("got %d units, want exactly 1 (feedback loop?)", len(sess.Units))
	}
}
