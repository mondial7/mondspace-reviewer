package usecase_test

import (
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
