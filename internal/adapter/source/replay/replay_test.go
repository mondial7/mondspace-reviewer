package replay_test

import (
	"context"
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
