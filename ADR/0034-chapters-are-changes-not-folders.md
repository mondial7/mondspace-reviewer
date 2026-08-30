# 0034 — Chapters are changes, not folders

- **Status:** accepted
- **Date:** 2026-08-30

## Context

The story grouped files by directory and asked a model to title the groups. The
model was shown the area names and a few filenames, and nothing else — not one
line of the diff. So the best answer available to it was the name of a
directory, and that is what it gave: "internal", "cmd", "docs".

That degrades exactly as a change grows. Ten files under `internal/` is a
chapter called "internal"; a hundred files under `internal/` is still a chapter
called "internal". A reviewer does not need to be told that `cmd/` changed —
they can see that in the column beside it. They need to be told that tags now
resolve to the commit they point at.

## Decision

**A chapter is one change in behaviour, and the model is shown the change.**

The narration prompt carries every changed file with the important part of its
diff, bounded so a small local context can still hold it — 24 files, 26 lines
each, using the same compaction the audits use. It asks what the code now does
that it did not: which rule or default moved, what was added, removed or
renamed, and what that means for whoever calls it.

Chapters are titled as the change, in the present tense: *"Tokens expire after
an hour, not a day"*, never *"auth"* or *"changes to token.go"*. The prompt says
outright that a title which is a path or a filename is wrong.

**A chapter names files, not folders.** Two files in different directories that
do one thing belong in one chapter; one directory doing three unrelated things
is three chapters. The schema constrains the file list to an enum of the paths
that actually changed, so a chapter cannot be written about a file that is not
in the review — the same defence the old area enum gave, in the vocabulary a
behaviour is described in.

**Say only what the diff shows.** Where the model cannot tell why something
changed, it is told to say what changed and stop: a reason nobody gave is worse
than no reason, and the whole story is labelled inferred (ADR 0003).

### The old shape is the fallback, not the plan

Without diffs — a caller that has none, a model that will not answer — narration
falls back to what it did before: areas by path, narrated one at a time. That
path is worse and it is never a hole in the page, which is the trade it exists
to make.

## Consequences

- Chapters say something a reviewer could not get from the file list, which is
  the only reason to have them.
- The narration prompt is much larger. It is one call per review, and the audits
  already send diffs of the same size, so the cost is a call that was already
  being paid in kind.
- It asks more of the model than titling a folder did. Measured on this
  repository, a 4B instruct model produces usable behaviour chapters —
  "Sidebar replaces top rail for navigation", "Theme menu appears in front of
  cards" — where before it produced "internal" and "docs". A model that cannot
  manage it falls back, visibly, to the areas.
- A file the model leaves out still appears, in the catch-all chapter
  `reconcileChapters` has always added. Nothing is lost from the story because
  the model forgot it.
