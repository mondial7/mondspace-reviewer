// Package tui is the interactive review queue: an unread queue with a cursor,
// not a live feed. Nothing scrolls away, nothing auto-advances; the cursor moves
// only on a keypress. The model is pure and tested at the Update level.
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oklog/ulid/v2"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/port"
)

type Model struct {
	units    []domain.Unit
	notes    []domain.Note
	store    port.Store
	cursor   int             // index into the visible units
	expanded map[string]bool // unit ID -> expanded
	read     map[string]bool // unit ID -> reviewed (ok)

	unreadOnly bool

	newID func() string
	now   func() time.Time
}

func New(units []domain.Unit, notes []domain.Note, store port.Store) Model {
	return Model{
		units:    units,
		notes:    notes,
		store:    store,
		expanded: map[string]bool{},
		read:     map[string]bool{},
		newID:    func() string { return ulid.Make().String() },
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// WithClock overrides ID and time generation for deterministic tests.
func (m Model) WithClock(newID func() string, now func() time.Time) Model {
	m.newID, m.now = newID, now
	return m
}

// Read reports whether a unit has been reviewed and accepted (an ok note).
func (m Model) Read(unitID string) bool { return m.read[unitID] }

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
	case "o":
		m = m.annotate(domain.NoteOK)
		m.read[mustID(m)] = true
		m.cursor = clamp(m.cursor+1, 0, len(m.visible())-1)
	case "?":
		m = m.annotate(domain.NoteQuestion)
	case "x":
		m = m.annotate(domain.NoteObjection)
	case "d":
		m = m.annotate(domain.NoteDebt)
	case "n":
		m = m.annotate(domain.NoteNote)
	case "tab":
		m.unreadOnly = !m.unreadOnly
		m.cursor = clamp(m.cursor, 0, len(m.visible())-1)
	}
	return m, nil
}

// annotate attaches a note of the given kind to the current unit and persists it.
func (m Model) annotate(kind domain.NoteKind) Model {
	u, ok := m.current()
	if !ok {
		return m
	}
	note := domain.Note{
		ID:        m.newID(),
		SessionID: u.SessionID,
		UnitID:    u.ID,
		Kind:      kind,
		TS:        m.now(),
	}
	m.notes = append(m.notes, note)
	if m.store != nil {
		_ = m.store.AppendNote(note)
	}
	return m
}

// mustID returns the current unit's ID, or "" if there is none.
func mustID(m Model) string {
	if u, ok := m.current(); ok {
		return u.ID
	}
	return ""
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

// VisibleCount is the number of units currently shown.
func (m Model) VisibleCount() int { return len(m.visible()) }

// visible returns the indices of the units currently shown, honouring the
// unread-only toggle.
func (m Model) visible() []int {
	var idx []int
	for i, u := range m.units {
		if m.unreadOnly && m.read[u.ID] {
			continue
		}
		idx = append(idx, i)
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
