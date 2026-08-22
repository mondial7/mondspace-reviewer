package plain_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/plain"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

func TestPresentShowsUnitTimeFromEvents(t *testing.T) {
	ts := time.Date(2026, 8, 22, 14, 5, 9, 0, time.UTC)
	events := []domain.Event{
		{ID: "e1", Kind: domain.KindEdit, TS: ts.Add(-2 * time.Second)},
		{ID: "e2", Kind: domain.KindWrite, TS: ts}, // sealing event
	}

	var buf bytes.Buffer
	if err := plain.New(&buf).Present(domain.Unit{ID: "u1"}, events); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "14:05:09") {
		t.Errorf("expected the unit's seal time in the header:\n%s", buf.String())
	}
}

func TestPresentShowsRepoRelativePaths(t *testing.T) {
	base := "/Users/me/project"
	u := domain.Unit{
		ID:    "u1",
		Files: []string{base + "/internal/adapter/tui.go", "/etc/outside.go"},
	}

	var buf bytes.Buffer
	if err := plain.New(&buf).RelativeTo(base).Present(u, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "internal/adapter/tui.go") || strings.Contains(out, base+"/internal") {
		t.Errorf("path under base should be shown relative:\n%s", out)
	}
	// A path outside the base is left absolute rather than mangled with ../.
	if !strings.Contains(out, "/etc/outside.go") {
		t.Errorf("path outside base should stay absolute:\n%s", out)
	}
}

func TestVerbosePresentListsMemberEventsAndSnapshots(t *testing.T) {
	u := domain.Unit{
		ID:    "s-u001",
		Files: []string{"auth/token.go", "auth/port.go"},
		From:  domain.SnapshotRef{Commit: "abc1234567"},
		To:    domain.SnapshotRef{Commit: "def7654321"},
		Flags: []domain.Flag{domain.FlagNoTest},
	}
	events := []domain.Event{
		{ID: "e1", Kind: domain.KindEdit, Tool: "Edit", Files: []string{"auth/token.go"}, StatedIntent: "extract validation"},
		{ID: "e2", Kind: domain.KindWrite, Tool: "Write", Files: []string{"auth/port.go"}},
		{ID: "e3", Kind: domain.KindBash, Tool: "Bash", Failed: true},
	}

	var buf bytes.Buffer
	if err := plain.New(&buf).Verbose().Present(u, events); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, want := range []string{
		"edit", "auth/token.go", "extract validation", // the edit with its intent
		"write", "auth/port.go",
		"bash", "failed", // the failed bash
		"abc1234", "def7654", // snapshot refs (short)
	} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose output missing %q\n---\n%s", want, out)
		}
	}

	// Non-verbose output must NOT list member events.
	var plainBuf bytes.Buffer
	if err := plain.New(&plainBuf).Present(u, events); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plainBuf.String(), "extract validation") {
		t.Errorf("non-verbose output should not list events:\n%s", plainBuf.String())
	}
}

func TestPresentRendersFlags(t *testing.T) {
	var buf bytes.Buffer
	u := domain.Unit{
		ID:    "u",
		Flags: []domain.Flag{domain.FlagNoTest, domain.FlagLarge},
	}
	if err := plain.New(&buf).Present(u, nil); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "FLAG  no-test · large") {
		t.Errorf("flags not rendered:\n%s", got)
	}

	// No flags still shows the placeholder.
	buf.Reset()
	if err := plain.New(&buf).Present(domain.Unit{ID: "u"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "FLAG  —") {
		t.Errorf("empty flags should render placeholder:\n%s", got)
	}
}

func TestPresentDistinguishesStatedFromInferred(t *testing.T) {
	render := func(h domain.Headline) string {
		var buf bytes.Buffer
		if err := plain.New(&buf).Present(domain.Unit{ID: "u", Headline: h}, nil); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	stated := render(domain.Headline{Text: "t", Why: "swap the lib", WhySrc: domain.WhyStated})
	if !strings.Contains(stated, "WHY   stated: swap the lib") {
		t.Errorf("stated rendering wrong:\n%s", stated)
	}

	inferred := render(domain.Headline{Text: "t", Why: "", WhySrc: domain.WhyInferred})
	if !strings.Contains(inferred, "WHY   inferred: (none stated)") {
		t.Errorf("inferred rendering wrong:\n%s", inferred)
	}

	// The label word itself must differ, not just the text — a confabulated
	// rationale presented as stated fact destroys trust.
	if strings.Contains(inferred, "stated:") {
		t.Errorf("inferred output must not use the word 'stated':\n%s", inferred)
	}
}

func TestPresentRendersFourSlots(t *testing.T) {
	var buf bytes.Buffer
	p := plain.New(&buf)

	u := domain.Unit{
		ID:       "sess-basic-u001",
		Files:    []string{"auth/token.go", "auth/port.go"},
		Headline: domain.Headline{Text: "1 edit, 1 write across 2 files", Why: "extract validation", WhySrc: domain.WhyStated},
	}
	if err := p.Present(u, nil); err != nil {
		t.Fatalf("Present: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"[sess-basic-u001]",
		"auth/token.go, auth/port.go",
		"WHAT  1 edit, 1 write across 2 files",
		"WHY   stated: extract validation",
		"FLAG  —",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}
