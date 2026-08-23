# 0003 — The model may never assert a stated rationale

- **Status:** accepted
- **Date:** 2026-08-22

## Context

A headline carries a *why*. That why is either the agent's own words (`stated`)
or a guess (`inferred`). A single confabulated rationale presented as fact
destroys trust in the entire feed — after that, a reviewer has to verify
everything, and the tool is worse than useless.

## Decision

`WhySrc` is decided from the event log alone, never from the model's claim:

- If the unit's events contain a stated intent, it is kept **verbatim** and marked
  `stated`; the model may only sharpen the *what* text.
- Otherwise the model's rationale is marked `inferred`.
- A model that returns `WhySrc: stated` is ignored — the field is overwritten.
- Any summarizer error degrades to the mechanical headline.

`stated` and `inferred` are rendered with a different label word **and** a
different colour, in every view including exports.

## Consequences

- The reviewer can always tell agent testimony from model inference at a glance.
- The discipline lives in one pure function (`usecase.Summarize`), so the HTTP
  adapter stays dumb and the rule is table-testable.
- Cost: the model cannot enrich a why even when it would be right to.
