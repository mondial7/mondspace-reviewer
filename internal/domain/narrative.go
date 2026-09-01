package domain

import "time"

// Narrative is a session read as a story: chapters of related change, each with
// prose explaining it. Grouping may come from a model, but every fact shown
// beside the prose (files, stats, flags) comes from git.
type Narrative struct {
	SessionID string    `json:"session_id"`
	Title     string    `json:"title"`
	Intro     string    `json:"intro"`
	Chapters  []Chapter `json:"chapters"`
	// Source is "model" when a model grouped and narrated the session, and
	// "mechanical" when it fell back to deterministic grouping (ADR 0013).
	Source string `json:"source"`
	// Model names which model wrote the title and prose, so a reader can tell
	// whose reading of the session they are looking at.
	Model string `json:"model,omitempty"`
	// Emoji is three to five pictographs a model chose for this change, for the
	// card that answers "what even is this" at a glance. Inferred like the
	// prose, and empty whenever nothing usable came back — a flourish is the
	// last thing that should be filled in with a guess (ADR 0003).
	Emoji []string `json:"emoji,omitempty"`
	// Meanings is what each group of changes is for, keyed by group id. Written
	// by a model and therefore inferred, like the prose.
	Meanings map[string]string `json:"meanings,omitempty"`
	// Highlights are the lines a model called out as the ones worth reading, by
	// the same key as Meanings. One to three per file: a highlight that covers
	// half the diff is not a highlight.
	Highlights map[string][]string `json:"highlights,omitempty"`
	// WrittenAt is when a model last read this review. Stored with the story, so
	// switching between targets answers "has this been read, and how long ago"
	// without another model call.
	WrittenAt time.Time `json:"written_at,omitempty"`
	// Fingerprint identifies the review this story was written for. A stored
	// story is reused while it matches, so opening the page again costs nothing.
	Fingerprint string `json:"fingerprint,omitempty"`
	// Prints is what each file looked like when this was written, keyed by
	// path. The fingerprint says whether the story is out of date; this says
	// which chapters of it are, which is what makes re-reading two files
	// cheaper than re-reading the review (ADR 0038).
	Prints map[string]string `json:"prints,omitempty"`
	// Engine is what actually answered, and Fallback says it was not the engine
	// the story is routed to (ADR 0039).
	Engine   Engine `json:"engine,omitempty"`
	Fallback bool   `json:"fallback,omitempty"`
}

// Chapter is one theme of the session.
type Chapter struct {
	Title   string   `json:"title"`
	Prose   string   `json:"prose"`
	UnitIDs []string `json:"unit_ids"`
}

const (
	NarrativeModel      = "model"
	NarrativeMechanical = "mechanical"
)
