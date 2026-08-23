# 0002 — Retroactive review is the net change per file, from git

- **Status:** accepted
- **Date:** 2026-08-23

## Context

The first model sealed a unit at every agent batch boundary and recorded a git
snapshot per unit. Two problems showed up as soon as it was used on a real
session:

1. **Empty diffs when reviewing after the fact.** Snapshots were taken at review
   time, so a retroactive run captured the same final tree for every unit and each
   diff came out empty.
2. **One unit per keystroke.** A file the agent touched ten times became ten units
   of "1 edit across 1 file" — reproducing the agent's back-and-forth, which is the
   exact cognitive load the tool exists to remove.

## Decision

Retroactive review reconstructs the session's **net** change from git:

- **Baseline** = the commit at or before the session's first event (empty tree if
  there is none).
- **One unit per changed file**, diffed baseline → working tree, including
  untracked files.
- Back-and-forth collapses into a single change per file, with real `+X -Y` stats
  and the actual diff on expand.

Live review keeps incremental per-batch units, where snapshots are taken as the
work happens and are therefore correct.

## Consequences

- Retroactive review reads like `git diff` or a pull request, which is the mental
  model reviewers already have.
- Works whether the agent committed or left changes in the working tree.
- The event log stops being the unit boundary and becomes context: ordering, the
  task prompt, stated intent, and the baseline timestamp.
- Cost: two chunking models to maintain (live incremental, retroactive per-file),
  and per-file granularity can still be coarse for a very large file.
