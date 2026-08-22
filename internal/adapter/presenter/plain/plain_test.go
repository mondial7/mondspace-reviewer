package plain_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/plain"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

func TestPresentRendersFlags(t *testing.T) {
	var buf bytes.Buffer
	u := domain.Unit{
		ID:    "u",
		Flags: []domain.Flag{domain.FlagNoTest, domain.FlagLarge},
	}
	if err := plain.New(&buf).Present(u); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "FLAG  no-test · large") {
		t.Errorf("flags not rendered:\n%s", got)
	}

	// No flags still shows the placeholder.
	buf.Reset()
	if err := plain.New(&buf).Present(domain.Unit{ID: "u"}); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "FLAG  —") {
		t.Errorf("empty flags should render placeholder:\n%s", got)
	}
}

func TestPresentDistinguishesStatedFromInferred(t *testing.T) {
	render := func(h domain.Headline) string {
		var buf bytes.Buffer
		if err := plain.New(&buf).Present(domain.Unit{ID: "u", Headline: h}); err != nil {
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
	if err := p.Present(u); err != nil {
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
