package tui_test

import (
	"testing"

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
