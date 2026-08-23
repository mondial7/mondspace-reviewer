# 0015 — A cockpit: the session at a glance, on one screen

- **Status:** accepted
- **Date:** 2026-08-23
- **Builds on:** [ADR 0004](0004-web-ui.md), [ADR 0012](0012-focus-mode.md), [ADR 0013](0013-narrative-story-view.md)

## Context

The web app has three ways to read a session and none of them answer the
question you actually have while an agent is *still working*: is it still going,
and what has it done so far? `/` is a review queue you work through, `/story` is
long-form reading, `/activity` is a provenance log. All three assume the session
is over.

## Decision

Add `/cockpit`: one desktop screen, three panes, meant to be left open on a
second monitor while the agent works.

- **Pulse (top left).** An isometric grid of blocks, one per changed file,
  that breathes while the session is live and settles when it goes quiet.
  Liveness is `data-live` on `<body>`, read fresh each frame, so live.js
  swapping regions cannot leave the animation stale.
- **Stats (top right).** Time open, files, lines, commits, pull requests.
- **Feed (bottom).** Every change, newest first, as a one-line description plus
  its diff. The only scrolling region on the page.

Three rules keep it honest:

- **Every number comes from git or the event log.** No stat is model-derived.
  The cockpit sits beside a narration feature precisely so the two can be told
  apart at a glance.
- **A diff is compacted, never truncated in silence.** `CompactDiff` keeps every
  hunk header — the shape of the change — and always states how many lines it
  dropped. One 900-line generated file must not push everything else off the
  screen, but a review tool that quietly hides diff content is worse than one
  that shows none.
- **The geometry is decoration.** Like ADR 0012's starfield, the scene reads the
  DOM and never feeds it. No WebGL, no Three.js, reduced motion, or focus mode
  and it bows out; the numbers and the feed are untouched.

## Pull requests, honestly

`msr` talks to no forge. Pull requests are counted by matching commit subjects
against GitHub's two shapes — the merge commit (`Merge pull request #42`) and
the squash-merge suffix (`Subject (#42)`) — and counting **distinct** references,
since two commits can land one PR. Verified against this repository: the five
detected matched exactly the five merged PRs.

The limits are real and worth stating: a forge with a different merge subject
convention will report zero, a rebase-merge that drops the reference is
invisible, and an *open* PR is not a commit and cannot be seen at all. This is a
count of pull requests that **landed**, not of pull requests that exist.

## Consequences

- A reviewer can watch a session progress without reading the agent's stream.
- Stats refresh on a 15-second tick from git and the event log. No model is
  involved, so it is cheap enough to run on a timer — unlike narration, which
  ADR 0014 restricts to once per review.
- `/` stays the landing page. The review log is the product; the cockpit is a
  monitor for it, and demoting the queue to make room would invert that.
- Cost: a fourth page to keep consistent, and a second Three.js scene — though
  it reuses the same vendored module, so no new bytes are shipped.
