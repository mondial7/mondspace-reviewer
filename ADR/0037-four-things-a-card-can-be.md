# 0037 — Four things a card can be

- **Status:** accepted
- **Date:** 2026-09-01

## Context

An analysis card had one verb: *run again*. Everything else about its standing
had to be inferred from the words in the corner — `clean · 3m`, `changed since`
— and those words mixed two different questions together. "Clean" is what it
found. "Changed since" is whether what it found is still about the code in front
of you. A card could not say both, so it said whichever was worse, and a
reviewer reading `clean · 3m` had no way to know whether that described the diff
on screen or one from before lunch.

Worse, the staleness check was broken in the one mode it mattered most. An
audit's print was `Fingerprint(units)` — unit ids, file names, and the two
snapshot refs. A live review diffs against the working tree, so the far ref is
empty and never moves. An agent could rewrite every file in the review and the
security card would go on calling itself current.

And the result was stored per `(target, kind)` only. Audit a review, walk away,
come back to it unchanged: the stored print still matched, which was the one
case that worked. Audit it, let the code move, move it back, or switch to
another review and return — and the answer you had already paid for was gone,
replaced by an invitation to buy it again.

## Decision

**A card is in exactly one of five states, and the state is a separate axis
from the result.**

- **absent** — nobody has run this.
- **running** — with a line saying what it is reading and how much of it.
- **fresh** — this describes the change on screen. The card says so, in words.
- **stale** — the code moved; the previous answer is still shown, still
  colourable, and the run button changes from *run again* to *re-run on this
  change*.
- **failed** — it could not run. This outranks a stored result, because a card
  going quiet with an old answer on it is how "could not run" comes to look
  like "found nothing".

`Result` — clean or found — sits beside `State` rather than inside it. Where
they disagree the lifecycle wins: a stale card is grey however alarming its
findings were, because they are about code that is no longer there.

**Fresh says so out loud.** The stale line was already there; the current line
was not, and a card that only ever volunteers the bad news is silent in exactly
the case a reviewer is relying on it.

**An audit fingerprints the diff, not the unit list.** `ChangeFingerprint`
hashes the diff text, so a rewritten line moves it and a live review can go
stale — which is the whole point of the state.

**Results are stored per `(target, kind, diff)`.** The store keeps the result
for each diff it was actually about, and separately the most recent result
whatever diff that was. Reading prefers the exact match and falls back to the
most recent, which is precisely the two behaviours the card needs: coming back
to an unchanged review is free, and coming back to a moved one shows the old
answer under a line saying the code has moved.

The JSONL store keeps the last twelve per audit and prunes the rest; audits only
run when somebody clicks, so twelve is generous. Postgres gains a `print` column
and a three-part primary key.

## Consequences

- No card can be ambiguous about whether it describes the code on screen. That
  was the point.
- Every audit stored before this reads as stale once, because its print was
  computed a different way. That is the honest outcome: we can no longer vouch
  for what those prints meant.
- Live reviews now go stale, often, because they should. A reviewer watching an
  agent work will see `re-run on this change` a lot — which is information, not
  noise, and is the direct consequence of the fingerprint finally working.
- Two axes on one card is more to hold than one. It is worth it: the alternative
  is the state and the result taking turns to be the thing that got said.
- The store grows a file per audited diff, bounded. Nothing reads the old ones
  except the exact-match lookup, which is what makes them worth keeping.
