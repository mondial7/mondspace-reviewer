# 0014 — Enforce model output with a schema, not a parser

- **Status:** accepted
- **Date:** 2026-08-23
- **Amends:** [ADR 0013](0013-narrative-story-view.md)

## Context

ADR 0013 asks a local model for the session story as JSON and then defends
against everything it might do instead: prose around the JSON, a code fence,
invented area names, forgotten units, an empty reply. The defence works — the
story is never wrong — but it is a lot of machinery, and every failure it
catches costs a whole model call and lands the reader on a weaker story.

Two measurements changed what is worth building:

- A reasoning model spends most of a small context **thinking**: an 81-token
  prompt drew 1,400–3,800 reasoning tokens before any output. That, not prompt
  size, is what exhausts a 4k window.
- LM Studio implements OpenAI structured output. The schema is compiled into a
  **llama.cpp grammar** (GGUF) or **Outlines** (MLX) and constrains sampling, so
  the reply cannot be malformed rather than merely being asked not to be.

## Decision

**Ask the server to enforce the shape; keep the parser as a backstop.**

- `port.SchemaAnswerer` is an *optional* capability of a Summarizer. The usecase
  type-asserts for it and works without it — not every endpoint implements
  structured output, and it is unreliable below about 7B parameters.
- Both narration calls carry a schema: the whole-session call and the per-area
  fallback. The per-area call is where it matters most, because it runs
  precisely when the model is short of room, which is when it rambles.
- **Allowed area names are an `enum` of the real areas.** A hallucinated area
  stops being something to detect after the fact and becomes something the
  sampler cannot emit. `reconcileChapters` stays, now as a backstop for
  endpoints that cannot enforce a schema.
- **A rejection is not a failure.** A 4xx means the endpoint refused the request
  as malformed, so the call is retried without the schema. A 5xx surfaces:
  the server broke, and retrying differently would only hide the fault.
- `MSR_NO_THINKING=1` sends `chat_template_kwargs: {"enable_thinking": false}`,
  which LM Studio forwards to the chat template. Measured: reasoning fell from
  1,400–3,800 to 839 tokens, still valid JSON, 31s. Opt-in, because it trades
  prose quality for speed and only some chat templates honour it.

## Consequences

- The common path stops depending on salvaging JSON out of prose, and a story
  degrades to the per-area fallback far less often.
- The three-stage degradation of ADR 0013 is unchanged. This makes stage 1
  succeed more often; it does not remove stages 2 and 3, which still cover an
  unreachable server, a model too small to follow a grammar, and an endpoint
  with no structured-output support.
- Cost: the schema travels with every narration request, and a strict schema
  must list every property as required with `additionalProperties: false`, so
  the contract is now written twice — once as a Go struct, once as a schema.
- Raising the context window remains the bigger lever. Loading the same 9B model
  at 32k instead of 4k costs ~1.4 GiB and is what makes narration work at all;
  structured output makes it cheaper and more reliable, not possible.
