package domain

// Session is the reconstructed state of one watched agent session: its task
// prompt, the full event log, and the sealed units. It is rebuilt from the
// append-only log alone.
type Session struct {
	ID     string
	Prompt string
	Events []Event
	Units  []Unit
	Notes  []Note
}
