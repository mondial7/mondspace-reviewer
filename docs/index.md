---
title: mondspace-reviewer
---

**Review what your coding agent actually did — in your browser, while it works.**

`msr` reads a repository's git history and turns any part of it into a review you
can actually read: the change told as a story, beside the real diffs, with a
local model explaining what each piece is *for*. It watches; it never writes to
your code or your agent.

## Install and run

```sh
brew install mondial7/tap/mondspace-reviewer

cd ~/your-project
msr web
```

It opens `http://127.0.0.1:7777` on the newest thing worth reviewing. No
configuration, no database, no account, and **no session to record first** — it
reads the git history that is already there.

Everything runs offline. The prose is optional: point it at any
OpenAI-compatible endpoint — a local
[llama.cpp](https://github.com/ggml-org/llama.cpp) server by default — and it
falls back to a mechanical grouping when there is none.

## Why

Reviewing an agent's work is a specific kind of miserable. The diff is large, it
arrives all at once, and the *why* is buried in a transcript you no longer want
to read.

- **Net change, not keystrokes.** One reviewable unit per changed file, so an
  agent editing the same file eleven times reads as one change — and you can
  still open the eleven if you want them.
- **Meaning, not filenames.** Files that changed together are grouped, and each
  group gets one sentence about what it is *for*. "edited jsonl.go" is never
  shown: the filename above it already said that.
- **`stated` vs `inferred`, always.** A rationale in the agent's own words looks
  different — different colour *and* label — from one a model guessed. A model
  can sharpen a headline; it can never assert a reason nobody gave.
- **Flags with no model at all.** `no-test`, `new-dep`, `swallowed-err`,
  `public-api`, `large`, `todo` — offline, instant, and what make you stop.
- **Every number is a git fact.** Files, lines, commits, tags, pull requests.
  Nothing on the panel is model-derived, which is the point of putting it beside
  a narration feature.

## What you can review

Anything in git — not just recorded agent runs:

| | |
| --- | --- |
| **a commit** | `parent..commit` — what that one commit did |
| **a tag** | everything since the previous tag |
| **a pull request** | the commits that reference it, together |
| **live** | the working tree against HEAD, updating as you watch — offered first, always |
| **an agent session** | a recorded run, if you installed the hooks |
| **any two points** | compare a tag against a tag, a branch against a commit, anything against your working tree |

A recorded session is *one kind among them*, not the index — and every other
target lists the sessions overlapping it, so the intent behind a commit is one
click away.

## The cockpit

One page, three columns: a fixed panel with what this change is and the numbers;
the change as chapters of prose; and every file, folded, with its diff, its git
history and a place to annotate it.

A card across the top says whether the assistant has read what you are looking
at, how long ago, how far it got, and gives you the button to read it again —
which is the first thing you want after switching to something else.

**Folder, then file.** Files that changed together are grouped, and each group
gets one sentence about what the change is *for*. Ask the same of any single
file in it. That is the reading path: *what happened in this folder* → *what
happened to this one*.

Click a chapter and its files come up beside it. Click a filename for its diff;
`open full history` steps through that file's past with the arrow keys, and a
**tree** toggle swaps the diffs for an indented folder listing when you want the
shape rather than the detail.

**Watch it happen.** msr opens on the **live** target: the working tree against
HEAD. While you read it, it holds still — work the agent does afterwards queues
up in a banner naming the files, and flagging any you have already annotated.
Then you choose: keep reading, include them, or review just the new work. And a
small toast in the corner says when the repository moves — a commit, a tag,
files changing — and clicking it opens what it names.

**Finish it.** Mark a target reviewed, with a closing comment. Reopening says so,
and the picker ticks what is done. `j`/`k` move between files, `[`/`]` between
reviews, `{`/`}` between repositories, and `?` lists every shortcut.

**The review log is the product.** The prose and the assistant exist only to help
you produce it — annotate as `ok`, `question`, `objection`, `debt` or `note`, and
export when you are done.

`⌘K` is a palette over every page and every changed file, `⌘Z` hides the shell,
`⌘J` switches theme.

Model calls are slow locally, so the waiting is visible: a spinner on the rail
from **every** page, and `/status` showing what is running, what it cost, why
anything failed, and a button to run it again. `/activity` keeps the whole
trail — every model call and every change to the review, across the workspace.

## Several repositories at once

```sh
msr web --repo=. --repo=../api --repo=../web
```

Each keeps its own store; a review remembers which repository it belongs to.
Checkouts found nearby are listed in the app, one click from being watched — and
one click from being unwatched again, which closes a window without touching
anything on disk.

Nothing is asked at launch: choosing belongs where it can change without a
restart.

## Watching an agent live

Reviewing git needs nothing. To also capture an agent's **stated intent** — its
own words about why it did something — install the hooks:

```sh
msr install-hooks --dir=.
```

Each hook is an atomic append of one JSON line that exits immediately: your agent
runs fine whether or not `msr` is attached, and attaching later replays the whole
log.

## Also there

A small CLI for scripting — `msr export` to Markdown, JSON or a Slack message,
`msr ask` for one question, `msr review --plain` for line-oriented output. Run
`msr help` for the list.

The original terminal UI (`msr review --tui`) still works and is **no longer
developed**; everything since v3 is web only.

---

Full documentation and the command reference are in the
[README on GitHub](https://github.com/mondial7/mondspace-reviewer#readme).

<p align="center"><em>Stay oriented while the agent works.</em></p>
