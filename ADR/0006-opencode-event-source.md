# 0006 — An OpenCode event source, mapped onto the same domain.Event

- **Status:** accepted
- **Date:** 2026-08-23

## Context

`msr` watched exactly one agent, Claude Code, through `internal/adapter/source/hooks`.
ADR 0001 already made the domain agent-agnostic in principle — `port.EventSource`
just emits `domain.Event`s, and `domain`/`usecase` know nothing about hooks or
tool-call JSON. Issue #1 asks for a second agent, OpenCode, to prove that promise
rather than merely assert it.

There is no live OpenCode install available in this environment to observe a
real event stream from, so the adapter cannot be built against an authoritative
wire format. The choice is between blocking on that access or defining a
tolerant, documented shape now and adjusting field names later if the real
format differs.

## Decision

Add `internal/adapter/source/opencode`, a second `EventSource`, and define the
payload it parses explicitly rather than guessing at OpenCode's actual
internals. It is a JSONL log, one event per line, documented in the package
comment:

```json
{
  "id":        "evt_01",
  "sessionId": "ses_abc123",
  "timestamp": "2026-08-23T10:00:00Z",
  "type":      "tool.edit",
  "tool":      "edit",
  "files":     ["auth/token.go"],
  "reasoning": "extract validation behind an interface",
  "text":      "add token validation ...",
  "command":   "go test ./...",
  "exitCode":  0
}
```

Mapping onto `domain.Kind`:

| OpenCode `type` | `domain.Kind`  | notes                                    |
|------------------|----------------|-------------------------------------------|
| `user.prompt`    | `KindPrompt`   | `StatedIntent` = `text`                    |
| `tool.edit`      | `KindEdit`     | `Files` = `files`; `StatedIntent` = `reasoning` |
| `tool.write`     | `KindWrite`    | `Files` = `files`; `StatedIntent` = `reasoning` |
| `tool.bash`      | `KindBash`     | `Tool` = `command`; `Failed` = `exitCode != 0`  |
| `step.finish`    | `KindBatchEnd` | batch boundary, mirrors Claude Code's `PostToolBatch` |

Any other `type` — a future OpenCode event this adapter does not yet recognise —
is skipped, exactly like a malformed line, rather than treated as fatal.

The adapter mirrors the hooks adapter's robustness guarantees exactly: it
catches up from the start of the log, then follows appends via fsnotify with a
50ms polling fallback, skips malformed JSON, and stops cleanly on context
cancellation without ever blocking the agent.

It is wired into `cmd` next to `replay` and `hooks`: `msr review
--source=opencode ...` works for both `--plain` and `--tui`, sharing the same
git-snapshot and clustering pipeline as `hooks` — only the log format differs.

## Consequences

- The domain stays provably agent-agnostic: `internal/usecase` and
  `internal/domain` did not change at all to support a second agent — only a
  new adapter and a `cmd` wiring branch were added.
- The assumed payload shape is a best-effort, documented guess, not a verified
  contract with a real OpenCode install. If OpenCode's actual event log format
  differs, only `internal/adapter/source/opencode/opencode.go` — the mapping
  function and its doc comment — needs to change; no other package is
  affected.
- Unknown event types degrade gracefully (skipped, not fatal), so the adapter
  does not need to track OpenCode's event vocabulary exhaustively up front.
- Cost: a second adapter to keep in sync with the hooks adapter's robustness
  guarantees (tailing, cancellation, malformed-line handling) whenever those
  are improved — mitigated by both adapters being small and tested the same
  way.
