package usecase

import (
	"path/filepath"
	"regexp"
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
	if anyAddedLine(d.Text, isNewDep) {
		flags = append(flags, domain.FlagNewDep)
	}
	if anyAddedLine(d.Text, isSwallowedErr) {
		flags = append(flags, domain.FlagSwallowedErr)
	}
	if anyRemovedLine(d.Text, isExportedDecl) {
		flags = append(flags, domain.FlagPublicAPI)
	}
	return flags
}

// exportedDecl matches a Go declaration of an exported (capitalised) identifier:
// func, method, type, var, or const.
var exportedDecl = regexp.MustCompile(`^(func (\([^)]*\) )?|type |var |const )[A-Z]`)

func isExportedDecl(line string) bool {
	return exportedDecl.MatchString(strings.TrimSpace(line))
}

// anyRemovedLine reports whether any removed line (a "-" line that is not the
// --- header) satisfies pred. The leading "-" is stripped before testing.
func anyRemovedLine(diff string, pred func(string) bool) bool {
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "-") || strings.HasPrefix(line, "---") {
			continue
		}
		if pred(line[1:]) {
			return true
		}
	}
	return false
}

// swallowedErr matches assigning a call's result to the blank identifier, e.g.
// "_ = f.Close()" — the idiomatic way to drop a returned error on the floor.
var swallowedErr = regexp.MustCompile(`^_\s*=\s*\S.*\(`)

func isSwallowedErr(line string) bool {
	return swallowedErr.MatchString(strings.TrimSpace(line))
}

// goModRequire matches a go.mod dependency line, e.g. "github.com/x/y v1.2.3".
var goModRequire = regexp.MustCompile(`^[\w.\-/]+ v\d`)

// importPath matches a quoted external import path, e.g. "github.com/x/y".
var importPath = regexp.MustCompile(`^"[\w.\-/]*[./][\w.\-/]*"$`)

func isNewDep(line string) bool {
	t := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(t, "import "):
		return true
	case strings.HasPrefix(t, "require "):
		return true
	case importPath.MatchString(t):
		return true
	case goModRequire.MatchString(t):
		return true
	default:
		return false
	}
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
