package tui_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/tui"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
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

func quitsOnCtrlC(t *testing.T, m tui.Model, when string) {
	t.Helper()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatalf("ctrl+c %s: no command returned", when)
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c %s: got %T, want tea.Quit", when, cmd())
	}
}

func TestCtrlCAlwaysQuits(t *testing.T) {
	quitsOnCtrlC(t, tui.New(threeUnits(), nil, nil), "in normal mode")
	quitsOnCtrlC(t, send(tui.New(threeUnits(), nil, nil), '/'), "while filtering")
	quitsOnCtrlC(t, send(tui.New(threeUnits(), nil, nil), 'a'), "while asking")
	quitsOnCtrlC(t, tui.New(nil, nil, nil), "with an empty session")
}

func TestExpandFetchesAndShowsDiff(t *testing.T) {
	units := []domain.Unit{{ID: "s-u001", Files: []string{"a.go"},
		From: domain.SnapshotRef{Commit: "aaa"}, To: domain.SnapshotRef{Commit: "bbb"}}}
	m := tui.New(units, nil, nil).WithDiff(func(u domain.Unit) tea.Msg {
		return tui.DiffReadyMsg{UnitID: u.ID, Diff: domain.Diff{Text: "@@ -1 +1 @@\n-old body\n+new validated body\n"}}
	})

	// Expanding fires a command to fetch the unit's diff.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(tui.Model)
	if cmd == nil {
		t.Fatal("expanding should fetch the diff")
	}
	ready, ok := cmd().(tui.DiffReadyMsg)
	if !ok || ready.UnitID != "s-u001" {
		t.Fatalf("expand cmd yielded %#v, want a DiffReadyMsg for s-u001", cmd())
	}

	// Once the diff arrives it is shown in the expanded unit.
	after, _ := m.Update(ready)
	m = after.(tui.Model)
	view := m.View()
	if !strings.Contains(view, "new validated body") || !strings.Contains(view, "old body") {
		t.Errorf("expanded unit should show the diff:\n%s", view)
	}
}

func TestCollapsedLineShowsHeadlineOnceSummarized(t *testing.T) {
	units := []domain.Unit{{ID: "s-u001", Files: []string{"auth/token.go"},
		Headline: domain.Headline{Text: "1 edit across 1 file"}}}
	m := tui.New(units, nil, nil)

	// Offline: the collapsed line anchors on the file.
	if !strings.Contains(m.View(), "auth/token.go") {
		t.Errorf("should show the file before a summary arrives:\n%s", m.View())
	}

	// After the model summarizes, the queue reads as a storyline.
	next, _ := m.Update(tui.HeadlineReadyMsg{UnitID: "s-u001",
		Headline: domain.Headline{Text: "extracted validation behind a TokenValidator interface", WhySrc: domain.WhyInferred}})
	m = next.(tui.Model)
	if !strings.Contains(m.View(), "extracted validation behind a TokenValidator interface") {
		t.Errorf("collapsed line should show the model headline once summarized:\n%s", m.View())
	}
}

func TestUnitAddedStreamsIntoQueueWithoutMovingCursor(t *testing.T) {
	m := tui.New(nil, nil, nil)
	if m.VisibleCount() != 0 {
		t.Fatalf("fresh live queue should be empty, got %d", m.VisibleCount())
	}

	add := func(m tui.Model, id string) tui.Model {
		next, _ := m.Update(tui.UnitAddedMsg{Unit: domain.Unit{ID: id, Files: []string{"a.go"}}})
		return next.(tui.Model)
	}

	m = add(m, "s-u001")
	m = add(m, "s-u002")
	if m.VisibleCount() != 2 {
		t.Errorf("streamed units should append, got %d want 2", m.VisibleCount())
	}
	// The cursor never jumps to new arrivals — the reviewer stays where they are.
	if m.Cursor() != 0 {
		t.Errorf("cursor should stay put as units stream in, got %d", m.Cursor())
	}
	if !strings.Contains(m.View(), "u002") {
		t.Errorf("streamed unit not shown:\n%s", m.View())
	}
}

