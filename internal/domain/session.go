package domain

import "time"

// Session is the reconstructed state of one watched agent session: its task
// prompt, the full event log, and the sealed units. It is rebuilt from the
// append-only log alone.
type Session struct {
	ID     string
	Prompt string
	Events []Event
	Units  []Unit
	Notes  []Note
	// Exchanges is the review conversation. It is part of the review, not a
	// transient UI state: a reviewer must be able to pick a thread up tomorrow.
	Exchanges []Exchange
}

// Exchange is one question to the reviewer assistant and its answer.
type Exchange struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	TS        time.Time `json:"ts"`
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	// Failed marks a question the model could not answer, kept so the reviewer
	// can see they asked rather than wondering whether they did.
	Failed bool `json:"failed,omitempty"`
}
