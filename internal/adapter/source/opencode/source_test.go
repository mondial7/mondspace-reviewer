package opencode_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/source/opencode"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// collectN reads n events or fails on timeout, so a following source that
// never closes its channel cannot hang the test.
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

func TestSourceEmitsExistingEventsAtStart(t *testing.T) {
	path := t.TempDir() + "/opencode.jsonl"
	writeFile(t, path,
		`{"id":"e1","sessionId":"s","type":"tool.edit","files":["a.go"]}
{"id":"e2","sessionId":"s","type":"step.finish"}
`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := opencode.New(path).Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	got := collectN(t, ch, 2)

	if got[0].ID != "e1" || got[1].ID != "e2" {
		t.Errorf("got IDs %q,%q, want e1,e2", got[0].ID, got[1].ID)
	}
	if got[0].Kind != domain.KindEdit || got[1].Kind != domain.KindBatchEnd {
		t.Errorf("got kinds %q,%q, want edit,batch_end", got[0].Kind, got[1].Kind)
	}
}

func TestSourceFollowsAppendedLines(t *testing.T) {
	path := t.TempDir() + "/opencode.jsonl"
	writeFile(t, path, `{"id":"e1","sessionId":"s","type":"tool.edit","files":["a.go"]}`+"\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := opencode.New(path).Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}

	// Drain the catch-up event first.
	if got := collectN(t, ch, 1); got[0].ID != "e1" {
		t.Fatalf("catch-up = %q, want e1", got[0].ID)
	}

	// Now append after Events was already running; it must be followed.
	appendLine(t, path, `{"id":"e2","sessionId":"s","type":"step.finish"}`)

	got := collectN(t, ch, 1)
	if got[0].ID != "e2" {
		t.Errorf("followed event = %q, want e2", got[0].ID)
	}
}

func TestSourceSkipsMalformedAppendedLine(t *testing.T) {
	path := t.TempDir() + "/opencode.jsonl"
	writeFile(t, path, `{"id":"e1","sessionId":"s","type":"tool.edit"}`+"\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := opencode.New(path).Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	collectN(t, ch, 1) // e1

	appendLine(t, path, `{ truncated garbage`)
	appendLine(t, path, `{"id":"e3","sessionId":"s","type":"tool.edit"}`)

	got := collectN(t, ch, 1)
	if got[0].ID != "e3" {
		t.Errorf("got %q, want e3 (malformed line must be skipped)", got[0].ID)
	}
}

func TestSourceSkipsUnknownAppendedEventType(t *testing.T) {
	path := t.TempDir() + "/opencode.jsonl"
	writeFile(t, path, `{"id":"e1","sessionId":"s","type":"tool.edit"}`+"\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := opencode.New(path).Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	collectN(t, ch, 1) // e1

	appendLine(t, path, `{"id":"e2","sessionId":"s","type":"session.idle"}`)
	appendLine(t, path, `{"id":"e3","sessionId":"s","type":"tool.edit"}`)

	got := collectN(t, ch, 1)
	if got[0].ID != "e3" {
		t.Errorf("got %q, want e3 (unknown event type must be skipped)", got[0].ID)
	}
}

func TestSourceStopsOnContextCancel(t *testing.T) {
	path := t.TempDir() + "/opencode.jsonl"
	writeFile(t, path, `{"id":"e1","sessionId":"s","type":"tool.edit"}`+"\n")

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := opencode.New(path).Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	collectN(t, ch, 1) // e1, now following

	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			// A late buffered event is fine, but the channel must then close.
			if _, ok := <-ch; ok {
				t.Error("channel still open after cancel")
			}
		}
	case <-time.After(2 * time.Second):
		t.Error("channel not closed within 2s of cancel")
	}
}
