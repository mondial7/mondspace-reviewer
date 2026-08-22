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
