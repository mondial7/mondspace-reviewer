package tui_test

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/presenter/tui"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func threeUnits() []domain.Unit {
	return []domain.Unit{
		{ID: "s-u001", SessionID: "s", Files: []string{"a.go"}},
		{ID: "s-u002", SessionID: "s", Files: []string{"b.go"}},
		{ID: "s-u003", SessionID: "s", Files: []string{"c.go"}},
	}
}

// send applies a sequence of key runes to the model, returning the final model.
func send(m tui.Model, runes ...rune) tui.Model {
	for _, r := range runes {
		next, _ := m.Update(key(r))
		m = next.(tui.Model)
	}
	return m
}

// recordingStore captures notes the TUI persists.
type recordingStore struct{ notes []domain.Note }

func (s *recordingStore) AppendEvent(domain.Event) error { return nil }
func (s *recordingStore) AppendUnit(domain.Unit) error   { return nil }
func (s *recordingStore) AppendNote(n domain.Note) error { s.notes = append(s.notes, n); return nil }
func (s *recordingStore) Load(string) (domain.Session, error) {
	return domain.Session{}, nil
}

func fixedClock(m tui.Model) tui.Model {
	i := 0
	return m.WithClock(
		func() string { i++; return "n" + string(rune('0'+i)) },
		func() time.Time { return time.Unix(0, 0).UTC() },
	)
}

func TestAnnotateOKWritesNoteAdvancesAndMarksRead(t *testing.T) {
	store := &recordingStore{}
	m := fixedClock(tui.New(threeUnits(), nil, store))

	m = send(m, 'o')

	if len(store.notes) != 1 {
		t.Fatalf("store got %d notes, want 1", len(store.notes))
	}
	n := store.notes[0]
	if n.Kind != domain.NoteOK || n.UnitID != "s-u001" || n.SessionID != "s" {
		t.Errorf("note = %+v, want ok on s-u001 in s", n)
	}
	if m.Cursor() != 1 {
		t.Errorf("ok should advance the cursor, got %d", m.Cursor())
	}
	if !m.Read("s-u001") {
		t.Error("ok should mark the unit read")
	}
}

func TestAnnotateOtherKindsDoNotAdvance(t *testing.T) {
	cases := []struct {
		key  rune
		kind domain.NoteKind
	}{
		{'?', domain.NoteQuestion},
		{'x', domain.NoteObjection},
		{'d', domain.NoteDebt},
		{'n', domain.NoteNote},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			store := &recordingStore{}
			m := fixedClock(tui.New(threeUnits(), nil, store))

			m = send(m, tc.key)

			if len(store.notes) != 1 || store.notes[0].Kind != tc.kind {
				t.Fatalf("notes = %+v, want one %s", store.notes, tc.kind)
			}
			if m.Cursor() != 0 {
				t.Errorf("%s should not advance the cursor, got %d", tc.kind, m.Cursor())
			}
			if m.Read("s-u001") {
				t.Errorf("%s should not mark the unit read", tc.kind)
			}
		})
	}
}

func TestTabTogglesUnreadOnly(t *testing.T) {
	m := fixedClock(tui.New(threeUnits(), nil, &recordingStore{}))

	m = send(m, 'o') // mark u1 read, cursor -> u2
	if m.VisibleCount() != 3 {
		t.Fatalf("all units visible before toggle, got %d", m.VisibleCount())
	}

	tabbed, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = tabbed.(tui.Model)
	if m.VisibleCount() != 2 {
		t.Errorf("unread-only should hide the 1 read unit, visible = %d, want 2", m.VisibleCount())
	}

	tabbed, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = tabbed.(tui.Model)
	if m.VisibleCount() != 3 {
		t.Errorf("toggling off should show all 3, got %d", m.VisibleCount())
	}
}

func TestSlashFiltersByFileFlagAndKind(t *testing.T) {
	units := []domain.Unit{
		{ID: "s-u001", SessionID: "s", Files: []string{"auth/token.go"}, Flags: []domain.Flag{domain.FlagNoTest}},
		{ID: "s-u002", SessionID: "s", Files: []string{"http/mw.go"}},
		{ID: "s-u003", SessionID: "s", Files: []string{"auth/util.go"}, Flags: []domain.Flag{domain.FlagLarge}},
	}
	notes := []domain.Note{{ID: "n1", UnitID: "s-u002", Kind: domain.NoteObjection}}

	filterTo := func(q string) int {
		m := tui.New(units, notes, nil)
		m = send(m, '/')
		m = send(m, []rune(q)...)
		return m.VisibleCount()
	}

	if n := filterTo("auth"); n != 2 {
		t.Errorf("filter 'auth' (file) = %d, want 2", n)
	}
	if n := filterTo("large"); n != 1 {
		t.Errorf("filter 'large' (flag) = %d, want 1", n)
	}
	if n := filterTo("objection"); n != 1 {
		t.Errorf("filter 'objection' (note kind) = %d, want 1", n)
	}
}

func TestModelStartsAtFirstUnit(t *testing.T) {
	m := tui.New(threeUnits(), nil, nil)
	if m.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0", m.Cursor())
	}
}

func enter(m tui.Model) tui.Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return next.(tui.Model)
}

func TestEnterTogglesExpand(t *testing.T) {
	m := tui.New(threeUnits(), nil, nil)
	if m.IsExpanded() {
		t.Fatal("unit should start collapsed")
	}
	m = enter(m)
	if !m.IsExpanded() {
		t.Error("enter should expand the current unit")
	}
	m = enter(m)
	if m.IsExpanded() {
		t.Error("enter again should collapse")
	}

	// Expansion is per-unit: moving to another unit shows its own state.
	m = enter(m)      // expand u1
	m = send(m, 'j')  // move to u2
	if m.IsExpanded() {
		t.Error("u2 should be collapsed independently of u1")
	}
}

func TestGoToTopAndBottom(t *testing.T) {
	m := tui.New(threeUnits(), nil, nil)

	m = send(m, 'j') // cursor 1
	m = send(m, 'G')
	if m.Cursor() != 2 {
		t.Errorf("G: cursor = %d, want 2 (bottom)", m.Cursor())
	}
	m = send(m, 'g')
	if m.Cursor() != 0 {
		t.Errorf("g: cursor = %d, want 0 (top)", m.Cursor())
	}
}

func TestCursorMovesAndClamps(t *testing.T) {
	m := tui.New(threeUnits(), nil, nil)

	m = send(m, 'j')
	if m.Cursor() != 1 {
		t.Errorf("after j: cursor = %d, want 1", m.Cursor())
	}
	m = send(m, 'j', 'j', 'j') // past the end
	if m.Cursor() != 2 {
		t.Errorf("cursor should clamp at 2, got %d", m.Cursor())
	}
	m = send(m, 'k', 'k', 'k', 'k') // past the top
	if m.Cursor() != 0 {
		t.Errorf("cursor should clamp at 0, got %d", m.Cursor())
	}
}
