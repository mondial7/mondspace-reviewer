---
title: mondspace-reviewer
---

**`msr` turns an autonomous coding agent's activity into a review you can
actually read** — the session told as a story, beside the real diffs, live while
the agent is still working. It watches; it never writes to the agent.

> The **review log is the product.** Narration and interrogation exist only to
> help you produce annotations.

<p align="center">
  <img src="img/tui-review.png" alt="msr review queue" width="760">
</p>

## The problem

While a coding agent works in auto mode, you have no cheap way to stay oriented.
Reading the raw stream is high cognitive load; waiting for the final diff means
every piece of feedback arrives too late to be cheap. `msr` sits in the middle:
it reconstructs what actually changed, flags what is worth a second look **with
no model at all**, and lets a model explain the rest — clearly labelled as
inference.

## The cockpit

`msr web` opens one page with three columns: a fixed panel, the story, and the
changes. Only the two right-hand columns scroll, and clicking a chapter brings
its files up beside it.

- **The panel** carries a model-written title and brief for the session, the
  numbers, and an isometric field where **every block is a changed file** —
  height is lines changed, colour is growth or deletion or flagged, depth is
  recency. It moves only while the session is live. A still field means nothing
  is happening.
- **The changes** group files that changed together, because five files added
  under one package is one act of work. Each group gets one sentence about what
  the change is *for* — not a restatement of the filename.
- **`⌘K`** is a palette over every page and every changed file; **`⌘Z`** hides
  the shell entirely; **`⌘J`** switches dark, light, or follow-the-system.

## What makes it trustworthy

- **Net change, not keystrokes.** Review reconstructs the session's *net* diff
  from git — one unit per file, against the commit just before the session — so
  an agent's back-and-forth collapses into a single clear change. Each file
  still opens into its own history, so the collapse is visible, not silent.
- **`stated` vs `inferred`, always.** A rationale in the agent's own words looks
  different — different colour *and* label — from one a model guessed. When in
  doubt it is marked inferred. A model can sharpen a headline's *what*; it can
  never assert a stated reason.
- **Deterministic flags first.** `no-test`, `new-dep`, `swallowed-err`,
  `public-api`, `large`, `todo` — offline, instant, and what make you stop and
  look.
- **Every number is a git fact.** Time open, files, lines, commits, pull
  requests. Nothing on the panel is model-derived, which is the point of putting
  it beside a narration feature.
- **Model output is enforced, not hoped for.** Where the endpoint supports it,
  the story is requested as schema-constrained JSON with the real area names as
  an enum — so an invented area cannot be emitted at all.
- **Every model call is accounted for.** `/activity` shows which model answered,
  how long it took and whether it failed; `/status` shows whether it is online
  right now and what it has spent, down to how many tokens went on *reasoning*.

## Many repositories, one workspace

```sh
msr web --repo=. --repo=../api --repo=../web
```

Each repository keeps its own store; the workspace is the union of every session
found. A session remembers which repository it belongs to, so opening one reads
that repository's git tree — and sessions load on demand, so a large workspace
costs nothing until you look at it.

## Interrogate the log, then export your review

Ask questions answered only from the bounded context — the event log, unit
diffs, the task prompt, and your notes — never a re-read of the repo. Then
export a report: units grouped by note kind, a debt task list, and an open
agenda phrased as the next agent prompt.

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

Runs fully offline. The optional narration talks to any OpenAI-compatible
endpoint — a local [LM Studio](https://lmstudio.ai) server by default — and
degrades gracefully when there is none.

Full documentation, install options, and the command reference are in the
[README on GitHub](https://github.com/mondial7/mondspace-reviewer#readme).

---

<p align="center"><em>Stay oriented while the agent works.</em></p>
