# 0035 — A second engine for the analyses

- **Status:** accepted
- **Date:** 2026-08-30

## Context

msr's audits are the one place a small local model's confidence does real harm.
A story that reads a little flat is a mild disappointment; a security card that
invents a finding is a reviewer sent to look at nothing, and a reviewer who is
sent to look at nothing twice stops reading the card.

Measured on one commit of this repository — the one that added `msr mcp` — with
the 4B instruct model that ships as the default:

- **Security:** three findings, headed by *"New 'mcp' command adds
  unauthenticated access to review store data without input validation or access
  control."* The MCP server body was not in the digest it was shown, and the
  server is read-only over stdio with no network surface at all. The finding was
  assembled from a filename.
- **Breaking changes:** five findings, every one of them an addition — which the
  prompt forbids in as many words: *"A newly added function, type or field
  breaks nobody… Never report an addition."*

The same two prompts, unchanged, through the Claude Code CLI on the same commit:
both empty, with *"Nothing security-relevant in the visible change"* and
*"Everything here is additive"* — and, unprompted, a note that the diff it was
shown had been truncated and it would not report on what it could not see.

## Decision

**The Claude Code CLI is available as a second engine, per workload, and is not
the default.**

msr runs on your machine, offline, on a model you started. That is the product
and it does not change. This is one more adapter behind the same `port.Summarizer`,
for a reviewer who already has a Claude subscription and would rather spend it on
the analyses than read invented findings.

It is chosen where the model is chosen. The endpoint field already answers
"which thing answers this", so a scheme is enough: `claude://cli`, set globally
or against one workload. No second setting to fall out of step with the first.

**It is run with no tools.** The prompt already carries the change. A reviewer's
model that can read the filesystem is a different product with different risks,
and msr's claim is that it only ever reads what it was given.

**The prompt goes on stdin.** An audit prompt carries the diff and runs to
several kilobytes: the wrong size for argv on any platform, and the wrong place
for somebody's source code on all of them.

**It offers no `Headline`.** The per-file headline is the high-volume call, one
per changed file, and sending a hundred of them through a paid session is a bill
nobody asked for. Callers already fall back to the mechanical headline.

**It enforces no schema.** The CLI has no grammar to compile one into, so `ask`
degrades to a plain question and the existing JSON extraction handles the reply
— which is what that path was written for. The adapter normalises one thing the
CLI does: it returns the contents of a fenced block, because the CLI writes
prose around its JSON and often a paragraph after it, and hunting for the first
`{` and the last `}` is one brace in a sentence away from breaking.

**A model name meant for the other engine is not passed on.** The model field is
shared, so it usually holds whatever is loaded in llama-server. Handing
`qwen3-4b-instruct-2507` to the CLI fails the entire call over a name the
reviewer never chose for it, so anything not evidently a Claude model is treated
as "this field is about the other engine" and the CLI uses its own default.

## Consequences

- The analyses can be as good as the reviewer is willing to pay for, and the
  default stays free, local and offline.
- Two engines behind one port, which is what the port was for. Neither knows
  about the other.
- It costs money, per call, on somebody's subscription. It is opt-in, per
  workload, and typed in by hand — nothing turns it on for you.
- msr now depends on a binary it does not ship, when you choose it. A missing
  one is reported as a failure rather than quietly answered by something else:
  the reviewer chose this engine, and being silently served by another is worse
  than being told it is not there.
