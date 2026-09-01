# 0041 — Narration reads like prose, facts read like facts

- **Status:** accepted
- **Date:** 2026-09-01

## Context

Every word on the page was monospace. A chapter a model wrote and a line count
git measured were set identically, which said they were the same kind of thing.
ADR 0003 is the argument that they are not, and the page was quietly
contradicting it.

The flags had the same shape of problem in colour. `no-test` is true of half the
files in a normal review, and it was painted the same red as `swallowed-err`. A
warning colour on a thing that is always there is not a warning; it is a
texture, and `no-test` had become the chip you learn to look past.

Three more, each small and each a dead end:

- Commit subjects in the picker were cut mid-word on one line. That is the list a
  reviewer chooses their next target from, and choosing from
  `Put tags and recorded runs in the his…` is choosing from a fragment.
- A verdict was stored already truncated to the card's width, so the `…` on the
  card expanded to nothing on the report. An ellipsis that leads nowhere is
  worse than no ellipsis.
- `not a problem` was the only control on the report page. It had no border, no
  background, footnote weight, and wording that reads as a label on the finding
  rather than as something you could press.

And severity pills at `0.62rem` on a 22%-mixed tint made `MEDIUM` and `LOW` two
greys, in the one place a reviewer wants to scan a column.

## Decision

**A model's sentences are set in a proportional face; everything measured stays
monospace.** Chapters, group and file descriptions, verdicts, findings, answers
and the reviewer's own written notes get `--sans`. Paths, hashes, churn, flags,
severities, file names inside a sentence — all stay in `--mono`. System faces
only: msr is offline software and does not fetch a font.

This reads better, because prose in monospace always reads worse. It also
answers "did somebody count this, or did something guess it" without a label.

**Flags are neutral by default.** Colour is reserved for `swallowed-err`,
`public-api` and `failed` — the ones that mean stop. And the review carries a
tally above the files: every flag, counted once, so thirty identical chips
become one fact with a number beside it.

**Text is stored whole and cut where it is shown.** The audit keeps the
verdict and each finding at the schema's bound rather than the card's, the card
trims to a hundred characters with a template function, and the report shows the
sentence. The ellipsis now expands.

**Commit subjects wrap to two lines**, clamped, broken at a word.

**The dismiss control looks like a control and says what it does.**
"dismiss — not a problem", at secondary weight, with a paragraph under the list
explaining that dismissing greys the finding, stops it counting, and leaves it
on the list so the next run does not raise it again as though nobody had looked.

**Severities are solid.** High and medium on a filled ground, low as an outline
— present, findable, and not shouting.

**One rule for heading case:** lower case, in the words it was written in,
nothing transformed. Upper case survives only on the one-word key beside a
value, where it is doing the work of a colon rather than naming a region.

**Shortcuts sit beside what they move.** `j`/`k`/`o` by the file list, `[`/`]`
by the history. The `?` overlay is where you look a key up; it is not where
anybody learns one.

## Consequences

- Two typefaces on one page is a thing to be disciplined about. A new element
  has to be classified as narration or as fact, and the default — monospace —
  is the safe one, since being wrong that way understates rather than
  overstates.
- Neutral flags mean a reviewer scanning for red now sees red rarely, which is
  the point and is also a change in what an empty-looking review means.
- Verdicts and findings stored at the schema bound are up to sixty characters
  longer on disk. Nothing reads them unbounded.
- The tally is per review and does not say *which* files. That is the file list's
  job, and giving the tally a filter is the next thing, not this thing.
