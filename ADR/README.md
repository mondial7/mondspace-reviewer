# Architecture Decision Records

Each significant decision lives in its own file: `NNNN-short-title.md`.

**There is deliberately no index.** Records are append-only and self-contained so
parallel branches can add ADRs without ever conflicting on a shared file. To see
what exists, list the directory (`ls ADR/`) or grep it.

## Format

```markdown
# NNNN — Title

- **Status:** proposed | accepted | superseded by NNNN
- **Date:** YYYY-MM-DD

## Context
What forced the decision.

## Decision
What we chose.

## Consequences
What this makes easy, and what it costs.
```

## Numbering

Pick the next free number. If two branches happen to claim the same number,
renumber on merge — nothing links to ADRs by number.
