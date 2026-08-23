# 0010 — Surface failed tool calls as a unit flag, computed in the live seam

- **Status:** accepted
- **Date:** 2026-08-23

## Context

`msr ingest` already records a `PostToolUseFailure` hook as `Event.Failed =
true` (`internal/domain/event.go`), but nothing downstream ever looked at it —
a unit whose member tool call errored out reviewed exactly like one that
succeeded. That is a real signal: an agent that pressed on after a failed
edit, a failed test run, or a failed shell command is worth a second look.

The obvious place to add it would be `usecase.Flags(u domain.Unit, d
domain.Diff)` — it is where every other deterministic flag lives, and it is
covered by one shared, well-tested order-pinning test. But `Flags` is
deliberately pure over a unit and its diff; it has no view of the events that
made up the unit, and `domain.Unit` does not carry `Failed` itself (only its
member `EventIDs`). `Flags` is also called from `usecase.BuildFileUnits`
(`internal/usecase/filereview.go`), the retroactive per-file review path,
which reconstructs units straight from git — there are no events there at
all, by construction (ADR 0002). Threading events through `Flags`'s signature
just to serve one caller would break the other, and would make a pure
function reach for state it structurally cannot always have.

## Decision

Add `domain.FlagFailed = "failed"` alongside the existing flags, but compute
it outside `Flags`, in `usecase.ReviewLive` (`internal/usecase/reviewlive.go`)
— the one seam that both seals units incrementally *and* still has every
member event in hand at seal time. `finalize` now checks
`anyEventFailed(members)` after resolving `u.EventIDs` into `domain.Event`
values, and appends `domain.FlagFailed` to `u.Flags` if any member event
failed. `BuildFileUnits` is untouched: no events, no flag — a retroactively
reviewed file unit correctly cannot know whether the underlying tool calls
that produced it failed at the time.

## Consequences

- `Flags` stays pure and single-purpose: unit + diff in, deterministic flags
  out, no I/O, no hidden dependency on an event log that not every caller has.
- The failed flag only appears where it is honest to show it — the live
  session path, where the event stream is actually observed. Retroactive /
  `--since` review stays silent on it rather than guessing.
- Both presenters (`plain`, `tui`) and the exported report already render
  `u.Flags` generically as a joined list of flag names, so no presenter code
  changed to surface it — only `domain.ReportItem` gained a `Flags` field
  (`internal/domain/report.go`, populated in `usecase.BuildReport`) so the
  Markdown/JSON export carries it through too.
- Cost: the "is this event-derived or diff-derived" distinction is now split
  across two call sites (`Flags` and `ReviewLive`) instead of one, so a future
  event-derived flag has to make the same judgement call about where it
  belongs.
