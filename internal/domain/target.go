package domain

import "time"

// TargetKind is what sort of thing is under review. Most come from git; a
// session is one kind among them rather than the container of all of them
// (ADR 0017).
type TargetKind string

const (
	TargetCommit   TargetKind = "commit"
	TargetTag      TargetKind = "tag"
	TargetPR       TargetKind = "pull-request"
	TargetSession  TargetKind = "session"
	TargetWorktree TargetKind = "worktree"
	TargetRange    TargetKind = "range"
)

// Target is the thing under review: a range of history with a name. Reviewing
// one is what the engine always did — the net change per file between two
// snapshots — so a target only has to supply those two refs.
type Target struct {
	ID       string
	Repo     string
	Kind     TargetKind
	Title    string
	Subtitle string
	From     SnapshotRef
	To       SnapshotRef
	TS       time.Time
	Commits  int
	// Sessions overlapping this range. A commit made during a recorded run
	// carries that run, so the intent behind a change is one click away — the
	// session enriching the commit rather than containing it.
	Sessions []string
}

// Tag is a git tag, with the commit it points at.
type Tag struct {
	Name string    `json:"name"`
	Hash string    `json:"hash"`
	TS   time.Time `json:"ts"`
}
