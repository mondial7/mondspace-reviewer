package usecase_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

func loadFixture(t *testing.T, name string) []domain.Event {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", "sessions", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var events []domain.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e domain.Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("decoding fixture line: %v", err)
		}
		events = append(events, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

func ev(id string, kind domain.Kind, files ...string) domain.Event {
	return domain.Event{ID: id, SessionID: "sess-basic", Kind: kind, Files: files}
}

func evAt(id string, kind domain.Kind, ts time.Time, files ...string) domain.Event {
	e := ev(id, kind, files...)
	e.TS = ts
	return e
}

func TestClusterEmptyLog(t *testing.T) {
	units := usecase.Cluster("sess-basic", nil)

	if len(units) != 0 {
		t.Errorf("Cluster(empty) = %d units, want 0", len(units))
	}
}

func TestClusterBasicFixture(t *testing.T) {
	events := loadFixture(t, "basic.jsonl")

	units := usecase.Cluster("sess-basic", events)

	if len(units) != 8 {
		t.Fatalf("got %d units, want 8", len(units))
	}
	wantCounts := []int{2, 2, 3, 3, 2, 12, 1, 1}
	wantFiles := [][]string{
		{"auth/token.go", "auth/port.go"},
		{"auth/token_test.go"},
		{"http/middleware.go", "http/routes.go"},
		{"go.mod", "go.sum"},
		{"auth/token.go", "auth/token_test.go"},
		{"store/jsonl/store.go", "store/jsonl/store_test.go", "domain/session.go"},
		{"cmd/msr/main.go"},
		{"README.md"},
	}
	for i, u := range units {
		if len(u.EventIDs) != wantCounts[i] {
			t.Errorf("unit %d has %d events, want %d", i+1, len(u.EventIDs), wantCounts[i])
		}
		if !equalStrings(u.Files, wantFiles[i]) {
			t.Errorf("unit %d files = %v, want %v", i+1, u.Files, wantFiles[i])
		}
	}
	// The prompt event must not appear in any unit.
	for _, u := range units {
		for _, id := range u.EventIDs {
			if id == "01K39ZQK8T0000000000000001" {
				t.Errorf("prompt event leaked into unit %s", u.ID)
			}
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestUnitIDsAreSequentialAndDeterministic(t *testing.T) {
	events := []domain.Event{
		ev("e1", domain.KindEdit, "a.go"),
		ev("e2", domain.KindBatchEnd),
		ev("e3", domain.KindEdit, "b.go"),
		ev("e4", domain.KindBatchEnd),
		ev("e5", domain.KindEdit, "c.go"),
		ev("e6", domain.KindBatchEnd),
	}

	first := usecase.Cluster("sess-basic", events)
	second := usecase.Cluster("sess-basic", events)

	wantIDs := []string{"sess-basic-u001", "sess-basic-u002", "sess-basic-u003"}
	if len(first) != len(wantIDs) {
		t.Fatalf("got %d units, want %d", len(first), len(wantIDs))
	}
	for i, want := range wantIDs {
		if first[i].ID != want {
			t.Errorf("unit %d ID = %q, want %q", i, first[i].ID, want)
		}
		if second[i].ID != first[i].ID {
			t.Errorf("nondeterministic ID at %d: %q vs %q", i, first[i].ID, second[i].ID)
		}
	}
}

func TestUnitFilesAreDedupedUnionInFirstSeenOrder(t *testing.T) {
	events := []domain.Event{
		ev("e1", domain.KindEdit, "http/mw.go"),
		ev("e2", domain.KindEdit, "http/mw.go"),
		ev("e3", domain.KindEdit, "auth/token.go", "http/mw.go"),
		ev("e4", domain.KindBatchEnd),
	}

	units := usecase.Cluster("sess-basic", events)

	if len(units) != 1 {
		t.Fatalf("got %d units, want 1", len(units))
	}
	want := []string{"http/mw.go", "auth/token.go"}
	got := units[0].Files
	if len(got) != len(want) {
		t.Fatalf("Files = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Files = %v, want %v", got, want)
		}
	}
}

func TestPromptSealsAndIsNotAMember(t *testing.T) {
	events := []domain.Event{
		ev("e1", domain.KindEdit, "a.go"),
		ev("e2", domain.KindPrompt),
		ev("e3", domain.KindEdit, "b.go"),
	}

	units := usecase.Cluster("sess-basic", events)

	if len(units) != 2 {
		t.Fatalf("got %d units, want 2 (prompt seals)", len(units))
	}
	for _, u := range units {
		for _, id := range u.EventIDs {
			if id == "e2" {
				t.Errorf("prompt e2 must not be a member of unit %s", u.ID)
			}
		}
	}
	if got := units[0].EventIDs; len(got) != 1 || got[0] != "e1" {
		t.Errorf("unit 0 EventIDs = %v, want [e1]", got)
	}
	if got := units[1].EventIDs; len(got) != 1 || got[0] != "e3" {
		t.Errorf("unit 1 EventIDs = %v, want [e3]", got)
	}
}

func TestEndOfLogSealsTrailingUnit(t *testing.T) {
	events := []domain.Event{
		ev("e1", domain.KindEdit, "a.go"),
		ev("e2", domain.KindBatchEnd),
		ev("e3", domain.KindEdit, "b.go"),
		ev("e4", domain.KindEdit, "c.go"),
		// no trailing batch_end
	}

	units := usecase.Cluster("sess-basic", events)

	if len(units) != 2 {
		t.Fatalf("got %d units, want 2", len(units))
	}
	last := units[1]
	if !last.Sealed {
		t.Error("trailing unit must be sealed at end of log")
	}
	if got := last.EventIDs; len(got) != 2 || got[0] != "e3" || got[1] != "e4" {
		t.Errorf("trailing EventIDs = %v, want [e3 e4]", got)
	}
}

func TestTwelveActionsSealUnit(t *testing.T) {
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	var events []domain.Event
	for i := 0; i < 14; i++ {
		id := fmt.Sprintf("e%02d", i)
		events = append(events, evAt(id, domain.KindEdit, base.Add(time.Duration(i)*time.Second), "a.go"))
	}

	units := usecase.Cluster("sess-basic", events)

	if len(units) != 2 {
		t.Fatalf("got %d units, want 2 (12-event cap)", len(units))
	}
	if got := len(units[0].EventIDs); got != 12 {
		t.Errorf("unit 0 has %d events, want 12", got)
	}
	if got := len(units[1].EventIDs); got != 2 {
		t.Errorf("unit 1 has %d events, want 2", got)
	}
}

func TestTimestampGapSealsUnit(t *testing.T) {
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	events := []domain.Event{
		evAt("e1", domain.KindEdit, base, "a.go"),
		evAt("e2", domain.KindEdit, base.Add(2*time.Second), "b.go"),
		// 5s gap after e2 seals the first unit before e3.
		evAt("e3", domain.KindEdit, base.Add(7*time.Second), "c.go"),
	}

	units := usecase.Cluster("sess-basic", events)

	if len(units) != 2 {
		t.Fatalf("got %d units, want 2", len(units))
	}
	if got := units[0].EventIDs; len(got) != 2 || got[0] != "e1" || got[1] != "e2" {
		t.Errorf("unit 0 EventIDs = %v, want [e1 e2]", got)
	}
	if got := units[1].EventIDs; len(got) != 1 || got[0] != "e3" {
		t.Errorf("unit 1 EventIDs = %v, want [e3]", got)
	}
}

func TestBatchEndWithNoActionsEmitsNothing(t *testing.T) {
	events := []domain.Event{
		ev("e1", domain.KindBatchEnd),
		ev("e2", domain.KindEdit, "a.go"),
		ev("e3", domain.KindBatchEnd),
		ev("e4", domain.KindBatchEnd),
	}

	units := usecase.Cluster("sess-basic", events)

	if len(units) != 1 {
		t.Fatalf("got %d units, want 1 (empty boundaries must not seal)", len(units))
	}
	if got := units[0].EventIDs; len(got) != 1 || got[0] != "e2" {
		t.Errorf("EventIDs = %v, want [e2]", got)
	}
}

func TestBatchEndSealsPrecedingActions(t *testing.T) {
	events := []domain.Event{
		ev("e1", domain.KindEdit, "a.go"),
		ev("e2", domain.KindWrite, "b.go"),
		ev("e3", domain.KindBatchEnd),
	}

	units := usecase.Cluster("sess-basic", events)

	if len(units) != 1 {
		t.Fatalf("got %d units, want 1", len(units))
	}
	got := units[0]
	if !got.Sealed {
		t.Error("unit should be sealed at batch_end")
	}
	want := []string{"e1", "e2"}
	if len(got.EventIDs) != len(want) {
		t.Fatalf("EventIDs = %v, want %v", got.EventIDs, want)
	}
	for i := range want {
		if got.EventIDs[i] != want[i] {
			t.Errorf("EventIDs = %v, want %v", got.EventIDs, want)
		}
	}
}
