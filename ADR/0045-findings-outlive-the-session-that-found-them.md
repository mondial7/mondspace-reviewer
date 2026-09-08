# 0045 — Findings outlive the session that found them

- **Status:** proposed
- **Date:** 2026-09-08

## Context

The store is keyed by session: `<root>/<session-id>/{events,units,notes,ask}.jsonl`
(`internal/adapter/store/jsonl/jsonl.go`). A session is one agent run, and for
the log that is exactly right — the events, the units clustered from them and
the story told about them are the record of that run and of nothing else.

It is wrong for findings. ADR 0030 says a dismissal has to survive the next run
or it is not a dismissal, and today it survives the next run *of the same
session*: start a new one over the same branch and every dismissed finding comes
back as though nobody had looked at it. A post-mortem pass over `main..HEAD`
has no session at all and nowhere to write. And Phase 2 asks for the harder
version of the same thing — a finding raised live on Tuesday recognised as the
same record by a review of the merged branch on Friday.

There are 49 session directories in this repository's own store. Re-keying them
is not a migration anybody should be asked to run.

## Decision

**The session log stays session-keyed. Only items move out.** Events, units,
exchanges and the narrative are correctly keyed already and are not touched.
This is the whole reason the change is small: nothing that works today is
re-homed.

**Items live in one file, with the branch as a field rather than a
directory.** `findings.jsonl`, appended, last-write-wins per id on compaction.
Not a directory per branch:

- Branch names contain `/`, so a directory per branch means slugging a
  path-hostile string that arrives from `git`, and then a reverse mapping to
  show a reviewer which branch they are looking at.
- A rename orphans the file, and a merge leaves two of them describing one
  history.
- The query that matters is "everything ever said about this fingerprint",
  which one file answers by reading it and a tree answers by walking all of it.

**`SessionID` is attribution and never the key.** It says which run raised the
item, which is worth knowing and worth showing. It decides nothing about where
the item is stored or whether it is still open.

**The shared file goes in `.mondspace/`; the session log stays in
`.mondspace-reviewer/`.** Two directories rather than one, on purpose: they
have different commit policies. The session log is a per-machine record of what
an agent did and belongs to nobody else; the findings file is the surface the
planner reads and the place a team's dismissals accumulate, and a team that
wants those shared has to be able to commit them without also committing every
event of every run. The integration contract is that file and its schema, not
the directory name.

**Both are ignored by default, and committing `.mondspace/` is opt-in.** JSONL
merges badly — two branches appending to one file conflict on every line near
the end — and the default should not hand that to somebody who never asked for
it. This settles D5 the conservative way, and the cost of it is that a
dismissal made by one person is a dismissal only for them until they choose
otherwise.

**`msr gc` never touches items.** It reaps review refs whose session logs are
gone (`internal/usecase/gc.go`), and after this change a session log going away
is no longer any evidence about a finding.

## Consequences

- A dismissal survives the session that made it, which is what ADR 0030 already
  claimed and could not deliver across runs.
- `msr review main..HEAD` has somewhere to write without inventing a synthetic
  session id to satisfy the store.
- One appended file per repository is a contention point that per-session files
  were not: two msr processes on two worktrees of one repository now write to
  the same file. Appends of whole lines under `O_APPEND` are what the store
  already relies on, but this is the first time two *unrelated* runs rely on it.
- Nothing migrates. Old session directories keep working and keep their notes;
  items written from now on are in the new file. A reviewer with history in the
  old store does not get retroactive cross-session dismissals, and that is
  worth saying rather than pretending the boundary is invisible.
- Two dot-directories in a repository instead of one, which needs saying in the
  README and in `.gitignore`'s default.
