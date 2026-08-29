# 0029 — Measuring before bounding

- **Status:** accepted
- **Date:** 2026-08-29
- **Part of:** [issue #19](https://github.com/mondial7/mondspace-reviewer/issues/19)

## Context

The plan said a tag range across thousands of files was untested and that the
changes column was unpaginated, and proposed bounds. It also said **measure
first**, which turned out to be the whole value of the exercise: every bound it
proposed would have been the wrong fix.

A 600-file review took **28 seconds per page load**, and did not improve on
repeat.

## Decision

Measure, then fix what the measurement points at.

### What it was not

A benchmark rendering 600 files took **127ms** wired and 160ms bare. Rendering
was never the problem, and paginating the changes column would have made the
page worse for no gain. `/export`, which loads the same session and renders
almost nothing, answered in **6ms** — so loading was not the problem either.

Sampling the live process settled it: `handleCockpit` → `openSession` →
`BuildFileUnits` → `Flags`. The review was being rebuilt from git on **every
request**.

### Two causes, both real

**The cache was thrown away on every request.** The cockpit tells the loader on
each request whether the reviewer wants ignored files shown (ADR 0027), and that
was acted on unconditionally — including when the answer had not changed, which
is every request. Each one cleared the review cache and rebuilt from git. It now
does nothing when nothing changed.

**One git process per file.** `BuildFileUnits` diffed each changed file on its
own. At 600 files that is 600 subprocesses before anything can render, and it
happens before any cache exists to help. `DiffAll` does the range in one
invocation and splits git's output on its own `diff --git` boundaries; a test
asserts the batched text is *identical* to what the per-file path produced.

It is an optional capability, asserted at the call site, like every other
optional capability here — and the per-file path still runs for anything the
batch did not return, because an untracked file produces no `git diff` output at
all.

**28s → 0.45s**, and the first load — the one no cache can help — went from 28s
to 1.0s.

## Consequences

- No pagination, no caps, no truncation. The measurement said the size was never
  the problem; two bugs were.
- The benchmark stays, in two forms. The bare one missed a 28-second page
  entirely because none of the wiring was there to be slow, which is worth
  remembering about benchmarks generally: they measure what you thought to put
  in them.
- The lesson is the ADR. Three plausible fixes were proposed from reading the
  code — bound the units, paginate the column, cap the diff — and all three were
  wrong. The profiler took four minutes.
