package usecase_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// fakeDiffer serves a fixed set of changed files and per-file diffs.
type fakeDiffer struct {
	files []string
	diffs map[string]string
}

func (f fakeDiffer) ChangedFiles(context.Context, domain.SnapshotRef, domain.SnapshotRef) ([]string, error) {
	return f.files, nil
}

func (f fakeDiffer) Diff(_ context.Context, _, _ domain.SnapshotRef, paths []string) (domain.Diff, error) {
	return domain.Diff{Text: f.diffs[paths[0]], Files: paths}, nil
}

func TestBuildFileUnits(t *testing.T) {
	d := fakeDiffer{
		files: []string{"auth/token.go", "auth/token_test.go", ".mondspace-reviewer/s/events.jsonl"},
		diffs: map[string]string{
			"auth/token.go":      "@@ -1 +1 @@\n-old\n+new\n+more\n",
			"auth/token_test.go": "diff --git a/x b/x\nnew file mode 100644\n+func TestX(t *testing.T) {}\n",
		},
	}
	baseline := domain.SnapshotRef{Commit: "base"}

	units, diffs, err := usecase.BuildFileUnits(context.Background(), d, "s", baseline, domain.SnapshotRef{},
		func(f string) bool { return strings.HasPrefix(f, ".mondspace-reviewer/") })
	if err != nil {
		t.Fatalf("BuildFileUnits: %v", err)
	}

	if len(units) != 2 {
		t.Fatalf("got %d units, want 2 (the store path excluded)", len(units))
	}
	if units[0].Files[0] != "auth/token.go" || units[0].SessionID != "s" {
		t.Errorf("unit 0 = %+v, want auth/token.go in session s", units[0])
	}
	if units[0].From != baseline {
		t.Errorf("unit should record the baseline as From, got %+v", units[0].From)
	}
	if !units[0].Sealed {
		t.Error("units built from a net diff are sealed")
	}
	// Ids are stable and per-file.
	if units[0].ID == units[1].ID || units[0].ID == "" {
		t.Errorf("unit ids must be unique and non-empty: %q, %q", units[0].ID, units[1].ID)
	}
	// The diff for each unit is returned alongside.
	if !strings.Contains(diffs[units[0].ID].Text, "+new") {
		t.Errorf("diff not returned for unit 0: %q", diffs[units[0].ID].Text)
	}
	// Headline is mechanical and file-aware; the test file reads as added.
	if units[1].Headline.Text != "added token_test.go" {
		t.Errorf("unit 1 headline = %q, want 'added token_test.go'", units[1].Headline.Text)
	}
	// TDD coverage: token.go is not flagged no-test because its test is present.
	if hasFlag(units[0].Flags, domain.FlagNoTest) {
		t.Errorf("token.go should not be flagged no-test — its test changed too: %v", units[0].Flags)
	}
}
