# 0018 — A target that follows HEAD, and pulses that say so

- **Status:** accepted
- **Date:** 2026-08-25
- **Builds on:** [ADR 0017](0017-git-first-review.md), [ADR 0004](0004-web-presenter.md)

## Context

ADR 0017 made a `Target` a named range and git the source of them. That is the
right model for reviewing what *has* happened, and it left two gaps for
reviewing what is happening *now* — which is the case msr exists for, since the
agent is still working while you read.

**Nothing told the reviewer that anything moved.** The cockpit refreshed itself
over SSE when the *open* review changed, but a commit landing, a tag being cut,
or files changing under a different target produced no signal at all. A reviewer
three screens deep in a diff had no way to know the agent had just committed.

**There was no target you could point at "now".** The working tree was offered
only when it was already dirty, and its id was a hash of the range it named —
which included HEAD. So it had two disqualifying properties for live use:

- it did not exist on a clean tree, so you could not *choose* to watch;
- the moment a commit landed, HEAD moved, the id changed, and the reviewer was
  silently on a different review — losing the story and every note attached to
  it, at exactly the moment they were watching most closely.

## Decision

### A live target, identified by repository rather than by range

`TargetLive` is always offered, always first, and always means "the working tree
against whatever HEAD is now".

Its id is a hash of the repository and the kind — **not** the range. This is a
deliberate exception to ADR 0017's rule that a target's identity is derived from
what it names, and the exception is the point: this target names a *moving*
thing, and its whole value is being the same review before and after a commit.
The range is resolved fresh every time it is reviewed (`usecase.ResolveLive`),
never at discovery time.

It follows that the live target must never be served from the loaded-target
cache. Caching is right for a fixed range — rebuilding it produces the same
answer — and wrong here, where a cache would make it the one part of the page
that lies. This applies even when it is the *open* session, which is the common
case since msr now opens on it by default.

### Pulses: SSE, not long polling

A **pulse** is one piece of news about a watched repository — a commit, a tag, a
change to the working tree — in the words it will be read in, carrying the
target that would show it.

The transport is the SSE stream that already exists, extended so an event can
carry a payload. Long polling was considered and rejected: the server is already
polling git on its own schedule, so a long poll would be a second, worse copy of
a mechanism we have — one connection per client per wait, no server-initiated
push, and nothing gained. The honest summary is that the *server* polls (git has
nothing to subscribe to) and the *browser* is pushed to.

Because the polling is server-side and shared, one watcher serves every open
cockpit however many there are. It runs at 2s when at least one page is
listening and 20s when none is, which is the only place the subscriber count
should influence behaviour: a cockpit on a second screen must feel immediate,
and an msr with no browser attached should not spin a git process every two
seconds for an audience of nobody.

Silence is the normal answer and sends nothing. The first observation is never
news, or every page load would greet the reviewer with a report of what was
already there when they arrived.

### Consequences

- A commit or a tag is a target that did not exist when the index was built, so
  the watcher re-runs discovery *before* announcing it. Otherwise the toast is a
  claim rather than a link: the reviewer clicks it and the picker has never
  heard of what it names.
- The target index is therefore written while requests are reading it, and is
  now guarded. It was already written by the compare handler; that it had not
  yet corrupted anything was luck rather than design.
- Notes previously anchored to a `worktree` target do not carry over to the live
  target. This is a real break, and a small one: that target's diff changed
  under it constantly, so it was already the least stable thing to annotate.
- The live target has no fixed size, so its panel says "live" rather than a
  count. A number that changes while you read it is worse than no number.
- msr's own store is excluded from what the watcher sees. It writes inside the
  repository by default, and on exactly the ticks it is watching — so without
  this it announces its own bookkeeping as though the agent had done it.
