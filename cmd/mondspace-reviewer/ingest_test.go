package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIngestMalformedJSONExitsZeroAndAppendsNothing(t *testing.T) {
	root := t.TempDir()
	stdin := strings.NewReader("{ this is not valid json")

	err := run([]string{"ingest", "--kind=edit", "--out=" + root}, stdin, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ingest must exit 0 even on malformed input, got err: %v", err)
	}

	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Errorf("nothing should be written for malformed input, found %v", entries)
	}
	// And certainly no events.jsonl anywhere under root.
	_ = filepath.Walk(root, func(path string, info os.FileInfo, _ error) error {
		if info != nil && info.Name() == "events.jsonl" {
			t.Errorf("events.jsonl was written for malformed input: %s", path)
		}
		return nil
	})
}
