package domain

import "time"

// Commit is one commit made while a session was running. It is a git fact, not
// a model inference: the cockpit shows it beside narration precisely so the two
// can be told apart.
type Commit struct {
	Hash    string    `json:"hash"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	TS      time.Time `json:"ts"`
}

// SessionStats is the session in numbers. Every field is derived from git or
// from the event log — nothing here is guessed.
type SessionStats struct {
	Started      time.Time
	Open         time.Duration
	Live         bool
	Files        int
	Added        int
	Removed      int
	Commits      int
	PullRequests int
}

// Edit is one recorded touch of a file: when, by which tool, and — only when the
// agent said so in its own words — why. Intent is verbatim and therefore
// `stated` in the sense of ADR 0003; nothing here is inferred.
type Edit struct {
	TS     time.Time `json:"ts"`
	Tool   string    `json:"tool"`
	Intent string    `json:"intent,omitempty"`
	Failed bool      `json:"failed,omitempty"`
}

// FileHistory is how a file reached its net change: how many times it was
// touched, over what span, and each touch in order. A net-change-per-file review
// deliberately collapses the agent's back-and-forth (ADR 0002); this is how a
// reviewer opens it back up when they want to.
type FileHistory struct {
	Count int
	First time.Time
	Last  time.Time
	Edits []Edit
}

// FileStat is one file's churn against a baseline. It is what `git diff
// --numstat` reports: cheap enough to poll, and sensitive to content, which a
// snapshot ref is not while a review diffs against the working tree.
type FileStat struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

// ChangeGroup is a set of files that changed together in one place. Five files
// added under one package is one act of work, not five, and reviewing them as
// five entries buries what actually happened.
//
// Meaning is model-written and therefore inferred (ADR 0003); the files, the
// churn and the histories beneath it come from git.
type ChangeGroup struct {
	ID      string
	Dir     string
	Units   []Unit
	Added   int
	Removed int
	Meaning string
	// Sample is a bounded slice of what changed, carried so a description can be
	// asked for without a second pass over the diffs.
	Sample string
}

// TreeNode is one row of the compact folder view: a directory to indent under,
// or a changed file with its churn and flags.
type TreeNode struct {
	Depth   int
	Name    string
	IsDir   bool
	UnitID  string
	Added   int
	Removed int
	Flags   []string
}
