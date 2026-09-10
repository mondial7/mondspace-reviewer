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
	"syscall"

	"github.com/mondial7/mondspace-reviewer/contract"
)

// SharedDir is the directory both binaries know about. The reviewer writes the
// findings file in it; the planner reads it and writes its own beside it.
const SharedDir = contract.Dir

// FileName is the reviewer's half of that contract.
const FileName = "findings.jsonl"

// LockName is what everybody writing the file holds while they do it.
//
// A lock on the file itself would not survive compaction: rewriting it is a
// rename, and a writer waiting on the old inode would wake up holding a lock on
// a file nothing reads any more, and append into it. So the lock is a file of
// its own, and it is never replaced.
const LockName = "findings.lock"

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

// locked runs fn while holding the store's write lock.
//
// Whole-line appends are already safe against each other (see Append); this is
// what makes them safe against a compaction, which replaces the file underneath
// everyone. A lock nobody can take is not worth failing a write over — a
// findings file is not worth losing to a permissions problem on a lock file —
// so an unlockable store proceeds unlocked.
func (s *Store) locked(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(filepath.Dir(s.path), LockName),
		os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fn()
	}
	defer f.Close()

	// A blocking flock returns EINTR when a signal lands, and giving up on it
	// would proceed unlocked during exactly the operation the lock is for.
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EINTR) {
			return fn()
		}
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	return fn()
}

// Append writes items as they are, in one open. A run that changed nothing
// calls this with nothing and writes nothing.
func (s *Store) Append(list ...contract.Item) error {
	if len(list) == 0 {
		return nil
	}
	return s.locked(func() error { return s.append(list) })
}

func (s *Store) append(list []contract.Item) error {
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	// One write per line, and no buffer in front of it.
	//
	// Two worktrees of one repository are two processes appending to this file,
	// which is a case ADR 0045 chose the format for. A buffered writer breaks
	// it: bufio copies what fits, flushes, and copies the rest, so a line that
	// straddles the buffer boundary reaches the file as two writes with another
	// process's line free to land between them. Measured at one line in five,
	// on ordinary items, with eight writers.
	//
	// Appending whole lines under O_APPEND is what makes this safe, and it only
	// holds if a line is one write.
	for _, item := range list {
		line, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	return nil
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

// Lines is how many lines the file holds, which is how many writes it has taken
// rather than how many items it holds. The difference between this and the
// length of All is what a compaction would save.
func (s *Store) Lines() (int, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		count++
	}
	return count, sc.Err()
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
	return s.locked(func() error { return s.compact() })
}

func (s *Store) compact() error {
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
