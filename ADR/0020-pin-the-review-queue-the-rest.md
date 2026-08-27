# 0020 — Pin the review, queue what arrives

- **Status:** accepted
- **Date:** 2026-08-28
- **Builds on:** [ADR 0018](0018-live-target-and-pulses.md)

## Context

ADR 0018 made the live target follow HEAD *and* the working tree, so a page left
open stayed current as the agent worked. That is right for a status display and
wrong for a review.

A reviewer forms a judgement about a specific state of the code. If the page
changes while they read it, three things go wrong at once: the diff they are
half-way through is replaced, a note they just wrote now describes a version
that no longer exists, and — worst — nothing tells them either happened. The
review silently becomes a review of something else.

The opposite is no better. Withholding the new work entirely lets someone finish
a careful review of code that was superseded ten minutes ago.

Neither is a defect of the mechanism; it is a decision only the reviewer can
make, and it was never put to them.

## Decision

**A live review stops at a pin**, and work arriving after that queues up where
it can be seen and acted on.

The pin is a snapshot taken when the review is opened — the existing
snapshotter, which records a tree without touching HEAD, the index, or the
working tree. Everything between the pin and the working tree is *pending*: not
part of what is being read, not hidden either.

Snapshotting costs a `git add -A` against a throwaway index, so it happens once
per review rather than once per request. Every later page load and every live
refresh reuses the pin, which is also exactly what makes the page hold still. A
commit moves HEAD out from under the pin, which makes it meaningless, so it is
retaken.

### The choice is the feature

The banner names what is waiting and offers three ways out:

| | |
| --- | --- |
| **keep reading** | do nothing. The most common answer, and it needs no server at all. |
| **include them** | move the pin to now. The review is rebuilt against it and covers the new work too. |
| **review just these** | leave this review as it stands and open `pin..now` — an ordinary range target, narrated and annotated like any other. |

That "review just these" is a plain range is what makes it cheap: no new kind of
thing, no new storage, and it inherits everything targets already do.

### Classification, not a count

"Three files changed" is a fact. It is not enough to decide on. Each waiting
file is classified against the review being read:

- **not in the review** — more work arriving, and it can wait;
- **on screen** — the page no longer matches the disk;
- **annotated** — the reviewer already ruled on this file, and did so against a
  version that no longer exists.

That last one is the reason to interrupt someone, so it leads the list, it is
the only thing here that is coloured, and it is the only clause added to the
headline: *"3 files changed since you opened this review — 1 you had already
annotated"*. A superseded note does not count: it has been dealt with, and
warning about it again would be noise.

### Dismissal has to stick

"Keep reading" is remembered against the sentence it dismissed. A banner that
reappears on the next two-second refresh is not dismissable, only slower. The
next *different* piece of news comes back, because it is different news.

## Consequences

- The live review no longer updates in place. That is a deliberate reversal of
  part of ADR 0018: what made a good status display makes a bad review.
- Pulses (ADR 0018) and pending are separate and both stay. A toast says the
  repository moved; the banner says what that means for what is on screen. A
  commit on another branch is news and changes nothing about the open review.
- msr's own store is excluded from the pending set, for the same reason it is
  excluded from pulses: it writes on exactly the ticks being watched.
- Pending is recomputed every couple of seconds but only broadcast when the
  sentence would read differently, so the page is not redrawn for a line that
  has not changed.
