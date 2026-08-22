package domain

// AskScope is how wide an interrogation reaches.
type AskScope string

const (
	AskUnit    AskScope = "unit"    // the current unit, narrow and fast
	AskSession AskScope = "session" // the whole session so far
)

// AskContext is the bounded context an answer is drawn from: the event log,
// unit diffs, the task prompt, and existing notes — never a re-read of the repo.
type AskContext struct {
	Scope     AskScope
	Prompt    string
	Units     []Unit
	Diff      Diff
	Notes     []Note
	HasStated bool // whether the current unit carries a stated intent
}
