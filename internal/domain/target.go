package domain

import "time"

// TargetKind is what sort of thing is under review. Most come from git; a
// session is one kind among them rather than the container of all of them
// (ADR 0017).
type TargetKind string

const (
	TargetCommit   TargetKind = "commit"
	TargetTag      TargetKind = "tag"
	TargetPR       TargetKind = "pull-request"
	TargetSession  TargetKind = "session"
	TargetWorktree TargetKind = "worktree"
	TargetRange    TargetKind = "range"
	// TargetLive follows HEAD instead of naming a point in history: it is
	// always "the working tree against whatever HEAD is now". It is the only
	// target whose range moves under it, which is why its identity is derived
	// from the repository alone (ADR 0018).
	TargetLive TargetKind = "live"
)

// Target is the thing under review: a range of history with a name. Reviewing
// one is what the engine always did — the net change per file between two
// snapshots — so a target only has to supply those two refs.
type Target struct {
	ID string
	// Ref is how a person names this point in history — a tag name, a short
	// commit hash, "#42". It is what the picker submits and what appears in the
	// URL, so a link reads /?target=v5.1.0 rather than a hex id.
	Ref      string
	Repo     string
	Kind     TargetKind
	Title    string
	Subtitle string
	From     SnapshotRef
	To       SnapshotRef
	TS       time.Time
	Commits  int
	// Sessions overlapping this range. A commit made during a recorded run
	// carries that run, so the intent behind a change is one click away — the
	// session enriching the commit rather than containing it.
	Sessions []string
}

// Tag is a git tag, with the commit it points at.
type Tag struct {
	Name string    `json:"name"`
	Hash string    `json:"hash"`
	TS   time.Time `json:"ts"`
}

// AgentConfig is how to reach the reviewer's model. It is the one piece of
// configuration worth persisting: everything else about a review is derived
// from git.
type AgentConfig struct {
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
	// NoThinking asks the server to skip the model's reasoning phase. Under
	// llama-server this is --reasoning-budget 0 and it works; under LM Studio it
	// was chat_template_kwargs and it did nothing (ADR 0014, ADR 0019).
	NoThinking bool `json:"no_thinking,omitempty"`
	// Overrides send a particular workload to a different model, or a different
	// server, or both. Absent means every workload shares the settings above,
	// which is the common case and stays the simple one.
	//
	// An override is the reviewer saying where this job goes, and it outranks
	// the routing table. The table is a default, not a policy.
	Overrides map[Workload]ModelRef `json:"overrides,omitempty"`
	// CLI is where the Claude Code CLI lives, for the jobs the routing table
	// sends there. An empty Model means the CLI's own default, which is almost
	// always what is wanted: the Model field above is about the other engine
	// and handing its name to the CLI fails the call (ADR 0035).
	//
	// An empty Endpoint means the CLI is not in play at all, and everything
	// stays on the local model — which is what msr does on a machine that has
	// never heard of Claude Code.
	CLI ModelRef `json:"cli,omitempty"`
}

// UsesCLI reports whether the Claude Code CLI is configured as an engine here.
func (c AgentConfig) UsesCLI() bool { return c.CLI.Endpoint != "" }
