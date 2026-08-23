---
title: mondspace-reviewer
---

**`msr` turns an autonomous coding agent's activity into a reviewable queue of
change units** — each collapsible to one scannable line, expandable on demand,
questionable in natural language, and annotatable in one keystroke. It watches;
it never writes to the agent.

> The **review log is the product.** Narration and interrogation exist only to
> help you produce annotations.

<p align="center">
  <img src="img/tui-review.png" alt="msr review queue" width="760">
</p>

## The problem

While a coding agent works in auto mode, you have no cheap way to stay oriented.
Reading the raw stream is high cognitive load; waiting for the final diff means
every piece of feedback arrives too late to be cheap. `msr` sits in the middle: it
clusters the agent's actions into units of meaning, flags the ones worth a second
look **with no model at all**, and keeps them in an unread queue with a cursor —
designed for being behind. Nothing scrolls away, nothing auto-advances.

## What makes it trustworthy

- **Net change, not keystrokes.** Retroactive review reconstructs the session's
  *net* diff from git — one unit per file, against the commit just before the
  session — so an agent's back-and-forth on a file collapses into a single, clear
  change (`auth/token.go · replace Validate… +9 -3`) with its real diff on expand.
  It reads like `git diff` or a PR, not a log of every touch.
- **Units, not tool calls.** A unit dismissed in one keystroke is cheap; 200
  micro-edits is unusable.
- **`stated` vs `inferred`, always.** A rationale in the agent's own words looks
  different — different colour *and* label — from one a model guessed. When in
  doubt, it's marked inferred. A model can sharpen a headline's *what*, but it can
  never assert a stated reason.
- **Deterministic flags first.** `no-test`, `new-dep`, `swallowed-err`,
  `public-api`, `large`, `todo` — offline, instant, and what make you stop and look.
- **Stable diffs forever.** Every unit records the git snapshots bracketing it, so
  its diff is viewable even after the file has been rewritten.

## Interrogate the log, then export your review

Ask questions answered only from the bounded context — the event log, unit diffs,
the task prompt, and your notes — never a re-read of the repo. Then export a
report: units grouped by note kind, a debt task list, and an open agenda phrased
as the next agent prompt.

<p align="center">
  <img src="img/ask.png" alt="ask the log" width="620">
  <img src="img/export.png" alt="export a review report" width="620">
</p>

## Get started

```sh
go install github.com/mondial7/mondspace-reviewer/cmd/mondspace-reviewer@latest

# Try it with zero setup — no agent, terminal, or network:
mondspace-reviewer review --source=replay \
  --file=testdata/sessions/basic.jsonl --plain
```

Full documentation, install options, and command reference are in the
[README on GitHub](https://github.com/mondial7/mondspace-reviewer#readme).

---

<p align="center"><em>Watch one agent, one session, one repo — excellently.</em></p>
