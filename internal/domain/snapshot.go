package domain

// SnapshotRef points at an immutable tree state bracketing a unit, so a unit's
// diff stays stable even as the working tree moves on.
type SnapshotRef struct {
	Commit string `json:"commit"`
	Label  string `json:"label"`
}

// Diff is the change between two snapshots, restricted to a unit's files.
type Diff struct {
	Text  string   `json:"text"`
	Files []string `json:"files"`
}
