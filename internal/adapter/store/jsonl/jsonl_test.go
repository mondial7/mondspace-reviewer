package jsonl_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

func TestLoadSkipsMalformedLine(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"id":"e1","session_id":"s","kind":"edit"}
{ truncated line
{"id":"e3","session_id":"s","kind":"edit"}
`
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	sess, err := jsonl.New(root).Load("s")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(sess.Events) != 2 {
		t.Fatalf("got %d events, want 2 (malformed skipped)", len(sess.Events))
	}
	if sess.Events[0].ID != "e1" || sess.Events[1].ID != "e3" {
		t.Errorf("got IDs %q,%q, want e1,e3", sess.Events[0].ID, sess.Events[1].ID)
	}
}

func TestLoadReconstructsSession(t *testing.T) {
	root := t.TempDir()
	s := jsonl.New(root)

	events := []domain.Event{
		{ID: "e1", SessionID: "s", Kind: domain.KindPrompt, StatedIntent: "add auth"},
		{ID: "e2", SessionID: "s", Kind: domain.KindEdit, Files: []string{"a.go"}},
	}
	for _, e := range events {
		if err := s.AppendEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AppendUnit(domain.Unit{ID: "s-u001", SessionID: "s", EventIDs: []string{"e2"}, Sealed: true}); err != nil {
		t.Fatal(err)
	}

	sess, err := jsonl.New(root).Load("s")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if sess.ID != "s" {
		t.Errorf("ID = %q, want s", sess.ID)
	}
	if sess.Prompt != "add auth" {
		t.Errorf("Prompt = %q, want %q", sess.Prompt, "add auth")
	}
	if len(sess.Events) != 2 {
		t.Errorf("got %d events, want 2", len(sess.Events))
	}
	if len(sess.Units) != 1 || sess.Units[0].ID != "s-u001" {
		t.Errorf("Units = %+v, want one unit s-u001", sess.Units)
	}
}

func TestLoadReconstructsNotes(t *testing.T) {
	root := t.TempDir()
	s := jsonl.New(root)

	if err := s.AppendNote(domain.Note{ID: "n1", SessionID: "s", UnitID: "s-u001", Kind: domain.NoteDebt, Text: "fix later"}); err != nil {
		t.Fatal(err)
	}

	sess, err := jsonl.New(root).Load("s")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(sess.Notes) != 1 {
		t.Fatalf("got %d notes, want 1", len(sess.Notes))
	}
	if sess.Notes[0].Kind != domain.NoteDebt || sess.Notes[0].UnitID != "s-u001" {
		t.Errorf("note = %+v, want debt on s-u001", sess.Notes[0])
	}
}

func TestAppendsAreAdditiveAcrossInstances(t *testing.T) {
	root := t.TempDir()

	first := jsonl.New(root)
	if err := first.AppendEvent(domain.Event{ID: "e1", SessionID: "s", Kind: domain.KindEdit}); err != nil {
		t.Fatal(err)
	}

	// A fresh Store, as if the process had restarted, must append, not truncate.
	second := jsonl.New(root)
	if err := second.AppendEvent(domain.Event{ID: "e2", SessionID: "s", Kind: domain.KindEdit}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, "s", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (append must not truncate)", len(lines))
	}
	if !strings.Contains(lines[0], `"id":"e1"`) || !strings.Contains(lines[1], `"id":"e2"`) {
		t.Errorf("lines out of order or missing: %q", lines)
	}
}

func TestStoreRejectsUnsafeSessionID(t *testing.T) {
	root := t.TempDir()
	s := jsonl.New(root)

	unsafe := []string{"../evil", "a/b", "..", "", ".", "/etc/passwd", `a\b`}
	for _, id := range unsafe {
		t.Run(id, func(t *testing.T) {
			if err := s.AppendEvent(domain.Event{ID: "e", SessionID: id, Kind: domain.KindEdit}); err == nil {
				t.Errorf("AppendEvent(%q) should be rejected", id)
			}
			if _, err := s.Load(id); err == nil {
				t.Errorf("Load(%q) should be rejected", id)
			}
		})
	}

	// Nothing may be written outside the store root.
	escaped := filepath.Join(root, "..", "evil")
	if _, err := os.Stat(escaped); err == nil {
		t.Errorf("a file escaped the store root at %s", escaped)
	}
}

func TestAppendNoteWritesToNotesFile(t *testing.T) {
	root := t.TempDir()
	s := jsonl.New(root)

	n := domain.Note{ID: "n1", SessionID: "s", UnitID: "s-u001", Kind: domain.NoteObjection, Text: "wrong choice"}
	if err := s.AppendNote(n); err != nil {
		t.Fatalf("AppendNote: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "s", "notes.jsonl"))
	if err != nil {
		t.Fatalf("reading notes.jsonl: %v", err)
	}
	if !strings.Contains(string(data), `"id":"n1"`) || !strings.Contains(string(data), `"kind":"objection"`) {
		t.Errorf("notes.jsonl missing note: %s", data)
	}
}

func TestAppendUnitWritesToUnitsFile(t *testing.T) {
	root := t.TempDir()
	s := jsonl.New(root)

	u := domain.Unit{ID: "sess-basic-u001", SessionID: "sess-basic", EventIDs: []string{"e1"}, Sealed: true}
	if err := s.AppendUnit(u); err != nil {
		t.Fatalf("AppendUnit: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "sess-basic", "units.jsonl"))
	if err != nil {
		t.Fatalf("reading units.jsonl: %v", err)
	}
	if !strings.Contains(string(data), `"id":"sess-basic-u001"`) {
		t.Errorf("units.jsonl missing unit id: %s", data)
	}
}

func TestAppendEventWritesOneLineCreatingDir(t *testing.T) {
	root := t.TempDir()
	s := jsonl.New(root)

	e := domain.Event{ID: "e1", SessionID: "sess-basic", Kind: domain.KindEdit, Files: []string{"a.go"}}
	if err := s.AppendEvent(e); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	path := filepath.Join(root, "sess-basic", "events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading events.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if !strings.Contains(lines[0], `"id":"e1"`) {
		t.Errorf("line does not contain event id: %s", lines[0])
	}
}

func TestNarrativeSurvivesARestart(t *testing.T) {
	// Narration costs several model calls. A story written once must still be
	// there next time the reviewer opens the page, or every visit pays again.
	root := t.TempDir()
	want := domain.Narrative{
		SessionID: "s", Title: "Locking down auth", Intro: "…",
		Source: domain.NarrativeModel, Fingerprint: "abc123",
		Chapters: []domain.Chapter{{Title: "Tokens", Prose: "p", UnitIDs: []string{"s-f001"}}},
	}

	if err := jsonl.New(root).SaveNarrative(want); err != nil {
		t.Fatalf("SaveNarrative: %v", err)
	}
	got, err := jsonl.New(root).LoadNarrative("s") // a fresh store: a new process
	if err != nil {
		t.Fatalf("LoadNarrative: %v", err)
	}

	if got.Title != want.Title || got.Fingerprint != want.Fingerprint || got.Source != want.Source {
		t.Errorf("LoadNarrative = %+v, want %+v", got, want)
	}
	if len(got.Chapters) != 1 || got.Chapters[0].Title != "Tokens" {
		t.Errorf("chapters = %+v, want the stored chapter", got.Chapters)
	}
}

func TestLoadNarrativeIsEmptyNotAnErrorForANewSession(t *testing.T) {
	// A session nobody has narrated yet is an ordinary state, not a failure:
	// the caller narrates it. Only a real I/O fault should be an error.
	got, err := jsonl.New(t.TempDir()).LoadNarrative("never-seen")

	if err != nil {
		t.Fatalf("LoadNarrative on a new session should not error: %v", err)
	}
	if got.Fingerprint != "" || len(got.Chapters) != 0 {
		t.Errorf("expected an empty narrative, got %+v", got)
	}
}

func TestExchangesSurviveARestart(t *testing.T) {
	// A review conversation is part of the review. Losing it when the process
	// stops means the reviewer cannot pick a thread back up tomorrow.
	root := t.TempDir()
	store := jsonl.New(root)

	first := domain.Exchange{
		SessionID: "s", Question: "why the retry?", Answer: "s-f001 adds a backoff.",
		TS: time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC),
	}
	second := domain.Exchange{
		SessionID: "s", Question: "and the tests?", Answer: "s-f002 covers it.",
		TS: time.Date(2026, 8, 23, 9, 5, 0, 0, time.UTC),
	}
	if err := store.AppendExchange(first); err != nil {
		t.Fatalf("AppendExchange: %v", err)
	}
	if err := store.AppendExchange(second); err != nil {
		t.Fatalf("AppendExchange: %v", err)
	}

	got, err := jsonl.New(root).Load("s") // a fresh store: a new process
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.Exchanges) != 2 {
		t.Fatalf("got %d exchanges, want both: %+v", len(got.Exchanges), got.Exchanges)
	}
	// Oldest first: a conversation reads forwards.
	if got.Exchanges[0].Question != "why the retry?" || got.Exchanges[1].Answer != "s-f002 covers it." {
		t.Errorf("exchanges = %+v, want them in order", got.Exchanges)
	}
}

func TestLoadHasNoExchangesForASessionNobodyAsked(t *testing.T) {
	got, err := jsonl.New(t.TempDir()).Load("quiet")

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Exchanges) != 0 {
		t.Errorf("got %+v, want no conversation", got.Exchanges)
	}
}
