// Package jsonl is an append-only JSONL store. Each session's log lives under
// <root>/<session-id>/ as events.jsonl and units.jsonl: crash-safe, tail-able,
// trivially diffable in tests.
package jsonl

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
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

func (s *Store) AppendNote(n domain.Note) error {
	return s.appendLine(n.SessionID, "notes.jsonl", n)
}

// Load reconstructs a Session from its append-only files. The task prompt is
// the first prompt event's stated intent.
func (s *Store) Load(sessionID string) (domain.Session, error) {
	sess := domain.Session{ID: sessionID}

	events, err := readLines[domain.Event](filepath.Join(s.root, sessionID, "events.jsonl"))
	if err != nil {
		return domain.Session{}, err
	}
	sess.Events = events
	for _, e := range events {
		if e.Kind == domain.KindPrompt {
			sess.Prompt = e.StatedIntent
			break
		}
	}

	units, err := readLines[domain.Unit](filepath.Join(s.root, sessionID, "units.jsonl"))
	if err != nil {
		return domain.Session{}, err
	}
	sess.Units = units

	notes, err := readLines[domain.Note](filepath.Join(s.root, sessionID, "notes.jsonl"))
	if err != nil {
		return domain.Session{}, err
	}
	sess.Notes = notes

	return sess, nil
}

// readLines decodes each JSON line of a file into T. A missing file yields no
// items. A malformed line is skipped, not fatal.
func readLines[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var items []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var v T
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			continue
		}
		items = append(items, v)
	}
	return items, sc.Err()
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
