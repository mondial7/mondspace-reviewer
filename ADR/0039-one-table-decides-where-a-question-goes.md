# 0039 — One table decides where a question goes

- **Status:** accepted
- **Date:** 2026-09-01
- **Supersedes part of:** 0035 (the Claude Code CLI is no longer opt-in)

## Context

ADR 0035 added the Claude Code CLI as a second engine and made it opt-in,
per workload, typed in by hand. The evidence in that record is the reason to
reverse it. On one commit of this repository the local 4B produced three
security findings, the headline one assembled from a filename for a file whose
body it had never been shown, and five breaking-change findings every one of
which was an addition — which the prompt forbids in as many words. The same two
prompts through the CLI came back empty and correct.

A reviewer with Claude Code installed should not have to find a configuration
field to stop being shown invented findings. And "which model reads my security
pass" was not answerable by reading anything: it was `pool.For(domain.Narration)`
in one file, a scheme check in another, and a workload table that only knew about
three coarse buckets.

There was also no fallback anywhere. A workload pointed at a CLI that was not
installed failed every call and the review degraded to mechanical — which ADR
0035 argued for deliberately, on the grounds that being silently served by a
different engine is worse than being told. That argument is right about
*silently*. It is not an argument against falling back and saying so.

## Decision

**`internal/domain/routing.go` is the table, and it is the only place routing
is decided.** Six rows, one per job, each naming the job, the workload that
answers it, the engine it goes to, what stands behind that engine, and why it
was put there. The shape is one sentence: judgement goes to the CLI, volume
stays local.

| job | engine | fallback |
| --- | --- | --- |
| story, security pass, breaking changes | claude cli | local model |
| group descriptions, file descriptions, questions | local model | — |

The volume jobs have no fallback on purpose. A paid call per changed file is a
bill nobody asked for, and it is the highest-volume thing msr does.

**The CLI is used when it is installed.** One `exec.LookPath` at start-up.
`MSR_CLAUDE_CLI=0` turns it off entirely, and a per-workload override still wins
over the table — the table is a default, not a policy.

**Nothing blocks on an engine.** `routed.Summarizer` puts one engine in front of
another: primary, then standby, then the failure. A cancelled call is never
retried on the other engine — that is a second call nobody is waiting for and,
on a paid engine, a second bill. Headlines go straight to the standby, which is
not a failure path: the CLI declines them on purpose (ADR 0035).

**A fallback result says so, on the card.** `Analysis` and `Narrative` carry the
engine that answered and whether it was the fallback, and the card prints "read
by the local model — the engine this is routed to was not available". This is
the answer to 0035's objection: the problem was being served silently, not being
served.

**The settings page reports each engine separately** — present or not, answering
or not, what the table sends to it, and what it has spent. Two engines summed
into one green light is how a review running entirely on the fallback comes to
look identical to one running as intended.

## Consequences

- **This spends money that the previous default did not.** A reviewer with
  Claude Code installed and no idea msr existed a minute ago now has their
  stories and audits going through their subscription. That is the brief's call,
  and it is mitigated three ways: one environment variable turns it off, the
  settings page shows the per-engine spend, and the high-volume jobs — the ones
  that would actually cost something — never leave the local model.
- msr is still free, local and offline on a machine with no Claude Code, and
  that path is unchanged and untouched by any of this.
- Routing is one file. Adding a fourth reading means adding a row, not finding
  the three places that decide.
- A workload is still the unit a summarizer is built for, so every job on one
  workload must agree on its engine. A test says so. The day two jobs disagree
  is the day the workload has to split first.
- Attribution is read from the summarizer straight after the call rather than
  returned with the answer. Two calls in flight are routed the same way, so the
  answer is the same either way — but it is a fact about the engine, not about
  the call, and a future engine that chose per-question would need the port to
  change.
