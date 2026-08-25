package domain

// RepoState is what one glance at a repository sees. It is deliberately small:
// it has to be cheap enough to re-read every couple of seconds, so everything
// here comes from three git commands that touch no file contents.
type RepoState struct {
	// Head is the current HEAD commit hash, and Subject its first line. A HEAD
	// that moved is the definition of "a commit happened".
	Head    string
	Subject string
	// Commits is how many commits arrived since the previous observation. It is
	// counted by the watcher, which knows the old HEAD; a state on its own
	// cannot say.
	Commits int
	// Tags are the tag names, newest first.
	Tags []string
	// DirtyFiles is how many files differ from HEAD, and DirtyPrint a
	// fingerprint of that difference. The count alone is not enough: editing a
	// line in an already-changed file leaves the count identical.
	DirtyFiles int
	DirtyPrint string
}

// PulseKind is what sort of movement a pulse reports. The reviewer sees it as a
// colour; the page uses it to decide what a click should open.
type PulseKind string

const (
	PulseCommit PulseKind = "commit"
	PulseTag    PulseKind = "tag"
	PulseFiles  PulseKind = "files"
)

// Pulse is one piece of news about a watched repository, in the words it will
// be shown in. It carries where to go, because a notification that cannot be
// acted on is only an interruption.
type Pulse struct {
	Kind PulseKind `json:"kind"`
	Text string    `json:"text"`
	// Ref is the target to open — a short hash, a tag name, or the live target.
	// Empty means there is nothing to open, and the pulse is only an FYI.
	Ref string `json:"ref,omitempty"`
}
