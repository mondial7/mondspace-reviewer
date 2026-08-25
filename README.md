# mondspace-reviewer (`msr`)

**Review what your coding agent actually did — in your browser, while it works.**

`msr` reads a repository's git history and turns any part of it into a review you
can actually read: the change told as a story, beside the real diffs, with a
local model explaining what each piece is *for*. It watches; it never writes to
your code or your agent.

[![CI](https://github.com/mondial7/mondspace-reviewer/actions/workflows/ci.yml/badge.svg)](https://github.com/mondial7/mondspace-reviewer/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mondial7/mondspace-reviewer.svg)](https://pkg.go.dev/github.com/mondial7/mondspace-reviewer)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

---

## Install and run

```sh
brew install mondial7/tap/mondspace-reviewer   # macOS
# or: go install github.com/mondial7/mondspace-reviewer/cmd/mondspace-reviewer@latest

cd ~/your-project
msr web
```

<details>
<summary><strong>macOS: "Apple could not verify mondspace-reviewer…"</strong></summary>

`msr` is **not signed with an Apple Developer ID**, so anything you download —
a release tarball, or the binary inside the Homebrew cask — arrives carrying a
quarantine attribute, and macOS refuses to run it. It offers you only *Move to
Bin*, which is unhelpfully final.

Installing with `brew` handles this: the cask strips the attribute for you.

If you downloaded a tarball by hand, do the same thing yourself:

```sh
xattr -dr com.apple.quarantine ./mondspace-reviewer
```

Be clear about what that does: it **skips Gatekeeper's check for that file**.
Only do it for a binary you got from
[this project's releases](https://github.com/mondial7/mondspace-reviewer/releases)
and whose checksum matches the `checksums.txt` published alongside it:

```sh
shasum -a 256 -c checksums.txt --ignore-missing
```

The proper fix is a Developer ID signature and Apple notarisation, which needs a
paid Apple developer account. It is not done yet — see
[issue tracker](https://github.com/mondial7/mondspace-reviewer/issues).

</details>

That's it. It opens `http://127.0.0.1:7777` on the newest thing worth reviewing.
No configuration, no database, no account, and **no session to record first** —
it reads the git history that is already there.

Everything works offline. The prose is optional: point `msr` at any
OpenAI-compatible endpoint — a local [LM Studio](https://lmstudio.ai) server by
default — and it degrades to a mechanical grouping when there is none.

## Why you would want it

Reviewing an agent's work is a specific kind of miserable. The diff is large, it
arrives all at once, and the *why* is buried in a transcript you no longer want
to read. `msr` attacks exactly that:

- **Net change, not keystrokes.** One reviewable unit per changed file, so an
  agent editing the same file eleven times reads as one change with one diff —
  and you can still open the eleven if you want them.
- **Meaning, not filenames.** Files that changed together are grouped, and each
  group gets one sentence about what the change is *for*. "edited jsonl.go" is
  never shown: the filename above it already said that.
- **`stated` vs `inferred`, always.** A rationale in the agent's own words looks
  different — different colour *and* label — from one a model guessed. A model
  can sharpen a headline; it can never assert a reason nobody gave.
- **Flags with no model at all.** `no-test`, `new-dep`, `swallowed-err`,
  `public-api`, `large`, `todo` — offline, instant, and what make you stop.
- **Every number is a git fact.** Files, lines, commits, tags, pull requests.
  Nothing on the panel is model-derived, which is the point of putting it beside
  a narration feature.

## What you can review

Anything in git, not just recorded agent runs:

| | |
| --- | --- |
| **a commit** | `parent..commit` — what that one commit did |
| **a tag** | everything since the previous tag — "what shipped in v4.1.0" |
| **a pull request** | the commits that reference it, together |
| **your working tree** | uncommitted work, offered first when it is dirty |
| **an agent session** | a recorded run, if you installed the hooks |

Pick from the selector at the top left — repository first, then what in it. Or
**compare two points** you choose: any tag, branch or commit against any other,
reviewed exactly like anything else. A recorded session is *one kind among
them*, not the index — and every other target lists the sessions that overlap
it, so the intent behind a commit is one click away ([ADR 0017](ADR/0017-git-first-review.md)).

## Using the cockpit

One page, three columns. Only the two right-hand ones scroll.

| | |
| --- | --- |
| **panel** (fixed) | what this change is, in a sentence; a live isometric field; the numbers |
| **story** | the change as chapters of prose, and the reviewer assistant |
| **changes** | every file, folded, with its diff, history and annotation |

**Read.** Start with what happened in a folder, then ask what happened to one
file in it — both are a click, and both are one sentence about what the change
is *for*. Click a chapter and its files come up beside it. Click a filename to
open its diff; long diffs are compacted with the hunk headers kept, and
`open full history` steps through that file's git history with `←`/`→`.

**Annotate.** Every change takes a note and one of `ok` · `question` ·
`objection` · `debt` · `note`. **The review log is the product** — the prose and
the assistant exist only to help you produce it. Export it when you are done.

**Ask.** The assistant answers only from this review: the diffs, the log, and
your own notes. Never a re-read of the repo, never the open internet.

**Keys.** `⌘K` a palette over every page and every changed file · `⌘Z` zen mode,
which hides the shell · `⌘J` dark, light, or follow the system.

Every model call is slow on a local model, so the waiting is visible: a spinner
on the rail from any page, and `/status` showing what is running, what it cost,
and a button to run it again.

### The other pages

- **`/activity`** — every model call and every change to the review, across the
  whole workspace.
- **`/status`** — is your model online, what it has spent (split into prompt,
  completion and *of which reasoning*), which repositories are open, and one
  click to watch another.

## Several repositories at once

```sh
msr web --repo=. --repo=../api --repo=../web
```

With no `--repo`, `msr` opens the checkout you are in — or, if you are in a
directory *of* checkouts, the first few of them. The rest appear on `/status`
under *found nearby*, one click from being watched. Nothing is asked at launch:
choosing belongs where it can change without a restart.

Each repository keeps its own store under `<repo>/.mondspace-reviewer`, and a
review remembers which repository it belongs to.

## Watching an agent live

Reviewing git needs nothing. To also capture an agent's **stated intent** — its
own words about why it did something — install the hooks:

```sh
msr install-hooks --dir=.
```

Four hooks, each an atomic append of one JSON line that exits immediately: your
agent runs fine whether or not `msr` is attached, and attaching later replays the
whole log. `--source=opencode` tails an OpenCode log instead.

That is the only thing sessions add — and it is why they are worth having, not
why they should be the index.

## The command line

The web app is the product. The CLI is there for scripting and for looking at a
review without a browser.

```sh
msr help                      # every command
msr help <command>            # one command's flags

msr export --format=md --session=<id>       # write the review up
msr export --format=slack --session=<id>    # one message, ready to post
msr ask --session=<id> "did the retry change have a stated reason?"
msr review --plain --since=v4.0.0           # line-oriented, scriptable
msr gc --dry-run                            # tidy throwaway review refs
```

Try the whole pipeline with no agent, terminal or network:

```sh
msr review --source=replay --file=testdata/sessions/basic.jsonl --plain
```

<details>
<summary><strong>The terminal UI (unmaintained)</strong></summary>

`msr review --tui` still opens the original bubbletea queue: `j`/`k` to move,
`enter` to expand, `o`/`q`/`x`/`d`/`n` to annotate, `a`/`A` to ask, `f` to filter
flagged, `e` to export.

It works, and it is **no longer developed**. Everything since v3 — the cockpit,
git-first review, workspaces, grouped changes, the assistant — is web only, and
the TUI will not catch up. Use `msr web`; use `--plain` when you want text.

</details>

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

**v5.1.0** — the web app is the product.

- **Cockpit** (`msr web`) — one page: the change as a story, the diffs,
  annotation, re-analysis, a live isometric field, and a workspace spanning any
  number of repositories.
- **Git-first review** — commits, tags, pull requests, the working tree, and
  recorded sessions, all reviewed by the same net-change-per-file engine
  ([ADR 0017](ADR/0017-git-first-review.md)).
- **Schema-enforced model output**, on-demand descriptions, persisted
  conversations, and full accounting of every call and token at `/activity` and
  `/status`.
- **Deterministic flags** and the `stated`/`inferred` discipline, both offline.
- **Storage**: append-only JSONL by default, or PostgreSQL in a dedicated schema.
- **Ingestion** from Claude Code hooks or OpenCode, for stated intent.
- **The TUI is unmaintained.** It still works; it will not gain anything new.

Decisions are recorded in [`ADR/`](ADR); planned work in
[issues](https://github.com/mondial7/mondspace-reviewer/issues).

## Contributing

This project is built strictly test-first. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

Found a vulnerability? See [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © Marco Mondini