func TestArrowKeysMoveCursor(t *testing.T) {
	m := tui.New(threeUnits(), nil, nil)

	down, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = down.(tui.Model)
	if m.Cursor() != 1 {
		t.Errorf("down arrow: cursor = %d, want 1", m.Cursor())
	}
	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = up.(tui.Model)
	if m.Cursor() != 0 {
		t.Errorf("up arrow: cursor = %d, want 0", m.Cursor())
	}
}

func TestViewUsesShortHandlesAndRelativePaths(t *testing.T) {
	base := "/home/me/proj"
	units := []domain.Unit{
		{ID: "abc-123-def-u007", SessionID: "abc-123-def", Files: []string{base + "/internal/tui.go"}},
	}
	view := tui.New(units, nil, nil).RelativeTo(base).View()

	if !strings.Contains(view, "u007") {
		t.Errorf("view should show the short unit handle:\n%s", view)
	}
	if strings.Contains(view, "abc-123-def-u007") {
		t.Errorf("collapsed view should not show the full unit id:\n%s", view)
	}
	if !strings.Contains(view, "internal/tui.go") || strings.Contains(view, base+"/internal") {
		t.Errorf("view should show repo-relative paths:\n%s", view)
	}
}

func TestViewNeverBlankAndShowsHelp(t *testing.T) {
	// Empty session: still a header, an empty-state message, and a quit hint.
	empty := tui.New(nil, nil, nil).View()
	if strings.TrimSpace(empty) == "" {
		t.Fatal("View is blank for an empty session")
	}
	if !strings.Contains(empty, "No units") {
		t.Errorf("empty session should explain itself:\n%s", empty)
	}
	if !strings.Contains(empty, "quit") {
		t.Errorf("View should show a quit hint:\n%s", empty)
	}

	// Populated session: the footer help is always present.
	full := tui.New(threeUnits(), nil, nil).View()
	if !strings.Contains(full, "quit") {
		t.Errorf("populated View should show the key hints:\n%s", full)
	}
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

func TestQuitSignalsQuit(t *testing.T) {
	m := tui.New(threeUnits(), nil, nil)

	_, cmd := m.Update(key('q'))
	if cmd == nil {
		t.Fatal("q should return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q command should be tea.Quit, got %T", cmd())
	}

	// While filtering, q types into the query rather than quitting.
	fm := send(tui.New(threeUnits(), nil, nil), '/')
	_, cmd = fm.Update(key('q'))
	if cmd != nil {
		t.Error("q while filtering should not quit")
	}
}

func TestViewRendersHeaderFlagsAndWhy(t *testing.T) {
	units := []domain.Unit{{
		ID:        "s-u001",
		SessionID: "s",
		Files:     []string{"auth/token.go"},
		Flags:     []domain.Flag{domain.FlagNoTest},
		Headline:  domain.Headline{Text: "1 edit across 1 file", Why: "swap the lib", WhySrc: domain.WhyStated},
	}}
	m := enter(tui.New(units, nil, nil)) // expand the current unit

	view := m.View()
	for _, want := range []string{"s-u001", "auth/token.go", "no-test", "1 edit across 1 file", "stated:", "swap the lib"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q\n---\n%s", want, view)
		}
	}
}

func TestViewShowsSupersededMarker(t *testing.T) {
	units := []domain.Unit{{ID: "s-u001", SessionID: "s", Files: []string{"a.go"}}}
	notes := []domain.Note{{
		ID: "n1", UnitID: "s-u001", Kind: domain.NoteObjection,
		Text: "wrong choice", SupersededBy: "s-u007",
	}}
	m := enter(tui.New(units, notes, nil))

	view := m.View()
	if !strings.Contains(view, "objection") || !strings.Contains(view, "wrong choice") {
		t.Errorf("view missing the note:\n%s", view)
	}
	if !strings.Contains(view, "superseded by s-u007") {
		t.Errorf("view missing supersession marker:\n%s", view)
	}
}

