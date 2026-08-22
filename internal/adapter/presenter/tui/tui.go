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
	units    []domain.Unit
	notes    []domain.Note
	store    port.Store
	cursor   int             // index into the visible units
	expanded map[string]bool // unit ID -> expanded
}

func New(units []domain.Unit, notes []domain.Note, store port.Store) Model {
	return Model{units: units, notes: notes, store: store, expanded: map[string]bool{}}
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
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = clamp(len(m.visible())-1, 0, len(m.visible())-1)
	case "enter":
		if u, ok := m.current(); ok {
			m.expanded[u.ID] = !m.expanded[u.ID]
		}
	}
	return m, nil
}

func (m Model) View() string { return "" }

// Cursor is the position within the visible units.
func (m Model) Cursor() int { return m.cursor }

// IsExpanded reports whether the unit under the cursor is expanded.
func (m Model) IsExpanded() bool {
	u, ok := m.current()
	return ok && m.expanded[u.ID]
}

// current returns the unit under the cursor.
func (m Model) current() (domain.Unit, bool) {
	vis := m.visible()
	if m.cursor < 0 || m.cursor >= len(vis) {
		return domain.Unit{}, false
	}
	return m.units[vis[m.cursor]], true
}

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
