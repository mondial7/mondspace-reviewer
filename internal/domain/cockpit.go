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
