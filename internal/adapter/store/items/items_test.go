package items_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/items"
)

func item(id, branch string) contract.Item {
	return contract.Item{
		ID:          id,
		Fingerprint: "fp-" + id,
		Source:      contract.SourceAnalyser,
		Branch:      branch,
		Title:       "something",
		FirstSeen:   time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC),
	}
}

func TestRoundTrip(t *testing.T) {
	store := items.New(t.TempDir())
	if err := store.Append(item("a", "main"), item("b", "main")); err != nil {
		t.Fatal(err)
	}

	got, err := store.All()
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("All = %+v, want a then b", got)
	}
}

// A repository nobody has reviewed is not an error.
func TestNoFileIsNoItems(t *testing.T) {
	got, err := items.New(t.TempDir()).All()
	if err != nil {
		t.Fatalf("All on a fresh directory: %v", err)
	}
	if got != nil {
		t.Errorf("All = %+v, want nothing", got)
	}
}

// The store is append-only, so the last line written for an id is what that id
// is now — that is how a dismissal, a push and a fix are recorded.
func TestLastWriteWins(t *testing.T) {
	store := items.New(t.TempDir())
	if err := store.Append(item("a", "main")); err != nil {
		t.Fatal(err)
	}

	dismissed := item("a", "main")
	dismissed.Verdict = contract.VerdictDismissed
	if err := store.Append(dismissed); err != nil {
		t.Fatal(err)
	}

	got, err := store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("All = %d items, want 1 resolved record", len(got))
	}
	if got[0].Verdict != contract.VerdictDismissed {
		t.Errorf("verdict = %q, want the last thing written", got[0].Verdict)
	}
}

func TestOnBranch(t *testing.T) {
	store := items.New(t.TempDir())
	if err := store.Append(item("a", "main"), item("b", "feature/x")); err != nil {
		t.Fatal(err)
	}

	got, err := store.OnBranch("feature/x")
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].ID != "b" {
		t.Errorf("OnBranch = %+v, want just b", got)
	}
}

// A branch name with a slash in it is the ordinary case, and the reason the
// branch is a field rather than a directory (ADR 0045).
func TestBranchNamesWithSlashesAreOrdinary(t *testing.T) {
	dir := t.TempDir()
	store := items.New(dir)
	if err := store.Append(item("a", "feature/deep/name")); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != items.FileName {
		t.Errorf("the store made %v, want one %s", entries, items.FileName)
	}
}

// A store half-written by a killed process costs the lines it lost and no more.
func TestAMalformedLineIsSkipped(t *testing.T) {
	dir := t.TempDir()
	store := items.New(dir)
	if err := store.Append(item("a", "main")); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, items.FileName), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"id\":\"b\",\"tit\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, err := store.All()
	if err != nil {
		t.Fatalf("All over a truncated store: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("All = %+v, want the one good record", got)
	}
}

func TestCompact(t *testing.T) {
	dir := t.TempDir()
	store := items.New(dir)
	for i := 0; i < 3; i++ {
		updated := item("a", "main")
		updated.Title = "revision"
		if err := store.Append(updated); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Compact(); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, items.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(strings.TrimSpace(string(body)), "\n") + 1; lines != 1 {
		t.Errorf("the compacted store has %d lines, want 1", lines)
	}

	got, err := store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "revision" {
		t.Errorf("All after compaction = %+v, want the resolved record", got)
	}
}

// Compaction is through a rename, so an interrupted one leaves the store as it
// was rather than half of it. The visible half of that promise: it never leaves
// its temporary file behind.
func TestCompactLeavesNoDebris(t *testing.T) {
	dir := t.TempDir()
	store := items.New(dir)
	if err := store.Append(item("a", "main")); err != nil {
		t.Fatal(err)
	}
	if err := store.Compact(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("after compaction the directory holds %v, want only the store", entries)
	}
}
