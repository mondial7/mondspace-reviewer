package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

func TestEventDecodesRecordedLine(t *testing.T) {
	line := `{"id":"01K39ZQK8T0000000000000002","session_id":"sess-basic",` +
		`"ts":"2026-08-22T10:00:04Z","source":"replay","kind":"edit","tool":"Edit",` +
		`"files":["auth/token.go"],"stated_intent":"extract validation",` +
		`"raw":{"lines_added":34}}`

	var got domain.Event
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := domain.Event{
		ID:           "01K39ZQK8T0000000000000002",
		SessionID:    "sess-basic",
		TS:           time.Date(2026, 8, 22, 10, 0, 4, 0, time.UTC),
		Source:       "replay",
		Kind:         domain.KindEdit,
		Tool:         "Edit",
		Files:        []string{"auth/token.go"},
		StatedIntent: "extract validation",
		Raw:          json.RawMessage(`{"lines_added":34}`),
	}

	if got.ID != want.ID || got.SessionID != want.SessionID || !got.TS.Equal(want.TS) {
		t.Errorf("identity: got %+v, want %+v", got, want)
	}
	if got.Source != want.Source || got.Kind != want.Kind || got.Tool != want.Tool {
		t.Errorf("classification: got %+v, want %+v", got, want)
	}
	if len(got.Files) != 1 || got.Files[0] != want.Files[0] {
		t.Errorf("Files = %v, want %v", got.Files, want.Files)
	}
	if got.StatedIntent != want.StatedIntent {
		t.Errorf("StatedIntent = %q, want %q", got.StatedIntent, want.StatedIntent)
	}
	if string(got.Raw) != string(want.Raw) {
		t.Errorf("Raw = %s, want %s", got.Raw, want.Raw)
	}
}

func TestEventRoundTripsPreservingRaw(t *testing.T) {
	first := domain.Event{
		ID:           "01K39ZQK8T0000000000000002",
		SessionID:    "sess-basic",
		TS:           time.Date(2026, 8, 22, 10, 0, 4, 0, time.UTC),
		Source:       "replay",
		Kind:         domain.KindEdit,
		Tool:         "Edit",
		Files:        []string{"auth/token.go", "auth/port.go"},
		StatedIntent: "extract validation",
		Raw:          json.RawMessage(`{"lines_added":34,"nested":{"a":1}}`),
	}

	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var second domain.Event
	if err := json.Unmarshal(encoded, &second); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if string(second.Raw) != string(first.Raw) {
		t.Errorf("Raw not preserved: got %s, want %s", second.Raw, first.Raw)
	}
	if second.ID != first.ID || !second.TS.Equal(first.TS) || second.Kind != first.Kind {
		t.Errorf("round-trip changed fields: got %+v, want %+v", second, first)
	}
	if len(second.Files) != len(first.Files) {
		t.Errorf("Files length changed: got %v, want %v", second.Files, first.Files)
	}
}
