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
