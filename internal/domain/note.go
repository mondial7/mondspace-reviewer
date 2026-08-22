package domain

import "time"

// NoteKind is the annotation a reviewer attaches to a unit. `ok` doubles as
// "mark read" — that is what keeps the queue moving.
type NoteKind string

const (
	NoteOK        NoteKind = "ok"
	NoteQuestion  NoteKind = "question"
	NoteObjection NoteKind = "objection"
	NoteDebt      NoteKind = "debt"
	NoteNote      NoteKind = "note"
)

// Note is a human annotation anchored to a Unit ID, never to file/line — the
// working tree is live, but unit IDs are immutable history.
type Note struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id"`
	UnitID       string    `json:"unit_id"`
	Kind         NoteKind  `json:"kind"`
	Text         string    `json:"text"`
	TS           time.Time `json:"ts"`
	SupersededBy string    `json:"superseded_by,omitempty"`
}
