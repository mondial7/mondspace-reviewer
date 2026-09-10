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
	var got, ran string
	adapter := delivery.Clipboard{
		// Said here rather than looked up, so this asks what the adapter does
		// on a machine with a clipboard rather than what the machine running
		// the test happens to have.
		Look: func() (string, []string) { return "pbcopy", nil },
		Run: func(name string, args []string, stdin string) error {
			ran, got = name, stdin
			return nil
		},
	}

	if err := adapter.Send(brief("batch-1")); err != nil {
		t.Fatal(err)
	}

	if ran != "pbcopy" {
		t.Errorf("ran %q, want the command the machine reported", ran)
	}
	if !strings.Contains(got, "use crypto/rand") {
		t.Errorf("the clipboard got %q", got)
	}
}

// And on a machine with none of them, it says so and names the alternative
// rather than quietly writing a file somebody will never look at.
func TestClipboardWithoutAClipboard(t *testing.T) {
	adapter := delivery.Clipboard{
		Look: func() (string, []string) { return "", nil },
		Run: func(string, []string, string) error {
			t.Error("nothing should be run when the machine has no clipboard")
			return nil
		},
	}

	err := adapter.Send(brief("batch-1"))

	if err == nil {
		t.Fatal("sending to a clipboard that does not exist reported success")
	}
	if !strings.Contains(err.Error(), "--to file") {
		t.Errorf("the error is %q, and does not say what to do instead", err)
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
