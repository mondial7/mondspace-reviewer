# 0053 — The store forgot what the page knew

- **Status:** proposed
- **Date:** 2026-09-10

## Context

ADR 0043 is unambiguous about pre-existing findings: every repository of any age
has hundreds of them, showing them next to a change turns "3 things to look at"
into "412 things to look at", and that is the same as none. The cockpit has
always honoured it — findings the change caused are shown, the rest are counted
and folded away.

The findings store did not. `msr scan` writes every finding an analyser reports,
`msr findings` listed all of them as work, `msr export` wrote all of them out,
and `msr push --batch` would have handed an agent the lot. Reproduced in four
lines: a package with a `go vet` error in `a.go`, a change that touches only
`b.go`, and a store holding a finding about `a.go`.

It is worse for the agent-facing paths than for the human one. A person reading
a list can tell "this is not mine" at a glance. An agent handed a directive with
a path and a snippet cannot, and will go and change code the work it is doing
never touched.

## Decision

**What the change caused is the default, everywhere the store is read for
action**: `msr findings`, `msr export`, `msr push --batch`, and the judge.

**The flag only means anything for a deterministic analyser.** A note a
reviewer typed and a reading a model produced are about this change by
construction, and neither carries `new`. Filtering on the flag alone would have
hidden every note in the store, which is the opposite of the point.

**Nothing is dropped, and the count is always shown.** `msr scan` says how many
were already there, `msr findings` says it and names the flag that shows them,
and `--standing` lists them. Silently having none and silently hiding four
hundred look identical, and one of them means the tool is broken (ADR 0043,
again).

## Consequences

- On a mature repository with gosec or staticcheck installed, `msr findings` is
  now a list somebody will read rather than a wall.
- A finding that was already there and is *also* on a line this change touched
  is still shown: the flag is about the line, not the file.
- Anything reading the store directly — the planner, `--format=jsonl` — still
  sees everything, and has the flag to sort it out. This is a decision about
  what msr's own surfaces default to, not about what is written down.
- One more thing to remember when adding a surface over the store. The
  alternative was filtering at write time, which throws away the count and with
  it the difference between "clean" and "not looked at".
