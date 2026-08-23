# 0017 — Git is the subject; a session is a lens on it

- **Status:** accepted
- **Date:** 2026-08-23
- **Supersedes parts of:** [ADR 0002](0002-net-change-per-file.md), [ADR 0016](0016-one-page-and-a-shared-shell.md)

## Context

Everything in `msr` hangs off a *session*: a recorded run of an agent. Units are
named `<session>-fNNN`, stores are keyed by session id, the workspace lists
sessions, and the cockpit reviews one.

That was right when the tool watched one agent work. It is wrong now, for three
reasons a reviewer runs into immediately:

- **Most of what you want to review is not a session.** A commit, a tag, the
  range between two releases, the commits behind a pull request — none of these
  have a session, and `--since` was bolted on precisely because of it.
- **A session is not a unit of change.** It is a unit of *authorship*. Asking
  "what changed in v3.0.0" is a normal question that the model could not express.
- **Sessions are the perishable part.** A repository and its history outlive any
  recording of how they were made. Building the primary index on the perishable
  thing means a review evaporates when the log is cleaned up.

Meanwhile the engine underneath was never session-shaped. `BuildFileUnits` takes
`(from, to)` snapshot refs and returns the net change per file; the session was
only ever supplying those two refs.

## Decision

**A `Target` is the thing under review**, and git supplies most of them.

```go
type Target struct {
    ID       string      // stable, derived from repo + range
    Repo     string
    Kind     TargetKind  // commit | tag | pull-request | session | range | worktree
    Title    string
    From, To SnapshotRef
    Sessions []string    // sessions overlapping this range — the lens
}
```

Reviewing a target is exactly what the engine already did: `BuildFileUnits(from,
to)`. Nothing about net-change-per-file changes (ADR 0002 stands); only what
supplies the two refs, and what a review is *called*.

**A session becomes one kind of target, not the container of all of them.** It
still answers a real question — "what did the agent do in that run" — and it
still supplies the stated intent and the event log that nothing else can. It
takes its place beside the commits rather than above them.

**Targets carry their sessions.** A commit made during a recorded run lists that
session, so a reviewer reading `v3.1.0` can see which agent runs produced it and
jump to the intent behind a change. That is the "grouped view": the session
enriches the commit, rather than the commit being buried inside the session.

### Identity, and why it is derived

A target's id is a hash of `repo + from + to`, not a stored key. Two consequences
matter:

- The same range always reviews to the same id, so notes and a written story
  attach to it across restarts, machines, and clones.
- Nothing needs migrating when a session is deleted: the commits it produced are
  still reviewable under their own ids.

Unit ids are seeded from the target id rather than the session id, which is what
makes a note about a file survive the session being cleaned up.

### What discovery looks like

Given a repository, `msr` offers, newest first:

- every recent **commit**, each as a one-commit range;
- every **tag**, as the range from the previous tag — "what shipped in v3.1.0";
- every **pull request** it can see, grouping the commits that reference it;
- the **working tree**, when it is dirty;
- every recorded **session**, as its own range.

Only git is consulted. `msr` still talks to no forge (ADR 0015), so a pull
request is recognised from commit subjects and nothing else.

## Consequences

- `--since`/`--until` stop being a special case: they name a range target like
  any other.
- The workspace's primary axis becomes repository → target, and a session is one
  row among several kinds rather than the index.
- Notes keyed to unit ids derived from a session id do not carry over to the same
  files reviewed as a commit. This is a real break, and the reason this is a
  major version: the old ids still resolve for the session targets that produced
  them, but a note is attached to a review, and reviewing the same code under a
  different range is a different review.
- The event log stays exactly where it is, keyed by session, because it *is*
  session data. Only the review artefacts — notes, stories, conversations —
  follow the target.
