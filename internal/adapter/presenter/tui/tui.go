// Package tui is the interactive review queue: an unread queue with a cursor,
// not a live feed. Nothing scrolls away, nothing auto-advances; the cursor moves
// only on a keypress. The model is pure and tested at the Update level.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/port"
)

type Model struct {
	units  []domain.Unit
	notes  []domain.Note
	store  port.Store
	cursor int // index into the visible units
}

func New(units []domain.Unit, notes []domain.Note, store port.Store) Model {
	return Model{units: units, notes: notes, store: store}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "j":
		m.cursor = clamp(m.cursor+1, 0, len(m.visible())-1)
	case "k":
		m.cursor = clamp(m.cursor-1, 0, len(m.visible())-1)
	}
	return m, nil
}

func (m Model) View() string { return "" }

// Cursor is the position within the visible units.
func (m Model) Cursor() int { return m.cursor }

// visible returns the indices of the units currently shown.
func (m Model) visible() []int {
	idx := make([]int, len(m.units))
	for i := range m.units {
		idx[i] = i
	}
	return idx
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
