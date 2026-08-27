# 0021 — Finishing a review is a thing you can do

- **Status:** accepted
- **Date:** 2026-08-28
- **Builds on:** [ADR 0020](0020-pin-the-review-queue-the-rest.md)

## Context

Notes answer "what do I think of this change to this file". That is the whole
annotation model, and it is the right unit for most of what a reviewer writes.

Nothing answered the other question: **am I done with this, and what do I think
of it as a whole.** So there was no way to tell, on opening a target, whether
you had already been through it — the page looked identical whether you had
reviewed it yesterday or never seen it. On a workspace of forty commits and
tags, that is the difference between a queue and a pile.

The review card did say "reviewed", but about the *assistant* having read the
change. Two different facts wearing one word.

## Decision

**A `Signoff` records that a reviewer finished with a target**, when, and
optionally what they wanted to say about the change as a whole. The comment is
optional: being done is worth recording even with nothing to add.

It is stored per target, alongside the narrative, rewritten rather than appended
— a target has one current verdict, and reviewing it again replaces it. Both
stores implement it, asserted at compile time for the same reason the narrative
cache is: a store that silently forgot every verdict would show no symptom other
than reviews that never look finished.

### It has to be honest about going stale

A sign-off is a judgement about a specific state of the code. Reporting a bare
"reviewed" for something that has changed since would be exactly the lie ADR
0020 exists to prevent, one level up.

So the sign-off records the review's fingerprint and file count at the moment it
was made, and reopening compares. A count alone is not enough — editing a line
inside an already-changed file leaves it identical — so the fingerprint decides,
and the counts are quoted only when they actually differ, because "4 files then,
4 now" reads as though nothing happened.

The result is three states rather than two: not reviewed, reviewed, and
*reviewed but it has changed since*. The third is the one that was missing, and
it is the one that matters when watching an agent work.

### Two facts, two words

The assistant having read a change and you having reviewed it are different
facts about different actors. The card now says **"read by the assistant"** for
the first and keeps **"reviewed"** for the second. The button changed with it:
"ask it to read this" rather than "review this".

### The tick is the point

A signed-off target is ticked in the picker. Recording a verdict that could only
be seen by opening the thing would answer the question only for someone who had
already stopped needing to ask it — what is left to look at has to be readable
from the list.

## Consequences

- Sign-off is keyed by target id, which is stable by ADR 0017 and — for the live
  target — deliberately stable across commits by ADR 0018. A verdict therefore
  survives restarts, machines and clones for every kind of target.
- Reading a verdict and writing one are separate capabilities. A store that can
  answer but not record still shows the state; the form is what disappears.
- Discovery reads one small file per target to build the ticks. That is bounded
  by the target list and happens on start-up and when history moves, not per
  request.
- A corrupt verdict reads as "not reviewed". That is the safe way to be wrong:
  it invites another look rather than claiming one happened.
