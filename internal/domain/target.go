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
	// TargetLive follows HEAD instead of naming a point in history: it is
	// always "the working tree against whatever HEAD is now". It is the only
	// target whose range moves under it, which is why its identity is derived
	// from the repository alone (ADR 0018).
	TargetLive TargetKind = "live"
)

// Target is the thing under review: a range of history with a name. Reviewing
// one is what the engine always did — the net change per file between two
// snapshots — so a target only has to supply those two refs.
type Target struct {
	ID string
	// Ref is how a person names this point in history — a tag name, a short
	// commit hash, "#42". It is what the picker submits and what appears in the
	// URL, so a link reads /?target=v5.1.0 rather than a hex id.
	Ref      string
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

// AgentConfig is how to reach the reviewer's model. It is the one piece of
// configuration worth persisting: everything else about a review is derived
// from git.
type AgentConfig struct {
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
	// NoThinking asks the server to skip the model's reasoning phase. Only some
	// chat templates honour it (ADR 0014).
	NoThinking bool `json:"no_thinking,omitempty"`
}
