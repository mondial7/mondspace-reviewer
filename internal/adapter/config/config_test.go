package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/config"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

func TestSavedConfigComesBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	want := domain.AgentConfig{
		Endpoint: "http://192.168.1.9:1234/v1", Model: "qwen/qwen3.5-9b", NoThinking: true,
	}

	if err := config.Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got != want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

func TestNoConfigIsNotAnError(t *testing.T) {
	// Running without ever having configured anything is the normal case, and
	// the defaults are good. It must not be a failure.
	got, err := config.Load(filepath.Join(t.TempDir(), "absent.json"))

	if err != nil {
		t.Fatalf("a missing config should not error: %v", err)
	}
	if got != (domain.AgentConfig{}) {
		t.Errorf("got %+v, want nothing set", got)
	}
}

func TestACorruptConfigIsReported(t *testing.T) {
	// Silently ignoring it would send the reviewer to a default endpoint while
	// their file says otherwise, with nothing to explain the difference.
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := config.Load(path); err == nil {
		t.Error("a corrupt config should be reported, not ignored")
	}
}

func TestSaveIsAtomicAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, domain.AgentConfig{Endpoint: "http://a/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(path, domain.AgentConfig{Endpoint: "http://b/v1"}); err != nil {
		t.Fatal(err)
	}

	got, _ := config.Load(path)
	if got.Endpoint != "http://b/v1" {
		t.Errorf("Endpoint = %q, want the second write", got.Endpoint)
	}
	// It may hold an endpoint on a private network; it is not world-readable.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("permissions are %v, want no group or other access", perm)
	}
	// And no temporary file is left behind.
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("left %d files behind: %v", len(entries), entries)
	}
}

func TestDefaultPathIsUnderTheUsersConfigDirectory(t *testing.T) {
	got := config.DefaultPath()

	if got == "" {
		t.Fatal("there must be a default location")
	}
	if filepath.Base(got) != "config.json" || filepath.Base(filepath.Dir(got)) != "mondspace-reviewer" {
		t.Errorf("DefaultPath = %q, want …/mondspace-reviewer/config.json", got)
	}
}
