package delivery_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/delivery"
	"github.com/mondial7/mondspace-reviewer/internal/usecase/handoff"
)

func brief(batch string) handoff.Brief {
	return handoff.Assemble(batch, "main", time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), []contract.Item{{
		ID:        "id-1",
		Source:    contract.SourceAnalyser,
		Producer:  "gosec",
		Location:  contract.Location{Path: "internal/a.go", StartLine: 12},
		Severity:  contract.SeverityHigh,
		Directive: "use crypto/rand",
	}})
}

func TestFileWritesTheBrief(t *testing.T) {
	dir := t.TempDir()
	adapter := &delivery.File{Dir: dir}

	if err := adapter.Send(brief("batch-1")); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(dir, delivery.HandoffDir, "batch-1.md")
	if adapter.Written != want {
		t.Errorf("wrote %q, want %q", adapter.Written, want)
	}
	body, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "use crypto/rand") {
		t.Errorf("the written brief has no directive in it:\n%s", body)
	}
}

// A batch id reaches this from a flag as well as from msr, and it becomes a
// filename.
func TestFileKeepsTheBatchIdToOneFile(t *testing.T) {
	dir := t.TempDir()
	adapter := &delivery.File{Dir: dir}

	if err := adapter.Send(brief("../../etc/passwd")); err != nil {
		t.Fatal(err)
	}

	if got := filepath.Dir(adapter.Written); got != filepath.Join(dir, delivery.HandoffDir) {
		t.Errorf("wrote to %q, outside the handoff directory", adapter.Written)
	}
}

func TestStdoutWritesTheBrief(t *testing.T) {
	var buf bytes.Buffer

	if err := (delivery.Stdout{W: &buf}).Send(brief("batch-1")); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "batch-1") {
		t.Errorf("nothing recognisable on stdout:\n%s", buf.String())
	}
}

func TestClipboardSendsTheBriefOnStdin(t *testing.T) {
	var got string
	adapter := delivery.Clipboard{Run: func(name string, args []string, stdin string) error {
		got = stdin
		return nil
	}}

	if err := adapter.Send(brief("batch-1")); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "use crypto/rand") {
		t.Errorf("the clipboard got %q", got)
	}
}

func TestPick(t *testing.T) {
	for _, name := range []string{"", "file", "stdout", "clipboard"} {
		if _, err := delivery.Pick(name, t.TempDir(), nil); err != nil {
			t.Errorf("Pick(%q): %v", name, err)
		}
	}

	// A push that silently went somewhere other than where it was sent is worse
	// than one that did not go.
	if _, err := delivery.Pick("claude-code", t.TempDir(), nil); err == nil {
		t.Error("an unknown delivery was accepted")
	}
}
