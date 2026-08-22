package usecase

import (
	"path/filepath"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// SuppressCoveredNoTest removes the no-test flag from any unit whose changed
// source files all have a matching *_test file elsewhere in the session.
//
// This is the TDD pattern: the test is written first, in its own unit, and the
// implementation lands in a follow-up unit — which should not read as untested
// just because the test lives one chunk away. It is a pure, order-independent
// projection over the whole unit set.
func SuppressCoveredNoTest(units []domain.Unit) []domain.Unit {
	present := map[string]bool{}
	for _, u := range units {
		for _, f := range u.Files {
			present[f] = true
		}
	}

	out := make([]domain.Unit, len(units))
	for i, u := range units {
		out[i] = u
		if hasFlagValue(u.Flags, domain.FlagNoTest) && sourceHasTests(u.Files, present) {
			out[i].Flags = withoutFlag(u.Flags, domain.FlagNoTest)
		}
	}
	return out
}

// sourceHasTests reports whether every non-test source file in files has a
// matching *_test file among present (and there is at least one source file).
func sourceHasTests(files []string, present map[string]bool) bool {
	any := false
	for _, f := range files {
		if isTestFile(filepath.Base(f)) || !sourceExts[strings.ToLower(filepath.Ext(f))] {
			continue
		}
		if !present[expectedTestFile(f)] {
			return false
		}
		any = true
	}
	return any
}

// expectedTestFile is the conventional test path for a source file: foo.go →
// foo_test.go.
func expectedTestFile(f string) string {
	ext := filepath.Ext(f)
	return strings.TrimSuffix(f, ext) + "_test" + ext
}

func hasFlagValue(flags []domain.Flag, want domain.Flag) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

func withoutFlag(flags []domain.Flag, drop domain.Flag) []domain.Flag {
	out := make([]domain.Flag, 0, len(flags))
	for _, f := range flags {
		if f != drop {
			out = append(out, f)
		}
	}
	return out
}
