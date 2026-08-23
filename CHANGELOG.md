# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.0] — 2026-08-23

The web app becomes the primary interface, and review becomes the *net change* of
a session rather than a log of every keystroke.

### Added

- **Web application** (`msr web`) — server-rendered Go templates, hand-written
  BEM CSS, no build step and no client framework (ADR 0004):
  - A **cinematic storyline** of the session rendered with Three.js (vendored
    locally, MIT, works offline), and **focus mode** (`f`) — a first-class plain,
    dense, motionless view. `prefers-reduced-motion` forces it; missing WebGL
    falls back to it. All review content is server-rendered, never canvas-only
    (ADR 0012).
  - Scrollable, colour-classified diffs; click-to-annotate; a **multi-session
    workspace** across repos and agents; a **reviewer-assistant chat** that keeps
    the conversation as context; **per-unit re-analysis** with model attribution;
    and an **audit log** of every interaction.
- **`--since=<ref>` / `--until=<ref>`** — review any commit/branch/tag range with
  no session at all (ADR 0005).
- **OpenCode event source** (`--source=opencode`), alongside Claude Code hooks
  (ADR 0006).
- **PostgreSQL storage**, opt-in via `MSR_POSTGRES_DSN`, always in a dedicated
  schema and never `public` (ADR 0007).
- **`msr gc`** to delete throwaway review refs (ADR 0009); **Slack export**
  (`--format=slack`) as one concise message (ADR 0008).
- New flags: `failed` (a failed tool call, ADR 0010) and `solo-iface` (pure diff
  heuristic, ADR 0011). Prebuilt binaries via goreleaser on tagged releases.
- Architecture decisions are now recorded in [`ADR/`](ADR).

### Changed

- **Retroactive review is the net change per file** against the commit just
  before the session, so back-and-forth edits collapse into one reviewable change
  with a real diff (ADR 0002). Previously every batch boundary produced a unit,
  and retroactive diffs came out empty.
- `no-test` is **session-aware**: the implementation chunk is no longer flagged
  when its test was written in another unit of the same session (TDD).
- The TUI was redesigned for readability (short handles, repo-relative paths,
  net `+/-` stats, diffs on expand) and now quits on `ctrl+c` and never renders
  a blank screen.
- The summarizer defaults to `http://localhost:1234/v1` and supports bearer-token
  auth via `MSR_API_KEY`.
- `install-hooks` writes the binary's **absolute path**, since hooks run under
  `/bin/sh` with no aliases and a bare PATH.
- Requires **Go 1.25+**.

### Security

- Session identifiers are validated before use as directory names (path traversal).
- The web app binds to localhost, holds no credentials, and vendors its one
  frontend dependency rather than loading it from a CDN.

## [1.0.0] — 2026-08-22

First public release. Watching one agent, one session, one repo.

### Added

- **Ingestion** — `msr ingest` (atomic append, always exits 0) and
  `msr install-hooks` (merges into `.claude/settings.json`); a `hooks` event
  source that tails `events.jsonl` with fsnotify and a poll fallback.
- **Clustering** — consecutive events sealed into review units at batch
  boundaries, a 5s inactivity gap, or a 12-event cap. Pure over the event log.
- **Git snapshots** — throwaway commits under `refs/mondspace/review/<session>`
  that never touch `HEAD`, the index, or the working tree, so every unit's diff
  is stable after the file is rewritten.
- **Deterministic flags** — `no-test`, `new-dep`, `swallowed-err`, `public-api`,
  `large`, `todo`. No model, offline, instant.
- **Interactive TUI** — a bubbletea unread-queue with navigation, filtering,
  expand/collapse, five annotation kinds, and file-level supersession.
- **Headlines** — LM Studio (any OpenAI-compatible) summarizer with strict
  `stated`/`inferred` discipline and async fill-in that never blocks the queue;
  graceful offline degradation to mechanical headlines.
- **Interrogation** — `a` / `A` in the TUI and a scriptable `msr ask`, answering
  from a bounded context (log, diffs, prompt, notes) and never re-reading the repo.
- **Export** — `msr export --format=md|json`: review report grouped by note kind,
  a debt task list, an open agenda phrased as the next agent prompt, plus
  superseded and unreviewed sections.
- **Scriptable core** — the `replay` source + `plain` presenter exercise the whole
  app with no agent, terminal, or network.

### Security

- Session identifiers are validated to prevent path traversal outside the store
  root.

[2.0.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v2.0.0
[1.0.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v1.0.0
