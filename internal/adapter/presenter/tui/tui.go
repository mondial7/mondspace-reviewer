// Package tui is the interactive review queue: an unread queue with a cursor,
// not a live feed. Nothing scrolls away, nothing auto-advances; the cursor moves
// only on a keypress. The model is pure and tested at the Update level.
package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/oklog/ulid/v2"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

type Model struct {
	units      []domain.Unit
	notes      []domain.Note
	store      port.Store
	cursor     int                    // index into the visible units
	expanded   map[string]bool        // unit ID -> expanded
	read       map[string]bool        // unit ID -> reviewed (ok)
	summarized map[string]bool        // unit ID -> model headline filled in
	diffs      map[string]domain.Diff // unit ID -> fetched diff

	unreadOnly bool
	filtering  bool
	query      string

	asking   bool
	askScope domain.AskScope
	question string
	answer   string

	base  string // relativise absolute file paths against this dir
	width int    // terminal width, for rules and wrapping

	newID func() string
	now   func() time.Time

	// summarize, when set, turns a unit into a HeadlineReadyMsg asynchronously.
	summarize func(domain.Unit) tea.Msg
	// ask, when set, answers a question asynchronously.
	ask func(scope domain.AskScope, unit domain.Unit, question string) tea.Msg
	// fetchDiff, when set, loads a unit's diff on expand (a DiffReadyMsg).
	fetchDiff func(domain.Unit) tea.Msg
}

// AnswerReadyMsg carries an interrogation answer back into the queue.
type AnswerReadyMsg struct{ Text string }

// UnitAddedMsg streams a newly-sealed unit into a live queue.
type UnitAddedMsg struct{ Unit domain.Unit }

// DiffReadyMsg carries a unit's fetched diff back into the queue.
type DiffReadyMsg struct {
	UnitID string
	Diff   domain.Diff
}

func New(units []domain.Unit, notes []domain.Note, store port.Store) Model {
	return Model{
		units:      units,
		notes:      notes,
		store:      store,
		expanded:   map[string]bool{},
		read:       map[string]bool{},
		summarized: map[string]bool{},
		diffs:      map[string]domain.Diff{},
		newID:      func() string { return ulid.Make().String() },
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// WithClock overrides ID and time generation for deterministic tests.
func (m Model) WithClock(newID func() string, now func() time.Time) Model {
	m.newID, m.now = newID, now
	return m
}

// RelativeTo displays absolute file paths relative to base (e.g. the repo root).
func (m Model) RelativeTo(base string) Model {
	if abs, err := filepath.Abs(base); err == nil {
		m.base = abs
	} else {
		m.base = base
	}
	return m
}

// WithSummarize wires an async headline generator; each unit is summarized on
// Init, filling in over the mechanical headlines as results arrive.
func (m Model) WithSummarize(fn func(domain.Unit) tea.Msg) Model {
	m.summarize = fn
	return m
}

// WithAsk wires an async interrogation handler used by the a/A keys.
func (m Model) WithAsk(fn func(domain.AskScope, domain.Unit, string) tea.Msg) Model {
	m.ask = fn
	return m
}

// WithDiff wires an async diff loader; a unit's diff is fetched when it expands.
func (m Model) WithDiff(fn func(domain.Unit) tea.Msg) Model {
	m.fetchDiff = fn
	return m
}

// Read reports whether a unit has been reviewed and accepted (an ok note).
func (m Model) Read(unitID string) bool { return m.read[unitID] }

func (m Model) Init() tea.Cmd {
	if m.summarize == nil {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(m.units))
	for _, u := range m.units {
		u := u
		cmds = append(cmds, func() tea.Msg { return m.summarize(u) })
	}
	return tea.Batch(cmds...)
}

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
	if ans, ok := msg.(AnswerReadyMsg); ok {
		m.answer = ans.Text
		return m, nil
	}
	if dr, ok := msg.(DiffReadyMsg); ok {
		m.diffs[dr.UnitID] = dr.Diff
		return m, nil
	}
	if added, ok := msg.(UnitAddedMsg); ok {
		units := make([]domain.Unit, len(m.units)+1)
		copy(units, m.units)
		units[len(m.units)] = added.Unit
		m.units = units
		if m.summarize != nil {
			u := added.Unit
			return m, func() tea.Msg { return m.summarize(u) }
		}
		return m, nil
	}
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = ws.Width
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	// Ctrl+C must always quit, in any mode — in raw mode it is a key, not a
	// signal, so nothing else will exit the program.
	if key.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.asking {
		return m.updateAsk(key)
	}
	if m.filtering {
		return m.updateFilter(key), nil
	}
	switch key.String() {
	case "j", "down":
		m.cursor = clamp(m.cursor+1, 0, len(m.visible())-1)
	case "k", "up":
		m.cursor = clamp(m.cursor-1, 0, len(m.visible())-1)
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = clamp(len(m.visible())-1, 0, len(m.visible())-1)
	case "enter":
		if u, ok := m.current(); ok {
			m.expanded[u.ID] = !m.expanded[u.ID]
			if m.expanded[u.ID] && m.fetchDiff != nil {
				if _, loaded := m.diffs[u.ID]; !loaded {
					uu := u
					return m, func() tea.Msg { return m.fetchDiff(uu) }
				}
			}
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
	case "a":
		m.asking, m.askScope, m.question, m.answer = true, domain.AskUnit, "", ""
	case "A":
		m.asking, m.askScope, m.question, m.answer = true, domain.AskSession, "", ""
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

// updateAsk handles keys while the ask prompt is open. Enter submits, Esc
// cancels, and every other key edits the question text.
func (m Model) updateAsk(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEnter:
		q, scope := m.question, m.askScope
		m.asking, m.question = false, ""
		if m.ask == nil || q == "" {
			return m, nil
		}
		unit, _ := m.current()
		return m, func() tea.Msg { return m.ask(scope, unit, q) }
	case tea.KeyEsc:
		m.asking, m.question = false, ""
	case tea.KeyBackspace:
		if m.question != "" {
			m.question = m.question[:len(m.question)-1]
		}
	case tea.KeyRunes, tea.KeySpace:
		m.question += string(key.Runes)
	}
	return m, nil
}

// Asking reports whether the ask prompt is open.
func (m Model) Asking() bool { return m.asking }

// AskScope is the scope of the current question.
func (m Model) AskScope() domain.AskScope { return m.askScope }

// Question is the text typed into the ask prompt.
func (m Model) Question() string { return m.question }

// Answer is the last answer received.
func (m Model) Answer() string { return m.answer }

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
	m.summarized[ready.UnitID] = true
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
	titleStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	ruleStyle     = lipgloss.NewStyle().Faint(true)
	labelStyle    = lipgloss.NewStyle().Faint(true)
	cursorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")) // magenta
	handleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")) // cyan
	statedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))            // green
	inferredStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))            // yellow
	flagStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))            // red
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

