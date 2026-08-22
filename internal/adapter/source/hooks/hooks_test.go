package hooks_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/source/hooks"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

// collectN reads n events or fails on timeout, so a following source that never
// closes its channel cannot hang the test.
func collectN(t *testing.T, ch <-chan domain.Event, n int) []domain.Event {
	t.Helper()
	var got []domain.Event
	timeout := time.After(2 * time.Second)
	for len(got) < n {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed after %d events, want %d", len(got), n)
			}
			got = append(got, e)
		case <-timeout:
			t.Fatalf("timed out after %d events, want %d", len(got), n)
		}
	}
	return got
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

func TestHooksFollowsAppendedLines(t *testing.T) {
	path := t.TempDir() + "/events.jsonl"
	writeFile(t, path, `{"id":"e1","session_id":"s","kind":"edit","files":["a.go"]}`+"\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := hooks.New(path).Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}

	// Drain the catch-up event first.
	if got := collectN(t, ch, 1); got[0].ID != "e1" {
		t.Fatalf("catch-up = %q, want e1", got[0].ID)
	}

	// Now append after Events was already running; it must be followed.
	appendLine(t, path, `{"id":"e2","session_id":"s","kind":"batch_end"}`)

	got := collectN(t, ch, 1)
	if got[0].ID != "e2" {
		t.Errorf("followed event = %q, want e2", got[0].ID)
	}
}

func TestHooksSkipsMalformedAppendedLine(t *testing.T) {
	path := t.TempDir() + "/events.jsonl"
	writeFile(t, path, `{"id":"e1","session_id":"s","kind":"edit"}`+"\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := hooks.New(path).Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	collectN(t, ch, 1) // e1

	appendLine(t, path, `{ truncated garbage`)
	appendLine(t, path, `{"id":"e3","session_id":"s","kind":"edit"}`)

	got := collectN(t, ch, 1)
	if got[0].ID != "e3" {
		t.Errorf("got %q, want e3 (malformed line must be skipped)", got[0].ID)
	}
}

func TestHooksEmitsExistingEventsAtStart(t *testing.T) {
	path := t.TempDir() + "/events.jsonl"
	writeFile(t, path,
		`{"id":"e1","session_id":"s","kind":"edit","files":["a.go"]}
{"id":"e2","session_id":"s","kind":"batch_end"}
`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := hooks.New(path).Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	got := collectN(t, ch, 2)

	if got[0].ID != "e1" || got[1].ID != "e2" {
		t.Errorf("got IDs %q,%q, want e1,e2", got[0].ID, got[1].ID)
	}
}
