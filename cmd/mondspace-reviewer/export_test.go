package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

func seedReviewedSession(t *testing.T, root string) {
	t.Helper()
	store := jsonl.New(root)
	if err := store.AppendEvent(domain.Event{ID: "e1", SessionID: "s", Kind: domain.KindPrompt, StatedIntent: "add auth"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendUnit(domain.Unit{ID: "s-u001", SessionID: "s", Files: []string{"a.go"},
		Headline: domain.Headline{Text: "added validator", WhySrc: domain.WhyInferred}}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendNote(domain.Note{ID: "n1", SessionID: "s", UnitID: "s-u001", Kind: domain.NoteObjection, Text: "wrong layer"}); err != nil {
		t.Fatal(err)
	}
}

func TestExportCommandMarkdown(t *testing.T) {
	root := t.TempDir()
	seedReviewedSession(t, root)

	var out bytes.Buffer
	if err := run(context.Background(), []string{"export", "--format=md", "--session=s", "--out=" + root}, nil, &out); err != nil {
		t.Fatalf("export: %v", err)
	}

	s := out.String()
	if !bytes.Contains(out.Bytes(), []byte("## Review Report")) || !bytes.Contains(out.Bytes(), []byte("wrong layer")) {
		t.Errorf("markdown export missing content:\n%s", s)
	}
}

func TestExportCommandJSONAndUnknownFormat(t *testing.T) {
	root := t.TempDir()
	seedReviewedSession(t, root)

	var out bytes.Buffer
	if err := run(context.Background(), []string{"export", "--format=json", "--session=s", "--out=" + root}, nil, &out); err != nil {
		t.Fatalf("export json: %v", err)
	}
	var report domain.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json export is not valid JSON: %v\n%s", err, out.String())
	}
	if report.SessionID != "s" {
		t.Errorf("json export session = %q", report.SessionID)
	}

	if err := run(context.Background(), []string{"export", "--format=xml", "--session=s", "--out=" + root}, nil, &bytes.Buffer{}); err == nil {
		t.Error("unknown format should error")
	}
}
