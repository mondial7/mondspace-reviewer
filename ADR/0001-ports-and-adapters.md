# 0001 — Ports and adapters, dependencies inward only

- **Status:** accepted
- **Date:** 2026-08-22

## Context

The tool watches an agent through hooks, snapshots a git repo, talks to a model
over HTTP, and renders to a terminal. If any of that I/O leaked into the review
logic, the logic would only be testable with an agent, a repo, a network, and a
TTY present — which is exactly the setup we cannot rely on in tests or CI.

## Decision

Ports and adapters. `domain`, `usecase`, and `port` import nothing from
`internal/adapter/...`; adapters implement the ports and are wired in `cmd`.
Clustering, flagging, supersession, WhySrc discipline, ask-context assembly, and
export are pure functions over the log and diffs.

An architecture test (`arch/arch_test.go`) walks the import graph and fails the
build on any adapter import from an inner package. It also proves it can detect a
violation, so it can never pass vacuously.

## Consequences

- The whole app is exercisable via the `replay` source and `plain` presenter with
  no agent, terminal, or network.
- New surfaces (a web UI, another agent, another model backend) are new adapters,
  not rewrites — the review engine is reused verbatim.
- Cost: some indirection, and a discipline to keep enforcing when wiring `cmd`.
