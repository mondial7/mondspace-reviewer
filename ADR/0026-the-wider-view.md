# 0026 — The wider view, and a switch you can reach

- **Status:** accepted
- **Date:** 2026-08-29
- **Builds on:** [ADR 0025](0025-watching-the-remote.md)

## Context

ADR 0025 deliberately scoped remote watching to the upstream of the current
branch: *am I behind, and who moved it*. That is the question you have while
working, and it kept the history card readable in a 20rem panel.

It left the other question unanswered. *What is everyone else working on* is a
real question — before starting something, before reviewing a pull request,
before wondering whether the thing you are about to build already exists on a
branch. And ADR 0025's watch was a start-up flag, which meant turning it on
required stopping msr and starting it again. A setting you can only change by
restarting is not a setting you can put on a status page.

## Decision

### A page, not a card

Branches get `/branches`, not a panel section. The panel column is about twenty
rem wide and a useful branch row carries a name, a subject, an author, a
timestamp and two divergence counts. Squeezing that into the panel would have
produced something nobody reads, and the whole point of a wider view is that it
is wider.

Each branch shows how far it has drifted from the mainline — found on its own
from `origin/HEAD`, falling back to the usual names. **Ahead is the number the
page is for**: it is how much there would be to review. Behind is context.

### A branch row opens a real review

Reviewing a colleague's branch is the range `base..branch`, which msr already
supports as an ordinary comparison target (ADR 0017). So the row links to it,
and what opens is a normal review — narrated, annotated, signed off, audited
like anything else. No new kind of thing, no new storage.

That is the whole reason this was cheap to build, and it is worth stating: the
git-first model from ADR 0017 keeps paying out. A branch is not a special case;
it is two refs.

### Merged branches are kept but quietened

A branch the base already contains has nothing left to review and is usually
waiting to be deleted. Removing it would be lying about what exists; letting it
compete for attention would bury the two branches that matter. So it is dimmed,
labelled, and given no review link.

### The switch moved out of the command line

Whether msr fetches is now a value the watcher reads on every tick rather than a
flag captured at start-up, so `/status` can turn it on and off while it runs.
The interval is shown in the syntax the form accepts, so what is on the page can
be typed straight back into it, and it has a floor: this is a network call
against somebody else's server, and a typo in a form field must not turn msr
into a hammer.

An unchecked checkbox submits nothing at all, so **absent has to mean off**.
Treating it as "leave it as it was" would make the setting impossible to turn
off, which is the failure mode that matters here — the direction people will
want in a hurry is *stop*.

## Consequences

- `/status` now says whether msr is fetching and how often. That was previously
  knowable only from how the process was started, which is the wrong place for
  a fact about what a program is doing to your repository and your network.
- Divergence costs one `git rev-list` per branch, so the list is capped at 40
  and computed when the page is opened rather than on a timer.
- `refs/remotes/origin/HEAD` is a symbolic alias, and git renders its short name
  as the bare remote name — so filtering on a `/HEAD` suffix let a phantom
  branch called "origin" onto the page. Asking whether the ref is symbolic is
  exact; matching its name is not.
