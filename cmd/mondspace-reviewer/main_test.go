package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewCommandPrintsClusteredUnits(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"review",
		"--source=replay",
		"--file=" + filepath.Join("..", "..", "testdata", "sessions", "basic.jsonl"),
		"--plain",
		"--out=" + t.TempDir(),
	}

	if err := run(context.Background(), args, nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := out.String()

	// All eight units must appear, in order.
	for i, id := range []string{
		"sess-basic-u001", "sess-basic-u002", "sess-basic-u003", "sess-basic-u004",
		"sess-basic-u005", "sess-basic-u006", "sess-basic-u007", "sess-basic-u008",
	} {
		if !strings.Contains(got, "["+id+"]") {
			t.Errorf("output missing unit %d (%s)", i+1, id)
		}
	}

	golden := filepath.Join("testdata", "basic.plain.txt")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading golden (set UPDATE_GOLDEN=1 to create): %v", err)
	}
	if got != string(want) {
		t.Errorf("output does not match golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
