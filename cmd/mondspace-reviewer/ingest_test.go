package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

func TestIngestAssignsULIDAndTimestamp(t *testing.T) {
	root := t.TempDir()
	stdin := strings.NewReader(`{"session_id":"s","hook_event_name":"PostToolUse","tool_name":"Edit","tool_input":{"file_path":"a.go"}}`)

	before := time.Now().Add(-time.Second)
	if err := run(context.Background(), []string{"ingest", "--kind=edit", "--out=" + root}, stdin, &bytes.Buffer{}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "s", "events.jsonl"))
	if err != nil {
		t.Fatalf("reading events.jsonl: %v", err)
	}
	var e domain.Event
	if err := json.Unmarshal(bytes.TrimSpace(data), &e); err != nil {
		t.Fatalf("decoding appended event: %v", err)
	}

	if _, err := ulid.Parse(e.ID); err != nil {
		t.Errorf("ID %q is not a valid ULID: %v", e.ID, err)
	}
	if e.TS.Before(before) || e.TS.After(time.Now().Add(time.Second)) {
		t.Errorf("TS %v not within the ingest window", e.TS)
	}
}

func TestConcurrentIngestsAllLandIntact(t *testing.T) {
	root := t.TempDir()
	const n = 20

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := fmt.Sprintf(`{"session_id":"s","hook_event_name":"PostToolUse","tool_name":"Edit","tool_input":{"file_path":"f%d.go"}}`, i)
			_ = run(context.Background(), []string{"ingest", "--kind=edit", "--out=" + root}, strings.NewReader(payload), &bytes.Buffer{})
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(filepath.Join(root, "s", "events.jsonl"))
	if err != nil {
		t.Fatalf("reading events.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("got %d lines, want %d (append not atomic?)", len(lines), n)
	}
	for i, l := range lines {
		var e domain.Event
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Errorf("line %d is not intact JSON (interleaved write): %q", i, l)
		}
	}
}

func TestIngestAppendsExactlyOneLineAtSessionPath(t *testing.T) {
	root := t.TempDir()
	stdin := strings.NewReader(`{"session_id":"sess-xyz","hook_event_name":"PostToolBatch"}`)

	if err := run(context.Background(), []string{"ingest", "--kind=batch_end", "--out=" + root}, stdin, &bytes.Buffer{}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	path := filepath.Join(root, "sess-xyz", "events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected events.jsonl at %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("got %d lines, want exactly 1", len(lines))
	}
}

func TestIngestMalformedJSONExitsZeroAndAppendsNothing(t *testing.T) {
	root := t.TempDir()
	stdin := strings.NewReader("{ this is not valid json")

	err := run(context.Background(), []string{"ingest", "--kind=edit", "--out=" + root}, stdin, &bytes.Buffer{})
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
