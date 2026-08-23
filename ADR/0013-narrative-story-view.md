# 0013 — A narrative story view: deterministic grouping, model-written prose

- **Status:** accepted
- **Date:** 2026-08-23

## Context

The review page answers "what changed, file by file". It does not answer "what
was this session *about*". A reviewer arriving cold still has to assemble the
plot themselves from 40 file rows.

A local model can do better: cluster related files into themes and narrate them.
But a 9B model asked for structured output will sometimes invent file names,
merge unrelated work, return prose where JSON was asked for, or be unreachable
entirely. If the page depended on that output being well-formed, the feature
would be unreliable exactly when it matters.

## Decision

Add a **story view** (`/story`): the session read as chapters, in a
parallax, long-form reading layout.

Responsibilities are split so the fragile part is never load-bearing:

- **Grouping is deterministic by default.** `GroupByPath` clusters units by their
  top-level path segment. It is pure, instant, offline, and always produces a
  sane result.
- **The model may re-group and always writes the prose.** It is asked for
  chapters as JSON. Its output is *validated against the real units*: every unit
  id it names must exist, unknown ids are dropped, and any unit it forgets is
  appended to a final chapter. If the response is unparseable, or the summarizer
  is unreachable, the deterministic grouping stands with mechanical prose.
- **No invented facts.** Chapter prose is model-written and therefore labelled as
  inferred, consistent with ADR 0003. File lists, diffs, stats and flags shown
  beside the prose always come from git, never from the model.

The page reuses the review engine (`BuildFileUnits`) and the existing focus mode:
pressing `f` drops the parallax and renders the same chapters as plain prose.

## Narrating with a small local model

Measured against a local reasoning model on a 4k context: an 81-token prompt drew
~1,400 *reasoning* tokens before any output, and a ~250-token prompt exceeded the
window outright, returning empty content. `/no_think` did not stop it. A single
whole-session call is therefore unreliable on small contexts.

So narration degrades in stages rather than failing:

1. **One call for the whole session** — best grouping, when the model has room.
2. **One call per area** — a much smaller prompt and answer, which fits
   comfortably. The grouping stays deterministic; the model writes the words, and
   each chapter is published the moment it is written, so the page fills in
   progressively rather than after the last chapter.
3. **Deterministic grouping with mechanical prose**, and an error that names the
   likely cause (a context window too small) instead of failing silently.

Narration runs in the background: the page is served immediately with stage 3 and
upgraded in place. It never blocks a request.

## Consequences

- A reviewer can read a session as a story and follow links into the exact diffs.
- The feature degrades in three stages rather than failing: model chapters →
  deterministic chapters with mechanical prose → the file-by-file review.
- Cost: one model call per session (bounded context: headlines, files, stats —
  not full diffs), and a JSON contract that a small model will sometimes miss,
  which is why every field is validated rather than trusted.
