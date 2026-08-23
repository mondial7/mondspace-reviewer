package domain

// Flag is a deterministic, model-free signal that a unit deserves a closer look.
type Flag string

const (
	FlagNoTest       Flag = "no-test"
	FlagLarge        Flag = "large"
	FlagTodo         Flag = "todo"
	FlagNewDep       Flag = "new-dep"
	FlagSwallowedErr Flag = "swallowed-err"
	FlagPublicAPI    Flag = "public-api"
	FlagFailed       Flag = "failed"
)
