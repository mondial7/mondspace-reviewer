# 0019 — llama-server, and a model per workload

- **Status:** accepted
- **Date:** 2026-08-27
- **Supersedes parts of:** [ADR 0014](0014-schema-enforced-model-output.md)

## Context

msr talked to LM Studio, and two of its behaviours had shaped the client in ways
that turned out to be wrong.

**The answer arrived in the wrong field.** With a schema in force, LM Studio put
the grammar-constrained JSON in `reasoning_content` and left `content` empty —
the grammar was constraining sampling inside the chat template's thinking block.
msr worked around it by reading `reasoning_content` when `content` was empty and
a schema had been sent. That worked, and it was a mistake: it made a model that
*thought instead of answering* indistinguishable from one that answered. The
workaround hid the failure it was working around.

**Thinking could not be turned off.** `chat_template_kwargs: {enable_thinking:
false}` was measured, twice, as having no effect on qwen3.5-9b — reasoning
tokens were identical with and without it (ADR 0014).

Separately, the three workloads are not one problem. Narration — a large schema
whose `groups` items are an enum of real path names — is the demanding call.
Per-file description is short, high-volume and latency-bound. They shared a
model because there was only ever one endpoint to configure.

## Decision

### llama-server is the backend, and the client stops guessing

llama.cpp separates the two channels correctly on its own: the JSON arrives in
`content` and any thinking in `reasoning_content`. The LM Studio behaviour
simply does not occur. So **the fallback goes**: an empty `content` is now a
fault, always, schema or no schema, and the error names the server flag that
would cause it. A client that guesses which field holds the answer cannot tell
success from failure, and this one no longer guesses.

### One model, all three workloads — measured, not assumed

The plan this work started from was a two-server split: a small instruct model
for descriptions and questions, Qwen3.5-9B for narration. Measurement on this
machine (M4 Pro, 24 GB) rejected it.

| | Qwen3-4B-Instruct-2507 Q6_K | Qwen3.5-9B Q4_K_M |
| --- | --- | --- |
| narration, 6 runs at temp 0 | **6/6 valid**, enum members all real | **0/6** — burns the entire 2048-token budget thinking and never answers |
| latency | 6.7–8.9 s | 64–115 s, no answer |
| reasoning tokens | **none — no thinking mode at all** | unstoppable (see below) |

The 4B gets the enum-constrained narration schema right every time, in a tenth
of the latency, with no reasoning phase to suppress. So it answers everything,
and the second server is not started.

Two further findings decided the flags:

- **`--reasoning-format none` breaks grammars.** Combined with a `json_schema`
  request on Qwen3.5-9B it fails outright: `400 Failed to initialize samplers`,
  for any schema, trivial ones included. It is fine on the 4B only because that
  model never emits a reasoning channel — which also makes it pointless there.
  So it is not in the documented command: it can only do harm.
- **`--reasoning-budget 0` does not suppress Qwen3.5-9B's thinking.** It spent
  334 completion tokens producing `{"a": "Hello"}`. This is the same result as
  LM Studio's `enable_thinking: false`: that model's template ignores both
  switches. The conclusion from ADR 0014 stands, and now has a second backend
  confirming it.

The documented command is therefore:

```sh
llama-server -hf bartowski/Qwen_Qwen3-4B-Instruct-2507-GGUF:Q6_K \
  --host 127.0.0.1 --port 8081 -c 32768 -fa on \
  --cache-type-k q8_0 --cache-type-v q8_0 --jinja
```

**Two resident models cost more than they are worth on 24 GB.** With the 9B also
loaded, the 4B's narration went from 6.8 s to 28 s — a fourfold slowdown from
memory pressure alone. A split is viable on this machine only if the second
model earns four times its keep.

### The configuration survives the finding

Per-workload overrides stay, because the finding is about *this* pair of models
on *this* machine, not about the shape of the problem. `AgentConfig` resolves
overrides field by field over the shared settings, so one server is the simple
case and a split is available without being imposed. There is deliberately no
default override: one pointing at a port nobody started would leave exactly one
workload silently falling back to mechanical prose, the failure hardest to
notice from the page.

### The pool

Two things must hold at once and they pull in opposite directions. Each workload
needs a **stable handle**, because everything downstream captures its summarizer
once at wiring time — handing out a new one on reconfigure would strand every
caller on the old model, the exact failure `switchable` exists to prevent,
reintroduced one level up. And each distinct **model** must be built once, not
once per workload, or three workloads on one model would mean three connections,
three liveness probes, and `/status` reporting the same spend three times.

So: a permanent switchable per workload, over adapters shared and deduplicated
by model. `Online` means *every* model answers — one of two being down is the
assistant half-working, and that must not read as green.

## Consequences

- A stored LM Studio endpoint still wins over the new defaults, by design.
  Anyone migrating changes it once on `/status`, and the page names the endpoint
  in use so it is visible rather than silent.
- `Usage` is summed across models: "what has this cost me" was never a
  per-server question.
- Verified end to end: msr narrated a real commit through llama-server into two
  model-written chapters, 0 failures, **0 reasoning tokens**. The same run
  against LM Studio produced the new error verbatim, which is the workaround's
  removal doing its job.

## Addendum, 2026-08-30 — measured

Two things were tested on the machine this is developed on (M-series, 24 GB, one
GPU), after narration started sending diffs (ADR 0034) and so became the
expensive call it was always expected to be.

**A second llama-server with the same model buys nothing.** Narration on its own
server and per-file descriptions on another: 12.7s for a full read, against
12.6s with both on one. The two servers share one GPU, and msr's narration and
descriptions are sequential within a review anyway, so there is no second queue
to fill. It cost 4.4 GB of resident memory to learn that. Split the workloads
when the models differ, not to add a server.

**A thinking model still cannot do the schema-constrained work, and how it is
served decides that.** Qwen3.5-9B under LM Studio returns the grammar-constrained
JSON in `reasoning_content` and leaves `content` empty — 58 of 59 completion
tokens spent reasoning, `finish_reason: stop`, nothing to parse. msr treats an
empty content as a fault rather than digging the answer out of the reasoning
channel, exactly so this is visible, and it falls back to the mechanical
chapters. The same weights under llama-server with `--reasoning-format none`
would answer in `content`; served by LM Studio they do not.

So the recommendation for a single-GPU machine stands as one server and one
non-thinking instruct model, with the per-workload split reserved for the case
it was designed for: a genuinely different model behind one job.
