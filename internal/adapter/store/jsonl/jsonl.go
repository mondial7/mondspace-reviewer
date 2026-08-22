// Package jsonl is an append-only JSONL store. Each session's log lives under
// <root>/<session-id>/ as events.jsonl and units.jsonl: crash-safe, tail-able,
// trivially diffable in tests.
package jsonl

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

type Store struct {
	root string
}

func New(root string) *Store {
	return &Store{root: root}
}

func (s *Store) AppendEvent(e domain.Event) error {
	return s.appendLine(e.SessionID, "events.jsonl", e)
}

func (s *Store) AppendUnit(u domain.Unit) error {
	return s.appendLine(u.SessionID, "units.jsonl", u)
}

func (s *Store) appendLine(sessionID, file string, v any) error {
	dir := filepath.Join(s.root, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, file), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}
