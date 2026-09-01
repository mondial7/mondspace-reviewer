package usecase

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// A fourth reading of a change, and the only one that is not a model (ADR 0043).
//
// msr ships no analysers. It detects what is already on `PATH` and adapts to
// it: a tool is a name, a command that proves it is installed, a command that
// runs it, and how to read what comes back. Adding one is a block of
// configuration, not a commit.

// Analyser is one deterministic tool msr knows how to run.
type Analyser struct {
	// Name is what the finding is attributed to. It is the reviewer's word for
	// the tool, and it goes in front of every rule id it produces.
	Name string `toml:"name"`
	// Detect is an argv that exits zero when the tool is installed. Usually
	// `--version`: cheap, and it proves the binary runs rather than only that
	// something of that name exists.
	Detect []string `toml:"detect"`
	// Run is the argv to analyse with, holding placeholders. `{files}` expands
	// to the changed files this run is scoped to — one argument each, never a
	// joined string; `{dirs}` to the directories holding them; `{dir}` to the
	// repository root; `{base}` to the ref the review is measured against.
	Run []string `toml:"run"`
	// Format is how to read the output: "sarif" or "lines". Two decoders, on
	// purpose. SARIF is the interchange format that means a tool emitting it
	// works with configuration alone, and "lines" covers the `file:line: msg`
	// convention that everything else follows.
	Format string `toml:"format"`
	// Severity maps the tool's own words onto msr's three levels. An unmapped
	// level becomes medium, which is what Severity.Normalise does anyway.
	Severity map[string]string `toml:"severity"`
	// On restricts the tool to files it has an opinion about, by extension or
	// exact name. Empty means every file.
	On []string `toml:"on"`
	// Scope says what the tool wants to be pointed at.
	Scope string `toml:"scope"`
	// Why is one line for the settings page: what this tool is for. Optional,
	// and only the built-ins have one — a reviewer configuring their own knows
	// why they added it.
	Why string `toml:"-"`
}

// The scopes an analyser can ask for.
const (
	// ScopeFiles hands the tool the changed files themselves. The cheapest and
	// the most precise; most tools accept it.
	ScopeFiles = "files"
	// ScopeDirs hands it the directories those files are in. This is what a
	// whole-package tool needs — `go vet` and `staticcheck` cannot analyse one
	// file of a package, because the rest of the package is the context.
	ScopeDirs = "dirs"
	// ScopeRepo points it at the repository and lets it find its own way. For
	// tools whose whole job is a repository-wide question: a lockfile audit, a
	// secret scan over history.
	ScopeRepo = "repo"
)

// The output formats msr can read.
const (
	FormatSARIF = "sarif"
	FormatLines = "lines"
)

// BuiltInAnalysers are sensible defaults for tools people actually have. None
// of them is bundled, installed, or required: each is used only if its detect
// command succeeds, and a machine with none of them installed shows nothing and
// nags about nothing (ADR 0043).
//
// Ordered by how much a reviewer wants the answer, because that is the order
// the findings are read in when several tools speak at once.
func BuiltInAnalysers() []Analyser {
	return []Analyser{
		{
			Name:   "gitleaks",
			Why:    "credentials committed by accident",
			Detect: []string{"gitleaks", "version"},
			Run: []string{"gitleaks", "detect", "--no-git", "--redact",
				"--report-format", "sarif", "--report-path", "/dev/stdout", "--source", "{dir}"},
			Format: FormatSARIF,
			Scope:  ScopeRepo,
			// Everything, deliberately. A secret is as likely to be in a YAML
			// file or a test fixture as in source.
			Severity: map[string]string{"error": "high", "warning": "high"},
		},
		{
			Name:     "gosec",
			Why:      "Go code that is unsafe rather than merely wrong",
			Detect:   []string{"gosec", "--version"},
			Run:      []string{"gosec", "-fmt", "sarif", "-quiet", "{dirs}"},
			Format:   FormatSARIF,
			Scope:    ScopeDirs,
			On:       []string{".go"},
			Severity: map[string]string{"error": "high", "warning": "medium", "note": "low"},
		},
		{
			Name: "osv-scanner",
			Why:  "known vulnerabilities in what this change depends on",
			// Paired with the `new-dep` flag: it is worth running exactly when a
			// lockfile moved, and worth nothing when one did not.
			Detect:   []string{"osv-scanner", "--version"},
			Run:      []string{"osv-scanner", "--format", "sarif", "--lockfile", "{files}"},
			Format:   FormatSARIF,
			Scope:    ScopeFiles,
			On:       LockfileNames,
			Severity: map[string]string{"error": "high", "warning": "medium"},
		},
		{
			Name: "golangci-lint",
			Why:  "the usual Go linters, already scoped to what changed",
			// `--new-from-rev` is diff-scoping done by the tool itself, which is
			// better than anything msr could do after the fact: it knows which
			// findings its own linters would have produced before.
			Detect: []string{"golangci-lint", "--version"},
			Run: []string{"golangci-lint", "run", "--out-format", "sarif",
				"--new-from-rev", "{base}", "{dirs}"},
			Format:   FormatSARIF,
			Scope:    ScopeDirs,
			On:       []string{".go"},
			Severity: map[string]string{"error": "high", "warning": "medium", "info": "low"},
		},
		{
			Name:   "staticcheck",
			Why:    "Go bugs a compiler will not catch",
			Detect: []string{"staticcheck", "--version"},
			Run:    []string{"staticcheck", "{dirs}"},
			Format: FormatLines,
			Scope:  ScopeDirs,
			On:     []string{".go"},
		},
		{
			Name:   "go vet",
			Why:    "the checks that ship with Go",
			Detect: []string{"go", "version"},
			Run:    []string{"go", "vet", "{dirs}"},
			Format: FormatLines,
			Scope:  ScopeDirs,
			On:     []string{".go"},
		},
		{
			Name:     "semgrep",
			Why:      "patterns, in whatever language this change is in",
			Detect:   []string{"semgrep", "--version"},
			Run:      []string{"semgrep", "--sarif", "--quiet", "--error", "{files}"},
			Format:   FormatSARIF,
			Scope:    ScopeFiles,
			Severity: map[string]string{"error": "high", "warning": "medium", "note": "low"},
		},
		{
			Name:     "ruff",
			Why:      "Python lint",
			Detect:   []string{"ruff", "--version"},
			Run:      []string{"ruff", "check", "--output-format", "sarif", "{files}"},
			Format:   FormatSARIF,
			Scope:    ScopeFiles,
			On:       []string{".py", ".pyi"},
			Severity: map[string]string{"error": "high", "warning": "medium"},
		},
		{
			Name:   "eslint",
			Why:    "JavaScript and TypeScript lint",
			Detect: []string{"eslint", "--version"},
			// Its own compact format rather than SARIF: the SARIF formatter is
			// a separate package nobody has installed, and a default that
			// requires an install is not a default.
			Run:    []string{"eslint", "--format", "unix", "--no-color", "{files}"},
			Format: FormatLines,
			Scope:  ScopeFiles,
			On:     []string{".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"},
		},
	}
}

