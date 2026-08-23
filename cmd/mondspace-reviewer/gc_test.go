package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// gcTestRepo makes a temp git repo with one commit and a review ref for each
// of the given session IDs, mirroring the internal/adapter/snapshot/git test
// pattern.
func gcTestRepo(t *testing.T, sessions ...string) string {
	t.Helper()
	dir := t.TempDir()
	gcGit(t, dir, "init", "-q")
	gcGit(t, dir, "config", "user.email", "t@t")
	gcGit(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gcGit(t, dir, "add", "a.txt")
	gcGit(t, dir, "commit", "-qm", "init")
	head := gcGit(t, dir, "rev-parse", "HEAD")
	for _, s := range sessions {
		gcGit(t, dir, "update-ref", "refs/mondspace/review/"+s, head)
	}
	return dir
}

func gcGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func gcReviewRefs(t *testing.T, dir string) []string {
	t.Helper()
	out := gcGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/mondspace/review/")
	if out == "" {
		return nil
	}
	var refs []string
	for _, l := range strings.Split(out, "\n") {
		refs = append(refs, strings.TrimPrefix(l, "refs/mondspace/review/"))
	}
	return refs
}

func TestGCWithSessionDeletesOnlyThatRef(t *testing.T) {
	dir := gcTestRepo(t, "sess-a", "sess-b")
	storeRoot := t.TempDir() // no session directories: irrelevant when --session is given

	var out bytes.Buffer
	err := run(context.Background(),
		[]string{"gc", "--session=sess-a", "--repo=" + dir, "--out=" + storeRoot}, nil, &out)
	if err != nil {
		t.Fatalf("gc: %v", err)
	}

	refs := gcReviewRefs(t, dir)
	if len(refs) != 1 || refs[0] != "sess-b" {
		t.Errorf("refs after gc = %v, want [sess-b]", refs)
	}
	if !strings.Contains(out.String(), "sess-a") {
		t.Errorf("gc output should report the deleted session:\n%s", out.String())
	}
}

func TestGCDryRunDeletesNothing(t *testing.T) {
	dir := gcTestRepo(t, "sess-a")
	storeRoot := t.TempDir()

	var out bytes.Buffer
	err := run(context.Background(),
		[]string{"gc", "--session=sess-a", "--repo=" + dir, "--out=" + storeRoot, "--dry-run"}, nil, &out)
	if err != nil {
		t.Fatalf("gc --dry-run: %v", err)
	}

	refs := gcReviewRefs(t, dir)
	if len(refs) != 1 || refs[0] != "sess-a" {
		t.Errorf("refs after dry-run gc = %v, want [sess-a] (nothing deleted)", refs)
	}
	if !strings.Contains(out.String(), "sess-a") || !strings.Contains(strings.ToLower(out.String()), "would") {
		t.Errorf("dry-run output should say what would be deleted:\n%s", out.String())
	}
}

func TestGCWithoutSessionDeletesRefsWithNoStoreDir(t *testing.T) {
	dir := gcTestRepo(t, "sess-live", "sess-gone")
	storeRoot := t.TempDir()

	// sess-live has a store directory (its log still exists); sess-gone does not.
	store := jsonl.New(storeRoot)
	if err := store.AppendEvent(domain.Event{ID: "e1", SessionID: "sess-live", Kind: domain.KindEdit}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := run(context.Background(),
		[]string{"gc", "--repo=" + dir, "--out=" + storeRoot}, nil, &out)
	if err != nil {
		t.Fatalf("gc: %v", err)
	}

	refs := gcReviewRefs(t, dir)
	if len(refs) != 1 || refs[0] != "sess-live" {
		t.Errorf("refs after gc = %v, want [sess-live]", refs)
	}
	if !strings.Contains(out.String(), "sess-gone") {
		t.Errorf("gc output should report the deleted session:\n%s", out.String())
	}
	if strings.Contains(out.String(), "sess-live") {
		t.Errorf("gc output should not mention the kept session:\n%s", out.String())
	}
}

func TestGCNothingToDoReportsThat(t *testing.T) {
	dir := gcTestRepo(t) // no refs at all
	storeRoot := t.TempDir()

	var out bytes.Buffer
	err := run(context.Background(), []string{"gc", "--repo=" + dir, "--out=" + storeRoot}, nil, &out)
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if out.Len() == 0 {
		t.Error("gc with nothing to collect should still report that, not print nothing")
	}
}
