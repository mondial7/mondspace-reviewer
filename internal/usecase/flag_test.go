package usecase_test

import (
	"strings"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

func hasFlag(flags []domain.Flag, want domain.Flag) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

// diffWithChangedLines builds a unified-diff body with n added and n removed
// content lines, plus file headers that must not be counted.
func diffWithChangedLines(n int) domain.Diff {
	var b strings.Builder
	b.WriteString("diff --git a/x.go b/x.go\n")
	b.WriteString("--- a/x.go\n")
	b.WriteString("+++ b/x.go\n")
	b.WriteString("@@ -1,1 +1,1 @@\n")
	for i := 0; i < n; i++ {
		b.WriteString("-old line\n")
		b.WriteString("+new line\n")
	}
	return domain.Diff{Text: b.String(), Files: []string{"x.go"}}
}

func TestFlagLarge(t *testing.T) {
	// 76 added + 76 removed = 152 changed lines, over the 150 threshold.
	if !hasFlag(usecase.Flags(domain.Unit{}, diffWithChangedLines(76)), domain.FlagLarge) {
		t.Error("expected large flag for 152 changed lines")
	}
	// 75 + 75 = 150, not over.
	if hasFlag(usecase.Flags(domain.Unit{}, diffWithChangedLines(75)), domain.FlagLarge) {
		t.Error("did not expect large flag for exactly 150 changed lines")
	}
}

func TestFlagNoTest(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  bool
	}{
		{"non-test source, no test", []string{"auth/token.go"}, true},
		{"source with a test file", []string{"auth/token.go", "auth/token_test.go"}, false},
		{"only a test file", []string{"auth/token_test.go"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := domain.Unit{Files: tt.files}
			got := hasFlag(usecase.Flags(u, domain.Diff{}), domain.FlagNoTest)
			if got != tt.want {
				t.Errorf("no-test = %v, want %v", got, tt.want)
			}
		})
	}
}
