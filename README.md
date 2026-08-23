# mondspace-reviewer (`msr`)

**A review companion for autonomous coding agents — in your browser or your terminal.**

While an agent works in auto mode, `msr` turns its raw activity into a
**reviewable storyline of change** — one unit per changed file, each with its
real diff, a concise summary, deterministic flags, and one-click annotation. It
watches; it never writes to the agent.

Run `msr web` for the full experience: a cinematic storyline of the session, with
**focus mode** (`f`) one keypress away when you just want the essence.

> The **review log is the product.** Narration and interrogation exist only to
> help you produce annotations.

<p align="center">
  <img src="docs/img/tui-review.png" alt="msr review queue" width="720">
</p>

[![CI](https://github.com/mondial7/mondspace-reviewer/actions/workflows/ci.yml/badge.svg)](https://github.com/mondial7/mondspace-reviewer/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mondial7/mondspace-reviewer.svg)](https://pkg.go.dev/github.com/mondial7/mondspace-reviewer)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

---

## Why

While a coding agent works, the human has no cheap way to stay oriented. Reading
the raw agent stream is high cognitive load; waiting for the final diff means all
feedback arrives too late to be cheap. `msr` sits in between: it clusters the
agent's actions into units of meaning, flags the ones worth a second look with no
model at all, and keeps them in an **unread queue with a cursor** — designed for
being behind. Nothing scrolls away, nothing auto-advances.

Three ideas do most of the work:

- **Units, not tool calls.** Consecutive edits are clustered into one reviewable
  unit. A unit dismissed in one keystroke is cheap; 200 micro-edits is unusable.
- **`stated` vs `inferred` is load-bearing.** A rationale taken verbatim from the
  agent's own words is shown differently — different colour *and* different label —
  from one a model guessed. A single confabulated rationale presented as fact
  destroys trust in the whole feed, so when in doubt it's marked `inferred`.
- **Deterministic flags first.** `no-test`, `new-dep`, `swallowed-err`, and friends
  are what make you stop and look. They run with no model, offline, instantly.

Expand a unit and you get the **story of that change** — a concise headline, the
`stated`/`inferred` rationale, and the **actual diff** — so you can review and
annotate even when a change is too big for line-by-line reading.

## Screenshots

| Ask the log (`a` / `A`) | Export a review report |
|---|---|
| ![ask](docs/img/ask.png) | ![export](docs/img/export.png) |

Scriptable, no terminal required — the `replay` source + `plain` presenter give
full end-to-end output with no agent, no TUI, and no network:

<p align="center"><img src="docs/img/plain-review.png" alt="plain output" width="620"></p>

## Install

Prebuilt binaries (darwin/linux, amd64/arm64) are attached to every
[GitHub Release](https://github.com/mondial7/mondspace-reviewer/releases) —
download the archive for your platform, extract it, and put
`mondspace-reviewer` on your `PATH`. Each release also ships a
`checksums.txt` to verify the download.

Or install with Go:

```sh
go install github.com/mondial7/mondspace-reviewer/cmd/mondspace-reviewer@latest
```

This installs a binary named `mondspace-reviewer`. Most people alias it:

```sh
alias msr=mondspace-reviewer
```

Or build from source:

```sh
git clone https://github.com/mondial7/mondspace-reviewer
cd mondspace-reviewer
go build -o msr ./cmd/mondspace-reviewer
```

Requires **Go 1.25+**, `git`, and no CGO. The optional headline/interrogation
features talk to any OpenAI-compatible endpoint (defaulting to a local
[LM Studio](https://lmstudio.ai) server) and degrade gracefully when it is absent.

## Quick start

### Try it with zero setup

The whole app is exercisable from a recorded log — no agent, terminal, or network:

```sh
msr review --source=replay --file=testdata/sessions/basic.jsonl --plain
```

### Watch a live Claude Code session

1. Install the hooks into your project (merges into `.claude/settings.json`):

   ```sh
   msr install-hooks --dir=.
   ```

   Hooks run under `/bin/sh` (no shell aliases, bare PATH), so `install-hooks`
   embeds the **absolute path** to the binary — no PATH or alias setup needed.
   Each hook does an atomic append of one JSON line and exits 0 immediately —
   **the agent runs fine whether or not the reviewer is attached**, and attaching
   later replays the whole log. Override the invoked command with
   `--command` if you keep the binary elsewhere.

2. Review the session in the interactive queue:

   ```sh
   msr review --tui --session=<session-id> --repo=.                  # retroactive: net change per file
   msr review --tui --source=hooks --session=<session-id> --repo=.   # live: units stream in as the agent works
   msr review --source=hooks --plain --session=<session-id>          # line-oriented, scriptable
   ```

   `--source=opencode` works the same way, tailing an OpenCode session log
   instead of Claude Code's hooks — `msr review --tui --source=opencode
   --session=<session-id> --repo=.`. The domain never knows which agent it is
   watching; only the adapter changes. See [ADR
   0006](ADR/0006-opencode-event-source.md) for the assumed OpenCode payload
   shape and its mapping onto `domain.Event`.

   **Retroactive review** reconstructs the session's *net* change from git — one
   reviewable unit per file, diffed against the commit just before the session —
   so an agent's back-and-forth on a file collapses into a single, clear change
   (`auth/token.go · replace Validate with a TokenValidator interface  +9 -3`)
   with its real diff on expand. It reads like `git diff` / a PR, not a keystroke log.

   **Live review** starts empty and fills as each unit seals — the cursor stays
   where you are (the agent outruns you by construction).

### Review an arbitrary range, no session required

`--since` reviews the net change from any commit, branch, or tag — no hooks,
no recorded session, and no `--session` flag needed:

```sh
msr review --tui   --since=main --repo=.                 # net change from main to the working tree
msr review --plain --since=v1.2.0 --until=v1.3.0          # net change between two tags
```

It reuses the same per-file net-diff engine as retroactive session review: one
unit per changed file, real diff, deterministic flags. Baseline is `--since`;
the far end is `--until` if given, otherwise the current working tree. With no
`--session`, unit ids are seeded from a synthesized `since-<ref>` handle, so
annotations still have a stable home.

3. Ask questions and export your review:

   ```sh
   msr ask --scope=session --session=<session-id> "did the retry change have a stated reason?"
   msr export --format=md --session=<session-id> > review.md
   msr export --format=slack --session=<session-id>   # one concise message, ready to post
   ```

## Commands

| Command | Purpose |
|---|---|
| `msr install-hooks --dir=.` | Write the four agent hooks into `.claude/settings.json` (merges, never clobbers). |
| `msr ingest --kind=…` | Append one hook event (reads hook JSON on stdin, always exits 0). |
| `msr review --source=replay\|hooks\|opencode [--plain\|--tui] [--verbose]` | Cluster and present the session. `--verbose` (`-v`) lists each unit's member events and snapshot refs. |
| `msr review --since=<ref> [--until=<ref>] [--plain\|--tui] [--repo=.]` | Review the net change from `--since` to `--until` (default: the working tree) — no `--session` required. |
| `msr ask --scope=unit\|session --session=… "question"` | Answer a question from the bounded log context. |
| `msr export --format=md\|json\|slack --session=…` | Produce the review report, debt list, and open agenda — or a concise single Slack message. |
| `msr web --session=… --repo=.` | Serve the review as a localhost web app (scrollable diffs, click-to-annotate). |
| `msr gc [--session=<id>] [--repo=.] [--dry-run]` | Delete throwaway review refs (`refs/mondspace/review/*`) for closed sessions — or one session's ref with `--session`. `--dry-run` prints what would be removed. |

## Flags (deterministic, no model)

Run before any model call, offline and instantly:

| Flag | Fires when |
|---|---|
| `no-test` | a unit touches non-test source with no `*_test.*` file |
| `new-dep` | an `import` / `require` / dependency line is added |
| `swallowed-err` | a returned error is dropped with `_ = call()` |
| `public-api` | an exported declaration is removed or changed |
| `large` | more than 150 lines change in one unit |
| `todo` | a `TODO` / `FIXME` / `XXX` is added |
| `solo-iface` | a new Go interface is declared with no implementing method added in the same diff ([diff-local heuristic](ADR/0011-solo-iface-diff-heuristic.md), can over/under-flag) |
| `failed` | a member tool call failed (live review only — see [ADR 0010](ADR/0010-failed-tool-calls.md)) |

## Keybindings (TUI)

```
j / k     next / prev unit        enter   expand / collapse
g / G     top / bottom            /       filter (flag, file, note kind)
tab       toggle unread-only      a / A   ask (unit / session)
o ? x d n annotate                q       quit
```

Annotations: `o` ok (accept + mark read + advance), `?` question, `x` objection,
`d` debt, `n` note. Annotations anchor to **unit IDs**, never file/line — the
working tree is live, but unit IDs are immutable history. When a later unit
rewrites the same file as an annotated one, the earlier note is surfaced as
**superseded**, never silently resolved.

## How it works

Ports and adapters; dependencies point inward only. The `domain`, `usecase`, and
`port` packages import nothing from `internal/adapter/...` — [enforced by a test](arch/arch_test.go).

```
internal/
  domain/    Event, Unit, Note, Session, Headline, Flag — types + invariants, zero I/O
  usecase/   Cluster, Flag, Summarize, Ask, Supersede, Export — pure functions over the log
  port/      EventSource, Snapshotter, Summarizer, Store, Presenter
  adapter/
    source/hooks     tails events.jsonl from Claude Code (fsnotify + poll fallback)
    source/opencode  tails an OpenCode session log — same tailing, different payload shape
    source/replay    replays a recorded log — the test source
    snapshot/git   throwaway snapshot commits; never touches HEAD/index/worktree
    summarizer/openai  LM Studio / any OpenAI-compatible endpoint
    summarizer/null    passthrough, used offline
    store/jsonl        append-only events/units/notes JSONL
    presenter/tui      bubbletea queue
    presenter/plain    line-oriented, scriptable output
cmd/mondspace-reviewer/
```

All state lives under `.mondspace-reviewer/<session-id>/` as three append-only
JSONL files (`events`, `units`, `notes`) — crash-safe, tail-able, and inspectable
with `jq`. Snapshots are throwaway commits under `refs/mondspace/review/<session>`,
so every unit's diff stays stable even after the file is rewritten.

## Web app

```sh
msr web --session=<session-id> --repo=.        # http://127.0.0.1:7777
```

A localhost web application (ADR 0004) served by the same binary: the same
net-change-per-file review, with scrollable diffs and click-to-annotate. It is
server-rendered Go templates with hand-written BEM CSS — no build step and no
client framework. It binds to localhost only and holds no credentials.

Annotations persist through the same store as the CLI. By default that is the
append-only JSONL log; set `MSR_POSTGRES_DSN` to use PostgreSQL instead, which
creates its tables in a dedicated schema (`--pg-schema`, default
`mondspace_reviewer`) and never in `public` (ADR 0007).

```sh
export MSR_POSTGRES_DSN='postgres://user:pass@localhost:5432/db?sslmode=disable'
msr web --session=<session-id> --repo=. --pg-schema=mondspace_reviewer
```

The web app is becoming the primary interface; the TUI remains supported.

## The story view

```sh
msr web --session=<session-id> --repo=.   # then open /story, or "read as a story →"
```

`/story` is the session as a **long-form, chaptered read** — a parallax landing
page rather than a table: a hero, then chapters of related work, each with prose
explaining what changed and why, and the real files, stats and flags beside it.
Press **`f`** for focus mode to read the same story plain and dense.

Grouping is deterministic (by area) and always works offline; a model regroups
and writes the prose when one is reachable. Nothing the model says is taken on
trust: chapter prose is labelled *inferred*, invented file or area names are
dropped, and anything it forgets is appended, so the story can neither lose nor
fabricate a change (ADR 0013). If the model is unavailable the page still reads,
mechanically, and says so.

The page is served immediately and the story is upgraded in the background, one
chapter at a time — it never waits on a model.

## The cockpit

```sh
msr web                                   # newest review in this repo
msr web --repo=../api --repo-also=../web  # one workspace, several repositories
msr web --session=<id>                    # a particular review
```

`msr web` **opens on the cockpit** — while an agent is still working the first
question is "is anything still happening", not "what shall I review first". The
review queue is one click away at `/review`, and every deep link from the
cockpit lands there. One desktop screen, three panes, and only the feed scrolls:

- an **isometric grid** — one block per changed file — that breathes while the
  session is live and settles when it goes quiet;
- the session **in numbers**: time open, files, lines, commits, pull requests;
- a **newest-first feed** of every change: one line of description and its diff.

Every number there comes from git or the event log. **Nothing on the cockpit is
model-derived** — that is the point of putting it beside a narration feature.

Long diffs are *compacted*, never silently truncated: hunk headers all survive
so the shape of the change does, git's per-file plumbing is dropped (the feed
already names the file), and the elision says how many lines it left out
(`… 37 more lines`).

**Pull requests, honestly.** `msr` talks to no forge. PRs are counted by matching
commit subjects against GitHub's merge-commit and squash-merge shapes, counting
distinct references. That means it counts pull requests that **landed** — an open
PR is not a commit and cannot be seen, and a forge with another subject
convention will report zero. See [ADR 0015](ADR/0015-cockpit-view.md).

## Where the model calls go

`msr web` shows every model call at **`/activity`**: what was asked, which model
served it, how long it took, and whether it failed. Narration, the one call a
reviewer never triggers, is recorded there too — otherwise it is invisible.

Narration is also the most expensive thing the app does, so it runs **once per
review**. The story is stored beside the session with a fingerprint of the
review it describes (file names and their snapshot commits, order-independent),
and reused while that matches. Re-opening the page, navigating away and back, or
restarting `msr web` costs nothing.

If narration falls back — the endpoint was down, the model rambled — the
fallback is stored too, so it is not silently retried by navigation. The story
page offers an explicit **"ask the model to narrate this session"** button
instead. It runs at most one narration at a time, so two tabs or an impatient
double click cannot start two.

## Summarizer configuration

Headlines and interrogation use any OpenAI-compatible chat endpoint, defaulting
to a local LM Studio server at `http://localhost:1234/v1`. The model turns each
unit into a one-line storyline and infers the *why*; expanding a unit still shows
the real diff regardless.

```sh
msr review --tui --session=<id> \
  --summarizer-url=http://localhost:1234/v1 \
  --model=qwen/qwen3.5-9b
```

For an endpoint that requires authentication (e.g. an LM Studio server with a
token), set a bearer token via the environment — it is never written to disk:

```sh
export MSR_API_KEY=sk-…
```

### Structured output

When the endpoint supports it, the story is requested as **schema-enforced JSON**
(`response_format: json_schema`). LM Studio compiles the schema into a llama.cpp
grammar (GGUF) or Outlines (MLX), so the reply is valid JSON by construction, and
the list of allowed area names is an `enum` — a model *cannot* name an area that
does not exist. An endpoint that rejects the schema is retried without it, so
this can never break a working setup.

The effect on a reasoning model is not subtle. Same prompt, same 32k context,
`qwen/qwen3.5-9b`:

| request | completion tokens | finish | time |
| --- | --- | --- | --- |
| plain | 299 (all reasoning) | `length` — truncated, no answer | 107s |
| schema-enforced | 54 | `stop` — complete JSON | **2.2s** |

Left to itself the model reasons until it runs out of budget and returns
nothing. The grammar simply does not let it.

### Reasoning models

A reasoning model spends its budget thinking before it emits any output, which
is what makes narration fail on a modest context window. Two things help, and
one thing that sounds like it should does not:

```sh
lms load qwen/qwen3.5-9b -c 32768 --parallel 1 --ttl 3600   # room to think
```

Loading at 32k instead of 4k costs ~1.4 GiB and is what makes the story view
work at all. Structured output (above) is the other half.

```sh
export MSR_NO_THINKING=1          # send chat_template_kwargs.enable_thinking=false
```

`MSR_NO_THINKING` is opt-in and **had no measurable effect on qwen/qwen3.5-9b
under LM Studio** — its chat template ignores the flag, and reasoning tokens
were unchanged. It is kept because other templates do honour it; do not count
on it without measuring your own model.

One quirk worth knowing: a schema-constrained reply from LM Studio arrives in
`reasoning_content` with `content` empty, because the grammar constrains
sampling inside the template's thinking block. `msr` reads it either way — a
reply filed as reasoning is still a reply.

If the endpoint is unreachable, `msr` silently falls back to mechanical headlines
(files + change counts) and an offline notice for questions — **the queue never
waits on the model.** The model can improve a headline's *what*, but it can never
assert a `stated` rationale: that discipline lives in a [pure function](internal/usecase/summarize.go).

## Testing

```sh
go test ./...              # unit + integration, no network, no agent
go test -race ./...
go vet ./...
```

One contract test talks to a real LM Studio server and is gated behind a tag:

```sh
MSR_SUMMARIZER_URL=http://localhost:1234/v1 MSR_MODEL=qwen/qwen3.5-9b \
  MSR_API_KEY=sk-… \
  go test -tags=integration ./internal/adapter/summarizer/openai/...
```

## Status

**v2.0.0** — everything below is built, tested, and shippable:

- **Web app** (`msr web`) — the primary interface: a cinematic storyline of the
  session (Three.js, vendored, offline) with **focus mode** (`f`) for dense,
  motionless review; scrollable diffs, click-to-annotate, a multi-session
  workspace, a persistent reviewer-assistant chat, per-unit re-analysis with
  model attribution, live updates over SSE, and an **activity page**
  (`/activity`) showing every model call with the model that served it and what
  it cost.
- **Net-change review** — one unit per changed file against the pre-session git
  baseline, so an agent's back-and-forth collapses into one clear change
  (ADR 0002). Also available for any range via `--since` / `--until`.
- Real ingestion (`ingest`, `install-hooks`, fsnotify tailing, git snapshots),
  from Claude Code `hooks` or `opencode`.
- Deterministic flags (`no-test`, `new-dep`, `swallowed-err`, `public-api`,
  `large`, `todo`, `failed`, `solo-iface`), supersession, and TDD-aware `no-test`.
- LM Studio headlines with `stated`/`inferred` discipline and async fill-in.
- Interrogation (`a` / `A`, the web chat, and a scriptable `ask`).
- Export to Markdown, JSON, and a single Slack message; `msr gc` for review refs.
- Storage: append-only JSONL by default, or PostgreSQL in a dedicated schema.
- The bubbletea TUI remains supported (ADR 0004 plans its eventual deprecation).

Decisions are recorded in [`ADR/`](ADR); planned work in
[issues](https://github.com/mondial7/mondspace-reviewer/issues).

## Contributing

This project is built strictly test-first. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

Found a vulnerability? See [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © Marco Mondini