func TestHeadlineReadyReplacesUnitHeadline(t *testing.T) {
	units := []domain.Unit{
		{ID: "s-u001", SessionID: "s", Files: []string{"a.go"},
			Headline: domain.Headline{Text: "2 edits across 1 file", WhySrc: domain.WhyInferred}},
	}
	m := enter(tui.New(units, nil, nil)) // expand so the WHAT text shows

	// Before fill-in, the queue shows the mechanical headline (it never waits).
	if !strings.Contains(m.View(), "2 edits across 1 file") {
		t.Fatalf("mechanical headline not shown initially:\n%s", m.View())
	}

	model := domain.Headline{Text: "added a retry loop", Why: "flaky network", WhySrc: domain.WhyInferred}
	next, _ := m.Update(tui.HeadlineReadyMsg{UnitID: "s-u001", Headline: model})
	m = next.(tui.Model)

	view := m.View()
	if !strings.Contains(view, "added a retry loop") {
		t.Errorf("model headline not filled in:\n%s", view)
	}
	if strings.Contains(view, "2 edits across 1 file") {
		t.Errorf("mechanical headline should have been replaced:\n%s", view)
	}
}

func TestFilledInInferredHeadlineRendersDistinctly(t *testing.T) {
	units := []domain.Unit{{ID: "s-u001", Headline: domain.Headline{Text: "mechanical"}}}
	m := enter(tui.New(units, nil, nil))

	inferred := domain.Headline{Text: "added a cache layer", Why: "cut repeated DB reads", WhySrc: domain.WhyInferred}
	next, _ := m.Update(tui.HeadlineReadyMsg{UnitID: "s-u001", Headline: inferred})
	m = next.(tui.Model)

	view := m.View()
	if !strings.Contains(view, "inferred: cut repeated DB reads") {
		t.Errorf("inferred rationale must stay labelled after fill-in:\n%s", view)
	}
	if strings.Contains(view, "stated:") {
		t.Errorf("a model rationale must never be shown as stated:\n%s", view)
	}
}

func TestHeadlineReadyForUnknownUnitIgnored(t *testing.T) {
	units := []domain.Unit{
		{ID: "s-u001", Headline: domain.Headline{Text: "original"}},
	}
	m := enter(tui.New(units, nil, nil))

	next, _ := m.Update(tui.HeadlineReadyMsg{UnitID: "does-not-exist", Headline: domain.Headline{Text: "ghost"}})
	m = next.(tui.Model)

	view := m.View()
	if strings.Contains(view, "ghost") {
		t.Errorf("headline for an unknown unit must be ignored:\n%s", view)
	}
	if !strings.Contains(view, "original") {
		t.Errorf("existing headline should be untouched:\n%s", view)
	}
}

func TestInitFiresSummarizeCommandsPerUnit(t *testing.T) {
	fn := func(u domain.Unit) tea.Msg {
		return tui.HeadlineReadyMsg{UnitID: u.ID, Headline: domain.Headline{Text: "model " + u.ID}}
	}
	m := tui.New(threeUnits(), nil, nil).WithSummarize(fn)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should fire summarize commands when a summarizer is wired")
	}

	// The batch fans out one command per unit.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init cmd = %T, want tea.BatchMsg", cmd())
	}
	got := map[string]bool{}
	for _, c := range batch {
		if ready, ok := c().(tui.HeadlineReadyMsg); ok {
			got[ready.UnitID] = true
		}
	}
	for _, id := range []string{"s-u001", "s-u002", "s-u003"} {
		if !got[id] {
			t.Errorf("no summarize command fired for %s", id)
		}
	}

	// With no summarizer wired, Init does nothing (offline / null).
	if tui.New(threeUnits(), nil, nil).Init() != nil {
		t.Error("Init should be nil when no summarizer is wired")
	}
}

