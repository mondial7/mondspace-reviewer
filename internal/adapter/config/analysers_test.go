package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/config"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.AnalyserFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func named(got []usecase.Analyser) []string {
	out := make([]string, 0, len(got))
	for _, a := range got {
		out = append(out, a.Name)
	}
	return out
}

func TestNoFileMeansTheBuiltInDefaults(t *testing.T) {
	// The normal case, and on a machine with none of those tools installed it
	// produces nothing and says nothing (ADR 0043).
	got, err := config.LoadAnalysers(t.TempDir())
	if err != nil {
		t.Fatalf("LoadAnalysers: %v", err)
	}
	if len(got) != len(usecase.BuiltInAnalysers()) {
		t.Errorf("got %d analysers, want the built-in set", len(got))
	}
}

func TestAddingOneHouseToolKeepsTheDefaults(t *testing.T) {
	dir := write(t, `
[[extra]]
name = "house-lint"
detect = ["house-lint", "--version"]
run = ["house-lint", "--sarif", "{files}"]
format = "sarif"
on = [".go"]

[extra.severity]
error = "high"
`)
	got, err := config.LoadAnalysers(dir)
	if err != nil {
		t.Fatalf("LoadAnalysers: %v", err)
	}
	names := strings.Join(named(got), " ")
	if !strings.Contains(names, "house-lint") {
		t.Errorf("the extra tool is missing: %s", names)
	}
	if !strings.Contains(names, "gosec") {
		t.Errorf("adding one tool should not drop the defaults: %s", names)
	}

	var house usecase.Analyser
	for _, a := range got {
		if a.Name == "house-lint" {
			house = a
		}
	}
	if house.Format != usecase.FormatSARIF || len(house.Run) != 3 {
		t.Errorf("house-lint = %+v", house)
	}
	if !house.Applies("a.go") || house.Applies("a.py") {
		t.Error("`on` should restrict it to Go")
	}
}

func TestListingAnalysersReplacesTheDefaults(t *testing.T) {
	// A reviewer who lists three tools means those three. Merging would keep
	// silently running the six they did not mention.
	dir := write(t, `
[[analyser]]
name = "only-this"
detect = ["only-this", "--version"]
run = ["only-this", "{files}"]
format = "lines"
`)
	got, err := config.LoadAnalysers(dir)
	if err != nil {
		t.Fatalf("LoadAnalysers: %v", err)
	}
	if len(got) != 1 || got[0].Name != "only-this" {
		t.Errorf("got %v, want only the one named", named(got))
	}
}

func TestABuiltInCanBeTurnedOff(t *testing.T) {
	dir := write(t, "off = [\"gosec\", \"semgrep\"]\n")
	got, err := config.LoadAnalysers(dir)
	if err != nil {
		t.Fatalf("LoadAnalysers: %v", err)
	}
	names := strings.Join(named(got), " ")
	if strings.Contains(names, "gosec") || strings.Contains(names, "semgrep") {
		t.Errorf("both should be off: %s", names)
	}
	if !strings.Contains(names, "gitleaks") {
		t.Errorf("the rest should still be on: %s", names)
	}
}

func TestADefinitionThatCouldNotWorkIsRefusedAtLoad(t *testing.T) {
	// Loudly, at load. A misspelled format produces a tool that silently never
	// reports anything, and a reviewer cannot tell that from "no findings".
	for _, body := range []string{
		"[[extra]]\nname = \"x\"\nrun = [\"x\"]\nformat = \"lines\"\n",
		"[[extra]]\nname = \"x\"\ndetect = [\"x\"]\nformat = \"lines\"\n",
		"[[extra]]\nname = \"x\"\ndetect = [\"x\"]\nrun = [\"x\"]\nformat = \"json\"\n",
		"[[extra]]\ndetect = [\"x\"]\nrun = [\"x\"]\nformat = \"lines\"\n",
	} {
		if _, err := config.LoadAnalysers(write(t, body)); err == nil {
			t.Errorf("this should have been refused:\n%s", body)
		}
	}
}

func TestAFileThatCannotBeParsedIsAnError(t *testing.T) {
	// Falling back to the defaults while the file says otherwise leaves nothing
	// to explain why a configured tool never ran.
	if _, err := config.LoadAnalysers(write(t, "this is not toml [[[")); err == nil {
		t.Error("an unreadable file should be reported, not ignored")
	}
}
