# mondspace-reviewer (`msr`)

**A terminal review companion for autonomous coding agents.**

While an agent works in auto mode, `msr` turns its raw activity into a
**reviewable queue of change units** — each collapsible to one scannable line,
expandable on demand, questionable in natural language, and annotatable in one
keystroke. It watches; it never writes to the agent.

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

Requires **Go 1.24+**, `git`, and no CGO. The optional headline/interrogation
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

3. Ask questions and export your review:

   ```sh
   msr ask --scope=session --session=<session-id> "did the retry change have a stated reason?"
   msr export --format=md --session=<session-id> > review.md
   ```

## Commands

| Command | Purpose |
|---|---|
| `msr install-hooks --dir=.` | Write the four agent hooks into `.claude/settings.json` (merges, never clobbers). |
| `msr ingest --kind=…` | Append one hook event (reads hook JSON on stdin, always exits 0). |
| `msr review --source=replay\|hooks\|opencode [--plain\|--tui] [--verbose]` | Cluster and present the session. `--verbose` (`-v`) lists each unit's member events and snapshot refs. |
| `msr ask --scope=unit\|session --session=… "question"` | Answer a question from the bounded log context. |
| `msr export --format=md\|json --session=…` | Produce the review report, debt list, and open agenda. |

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

**v1.0.0** — everything below is built, tested, and shippable:

- Real ingestion (`ingest`, `install-hooks`, fsnotify tailing, git snapshots)
- Deterministic flags + interactive bubbletea TUI with annotations and supersession
- LM Studio headlines with `stated`/`inferred` discipline and async fill-in
- Interrogation (`a` / `A`, plus a scriptable `ask`)
- Export to Markdown and JSON
- A second agent event source, `opencode` (ADR 0006), alongside `hooks`

Planned work is tracked in [issues](https://github.com/mondial7/mondspace-reviewer/issues)
(the `solo-iface` flag, live-streaming into the TUI, and more).

## Contributing

This project is built strictly test-first. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

Found a vulnerability? See [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © Marco Mondini
