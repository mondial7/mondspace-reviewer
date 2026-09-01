# 0038 — Read what moved

- **Status:** accepted
- **Date:** 2026-09-01

## Context

Every reading msr does — the story, the group descriptions, the two audits — was
all-or-nothing. A file changed, the whole thing ran again.

In a live review that is the wrong shape by an order of magnitude. Two files
move and fourteen do not. Re-reading sixteen chapters to learn about two is most
of a minute on a local model, and what comes back for the fourteen is the same
observation phrased differently, because that is what these models do. The
reviewer's place in the story moves under them for nothing.

For the audits it is worse than slow. `CarryJudgements` matches a dismissal to a
finding by file *and* claim, deliberately: a model saying something new about a
file is a new claim nobody has ruled on. A whole rerun re-derives every finding,
the wording drifts, the match fails, and the reviewer's dismissals quietly
return as open findings. The mechanism meant to make a dismissal stick was being
defeated by the thing that triggered it.

And underneath both, a unit id was its position in the file list —
`<review>-f001`, `-f002`, assigned in sorted order. An agent adding
`api/handler.go` to a review that already had `web/page.go` renumbered every
unit after it. Notes, chapters and findings are all pinned to unit ids. The
comment on `Note` says "anchored to a Unit ID, never to file/line — the working
tree is live, but unit IDs are immutable history". They were not.

## Decision

**Every reading records what each file said when it ran**, as
`Prints map[path]string` on both `Analysis` and `Narrative`. The whole-review
fingerprint answers "is this out of date"; this answers "which part of it".

**Audits merge.** `RunAuditIncremental` compares prints, shows the model only
the files that moved, and folds the answer into the previous result:

- a finding about a file the fresh reading did not see is carried across exactly
  as it was, verdict and all;
- a finding about a file that moved is dropped, because the reading that just
  ran is the current answer for that file;
- a finding that names no file is kept. It was a claim about the change as a
  whole, and a reading that saw two files out of fourteen is in no position to
  say it has stopped being true. Only a whole-change run clears it — a partial
  reading silently clearing a security finding is the one thing this must never
  do.

Nothing moved is answered without a model call at all. Everything moved is a
whole reading, because there is nothing to carry.

**The card says when a result was merged**: "re-read 2 of 14 files; the rest
carried forward". A merged result presented as a fresh whole-change reading
would be claiming something nobody checked.

**Stories and group descriptions get the same treatment.** `NarrateChanges`
rewrites only the chapters holding a file that moved and leaves the others
word-for-word; `DescribeChangedGroups` re-describes only the groups that moved.
Both decline and hand back to the full path when they cannot do better.

**Unit ids are derived from the path.** `FileUnitID` hashes the file name, so an
id means the same thing tomorrow. `PlaceNotes` re-anchors a note whose unit id
no longer exists onto the unit holding the file the note names — which recovers
every note written under the positional scheme. It never guesses: a note whose
file matches nothing, or matches more than one unit, stays exactly where it is.

**Nothing here touches a human annotation.** Notes live in their own log, keyed
by unit and file; no path in this change writes to them. Re-analysis recomputes
model output and only model output. That was already true structurally; it is
now true with a test saying so.

## Consequences

- A two-file change in a live review costs a two-file reading. That was the goal.
- Dismissals survive the thing that used to destroy them.
- A note keeps its file when a review gains one, which it did not before.
- The bound on a card had to grow: a merged card caps *standing* findings at
  five and keeps dismissed ones beyond that, because dropping a dismissal is how
  a dismissal becomes pointless. A hard ceiling of twenty stops an afternoon's
  judgements becoming a card nobody can read.
- Carrying prose forward means a chapter can keep a sentence written about an
  earlier draft of a *neighbouring* file. That is the trade: the alternative is
  paying for the whole story every time anything moves, and the chapter's own
  files are always current.
- Two shapes now exist for every reading — whole and merged — and a bug in the
  merge is harder to see than a bug in a rerun. The merge is pure and tested
  independently of any model for exactly that reason.
