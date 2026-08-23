# 0009 — `msr gc`: delete review refs for sessions that are done

- **Status:** accepted
- **Date:** 2026-08-23

## Context

Every session gets a throwaway ref, `refs/mondspace/review/<session-id>`
(ADR 0001, SPEC §7), chained with `commit-tree` on every snapshot. Nothing
ever deletes these. A long-lived repo watched by `msr` across many sessions
accumulates one ref (and its whole commit chain) per session forever — pure
debris once a session's log (`.mondspace-reviewer/<session-id>/`) is gone,
since without the log there is nothing left that can resolve a unit's `From`/
`To` back to a purpose. SPEC §7 already names the fix: "`msr gc` deletes
`refs/mondspace/review/*` for closed sessions."

Deleting a git ref is destructive and not undoable from `msr` itself (the
commits become unreachable and eventually get pruned), so the design leans on
two things: an obvious, cheap-to-reason-about rule for what counts as
"closed", and a `--dry-run` that is genuinely side-effect free.

## Decision

Split the work by what it touches:

- `internal/adapter/snapshot/git`: `Snapshotter.ReviewRefs(ctx)` lists the
  session IDs with a ref under `refs/mondspace/review/*` (`git for-each-ref`,
  sorted), and `Snapshotter.DeleteReviewRef(ctx, sessionID)` deletes one
  (`git update-ref -d`). Both are repo-wide — the receiver's own `sessionID`
  is irrelevant to either call, since GC operates on the whole ref namespace,
  not one session. Deleting an absent ref is not an error (verified in
  `git_test.go`): `git update-ref -d` already succeeds either way, and a
  caller may legitimately race with another gc run or an already-gone ref.
- `internal/usecase`: `SessionsToGC(refSessions, storedSessions []string)
  []string` is the pure policy — a ref session is eligible for deletion
  exactly when it has no matching entry in `storedSessions`. It knows nothing
  about git or the filesystem, so it is table-driven-tested with plain string
  slices.
- `cmd/mondspace-reviewer`: `msr gc [--session=<id>] [--repo=.] [--out=…]
  [--dry-run]` wires the two together. `--session` deletes exactly that ref,
  skipping the "closed" computation entirely (an explicit, single target is
  always honoured). Without it, `gc` lists every review ref, lists the
  session directories under `--out` (`os.ReadDir`; a missing `--out` is not
  an error — it just means nothing is stored, so everything ref'd is
  eligible), and deletes the difference via `SessionsToGC`.

Every target is printed — `deleted refs/mondspace/review/<id>` normally,
`would delete refs/mondspace/review/<id>` under `--dry-run` — and `--dry-run`
never calls `DeleteReviewRef`, so it is provably side-effect free rather than
just documented as such. Running `gc` with nothing eligible prints that
explicitly (`nothing to garbage-collect: …`) instead of silent success.

## Consequences

- Deleting is opt-in and legible: a human sees the exact ref list before (or
  instead of, under `--dry-run`) anything is removed.
- The "closed" rule is deliberately simple — no store directory, no matter
  why (gc'd already, moved, never written) — rather than trying to infer
  session lifecycle from event contents. That keeps `SessionsToGC` a one-line
  set difference, but means a session whose store directory was deleted by
  hand (rather than truly finished) is gc'd the same way; that is the
  intended trade for a mechanical, dependency-free rule.
- Cost: nothing prunes the now-unreachable commit objects themselves — that
  is `git gc`'s job on the underlying repo, not `msr gc`'s. `msr gc` only
  removes the refs that were keeping them reachable.