func TestAskSubmitFiresCommandYieldingAnswer(t *testing.T) {
	askFn := func(scope domain.AskScope, u domain.Unit, q string) tea.Msg {
		return tui.AnswerReadyMsg{Text: "scope=" + string(scope) + " unit=" + u.ID + " q=" + q}
	}
	m := tui.New(threeUnits(), nil, nil).WithAsk(askFn)

	m = send(m, 'a', 'h', 'i')
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(tui.Model)

	if m.Asking() {
		t.Error("submitting should close the ask prompt")
	}
	if cmd == nil {
		t.Fatal("enter should fire an ask command")
	}
	msg, ok := cmd().(tui.AnswerReadyMsg)
	if !ok {
		t.Fatalf("cmd yielded %T, want AnswerReadyMsg", cmd())
	}
	if msg.Text != "scope=unit unit=s-u001 q=hi" {
		t.Errorf("answer msg = %q, want the closure's output with scope/unit/question", msg.Text)
	}
}

func TestAskEscCancelsWithoutSubmitting(t *testing.T) {
	submitted := false
	askFn := func(domain.AskScope, domain.Unit, string) tea.Msg {
		submitted = true
		return tui.AnswerReadyMsg{Text: "x"}
	}
	m := tui.New(threeUnits(), nil, nil).WithAsk(askFn)

	m = send(m, 'a', 'h', 'i')
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(tui.Model)

	if m.Asking() {
		t.Error("esc should close the ask prompt")
	}
	if m.Question() != "" {
		t.Errorf("esc should clear the question, got %q", m.Question())
	}
	if cmd != nil {
		cmd()
	}
	if submitted {
		t.Error("esc must not submit the question")
	}
}

func TestViewRendersAskPromptAndAnswer(t *testing.T) {
	m := send(tui.New(threeUnits(), nil, nil), 'a', 'w', 'h', 'y')
	if !strings.Contains(m.View(), "why") {
		t.Errorf("view should show the in-progress question:\n%s", m.View())
	}

	next, _ := m.Update(tui.AnswerReadyMsg{Text: "s-u001 adds a Validator interface"})
	m = next.(tui.Model)
	if !strings.Contains(m.View(), "s-u001 adds a Validator interface") {
		t.Errorf("view should show the answer:\n%s", m.View())
	}
}

func TestAskModeCapturesCommandKeysAsText(t *testing.T) {
	store := &recordingStore{}
	m := fixedClock(tui.New(threeUnits(), nil, store))

	m = send(m, 'a')           // enter ask mode
	m = send(m, 'j', 'x', 'k') // these are navigation/annotation keys normally

	if m.Question() != "jxk" {
		t.Errorf("command keys should be captured as text, question = %q", m.Question())
	}
	if m.Cursor() != 0 {
		t.Errorf("cursor must not move while asking, got %d", m.Cursor())
	}
	if len(store.notes) != 0 {
		t.Errorf("'x' must not annotate while asking, got %d notes", len(store.notes))
	}
}

func TestAskModeEntryAndTyping(t *testing.T) {
	m := send(tui.New(threeUnits(), nil, nil), 'a')
	if !m.Asking() {
		t.Fatal("'a' should enter ask mode")
	}
	if m.AskScope() != domain.AskUnit {
		t.Errorf("scope = %q, want unit", m.AskScope())
	}
	m = send(m, 'w', 'h', 'y')
	if m.Question() != "why" {
		t.Errorf("question = %q, want 'why'", m.Question())
	}

	// Capital A opens a session-scope question.
	ms := send(tui.New(threeUnits(), nil, nil), 'A')
	if !ms.Asking() || ms.AskScope() != domain.AskSession {
		t.Errorf("'A' should enter session ask mode, got asking=%v scope=%q", ms.Asking(), ms.AskScope())
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
	m = enter(m)     // expand u1
	m = send(m, 'j') // move to u2
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
