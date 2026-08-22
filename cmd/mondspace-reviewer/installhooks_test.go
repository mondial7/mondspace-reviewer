package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallHooksCommandWritesSettingsFile(t *testing.T) {
	dir := t.TempDir()

	if err := run(context.Background(), []string{"install-hooks", "--dir=" + dir}, nil, &bytes.Buffer{}); err != nil {
		t.Fatalf("install-hooks: %v", err)
	}

	path := filepath.Join(dir, ".claude", "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected settings at %s: %v", path, err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok || hooks["PostToolUse"] == nil {
		t.Errorf("settings missing our hooks: %s", data)
	}
}

func TestInstallHooksCommandIsIdempotentAndPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claude, "settings.json"), []byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := run(context.Background(), []string{"install-hooks", "--dir=" + dir}, nil, &bytes.Buffer{}); err != nil {
			t.Fatalf("install-hooks run %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(claude, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["model"] != "opus" {
		t.Errorf("existing model key lost: %v", settings["model"])
	}
}