const helpLine = "j/k move · enter expand · o/?/x/d/n note · / filter · tab unread · a/A ask · q quit"

// View frames the queue: a header, one calm scannable line per unit (expanding
// to indented detail on demand), and a footer of keys. It is never blank.
func (m Model) View() string {
	rule := m.rule()
	var b strings.Builder

	b.WriteString("  " + titleStyle.Render("mondspace-reviewer") + "   " +
		dimStyle.Render(fmt.Sprintf("%d %s · %d unread", len(m.units), plural("unit", len(m.units)), m.unreadCount())) + "\n")
	b.WriteString(rule + "\n")

	if m.filtering {
		b.WriteString("  " + labelStyle.Render("filter ›") + " " + m.query + "\n")
	}
	if m.asking {
		b.WriteString("  " + labelStyle.Render("ask ["+string(m.askScope)+"] ›") + " " + m.question + "\n")
	}
	if m.answer != "" {
		b.WriteString("  " + labelStyle.Render("answer ›") + " " + m.answer + "\n")
	}

	visible := m.visible()
	if len(visible) == 0 {
		msg := "No units to review yet."
		if len(m.units) > 0 {
			msg = "No units match the current filter."
		}
		b.WriteString("\n  " + dimStyle.Render(msg) + "\n")
	}

	for pos, i := range visible {
		u := m.units[i]
		b.WriteString(m.unitLine(u, pos == m.cursor))
		if m.expanded[u.ID] {
			b.WriteString("\n" + m.details(u) + "\n")
		}
	}

	b.WriteString(rule + "\n")
	b.WriteString("  " + dimStyle.Render(helpLine) + "\n")
	return b.String()
}

