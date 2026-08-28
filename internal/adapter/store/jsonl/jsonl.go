// Package jsonl is an append-only JSONL store. Each session's log lives under
// <root>/<session-id>/ as events.jsonl and units.jsonl: crash-safe, tail-able,
// trivially diffable in tests.
package jsonl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
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

// AppendExchange records one question and its answer, so the review
// conversation outlives the process that had it.
func (s *Store) AppendExchange(e domain.Exchange) error {
	return s.appendLine(e.SessionID, "ask.jsonl", e)
}

func (s *Store) AppendNote(n domain.Note) error {
	return s.appendLine(n.SessionID, "notes.jsonl", n)
}

// Load reconstructs a Session from its append-only files. The task prompt is
// the first prompt event's stated intent.
func (s *Store) Load(sessionID string) (domain.Session, error) {
	if err := validSessionID(sessionID); err != nil {
		return domain.Session{}, err
	}
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

	exchanges, err := readLines[domain.Exchange](filepath.Join(s.root, sessionID, "ask.jsonl"))
	if err != nil {
		return domain.Session{}, err
	}
	sess.Exchanges = exchanges

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

// validSessionID guards against path traversal: a session ID arrives from an
// agent hook payload and is used as a directory name, so it must be a single,
// benign path segment.
func validSessionID(sessionID string) error {
	if sessionID == "" || sessionID == "." || sessionID == ".." {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	if strings.ContainsAny(sessionID, `/\`) || strings.Contains(sessionID, "..") {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	return nil
}

func (s *Store) appendLine(sessionID, file string, v any) error {
	if err := validSessionID(sessionID); err != nil {
		return err
	}
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

// narrativeFile is the one file in the store that is rewritten rather than
// appended to: a session has one current story, not a history of them.
const narrativeFile = "narrative.json"

// SaveNarrative stores a session's story so it survives a restart. It is written
// to a temporary file and renamed, so a crash mid-write leaves the previous
// story intact rather than a truncated one.
func (s *Store) SaveNarrative(n domain.Narrative) error {
	dir := filepath.Join(s.root, n.SessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, narrativeFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, narrativeFile))
}

// LoadNarrative returns the stored story, or a zero Narrative when the session
// has never been narrated. That is an ordinary state, not a failure: the caller
// narrates it. Only a real I/O fault is an error.
func (s *Store) LoadNarrative(sessionID string) (domain.Narrative, error) {
	body, err := os.ReadFile(filepath.Join(s.root, sessionID, narrativeFile))
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Narrative{}, nil
	}
	if err != nil {
		return domain.Narrative{}, err
	}
	var n domain.Narrative
	if err := json.Unmarshal(body, &n); err != nil {
		// A corrupt story is worth re-narrating, not worth failing over.
		return domain.Narrative{}, nil
	}
	return n, nil
}

// signoffFile records that a reviewer finished with a target, and what they
// said about it as a whole. Rewritten rather than appended: a target has one
// current verdict, and re-signing replaces it (ADR 0021).
const signoffFile = "signoff.json"

// SaveSignoff stores a target's verdict so reopening it says so. Written to a
// temporary file and renamed, so a crash mid-write leaves the previous verdict
// intact rather than a truncated one.
func (s *Store) SaveSignoff(v domain.Signoff) error {
	dir := filepath.Join(s.root, v.TargetID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, signoffFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, signoffFile))
}

// LoadSignoff returns a target's verdict, or a zero Signoff when nobody has
// finished with it. Never reviewed is the ordinary state, not a failure.
func (s *Store) LoadSignoff(targetID string) (domain.Signoff, error) {
	body, err := os.ReadFile(filepath.Join(s.root, targetID, signoffFile))
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Signoff{}, nil
	}
	if err != nil {
		return domain.Signoff{}, err
	}
	var v domain.Signoff
	if err := json.Unmarshal(body, &v); err != nil {
		// A corrupt verdict reads as "not reviewed", which is the safe way to
		// be wrong: it invites another look rather than claiming one happened.
		return domain.Signoff{}, nil
	}
	return v, nil
}

// analysisFile is where one audit's result lives, one file per kind.
//
// Per kind rather than one file holding all of them: two audits can be running
// at once, and a single file would mean read-modify-write races between two
// results that are supposed to be independent (ADR 0024).
func analysisFile(kind domain.AnalysisKind) string {
	return "analysis-" + string(kind) + ".json"
}

// SaveAnalysis stores one audit's result for one target.
func (s *Store) SaveAnalysis(a domain.Analysis) error {
	dir := filepath.Join(s.root, a.TargetID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(a)
	if err != nil {
		return err
	}

	name := analysisFile(a.Kind)
	tmp, err := os.CreateTemp(dir, name+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

// LoadAnalysis returns one audit's result, or a zero Analysis when it has never
// been run. Never run is the ordinary state, not a failure.
func (s *Store) LoadAnalysis(targetID string, kind domain.AnalysisKind) (domain.Analysis, error) {
	body, err := os.ReadFile(filepath.Join(s.root, targetID, analysisFile(kind)))
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Analysis{}, nil
	}
	if err != nil {
		return domain.Analysis{}, err
	}
	var a domain.Analysis
	if err := json.Unmarshal(body, &a); err != nil {
		// A corrupt result reads as "never run", which invites running it again
		// rather than presenting something unreadable as a finding.
		return domain.Analysis{}, nil
	}
	return a, nil
}
