package plain_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/presenter/plain"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

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
