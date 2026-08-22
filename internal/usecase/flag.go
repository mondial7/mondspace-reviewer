package usecase

import (
	"path/filepath"
	"strings"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

// sourceExts are the code extensions whose changes ought to carry tests.
var sourceExts = map[string]bool{
	".go": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".py": true, ".java": true, ".rb": true, ".rs": true,
	".c": true, ".cc": true, ".cpp": true, ".h": true, ".hpp": true,
}

// Flags derives the deterministic flags for a unit from its files and diff. It
// is a pure function: no model, no I/O.
func Flags(u domain.Unit, d domain.Diff) []domain.Flag {
	var flags []domain.Flag
	if noTest(u.Files) {
		flags = append(flags, domain.FlagNoTest)
	}
	return flags
}

func noTest(files []string) bool {
	hasSource, hasTest := false, false
	for _, f := range files {
		base := filepath.Base(f)
		if isTestFile(base) {
			hasTest = true
			continue
		}
		if sourceExts[strings.ToLower(filepath.Ext(f))] {
			hasSource = true
		}
	}
	return hasSource && !hasTest
}

func isTestFile(base string) bool {
	return strings.Contains(base, "_test.")
}
