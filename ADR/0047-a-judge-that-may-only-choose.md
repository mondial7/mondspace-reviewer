# 0047 — A judge that may only choose

- **Status:** proposed
- **Date:** 2026-09-09

## Context

Auto-mode is the feature where msr feeds findings to a running agent without
anybody asking it to. It is the most useful thing in the station and the most
dangerous, and it is the last thing built on purpose: it is a decision layer
over a push path that has to be trusted by hand first, and building it earlier
would mean debugging the judgement and the handoff at the same time.

The failure it has to be designed against is not "it pushed something silly".
It is the judge quietly becoming a second planner — authoring work nobody
wrote down, in a voice indistinguishable from the reviewer's, with no record of
what it decided not to say.

## Decision

**Off by default, and off is the shipped default.** With auto-mode disabled no
code path reaches an implementation agent without an explicit human command,
and there is a test that says so.

**The judge may only select, order and phrase findings that already exist.**
Everything it returns came out of what it was given; there is no branch through
the decision that constructs an item. An item that arrived with no directive is
held back with that as the reason rather than having one invented for it — the
judge phrases a directive, it does not write one.

**Every decision is logged, and rejections most of all.** `judge.jsonl` gets a
line per finding considered, with the reason, whether it went or not. A log of
what was pushed says nothing about the judgement that produced it; a log of
what was considered says everything. Decisions are written before delivery is
attempted, so a push that fails halfway still leaves its reasoning on disk.

**Bounded in four directions, all configurable, all conservative by default:**
five pushes a session, three items a batch, five minutes between them, and
`high` and above. An agent handed fifteen directives mid-task does none of them
well.

**It pushes at checkpoints, and `msr auto run` is how it is told one has been
reached.** A command rather than a daemon, because the thing that knows an
agent is between tasks is the agent. msr guessing would mean pushing mid-edit.

**`msr auto off` takes effect before the next checkpoint**, because the policy
is read from disk at the start of every decision rather than held in a process.
Nothing can be in flight to escape it.

**Any manual push stands auto-mode down for the rest of the session.** Two
things steering one agent is worse than either of them alone, and the human is
the one who gets to keep going. Turning it back on is the explicit act that
lifts the suspension.

**A judged push is recorded identically to a human one**, with `pushed_by` set
to `judge`. Same store, same batch, same state transition, same verification on
the next pass.

**Three files, not one.** The policy is TOML because a person edits it; the
session state is JSON because msr writes it; the log is append-only JSONL
because it is evidence. Merging them would mean rewriting a human's file to
record a machine's counter. None of them goes in `.msr.toml`, which is the
repository's analyser configuration and is deliberately not a settings file.

## Consequences

- The whole loop is inspectable through the same store: what was found, what
  was decided about it, what was sent, and whether it came back.
- The judge in this commit is a policy rather than a model: it sorts by
  severity and place and applies the bounds. That is a complete implementation
  of the constraints, and a model can be dropped into the same seam later
  without any of them moving — which is the point of writing them down as code
  rather than as a prompt.
- A per-session budget needs a session id to count against. Without one, every
  run looks like a fresh session and the budget is only the cooldown.
- The policy file is the one place a reviewer can make auto-mode dangerous, by
  setting the floor to `low` and the cooldown to zero. That is their call to
  make, and it is one edit in a file they own.
