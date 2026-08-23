# 0005 — Review an arbitrary ref range with `--since`, no session required

- **Status:** accepted
- **Date:** 2026-08-23

## Context

Retroactive review (ADR 0002) derives its git baseline from a recorded
session: the commit at or before the session's first event. That only covers
the case where `msr` watched the agent via hooks. A reviewer who wants to
review a plain `git diff`-shaped range — "what changed since I branched off
main", "what changed in this release", "what's in this PR" — has no session to
point at, and forcing one into existence just to pick a baseline would be
backwards: the baseline *is* the input here, not a derived fact.

## Decision

Add `--since=<commit|branch|tag>` (optionally bounded by `--until=<ref>`) as a
first-class review mode, independent of `--session`:

- **Baseline** = `--since`, resolved via `git rev-parse` (new
  `Snapshotter.ResolveRef`).
- **Far end** = `--until` if given, otherwise the current working tree — same
  "empty `to` means working tree" convention `Diff` already used.
- It reuses the *exact* per-file net-diff engine session-based retroactive
  review uses (extracted as `buildFileUnits`): one unit per changed file, real
  diff, deterministic flags, `DiffHeadline`, `SuppressCoveredNoTest`. The two
  modes differ only in how the baseline/until `SnapshotRef`s are obtained —
  from a session's first-event time, or directly from `--since`/`--until`.
- `--session` becomes optional under `--since`. Given, it's reused as-is (so
  annotations accumulate under the same id across repeated reviews of the same
  range). Omitted, a session id is synthesized as `since-<ref>` — there is no
  task prompt or event log in this mode, and that's fine: the queue is built
  from git alone.
- `--plain` and `--tui` both work; `--plain` presents the computed units
  directly through the plain presenter, with no store/session load on the hot
  path.

`ChangedFiles` gained a `to` parameter to support the bounded case: an empty
`to` keeps today's "diff to the working tree, include untracked files"
behaviour; a supplied `to` diffs commit-to-commit and drops the untracked-file
scan (there is no working tree state at a past commit).

## Consequences

- One engine, two entry points: no duplicated per-file diff/flag/headline
  logic to keep in sync.
- `msr review --plain --since=<ref>` works with zero setup — no hooks
  installed, no agent, no session — same "try it with zero setup" spirit as
  the `replay` source.
- Cost: `Snapshotter` now has two ways to obtain a baseline (`Baseline`, time-
  based, for sessions; `ResolveRef`, ref-based, for `--since`) that callers
  must not confuse. `ChangedFiles` gained a parameter, touching both call
  sites.
