# 0042 — Below the line

- **Status:** accepted
- **Date:** 2026-09-01

## Context

msr is one loop: open a change, read it, annotate it, hand the log back. Around
that loop have accumulated a branches page, remote watching, workspace search,
five themes, a tour and an isometric field. Every one of them works. None of
them serves the loop.

The rail listed six destinations at equal weight, which said they were six
equally good places to be. They are not: five of them are places you visit once
a week, and one of them is the product.

The second question is harder and cannot be answered by looking. Narration is
the most expensive thing msr does and the thing most of the roadmap sits on
top of. Nobody knows whether the story is read. If it is written once, glanced
at, and re-rolled — or folded away and never opened — then what msr actually is
is grouping plus flags plus the log, and the roadmap should say so.

## Decision

**A line in the rail.** Cockpit, activity and settings above it; branches,
search and the tour under a quiet "also". Nothing is removed, nothing is hidden,
and no route changes. What changes is the claim the rail was making.

The isometric field is already handled: idle, it is a strip (ADR 0040).

**Instrument it, minimally, locally.** The audit log already records narration,
descriptions, audits, annotations and sign-offs. Two lines are added to it — the
story rail being opened, and being folded away — through a `POST /noted`
endpoint with an allow-list of three names. An allow-list rather than a free
text field: this writes into the record a reviewer later reads, and "whatever
the page said" is not a thing to put there.

The settings usage pane gains **what gets used**: those counts, in one table,
with the question each answers. It is read from this workspace's own log, on
this machine, and goes nowhere.

**The decision this is for**, stated now so it is not re-litigated later: if
after a week of real use `story rail folded away` and `stories written` are of
the same order and `notes written` dwarfs both, then narration is demoted from
the thing msr does to a thing msr offers, and the roadmap is rewritten around
grouping, flags and the log.

## Consequences

- Somebody looking for branches has one more thing to read past. That is the
  cost of the rail saying something true.
- The instrumentation can only see what happens in this browser. A story read
  and then left open records one `story-opened` however long it was read for,
  and a rail left open by default records nothing at all. It answers "is it
  asked for", not "is it read", and the table should not be read as more than
  that.
- Counting is done by scanning the audit log on every settings render. That is
  fine at the size these logs are and would not be at ten thousand entries.
- A decision criterion written down in advance is one that can be held to. It
  is also one that can be wrong, and a week is a small sample of one person.
