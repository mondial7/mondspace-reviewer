package usecase

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
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
	if takesOnDependency(u.Files, d.Text) {
		flags = append(flags, domain.FlagNewDep)
	}
	if anyAddedLine(d.Text, isSwallowedErr) {
		flags = append(flags, domain.FlagSwallowedErr)
	}
	if anyRemovedLine(d.Text, isExportedDecl) {
		flags = append(flags, domain.FlagPublicAPI)
	}
	if hasSoloIface(d.Text) {
		flags = append(flags, domain.FlagSoloIface)
	}
	return flags
}

// interfaceDecl matches an added Go interface declaration, e.g.
// "type Validator interface {".
var interfaceDecl = regexp.MustCompile(`^type\s+([A-Za-z_]\w*)\s+interface\s*\{`)

// ifaceMethodSig matches a method signature line inside an interface body,
// e.g. "Validate(token string) error", capturing the method name.
var ifaceMethodSig = regexp.MustCompile(`^([A-Z]\w*)\s*\(`)

// methodWithRecv matches an added method declaration with a receiver, e.g.
// "func (v *tokenValidator) Validate(token string) error {", capturing the
// method name.
var methodWithRecv = regexp.MustCompile(`^func\s*\([^)]*\)\s*([A-Za-z_]\w*)\s*\(`)

// hasSoloIface implements the SPEC §9 "solo-iface" heuristic: the diff
// declares a new Go interface, and the same diff adds no method (with a
// receiver) whose name matches one of the interface's declared methods.
//
// This is a PURE DIFF HEURISTIC — it never looks at the rest of the repo
// (ADR 0001 forbids usecase code from doing I/O), only at the lines this one
// diff adds. That means it can both over-flag (an implementation that lands
// in a later, separate unit/diff still reads as "solo") and under-flag (a
// same-diff type with an unrelated method that happens to share a name is
// treated as an implementation). See ADR 0011 for the tradeoff.
func hasSoloIface(diff string) bool {
	var added []string
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		added = append(added, strings.TrimSpace(line[1:]))
	}

	methods := map[string]bool{}
	found := false
	for i, line := range added {
		m := interfaceDecl.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		found = true
		for j := i + 1; j < len(added) && added[j] != "}"; j++ {
			if mm := ifaceMethodSig.FindStringSubmatch(added[j]); mm != nil {
				methods[mm[1]] = true
			}
		}
	}
	if !found {
		return false
	}

	for _, line := range added {
		if mm := methodWithRecv.FindStringSubmatch(line); mm != nil && methods[mm[1]] {
			return false
		}
	}
	return true
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

// dependencyManifests are the files whose job is to declare what a project
// depends on. The flag asks a supply-chain question — "does this change take on
// something new" — and only these files can answer it.
var dependencyManifests = map[string]bool{
	"go.mod": true, "package.json": true, "Cargo.toml": true,
	"requirements.txt": true, "requirements.in": true, "Pipfile": true,
	"pyproject.toml": true, "Gemfile": true, "composer.json": true,
	"pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
	"pubspec.yaml": true, "mix.exs": true, "Package.swift": true,
}

// dependencyLocks pin what a manifest asked for. Their contents are generated
// and look nothing like a manifest's, so any addition to one is a dependency
// change by definition — that is the only thing they contain.
var dependencyLocks = map[string]bool{
	"go.sum": true, "package-lock.json": true, "yarn.lock": true,
	"pnpm-lock.yaml": true, "Cargo.lock": true, "Gemfile.lock": true,
	"composer.lock": true, "poetry.lock": true, "Pipfile.lock": true,
	"pubspec.lock": true, "mix.lock": true, "Package.resolved": true,
}

// goModRequire matches a go.mod dependency line, e.g. "github.com/x/y v1.2.3".
var goModRequire = regexp.MustCompile(`^[\w.\-/]+ v\d`)

// namedVersion matches a dependency named with a version in the formats the
// other manifests use: `"lodash": "^4.17.21"`, `serde = "1.0"`, `requests==2.31.0`,
// `gem "rails", "~> 7.0"`.
var namedVersion = regexp.MustCompile(
	`^(?:"[^"]+"\s*:\s*"[^"]*"|[\w.\-]+\s*=\s*[\["]|[\w.\-]+\s*(?:[=~^><]=?|@)\s*[\dv"]|gem\s+["'])`)

// takesOnDependency reports whether a change adds a dependency.
//
// It is scoped to the files that manage dependencies. It used to fire on any
// added `import` line anywhere, which meant every source file that gained an
// internal import carried the flag — and a flag that fires constantly is one a
// reviewer learns to scroll past, which costs more than the flag was worth.
func takesOnDependency(files []string, diff string) bool {
	manifest, lock := false, false
	for _, f := range files {
		base := filepath.Base(filepath.ToSlash(f))
		if dependencyManifests[base] {
			manifest = true
		}
		if dependencyLocks[base] {
			lock = true
		}
	}

	// A lock file holds nothing but pinned dependencies.
	if lock && anyAddedLine(diff, func(string) bool { return true }) {
		return true
	}
	return manifest && anyAddedLine(diff, isDependencyLine)
}

// isDependencyLine reports whether a manifest line names a dependency, as
// opposed to a language version, a module name, or a section header.
func isDependencyLine(line string) bool {
	t := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(t, "require "):
		return true
	case goModRequire.MatchString(t):
		return true
	case namedVersion.MatchString(t):
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
