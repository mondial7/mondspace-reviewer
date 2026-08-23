# 0004 — A localhost web app becomes the primary interface; the TUI is kept, then deprecated

- **Status:** accepted
- **Date:** 2026-08-23

## Context

The TUI reached real ceilings for the core use case — reading a session's changes
and annotating them:

- No scrolling: a long diff truncates rather than scrolls.
- No side-by-side diff, no syntax highlighting.
- Terminal glyph and colour handling varies by emulator.
- Rendering is only testable through string matching on a frame.

The interface is an adapter (see ADR 0001), so the review engine does not care
what renders it.

## Decision

Build `internal/adapter/presenter/web`: a localhost web application served by the
same binary (`msr web`), reusing `usecase` verbatim.

Stack, deliberately minimal and dependency-free:

- **Go** `net/http` + `html/template`, no web framework.
- **htmx** for interaction (expand, annotate, ask) — no SPA, no build step.
- **CSS with BEM** naming, hand-written, no framework.
- **Vanilla JavaScript** only where htmx is not enough.

No external runtime libraries are vendored beyond htmx itself, which is served
locally so the app works offline.

The TUI stays working and tested while the web app matures, and is deprecated
once the web app covers its use cases. Nothing is removed in this change.

## Consequences

- Scrollable, syntax-highlighted, side-by-side diffs become possible; multi-session
  and cross-repo views (issue #8) have a home.
- Handlers are testable with `httptest` against real HTTP, which is stronger than
  frame-string assertions.
- The server binds to localhost only and holds no credentials; it is a local tool,
  not a service.
- Cost: two interfaces to maintain during the transition, and a second place where
  review semantics could drift — mitigated by both calling the same `usecase`.
