package domain

type Unit struct {
	ID        string      `json:"id"`
	SessionID string      `json:"session_id"`
	EventIDs  []string    `json:"event_ids"`
	Files     []string    `json:"files"`
	From      SnapshotRef `json:"from"`
	To        SnapshotRef `json:"to"`
	Flags     []Flag      `json:"flags"`
	Headline  Headline    `json:"headline"`
	Sealed    bool        `json:"sealed"`
}
