# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[1.0.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v1.0.0
