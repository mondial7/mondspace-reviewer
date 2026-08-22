// Package tui is the interactive review queue: an unread queue with a cursor,
// not a live feed. Nothing scrolls away, nothing auto-advances; the cursor moves
// only on a keypress. The model is pure and tested at the Update level.
package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	filtering  bool
	query      string

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

// HeadlineReadyMsg carries a model-generated headline back into the queue once
// the summarizer returns. The queue never blocks waiting for it.
type HeadlineReadyMsg struct {
	UnitID   string
	Headline domain.Headline
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ready, ok := msg.(HeadlineReadyMsg); ok {
		return m.fillHeadline(ready), nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.filtering {
		return m.updateFilter(key), nil
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
	case "/":
		m.filtering = true
		m.query = ""
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

// updateFilter handles keys while the filter prompt is open. Enter commits the
// query, Esc cancels it, and typing edits it live.
func (m Model) updateFilter(key tea.KeyMsg) Model {
	switch key.Type {
	case tea.KeyEnter:
		m.filtering = false
	case tea.KeyEsc:
		m.filtering = false
		m.query = ""
	case tea.KeyBackspace:
		if m.query != "" {
			m.query = m.query[:len(m.query)-1]
		}
	case tea.KeyRunes, tea.KeySpace:
		m.query += string(key.Runes)
	}
	m.cursor = clamp(m.cursor, 0, len(m.visible())-1)
	return m
}

// fillHeadline swaps in a model headline for the matching unit. A message for
// an unknown unit is ignored.
func (m Model) fillHeadline(ready HeadlineReadyMsg) Model {
	units := make([]domain.Unit, len(m.units))
	copy(units, m.units)
	for i := range units {
		if units[i].ID == ready.UnitID {
			units[i].Headline = ready.Headline
			break
		}
	}
	m.units = units
	return m
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

var (
	cursorStyle   = lipgloss.NewStyle().Bold(true)
	statedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	inferredStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	flagStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
)

// View renders the queue: one scannable line per unit, expanding to slots on
// demand. Stated and inferred rationale differ in both colour and label word.
func (m Model) View() string {
	var b strings.Builder
	if m.filtering {
		b.WriteString("/" + m.query + "\n")
	}
	for pos, i := range m.visible() {
		u := m.units[i]
		marker := "  "
		if pos == m.cursor {
			marker = cursorStyle.Render("▶ ")
		}
		read := " "
		if m.read[u.ID] {
			read = "✓"
		}
		b.WriteString(marker + read + " [" + u.ID + "] " + strings.Join(u.Files, ", ") + "  " + renderFlags(u.Flags) + "\n")
		if m.expanded[u.ID] {
			b.WriteString(m.details(u))
		}
	}
	return b.String()
}

func (m Model) details(u domain.Unit) string {
	var b strings.Builder
	b.WriteString("    WHAT  " + u.Headline.Text + "\n")
	b.WriteString("    WHY   " + renderWhy(u.Headline) + "\n")
	for _, n := range m.notes {
		if n.UnitID != u.ID {
			continue
		}
		line := "    NOTE  " + string(n.Kind)
		if n.Text != "" {
			line += ": " + n.Text
		}
		if n.SupersededBy != "" {
			line += " (superseded by " + n.SupersededBy + ")"
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func renderFlags(flags []domain.Flag) string {
	if len(flags) == 0 {
		return "—"
	}
	names := make([]string, len(flags))
	for i, f := range flags {
		names[i] = string(f)
	}
	return flagStyle.Render(strings.Join(names, " · "))
}

// renderWhy keeps stated and inferred rationale visually distinct: different
// colour and different label word.
func renderWhy(h domain.Headline) string {
	if h.WhySrc == domain.WhyStated {
		return statedStyle.Render("stated: " + h.Why)
	}
	if h.Why == "" {
		return inferredStyle.Render("inferred: (none stated)")
	}
	return inferredStyle.Render("inferred: " + h.Why)
}

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
		if m.query != "" && !m.matches(u, m.query) {
			continue
		}
		idx = append(idx, i)
	}
	return idx
}

// matches reports whether a unit satisfies the filter query, testing its files,
// flags, and the kinds of any notes attached to it.
func (m Model) matches(u domain.Unit, q string) bool {
	q = strings.ToLower(q)
	for _, f := range u.Files {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	for _, f := range u.Flags {
		if strings.Contains(strings.ToLower(string(f)), q) {
			return true
		}
	}
	for _, n := range m.notes {
		if n.UnitID == u.ID && strings.Contains(strings.ToLower(string(n.Kind)), q) {
			return true
		}
	}
	return false
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
