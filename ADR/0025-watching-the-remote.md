# 0025 — Watching the remote, on purpose

- **Status:** accepted
- **Date:** 2026-08-29
- **Closes:** [issue #18](https://github.com/mondial7/mondspace-reviewer/issues/18)
- **Builds on:** [ADR 0018](0018-live-target-and-pulses.md)

## Context

msr could tell you everything about the change in front of you and nothing about
where it sat. A reviewer working through a diff has two questions it could not
answer: *where am I against everything that has landed*, and *what has the rest
of the team pushed while I was reading*.

The second one matters more than it sounds. Finding out an hour later that a
colleague landed the thing you were reviewing around is finding out too late.

## Decision

**A history card in the panel**, listing recent commits with three marks that
turn a list into an answer:

- **where you are** — the commit currently open;
- **what you have signed off** (ADR 0021), so the card answers "what is left";
- **what has not left this machine**, and **what has not arrived yet**.

The log walks HEAD *and* the upstream, so a colleague's commit appears at the
top of your history marked `incoming` rather than only as a number saying you
are behind. Every row opens that commit as a review.

Alongside it, the pulse mechanism (ADR 0018) grows one more kind: *somebody
pushed*. It names the count, the branch and who — "3 new commits on origin/main
· Alice" — because "you are 3 behind" and "Alice moved main" answer different
questions and the reviewer wants both in one line. New branches are announced;
deleted ones are not, because branches are deleted constantly after merging and
a toast for each would be noise about work that is finished.

### Fetching is asked for, never assumed

This is the part worth being careful about. msr's promise is that it never
writes to your repository and makes no network call except to the model endpoint
you started yourself. Seeing what a colleague pushed requires `git fetch`, which
breaks both halves: it talks to the network and it writes remote-tracking refs.

So it is opt-in, by a flag that says what it does:

```sh
msr web --fetch --fetch-every=2m
```

Without it the feature still works, and is still honest: it reports whatever the
reviewer's own last `git fetch` or `git pull` brought in. That is stale, and it
is stale in a way the reviewer already understands, because they are the one who
last fetched.

When it is on, the fetch is `--prune --no-write-fetch-head --no-tags`, and a
test asserts what matters: HEAD, the index and the working tree are untouched,
including an uncommitted file. A failed fetch is not reported — it is usually a
sleeping laptop, and the next one catches up.

### What it is not

It is deliberately about the upstream of the current branch, not a picture of
every branch on the server. "Am I behind, and who moved it" is the question a
reviewer has while working; forty branches with their divergence is a different
tool, and putting it in a panel column would make the card unreadable.

## Consequences

- The card shows 30 commits. It is for seeing where you are in a morning's work,
  not a replacement for `git log`.
- The panel now has to share its spare room. The isometric field yields first —
  it is decoration and this is not.
- A commit's sign-off is looked up through the same target index the picker
  uses, so a tick on a row means exactly what a tick in the picker means.
- With no upstream, nothing is marked incoming. A repository with no remote must
  not paint its whole history as somebody else's work.
