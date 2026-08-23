package domain

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
