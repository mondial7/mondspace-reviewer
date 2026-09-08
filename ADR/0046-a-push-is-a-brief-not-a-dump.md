# 0046 — A push is a brief, not a dump

- **Status:** proposed
- **Date:** 2026-09-08

## Context

The station's output has to reach the agent that will act on it, and the
tempting version of that is a list of findings pasted into a terminal. It does
not work. Twenty findings concatenated in the order a linter happened to emit
them give an agent no order to work in, no way to tell which two are about the
same lines, and no locality — the agent may have nothing of the repository
loaded, and half the entries name a file it has never opened.

Nothing may reach a running agent without a human asking for it. That is the
default and this ADR is about the default; the judge that decides for itself is
a separate feature with its own switch.

## Decision

**A push produces a Brief.** Ordered by file and then by line, because an agent
working down one file at a time re-reads less than one bouncing between four.
Each entry carries the path, the line range, the snippet and the directive, so
it can be acted on without the review being open beside it.

**Overlaps are flagged at assembly, not discovered by the agent.** Two
directives touching the same lines are two instructions that may contradict
each other, and the agent will pick one silently. The human is told before
anything is sent, and can split the batch or drop one of them.

**A push is a batch, and the batch id is what makes it idempotent.** Sending
the same batch twice sends nothing the second time. This is the property that
makes a push safe to retry after a delivery adapter fails halfway.

**Pushed is a state, not a verdict.** The item moves to `pushed` with the
batch, the time and who sent it, and it goes on standing until a later pass
finds the code gone (ADR 0044). An item that was handed to an agent and not
fixed comes back on the next review, which is the only way anybody would ever
find out that the agent quietly ignored it.

**A dismissed item can never be pushed**, and an item already in flight is not
sent again. Those two rules live on the item itself so that every path to a
push — a lens, the CLI, and later the judge — gets them for free.

**Delivery is a port with `file` as the default.** Agents run in other
terminals and every CLI is different, so the thing msr can always do is write
`.mondspace/handoff/<batch>.md` and let the human hand it over. `stdout` for a
pipe and `clipboard` for a paste; a direct adapter for a named agent is one
more implementation of the same interface and is not required for the loop to
close.

**Push works offline.** The `file` adapter has no dependency on anything, which
is what keeps the default path honest on a train.

## Consequences

- The loop closes without any integration with any particular agent: write a
  file, hand it over. Everything after that is convenience.
- A batch is a unit of verification as well as of delivery. "Which of the
  things I sent on Tuesday actually got done" is answerable, and it is
  answerable because of the batch id rather than because anybody remembered.
- Conflict detection is line-range overlap and nothing cleverer. Two directives
  that contradict each other from different files are not caught, and cannot be
  without reading them.
- Items pushed by the judge are recorded identically, with `pushed_by` naming
  it. The loop stays inspectable through one store rather than two.
