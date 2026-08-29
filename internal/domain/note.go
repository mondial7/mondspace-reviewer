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
	// Anchor is the diff line this note is about, verbatim. Empty means the
	// note is about the file as a whole, which is what every note was before
	// line-level ones existed (ADR 0028).
	//
	// The line's *text* rather than its number: a diff grows above the line you
	// commented on constantly, and a number would drift onto something else
	// without ever looking wrong.
	Anchor string `json:"anchor,omitempty"`
	// AnchorNth tells identical lines apart — closing braces, blank lines and
	// `return nil` are everywhere, and text alone would put every note on the
	// first one.
	AnchorNth int `json:"anchor_nth,omitempty"`
}
