# 0036 — A probe before the poll

- **Status:** accepted
- **Date:** 2026-09-01

## Context

A review that an agent is still writing into is the case msr was built for, and
it was the case msr handled worst.

The background refresh ran every fifteen seconds, and every fifteen seconds it
did the same amount of work whether or not anything had happened: load the
session, rebuild every unit, diff every file, recompute the numbers. Idle cost
the same as busy.

Fifteen seconds is also too slow to read as live. The reviewer's question is
"has the agent touched the file I am looking at", and an answer that arrives on
a quarter-minute boundary is a page you reload by hand instead.

Worse, it mostly did not arrive at all. `SetSession` broadcasts `units` and
`SetStats` broadcasts `stats`; the page listened for neither. Files changing
under an open review reached the browser only if some *other* event happened to
wake it.

And the one thing it did compare was churn per file. A file whose added and
removed counts land back where they were reads as though nothing happened —
which is the exact shape of an agent rewriting the same function.

## Decision

**Poll every five seconds, and make the tick cost nothing.**

Three gates, cheapest first, and each one is allowed to end the pass:

1. **Nobody is watching.** With no event stream attached, the pass does not run.
   The page closes its stream when the tab is hidden and reopens it on focus,
   and reopening fires `Attention`, which brings the next check forward to now
   rather than to the next tick.
2. **`Snapshotter.Probe` is unchanged.** One `git status` and a stat of each
   path it names, hashed with where HEAD points. This is what runs on an idle
   tick, and it is the whole of what runs.
3. **The content is unchanged.** Only past the probe is it worth rebuilding
   units — and having rebuilt them, the comparison is `ChangeFingerprint`, over
   the diff text itself, so a touched file with identical contents does not
   redraw anybody's page.

`--no-optional-locks` on that `git status` is not decoration. Without it git
rewrites the index to refresh its stat cache, the index stamp moves on every
probe, and the probe answers "something changed" forever. It also keeps a check
running every five seconds from ever taking the index lock away from the agent
working in the same repository.

**The index mtime is in the probe; it is not what the probe rests on.** The
brief named HEAD, the index and the worktree status. Staging is already visible
in the status output and a commit already moves HEAD, so the index stamp is the
one signal that adds nothing and flaps the most. It stays, cheaply, behind the
lock flag that makes it honest.

**Stats stop shouting.** `SetStats` now wakes open pages only when the numbers
would read differently, with "open for" compared to the minute it is rendered
in. Recomputed every five seconds, the old unconditional broadcast would have
had every page re-fetch itself twelve times a minute for a figure that changes
sixty times less often than that.

## Consequences

- An agent writing a file shows up within five seconds, in the region that
  changed, with the reader's scroll position, open diffs and half-typed note
  intact — `live.js` already swapped regions rather than reloading; it simply
  was not being told.
- An idle review costs one `git status` every five seconds while somebody is
  reading it, and nothing at all while nobody is.
- A rewritten line now moves the review. A pure `touch` does not.
- The exact fingerprint is only as cheap as the rebuild that produces it, and
  that rebuild happens on any repository movement at all — including one that
  turns out not to concern this review. The probe is what keeps that rare.
- Disconnecting the stream on a hidden tab means events sent while it was hidden
  are never delivered. Nothing is lost: reconnecting re-fetches the whole page,
  which is a stronger guarantee than replaying them.
