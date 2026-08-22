package replay_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/source/replay"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

func collect(t *testing.T, ch <-chan domain.Event) []domain.Event {
	t.Helper()
	var got []domain.Event
	for e := range ch {
		got = append(got, e)
	}
	return got
}

func TestReplaySkipsMalformedLine(t *testing.T) {
	content := `{"id":"e1","kind":"edit","files":["a.go"]}
{ this is not valid json
{"id":"e3","kind":"edit","files":["c.go"]}
`
	file := filepath.Join(t.TempDir(), "partial.jsonl")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	ch, err := replay.New(file).Events(context.Background())
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	got := collect(t, ch)

	if len(got) != 2 {
		t.Fatalf("emitted %d events, want 2 (malformed line skipped)", len(got))
	}
	if got[0].ID != "e1" || got[1].ID != "e3" {
		t.Errorf("got IDs %q,%q, want e1,e3", got[0].ID, got[1].ID)
	}
}

func TestReplayEmitsEveryEventInOrder(t *testing.T) {
	file := filepath.Join("..", "..", "..", "..", "testdata", "sessions", "basic.jsonl")
	src := replay.New(file)

	ch, err := src.Events(context.Background())
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	got := collect(t, ch)

	if len(got) != 32 {
		t.Fatalf("emitted %d events, want 32", len(got))
	}
	if got[0].Kind != domain.KindPrompt {
		t.Errorf("first event kind = %q, want prompt", got[0].Kind)
	}
	if got[0].ID != "01K39ZQK8T0000000000000001" {
		t.Errorf("first event ID = %q", got[0].ID)
	}
	if last := got[len(got)-1]; last.Files[0] != "README.md" {
		t.Errorf("last event file = %v, want README.md", last.Files)
	}
}
