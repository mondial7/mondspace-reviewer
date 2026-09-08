// Package items is the findings store: one append-only file per repository,
// holding everything that outlives the session that raised it (ADR 0045).
//
// It is deliberately not beside the session log. The log is a per-machine
// record of what an agent did and belongs to nobody else; this file is the
// surface the planner reads and the place a team's dismissals accumulate, and
// those two want different answers to the question of whether they are
// committed.
package items

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/mondial7/mondspace-reviewer/contract"
)

// SharedDir is the directory both binaries know about. The reviewer writes the
// findings file in it; the planner reads it and writes its own beside it.
const SharedDir = ".mondspace"

// FileName is the reviewer's half of that contract.
const FileName = "findings.jsonl"

// BacklogFile is the planner's half of the shared directory. msr writes it only
// when asked to (`msr export --promote`): promotion is the planner's job by
// default, and a noisy analyser run dumping forty items onto somebody's board
// is exactly what that default is protecting (ADR 0045, and D1 in the phase 2
// spec).
const BacklogFile = "items.jsonl"

// Store is the findings file. It holds no state: two processes on two
// worktrees of one repository are two Stores over one file, and appending whole
// lines is what keeps that safe.
type Store struct {
	path string
}

// New opens the store under dir, which is the shared directory rather than the
// session log's root. Nothing is created until something is written.
func New(dir string) *Store {
	return &Store{path: filepath.Join(dir, FileName)}
}

// Path is where the store is, for a message that has to tell somebody.
func (s *Store) Path() string { return s.path }

// Append writes items as they are, in one open. A run that changed nothing
// calls this with nothing and writes nothing.
func (s *Store) Append(list ...contract.Item) error {
	if len(list) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, item := range list {
		line, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	return w.Flush()
}

// All is every item, resolved: the last line written for an id is what that id
// is now, and the order is the order the ids first appeared.
//
// A missing file is no items rather than an error — that is what a repository
// nobody has reviewed yet looks like. A malformed line is skipped: a store
// half-written by a killed process should cost the lines it lost and no more.
func (s *Store) All() ([]contract.Item, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var order []string
	latest := map[string]contract.Item{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var item contract.Item
		if err := json.Unmarshal(sc.Bytes(), &item); err != nil || item.ID == "" {
			continue
		}
		if _, seen := latest[item.ID]; !seen {
			order = append(order, item.ID)
		}
		latest[item.ID] = item
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	out := make([]contract.Item, 0, len(order))
	for _, id := range order {
		out = append(out, latest[id])
	}
	return out, nil
}

// OnBranch is every item raised against one branch.
func (s *Store) OnBranch(branch string) ([]contract.Item, error) {
	all, err := s.All()
	if err != nil {
		return nil, err
	}
	var out []contract.Item
	for _, item := range all {
		if item.Branch == branch {
			out = append(out, item)
		}
	}
	return out, nil
}

// Compact rewrites the file as one line per item.
//
// Through a temporary file and a rename, so a crash leaves the store as it was
// rather than half of it. Sorted by when each item was first seen, because a
// file somebody reads in a diff should have new things at the bottom.
func (s *Store) Compact() error {
	all, err := s.All()
	if err != nil {
		return err
	}
	if len(all) == 0 {
		return nil
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].FirstSeen.Before(all[j].FirstSeen) })

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, FileName+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	w := bufio.NewWriter(tmp)
	for _, item := range all {
		line, err := json.Marshal(item)
		if err != nil {
			tmp.Close()
			return err
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// Backlog is the planner's file in the same directory, addressed the same way.
func Backlog(dir string) *Store {
	return &Store{path: filepath.Join(dir, BacklogFile)}
}

// Promote copies items into the backlog, skipping anything already there.
//
// By id rather than by fingerprint: the planner may have edited what it took,
// and a second promotion must not overwrite that with the reviewer's wording.
func (s *Store) Promote(list []contract.Item) (int, error) {
	existing, err := s.All()
	if err != nil {
		return 0, err
	}
	known := make(map[string]bool, len(existing))
	for _, item := range existing {
		known[item.ID] = true
	}

	var fresh []contract.Item
	for _, item := range list {
		if known[item.ID] {
			continue
		}
		known[item.ID] = true
		fresh = append(fresh, item)
	}
	return len(fresh), s.Append(fresh...)
}
