package domain

type Unit struct {
	ID        string   `json:"id"`
	SessionID string   `json:"session_id"`
	EventIDs  []string `json:"event_ids"`
	Files     []string `json:"files"`
	Sealed    bool     `json:"sealed"`
}
