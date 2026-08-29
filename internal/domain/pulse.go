package domain

import "time"

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
	// PulseRemote is somebody else's work arriving: the upstream moved, or a
	// branch appeared that was not there before.
	PulseRemote PulseKind = "remote"
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

// RemoteState is what the remote looked like at one glance: where the branch
// being worked on sits against its upstream, and what branches exist (issue
// #18).
//
// It is deliberately about the *upstream of the current branch* rather than
// every branch on the server. "Am I behind, and who moved it" is the question a
// reviewer has while working; a full picture of forty branches is a different
// tool.
type RemoteState struct {
	Branch   string // the local branch checked out
	Upstream string // what it tracks, e.g. origin/main
	// UpstreamHash is where the upstream ref points. A moved hash is the
	// definition of "somebody pushed".
	UpstreamHash string
	Ahead        int // local commits the remote has not got
	Behind       int // upstream commits not here yet
	// LastSubject and LastAuthor describe the newest upstream commit, so news
	// about it can name who it came from.
	LastSubject string
	LastAuthor  string
	Branches    []string // remote-tracking branches, for spotting new ones
	// Fetched is when msr last fetched, zero when it never has — fetching is
	// opt-in, because it is a network call that writes refs (ADR 0025).
	Fetched time.Time
}
