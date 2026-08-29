# 0031 — An agent pulls the review

- **Status:** accepted
- **Date:** 2026-08-29

## Context

msr's whole output — what a reviewer questioned, objected to, marked as debt,
and signed off — lives in a store the coding agent that wrote the code cannot
see. The reviewer reads it in a browser and then retypes the relevant parts into
the agent's prompt. That is the artefact msr exists to produce, and it reaches
the thing it is about by hand.

The obvious fix is the wrong one. Pushing review feedback into the agent's
session — a hook, a file it watches, an injected message — interrupts a working
agent with something it did not ask for, at a moment nobody chose, and does it
whether or not there is anything to say.

## Decision

**msr serves the review over MCP, on stdin/stdout, and never speaks first.**

`msr mcp` is a read-only server an agent's client can be pointed at. The agent
asks when it wants to know: before starting on a file, after finishing a change,
when the user says "check the review". Pull rather than push is not a
performance choice — it is the only shape in which the agent's own turn stays
its own.

### Human-written first; inferred behind a name that says so

`review_status`, `review_feedback` and `review_file` serve only what a person
typed. `model_findings` serves what msr's audits inferred, and it is a
separately named call.

The judge is a small local model on the reviewer's machine. It is right often
enough to be worth reading and wrong often enough that acting on it unverified
is a mistake. Without the split, a finding it invented would arrive in an
agent's context indistinguishable from a reviewer's objection, the agent would
implement it, and msr would audit the result with the same model — a loop with
no human anywhere in it. This is ADR 0003's `stated` versus `inferred`
distinction at the one boundary where losing it does real damage.

So the warning travels **in the payload**, not only in the tool description. A
description is read once when the tool list is fetched; the findings are read
every time. Each finding also names the model that produced it and asks, in
words, to be checked.

`review_feedback` carries only what is still outstanding — questions, objections
and debt, minus anything superseded. Approvals and thinking-aloud notes are the
reviewer's record, not the agent's work. `review_file` is deliberately more
generous, because naming a file is a narrowing and someone who has narrowed that
far wants "a human read this and was happy with it" too.

A finding the reviewer **dismissed** is still shown, marked settled. Hiding it
would invite the agent to raise the same thing the reviewer already ruled on.

### Two tiers, because they cost different amounts

`review_*` covers the review the human has open — one directory, read on demand.
`workspace_feedback` and `workspace_search` read every stored review, and their
descriptions open with `EXPENSIVE` so the choice to spend that is made
knowingly. The common question is about the change in front of both of them.

### The store is the channel between the two processes

`msr mcp` is not the web app. It cannot ask a running server anything, so
opening a review in the app writes `open.json` in the store root, and appends to
`reviews.jsonl` so a review opened last week still has a name. With no pointer
at all, the server answers about the review most recently written to: the store
is on disk whether or not the app has ever run, and the likeliest answer beats
an error about a missing file.

### It reads the store and nothing else

No git, no model, no network. That is what makes it safe to leave configured in
an agent's client permanently: the worst it can do is report what a human wrote.
It is also why it stays fast enough to be asked casually.

### Written by hand, not with an SDK

The protocol msr needs is four methods over JSON-RPC 2.0. An SDK would be the
project's sixth direct dependency for that. The handshake echoes the client's
`protocolVersion` rather than asserting one of msr's own — guessing which
revision a client speaks fails the handshake for no reason.

A tool that cannot answer returns its reason as content marked `isError`, not as
a transport error. An agent can read words and work around them; it cannot act
on a JSON-RPC code.

## Consequences

- The review reaches the agent without the reviewer retyping it.
- An agent cannot mistake a small model's guess for a person's objection, unless
  it ignores a warning in every payload that says so.
- msr still never interrupts. Nothing here can push.
- The agent cannot write to the review. Deliberate: a review log an agent can
  edit is not a review of the agent.
- Diffs are not exposed. The agent already has the code.
- The pointer is best-effort. A store that cannot be written to fails the page,
  not the review — and the server falls back to guessing.
