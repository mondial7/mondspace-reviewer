package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/tui"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// fakeSnap returns a fixed diff.
type fakeSnap struct{ diff domain.Diff }

func (f fakeSnap) Snapshot(context.Context, string) (domain.SnapshotRef, error) {
	return domain.SnapshotRef{}, nil
}
func (f fakeSnap) Diff(context.Context, domain.SnapshotRef, domain.SnapshotRef, []string) (domain.Diff, error) {
	return f.diff, nil
}

// fakeSum returns a canned answer or error.
type fakeSum struct {
	answer string
	err    error
}

func (f fakeSum) Headline(context.Context, domain.Unit, domain.Diff) (domain.Headline, error) {
	return domain.Headline{}, nil
}
func (f fakeSum) Answer(context.Context, string, domain.AskContext) (string, error) {
	return f.answer, f.err
}

func TestAskFuncBuildsContextAndAnswers(t *testing.T) {
	sess := domain.Session{ID: "s", Prompt: "add x", Units: []domain.Unit{{ID: "s-u001", Files: []string{"a.go"}}}}

	fn := askFunc(sess, fakeSnap{diff: domain.Diff{Text: "+x\n"}}, fakeSum{answer: "s-u001 does the thing"})
	msg := fn(domain.AskUnit, sess.Units[0], "what?")
	if ans, ok := msg.(tui.AnswerReadyMsg); !ok || ans.Text != "s-u001 does the thing" {
		t.Errorf("answer = %+v, want the summarizer's reply", msg)
	}

	// Offline / error degrades to a readable notice, never a crash.
	off := askFunc(sess, fakeSnap{}, fakeSum{err: errors.New("summarizer offline")})
	msg = off(domain.AskSession, domain.Unit{}, "anything?")
	ans := msg.(tui.AnswerReadyMsg)
	if !strings.Contains(ans.Text, "offline") {
		t.Errorf("error answer = %q, want it to surface the offline error", ans.Text)
	}
}

func TestChooseSummarizerProbesAndFallsBack(t *testing.T) {
	u := domain.Unit{ID: "u1", Headline: domain.Headline{Text: "mechanical", WhySrc: domain.WhyInferred}}

	// Unreachable endpoint → null summarizer, which passes the mechanical
	// headline through unchanged (offline degradation, no crash).
	off := chooseSummarizer("http://127.0.0.1:1", "m")
	got, err := off.Headline(context.Background(), u, domain.Diff{})
	if err != nil || got.Text != "mechanical" {
		t.Errorf("offline: headline = %+v err = %v, want the mechanical headline", got, err)
	}

	// Reachable endpoint → openai summarizer, returning the model's WHAT.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			io.WriteString(w, `{"data":[]}`)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"WHAT: real model summary\nWHY: unknown"}}]}`)
	}))
	defer srv.Close()

	on := chooseSummarizer(srv.URL, "m")
	got, err = on.Headline(context.Background(), u, domain.Diff{})
	if err != nil {
		t.Fatalf("online Headline: %v", err)
	}
	if got.Text != "real model summary" {
		t.Errorf("online: Text = %q, want the model's summary", got.Text)
	}
}

func TestBuildTUIModelLoadsAndPersistsAnnotations(t *testing.T) {
	root := t.TempDir()
	store := jsonl.New(root)
	for _, u := range []domain.Unit{
		{ID: "s-u001", SessionID: "s", Files: []string{"a.go"}, Sealed: true},
		{ID: "s-u002", SessionID: "s", Files: []string{"b.go"}, Sealed: true},
	} {
		if err := store.AppendUnit(u); err != nil {
			t.Fatal(err)
		}
	}

	model, err := buildTUIModel(store, "s")
	if err != nil {
		t.Fatalf("buildTUIModel: %v", err)
	}
	if model.VisibleCount() != 2 {
		t.Fatalf("model shows %d units, want 2", model.VisibleCount())
	}

	// Objecting to the current unit must land in notes.jsonl.
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	data, err := os.ReadFile(filepath.Join(root, "s", "notes.jsonl"))
	if err != nil {
		t.Fatalf("notes.jsonl not written: %v", err)
	}
	if !strings.Contains(string(data), `"kind":"objection"`) || !strings.Contains(string(data), `"unit_id":"s-u001"`) {
		t.Errorf("note not persisted correctly: %s", data)
	}
}

func TestTeaPresenterStreamsUnits(t *testing.T) {
	var got tea.Msg
	p := teaPresenter{send: func(m tea.Msg) { got = m }}
	if err := p.Present(domain.Unit{ID: "s-u009"}, nil); err != nil {
		t.Fatal(err)
	}
	added, ok := got.(tui.UnitAddedMsg)
	if !ok || added.Unit.ID != "s-u009" {
		t.Errorf("presenter should send UnitAddedMsg for the unit, got %#v", got)
	}
}
