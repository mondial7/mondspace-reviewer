# 0007 — Postgres for web-app storage, in a dedicated schema

- **Status:** accepted
- **Date:** 2026-08-23

## Context

The CLI and TUI persist to append-only JSONL under `.mondspace-reviewer/`, which
is crash-safe, tail-able, and inspectable with `jq`. That is the right shape for
a single local session.

The web app changes the requirements (issues #8, #11, #12): reviews spanning many
sessions, agents and repos; annotations and ask-conversations that must survive
as records; and an audit trail with provenance. Querying and joining that across
sessions in flat files means reimplementing a database badly.

## Decision

The web app stores its data in **PostgreSQL**, accessed with `pgx/v5` (pure Go,
no CGO).

All objects live in a **dedicated schema**, never `public`. The schema name
defaults to `mondspace_reviewer` and is configurable, so the tool can share a
database with other applications without colliding with their tables or relying
on the default search path. Every statement is schema-qualified.

Configuration is a DSN from `MSR_POSTGRES_DSN`; there is no embedded credential
anywhere in the repo. Schema and tables are created idempotently on connect
(`CREATE SCHEMA IF NOT EXISTS` / `CREATE TABLE IF NOT EXISTS`).

The JSONL store stays as-is and remains the default for the CLI and TUI. Postgres
is opt-in for the web app; the two live behind the same `port.Store` interface,
so nothing above the adapter layer knows which is in use.

## Consequences

- Cross-session, cross-repo queries (#8) and an audit log (#11) become
  straightforward, and concurrent web requests get real transactional safety.
- Sharing a database with other projects is safe, because nothing is created in
  `public`.
- Integration tests need a live Postgres; they are skipped unless
  `MSR_POSTGRES_DSN` is set, so the default `go test ./...` stays hermetic and
  offline, exactly like the LM Studio contract test.
- Cost: a real dependency and a service to run. The JSONL path remains for anyone
  who wants the zero-setup local experience.
