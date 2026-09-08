# 0048 — The three types become one

- **Status:** proposed
- **Date:** 2026-09-09

## Context

ADR 0044 decided that a model's finding, an analyser's finding and a reviewer's
note are one thing with three provenances, and said the merge could not land in
one commit: it would move the schema, the store, the presenters, the MCP tools
and the web surface at once, and nothing about it could be reviewed. So the
first pass added `contract.Item` and converted at the seam, leaving
`domain.Finding`, `domain.Reported` and `domain.Note` in place behind it.

That window was supposed to be short, and this closes it. Anything added to a
finding while three shapes exist has to be added in three places, which is the
exact failure the ADR was about.

## Decision

**All three types are deleted.** `contract.Item` is what the decoders produce,
what the store holds, what the pages render and what the exports write. There
is no conversion left anywhere, because there is nothing to convert between.

**The item grew two fields and lost none.** `Kind` is the one axis a human note
has that a tool's finding does not — `ok` doubling as "mark read" is what keeps
the queue moving — and `SupersededBy` is a note being overtaken by a later
change. Both were on `Note`; nothing else needed adding, which is the evidence
that the three types really were one.

**Producing an item is two steps, not one.** A decoder knows the rule, the file
and the sentence. It does not know which branch this is, what the code around
the finding looks like, or what an agent should do about it — so `Sighting`
fills that in afterwards. Splitting it this way is what lets a SARIF reader, a
model's reply and a reviewer's keystroke arrive at the same place by the same
road.

**`Key` is unchanged, byte for byte.** It hashes the producer, the rule, the
path as reported and the message, exactly as `Reported.Key` did — including
*not* normalising the path. That hash is written into dismissal files that
already exist on people's machines, and changing what goes into it would
quietly invalidate every ruling in them.

**What older builds wrote is still read, and the effort is proportional to what
it would cost to lose it.**

- The analyser cache is thrown away. It is a cache of something reproducible;
  the next scan rewrites it. Records with no producer are dropped rather than
  shown half-read.
- A model's reading is migrated. It cost a model run and the verdicts on it are
  a human's, so the file and the sentence are put back on the way in.
- Notes are migrated with the most care of the three, because a note is
  something a person typed once and there is nowhere else to get it from. The
  text, the time and the file are restored; the id, the kind, the unit, the
  anchor and the supersession kept their names and need no help.

Both stores share one reader for this, in a package whose whole job is reading
what older builds wrote — so there is one obvious place to delete from when a
format is old enough to stop supporting.

**Severity and verdict are aliases, not copies.** They were defined identically
in two packages, and they cross the boundary: an item in the store and an item
the planner reads have to mean the same thing by "high".

## Consequences

- One type, one store, one renderer, and a planner that compiles against the
  same declaration msr writes.
- Every item now carries fields most items will never use: an analyser's
  finding has no kind, a note has no rule. That is the cost of one table
  instead of three, and it is visible in every JSON line written.
- The templates barely changed, because they were already written against
  methods rather than fields. Where they did — a note's text — it was two
  lines.
- A findings cache from an older build is silently empty for one scan. That is
  a second of work the tools redo, and the alternative was carrying a decoder
  for a format that regenerates itself.
- `internal/domain` is smaller by three types and one file. What is left there
  is what never leaves the process: events, units, sessions, narratives.
