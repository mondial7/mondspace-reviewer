# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [3.1.0] — 2026-08-23

### Added

- **`msr help`** — every command with a one-line summary, and `msr help <command>`
  for one command's flags. Typing `msr` with no arguments now shows it rather
  than an error: someone typing the bare name is asking what it does. **`msr
  version`** reports the build, stamped at release time.
- **Homebrew**: `brew install mondial7/tap/mondspace-reviewer`, published to
  `mondial7/homebrew-tap` on each release. Publishing needs a token with write
  access to the tap; without it the cask is skipped rather than failing the
  release.
- **The reviewer assistant keeps its conversation.** Exchanges are stored in
  both the JSONL and Postgres stores and reloaded, so a thread can be picked up
  tomorrow. Answers render as markdown, and a question is a mode: the story
  steps aside, the field grows, and the wait is explicit.
- **Groups can be described on demand.** The automatic pass is bounded, so most
  groups in a large session read "not yet described"; each now carries a control
  to ask, and the result is saved with the story.

### Fixed

- **The assistant was showing its own thinking as the answer.** A reasoning
  model that runs out of budget mid-thought leaves `content` empty and a
  monologue in `reasoning_content`, and the fallback added for schema replies
  took it. That fallback now applies only when a schema was in force; otherwise
  an empty answer is reported as one, naming the finish reason and the reasoning
  tokens spent. Questions also get a roomier token budget than headlines.
- **A session-scoped question carried no diffs**, so the assistant correctly
  answered that it could not say what changed. It now receives a bounded digest
  of the actual changes, capped per file, built from the units the page shows
  rather than whatever the store happened to hold.

## [3.0.0] — 2026-08-23

One page to review a session, across as many repositories as you like.

### Added

- **The cockpit** (`/`) — the whole review on one screen (ADR 0015, ADR 0016):
  a fixed panel carrying a model-written title and brief for the session; the
  story as prose; and the changes with diffs, notes, annotation and re-analysis.
  Clicking a chapter brings its files alongside it.
- **An isometric field that means something.** One block per changed file:
  height is lines changed (log-scaled), colour is growth / deletion / flagged,
  depth is recency, and it moves only while the session is live. A still field
  means nothing is happening.
- **A workspace of repositories.** `--repo` is repeatable; with none given `msr`
  opens the checkout it is in, or offers the checkouts one level below it. Past
  five it lists them and asks rather than opening forty. Sessions load on demand
  and each remembers which repository owns it. Repositories can also be opened
  while the app runs, from `/status`.
- **`/status`** — is the reviewer's model online (re-probed every 20s), what it
  has spent split into prompt / completion / *of which reasoning*, the open
  repositories, and every session. **`/activity`** — every model call and every
  change to the review, across the whole workspace.
- **Schema-enforced model output.** Where the endpoint supports it the story is
  requested as `json_schema` with the real area names as an `enum`, so an
  invented area cannot be emitted at all (ADR 0014). Measured on a 32k context:
  unconstrained the model spent all 299 completion tokens reasoning and returned
  nothing in 107s; schema-enforced it returned valid JSON in 54 tokens and 2.2s.
- **Per-file history.** How many times a file was touched and when, opening into
  a per-file log, and a full-diff overlay that steps through that file's git
  history with the arrow keys.
- **Grouped changes.** Files changed together in a directory are shown together,
  with one model-written sentence about what the change is *for*.
- **A shared shell**: a spatial backdrop and one nav rail on every page, **zen
  mode** (`⌘Z`, `Esc` to leave), a **command palette** (`⌘K`) over pages and
  files, and a **theme switch** (`⌘J`) — dark, light, or follow the system.
- **Token accounting.** The adapter records what every call cost; a cap
  (`max_tokens`) is sent on every request, which is LM Studio's own remedy for a
  model stuck inside an unclosed structure.
- A **deep-universe palette**, measured rather than eyeballed: every token clears
  WCAG AA against both the background and the panel.

### Changed

- **`/review` and `/story` are gone**, folded into the cockpit; both permanently
  redirect, so links and bookmarks still work.
- **`msr web` needs no `--session`** — it opens the newest review it can find.
- **Narration runs once per review, not once per launch.** The story is stored
  with a fingerprint of the review it describes and reused while that matches;
  re-opening the page costs nothing. A fallback is stored too, so a failure is
  retried by pressing a button rather than by navigating. While a session is
  still moving the review refreshes every 15s from one `git diff --numstat`
  call, and re-narration is bounded to once every five minutes.
- **Mechanical headlines are no longer shown.** "edited jsonl.go" above a row
  labelled `jsonl.go` said nothing; a per-file headline now appears only when a
  model wrote one. Diffs are folded by default and open on the file name.
- **Zen mode replaces focus mode**, and applies to every page rather than one.

### Fixed

- **A schema-constrained reply arrives in `reasoning_content` with `content`
  empty** — the grammar constrains sampling inside the chat template's thinking
  block. Reading only `content` is what made narration fall back silently.
- **An endpoint that rejects a schema** is retried unconstrained, and if that
  also fails the error names the rejection rather than only the second failure.
- **Listeners bound inside the changes column** were lost whenever a live update
  replaced it, which killed the history overlay, the tree links and the view
  switch within seconds on an active session. All are delegated now.
- **`--out` was both the pattern for finding a store and the resolved path**, so
  opening a repository at runtime silently found nothing.
- **`/dev/null` counted as a terminal**, so a script redirecting from it was
  shown a prompt nobody could answer.
- Postgres remembers a session's story too; it was JSONL-only, so the
  once-per-review rule silently did not apply to the store the web app is meant
  for.

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

[3.1.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v3.1.0
[3.0.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v3.0.0
[2.0.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v2.0.0
[1.0.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v1.0.0
