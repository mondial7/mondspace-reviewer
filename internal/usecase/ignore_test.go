package usecase_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func ignoreUnits() []domain.Unit {
	return []domain.Unit{
		{ID: "u1", Files: []string{"api/handler.go"}},
		{ID: "u2", Files: []string{"vendor/x/lib.go"}},
		{ID: "u3", Files: []string{"go.sum"}},
		{ID: "u4", Files: []string{"auth/token.go"}},
	}
}

func TestNothingIsHiddenWithoutRules(t *testing.T) {
	shown, hidden := usecase.SplitIgnored(ignoreUnits(), nil)

	if len(shown) != 4 || len(hidden) != 0 {
		t.Errorf("shown %d hidden %d, want everything shown", len(shown), len(hidden))
	}
}

func TestHiddenFilesAreSetAsideWithTheReasonTheyWent(t *testing.T) {
	// A review tool that hides files has to say which, and why. Silently
	// dropping them is the failure this feature must not become (ADR 0027).
	rules := map[string]string{
		"vendor/x/lib.go": "vendor/",
		"go.sum":          "go.sum",
	}

	shown, hidden := usecase.SplitIgnored(ignoreUnits(), rules)

	if len(shown) != 2 {
		t.Fatalf("shown = %d, want the reviewer's own two files", len(shown))
	}
	for _, u := range shown {
		if u.Files[0] == "go.sum" || u.Files[0] == "vendor/x/lib.go" {
			t.Errorf("%s should have been set aside", u.Files[0])
		}
	}

	if len(hidden) != 2 {
		t.Fatalf("hidden = %+v, want two", hidden)
	}
	by := map[string]string{}
	for _, h := range hidden {
		by[h.Path] = h.Pattern
	}
	if by["go.sum"] != "go.sum" || by["vendor/x/lib.go"] != "vendor/" {
		t.Errorf("hidden = %+v, want each to carry the pattern that hid it", by)
	}
}

func TestAUnitIsOnlyHiddenWhenEveryFileInItIs(t *testing.T) {
	// Units can cover more than one file. Hiding one because a single file in
	// it matched would take the reviewer's work with it.
	units := []domain.Unit{
		{ID: "u1", Files: []string{"api/handler.go", "api/handler.pb.go"}},
	}
	rules := map[string]string{"api/handler.pb.go": "*.pb.go"}

	shown, hidden := usecase.SplitIgnored(units, rules)

	if len(shown) != 1 {
		t.Errorf("a unit with real work in it stays, got %d shown", len(shown))
	}
	if len(hidden) != 0 {
		t.Errorf("hidden = %+v, want none — the unit is still partly real work", hidden)
	}
}

func TestTheHiddenAreSortedForReading(t *testing.T) {
	rules := map[string]string{
		"z/last.go":  "*.go",
		"a/first.go": "*.go",
	}
	units := []domain.Unit{
		{ID: "u1", Files: []string{"z/last.go"}},
		{ID: "u2", Files: []string{"a/first.go"}},
	}

	_, hidden := usecase.SplitIgnored(units, rules)

	if len(hidden) != 2 || hidden[0].Path != "a/first.go" {
		t.Errorf("hidden = %+v, want them in path order", hidden)
	}
}
