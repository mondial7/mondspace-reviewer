# Security Policy

## Reporting a vulnerability

Please report security issues privately via
[GitHub Security Advisories](https://github.com/mondial7/mondspace-reviewer/security/advisories/new)
rather than a public issue. You can expect an acknowledgement within a few days.

## Security posture

`mondspace-reviewer` is a **read-only review tool**. It never writes to the agent
and holds no credentials.

- **No secrets.** The summarizer talks to a local/self-hosted OpenAI-compatible
  endpoint and sends no API keys. The summarizer URL is operator-configured; point
  it only at endpoints you trust.
- **Subprocesses.** The git snapshotter invokes `git` with an explicit argument
  vector (never a shell), so diff paths and refs cannot inject commands. Snapshots
  use a throwaway index and never touch your `HEAD`, index, or working tree.
- **Untrusted input.** Hook payloads and JSONL logs are parsed defensively:
  malformed lines are skipped, and session identifiers (which name on-disk
  directories) are validated to prevent path traversal outside the store root.
- **The hook never fails the agent.** `msr ingest` swallows its own errors and
  always exits 0, so a broken or malicious reviewer cannot affect the watched
  agent.

## Scope

The tool watches one agent, one session, one local repository. It has no network
listeners, no authentication, and no multi-user surface. Treat any `events.jsonl`
you did not produce as untrusted data.
