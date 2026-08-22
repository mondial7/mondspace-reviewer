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
	if changedLines(d.Text) > largeThreshold {
		flags = append(flags, domain.FlagLarge)
	}
	if anyAddedLine(d.Text, hasTodo) {
		flags = append(flags, domain.FlagTodo)
	}
	return flags
}

var todoMarkers = []string{"TODO", "FIXME", "XXX"}

func hasTodo(line string) bool {
	for _, m := range todoMarkers {
		if strings.Contains(line, m) {
			return true
		}
	}
	return false
}

// anyAddedLine reports whether any added line (a "+" line that is not the +++
// header) satisfies pred. The leading "+" is stripped before testing.
func anyAddedLine(diff string, pred func(string) bool) bool {
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		if pred(line[1:]) {
			return true
		}
	}
	return false
}

// largeThreshold is the changed-line count above which a unit is "large".
const largeThreshold = 150

// changedLines counts added and removed content lines in a unified diff,
// ignoring the +++/--- file headers.
func changedLines(diff string) int {
	n := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			n++
		}
	}
	return n
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