// unitLine is the one-line collapsed form: cursor, read mark, short handle,
// files, and flags.
func (m Model) unitLine(u domain.Unit, selected bool) string {
	cursor := "  "
	if selected {
		cursor = cursorStyle.Render("▸ ")
	}
	mark := "  "
	if m.read[u.ID] {
		mark = okStyle.Render("✓ ")
	}
	// Once the model has summarized a unit, the collapsed line reads as a
	// storyline sentence; until then it anchors on the changed files.
	primary := m.relFiles(u.Files)
	if m.summarized[u.ID] && u.Headline.Text != "" {
		primary = u.Headline.Text
	}
	line := cursor + mark + handleStyle.Render(shortHandle(u.ID)) + "  " + primary
	if len(u.Flags) > 0 {
		line += "  " + renderFlags(u.Flags)
	}
	return line + "\n"
}

func (m Model) details(u domain.Unit) string {
	const ind = "      "
	var b strings.Builder
	b.WriteString(ind + labelStyle.Render("what ") + " " + u.Headline.Text + "\n")
	b.WriteString(ind + labelStyle.Render("why  ") + " " + renderWhy(u.Headline) + "\n")
	b.WriteString(ind + labelStyle.Render("files") + " " + m.relFiles(u.Files) + "\n")
	for _, n := range m.notes {
		if n.UnitID != u.ID {
			continue
		}
		b.WriteString(ind + labelStyle.Render("note ") + " " + renderNote(n) + "\n")
	}
	b.WriteString(ind + m.renderDiff(u) + "\n")
	b.WriteString(ind + dimStyle.Render("id     "+u.ID) + "\n")
	return b.String()
}

// renderDiff shows the unit's code change (fetched on expand), coloured and
// capped so a large change stays scannable.
func (m Model) renderDiff(u domain.Unit) string {
	d, ok := m.diffs[u.ID]
	if !ok {
		if m.fetchDiff != nil {
			return labelStyle.Render("diff ") + " " + dimStyle.Render("loading…")
		}
		return labelStyle.Render("diff ") + " " + dimStyle.Render("—")
	}
	lines := strings.Split(strings.TrimRight(d.Text, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return labelStyle.Render("diff ") + " " + dimStyle.Render("(no changes)")
	}

	var b strings.Builder
	b.WriteString(labelStyle.Render("diff ") + "\n")
	const maxLines = 40
	for i, ln := range lines {
		if i >= maxLines {
			b.WriteString("        " + dimStyle.Render(fmt.Sprintf("… %d more lines", len(lines)-maxLines)) + "\n")
			break
		}
		b.WriteString("        " + diffLineStyle(ln).Render(ln) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func diffLineStyle(line string) lipgloss.Style {
	switch {
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		return statedStyle // green
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		return flagStyle // red
	case strings.HasPrefix(line, "@@"):
		return handleStyle // cyan
	default:
		return dimStyle
	}
}

func renderNote(n domain.Note) string {
	sym, style := noteGlyph(n.Kind)
	s := style.Render(sym + " " + string(n.Kind))
	if n.Text != "" {
		s += " — " + n.Text
	}
	if n.SupersededBy != "" {
		s += dimStyle.Render(" (superseded by " + n.SupersededBy + ")")
	}
	return s
}

func noteGlyph(kind domain.NoteKind) (string, lipgloss.Style) {
	switch kind {
	case domain.NoteOK:
		return "✓", okStyle
	case domain.NoteObjection:
		return "✗", flagStyle
	case domain.NoteQuestion:
		return "?", inferredStyle
	case domain.NoteDebt:
		return "⚑", inferredStyle
	default:
		return "·", dimStyle
	}
}

func renderFlags(flags []domain.Flag) string {
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

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// shortHandle trims the session prefix, leaving the readable unit suffix (u013).
func shortHandle(id string) string {
	if i := strings.LastIndex(id, "-"); i >= 0 && i+1 < len(id) {
		return id[i+1:]
	}
	return id
}

func (m Model) unreadCount() int {
	n := 0
	for _, u := range m.units {
		if !m.read[u.ID] {
			n++
		}
	}
	return n
}

func (m Model) rule() string {
	w := m.width
	if w <= 0 {
		w = 64
	}
	if w > 100 {
		w = 100
	}
	return "  " + ruleStyle.Render(strings.Repeat("─", w-4))
}

func (m Model) relFiles(files []string) string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = m.rel(f)
	}
	return strings.Join(out, ", ")
}

func (m Model) rel(f string) string {
	if m.base == "" || !filepath.IsAbs(f) {
		return f
	}
	r, err := filepath.Rel(m.base, f)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return f
	}
	return r
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
