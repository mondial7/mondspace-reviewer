# 0024 — Analyses as independent cards

- **Status:** accepted
- **Date:** 2026-08-28
- **Builds on:** [ADR 0013](0013-three-stage-narrative-degradation.md), [ADR 0014](0014-schema-enforced-model-output.md)

## Context

msr had exactly one thing to say about a change: its story, told as chapters.
That is the right first question — *what even is this* — and it was the only
question the tool could ask.

A reviewer looking at a diff asks others. Is there anything here I should worry
about. Will this break someone. Those are different readings of the same code,
and folding them into the narration would have been the wrong shape twice over:
the story would grow a "and also, security-wise…" paragraph nobody asked for,
and a reviewer who only wanted to know whether an exported signature moved would
pay for prose about folder structure.

## Decision

**An analysis is a card.** The story becomes one of them, and there are two
more: a security pass and a breaking-change check. Each runs only when the
reviewer asks, and each is a separate reading of the same diff.

### They share nothing

An audit's prompt is built from the review and nothing else. No audit is ever
shown another's verdict or findings, and running one never triggers another.

This is enforced by the shape of the code — `Audit.ask` takes the units and
diffs, and has no way to reach a stored result — and pinned by a test that runs
the security audit, then the breaking audit, and fails if anything the first
said appears in what the second was asked.

The reason is not tidiness. A model given three questions at once answers the
first well and the rest as an afterthought, and a model shown a previous
finding anchors on it. Two independent readings of a diff are worth more than
one reading twice.

Results are stored one file per kind for the same reason: two audits can be in
flight at once, and a shared file would mean read-modify-write races between
results that are supposed to be independent.

### Concise by construction

The caps live in the schema, not in a politely-worded prompt: at most five
findings, and lengths that the card can actually hold. A grammar that cannot
emit a sixth finding is more reliable than an instruction not to.

Each finding is a file and one sentence. **There is no severity.** A 4B model
assigning "critical" or "medium" is false precision dressed as a verdict, and
this project already draws that line carefully between what was stated and what
was guessed (ADR 0003). The card says `inferred — worth a look, not a verdict`,
and the reviewer decides the weight.

The prompts are written to make *silence* easy: they say what is worth
reporting, what is not, that only what is visible in the diff counts, and that
finding nothing is a good answer. Measured against a change containing a
hardcoded secret, an `AUTH_DISABLED` bypass and a `fmt.Sprintf` SQL query, the
4B found all three and invented nothing; against a word-wrap helper and a README
it correctly said there was nothing.

### The states are the design

A card is `idle`, `running`, `failed`, `clean`, `found` or `stale`. Those six
exist so that **"nobody has run this", "it could not run" and "it ran and found
nothing" can never look alike** — which on a security card is the difference
between information and a false sense of safety. A failure shows its reason on
the card and is recorded in the activity log on every path it can fail.

`stale` carries the same discipline as a sign-off (ADR 0021): an audit
fingerprints what it read, and a reading of a version that no longer exists says
so rather than presenting itself as current.

## Consequences

- Nothing runs by itself. A model call is slow and costs something, and three
  of them firing on every page load would be worse than the feature is good.
- Adding a fourth reading is one entry in `usecase.Audits()` — a kind, a title,
  a purpose and a prompt. The card, the route, the storage and the states all
  follow from that.
- Audits use the model configured for narration (ADR 0019), because they are
  judgements about a whole change rather than the short per-file work.
- Building this surfaced a bug in the actions that already existed: an action
  naming a target that could not be resolved fell back to whatever was open,
  so an audit could silently land on a different review. Rendering may fall
  back — a stale link should not be a dead end — but acting must refuse.
