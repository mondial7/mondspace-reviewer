package usecase_test

import (
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func analyserNamed(t *testing.T, name string) usecase.Analyser {
	t.Helper()
	for _, a := range usecase.BuiltInAnalysers() {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no built-in analyser called %q", name)
	return usecase.Analyser{}
}

func TestEveryBuiltInAnalyserCanActuallyBeRun(t *testing.T) {
	// A default that is missing a detect command runs on a machine that does
	// not have the tool; one missing a format produces findings nobody can
	// read. Neither fails loudly, which is why this is checked here.
	seen := map[string]bool{}
	for _, a := range usecase.BuiltInAnalysers() {
		if a.Name == "" || len(a.Detect) == 0 || len(a.Run) == 0 {
			t.Errorf("%+v: needs a name, a detect command and a run command", a)
		}
		if a.Format != usecase.FormatSARIF && a.Format != usecase.FormatLines {
			t.Errorf("%s: unreadable format %q", a.Name, a.Format)
		}
		if a.Why == "" {
			t.Errorf("%s: a built-in should say what it is for", a.Name)
		}
		if seen[a.Name] {
			t.Errorf("%s offered twice", a.Name)
		}
		seen[a.Name] = true
	}
}

func TestAnAnalyserOnlySeesFilesItHasAnOpinionAbout(t *testing.T) {
	ruff := analyserNamed(t, "ruff")
	if !ruff.Applies("app/main.py") {
		t.Error("ruff should apply to python")
	}
	if ruff.Applies("internal/api/handler.go") {
		t.Error("ruff should not be handed Go files")
	}

	// A lockfile has no meaningful extension, so it is matched by name.
	osv := analyserNamed(t, "osv-scanner")
	if !osv.Applies("go.sum") {
		t.Error("osv-scanner should apply to go.sum")
	}
	if osv.Applies("internal/api/handler.go") {
		t.Error("osv-scanner should only run when a lockfile moved")
	}

	// No restriction means everything: a secret can be anywhere.
	leaks := analyserNamed(t, "gitleaks")
	if !leaks.Applies("testdata/fixture.yaml") {
		t.Error("gitleaks should look at everything")
	}
}

func TestArgvExpandsAPlaceholderToManyArgumentsNotOneString(t *testing.T) {
	// Joining paths into one argument is how a file with a space in its name
	// becomes two files that do not exist.
	a := usecase.Analyser{
		Name: "t", Run: []string{"tool", "--check", "{files}"},
		Scope: usecase.ScopeFiles,
	}
	got, ok := a.Argv([]string{"a b.go", "c.go"}, "/repo", "HEAD")
	if !ok {
		t.Fatal("two files is something to run over")
	}
	want := []string{"tool", "--check", "a b.go", "c.go"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("Argv = %q, want %q", got, want)
	}
}

func TestArgvSubstitutesInsideALargerArgument(t *testing.T) {
	a := usecase.Analyser{Name: "t", Run: []string{"tool", "--source={dir}", "--since={base}"}}
	got, ok := a.Argv([]string{"a.go"}, "/repo", "abc123")
	if !ok {
		t.Fatal("want an argv")
	}
	if got[1] != "--source=/repo" || got[2] != "--since=abc123" {
		t.Errorf("Argv = %q", got)
	}
}

func TestAWholePackageToolIsHandedDirectoriesOnce(t *testing.T) {
	// `go vet` cannot analyse one file of a package — the rest of the package
	// is the context — and running it twice for two files in one directory is
	// the same analysis twice.
	vet := analyserNamed(t, "go vet")
	got, ok := vet.Argv([]string{"internal/api/a.go", "internal/api/b.go", "main.go"}, "/repo", "HEAD")
	if !ok {
		t.Fatal("want an argv")
	}
	joined := strings.Join(got, " ")
	if strings.Count(joined, "./internal/api") != 1 {
		t.Errorf("a directory should appear once: %q", joined)
	}
	if !strings.Contains(joined, "./.") {
		t.Errorf("a file at the root should give the root package: %q", joined)
	}
}

func TestNothingToLookAtMeansNoProcess(t *testing.T) {
	ruff := analyserNamed(t, "ruff")
	if _, ok := ruff.Argv([]string{"main.go", "README.md"}, "/repo", "HEAD"); ok {
		t.Error("running a linter over no files is a process started for nothing")
	}
}

func TestSeverityIsMappedFromTheToolsOwnWords(t *testing.T) {
	gosec := analyserNamed(t, "gosec")
	if got := gosec.Level("error"); got != domain.SeverityHigh {
		t.Errorf("error = %q, want high", got)
	}
	if got := gosec.Level("note"); got != domain.SeverityLow {
		t.Errorf("note = %q, want low", got)
	}
	// Nothing said, or a word nobody mapped: "worth checking" is the honest
	// answer, and it is what the model's output gets for the same reason.
	if got := gosec.Level(""); got != domain.SeverityMedium {
		t.Errorf("unsaid = %q, want medium", got)
	}
	if got := gosec.Level("catastrophic"); got != domain.SeverityMedium {
		t.Errorf("unmapped = %q, want medium", got)
	}
}