// LockfileNames are the files whose changing is a reason to ask what this
// change now depends on. Matched by name, not extension.
var LockfileNames = []string{
	"go.sum", "go.mod", "package-lock.json", "pnpm-lock.yaml", "yarn.lock",
	"Cargo.lock", "poetry.lock", "uv.lock", "requirements.txt", "Gemfile.lock",
	"composer.lock",
}

// Applies reports whether this analyser has an opinion about a file.
//
// Matched on the extension, or on the whole name for the files that have no
// meaningful extension — a lockfile scanner cares that this is `go.sum`, not
// that it ends in `.sum`.
func (a Analyser) Applies(path string) bool {
	if len(a.On) == 0 {
		return true
	}
	name, ext := filepath.Base(path), filepath.Ext(path)
	for _, want := range a.On {
		if want == name || (strings.HasPrefix(want, ".") && want == ext) {
			return true
		}
	}
	return false
}

// Level maps one of the tool's own severity words onto msr's three.
//
// Anything unmapped or unrecognised becomes medium — "worth checking" is the
// honest answer when nothing said otherwise, and it is what Severity.Normalise
// does for the model's output for the same reason.
func (a Analyser) Level(word string) domain.Severity {
	if mapped, ok := a.Severity[strings.ToLower(strings.TrimSpace(word))]; ok {
		return domain.Severity(mapped).Normalise()
	}
	return domain.Severity(strings.ToLower(strings.TrimSpace(word))).Normalise()
}

// Argv expands the run command for a particular set of changed files.
//
// A placeholder standing alone as one argument expands to as many arguments as
// there are values — never to one joined string, which is how a path with a
// space in it becomes two files that do not exist. A placeholder inside a
// larger argument (`--source={dir}`) is substituted in place, where exactly one
// value makes sense.
//
// It reports false when the expansion would leave the tool with nothing to look
// at: running a linter over no files is a process started for nothing.
func (a Analyser) Argv(files []string, repoDir, base string) ([]string, bool) {
	mine := make([]string, 0, len(files))
	for _, f := range files {
		if a.Applies(f) {
			mine = append(mine, f)
		}
	}
	if len(mine) == 0 {
		return nil, false
	}

	values := map[string][]string{
		"{files}": mine,
		"{dirs}":  dirsOf(mine),
		"{dir}":   {repoDir},
		"{base}":  {base},
	}

	out := make([]string, 0, len(a.Run)+len(mine))
	for _, arg := range a.Run {
		if vals, whole := values[arg]; whole {
			out = append(out, vals...)
			continue
		}
		expanded := arg
		for name, vals := range values {
			if strings.Contains(expanded, name) {
				expanded = strings.ReplaceAll(expanded, name, first(vals))
			}
		}
		out = append(out, expanded)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// dirsOf is the distinct directories holding these files, sorted, each as a
// path a tool will accept as a package.
//
// `./` prefixed, because that is what the Go tools mean by a package path and
// what every other tool reads as a directory anyway. A file at the repository
// root gives `./`.
func dirsOf(files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		dir := filepath.ToSlash(filepath.Dir(filepath.ToSlash(f)))
		if dir == "." || dir == "" {
			dir = "."
		}
		path := "./" + strings.TrimPrefix(dir, "./")
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	sortStrings(out)
	return out
}

// sortStrings orders in place. A one-line wrapper so this file needs no import
// for it, which keeps the dependency list of the analyser model at zero.
func sortStrings(v []string) { sort.Strings(v) }

func first(vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}
