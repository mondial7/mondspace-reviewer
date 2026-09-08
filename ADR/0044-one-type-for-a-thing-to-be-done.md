# 0044 — One type for a thing to be done

- **Status:** proposed
- **Date:** 2026-09-08

## Context

Phase 2 makes the findings store the product and the export its output
contract: another binary — the planner — reads what msr writes and puts it on a
board. That only works if there is one thing to read.

There are three today, and they are the same thing three times:

- `domain.Finding` (`internal/domain/analysis.go`) — what a model said, inside
  an `Analysis`. File, note, severity, verdict.
- `domain.Reported` (`internal/domain/reported.go`) — what an analyser said.
  Tool, rule, file, line, message, severity, verdict, new, anchor.
- `domain.Note` (`internal/domain/note.go`) — what the reviewer said. Kind,
  text, unit, file, anchor, superseded-by.

Each has its own persistence, its own dismissal path, and its own renderer.
They differ by *where they came from*, not by *what they are*: every one of them
is a place in the code, a sentence about it, a weight, and a state. A fourth
consumer would need three translators, and the translators would rot — the
usual way, which is that a field is added to one of the three and the other two
keep working.

## Decision

**One type, `Item`, and provenance is a field on it.** `Source` says which of
the three readings produced it — `human`, `llm`, `analyzer`, `planner` — and
`Producer` names the model or tool. The distinction between `stated`,
`inferred` and `reported` (ADR 0003, ADR 0043) is the reason those three types
exist, and it survives intact; it is carried as data rather than as a Go type,
which is what lets one store hold all of it and one renderer read it.

**It lives in `contract/`, outside `internal/`.** A package `internal/` cannot
be imported by the planner, and the whole point is that both binaries compile
against the same declaration rather than two that agree today.

**A package now, a module when there is a second consumer.** The import path
is `github.com/mondial7/mondspace-reviewer/contract` whether or not there is a
`contract/go.mod` beside it, so adding one later changes nothing for anybody
importing it. Until the planner actually imports it there is no version skew to
manage and no second thing to tag. `contract` imports the standard library and
nothing else, which is what keeps that promotion cheap.

**The fields the spec left out are the ones that make it work here.** A
location of path and line is not enough on its own:

- `Anchor` and `AnchorNth` — the diff line's text, and which occurrence of it.
  A number drifts onto something else without ever looking wrong (ADR 0028).
- `UnitID` — a note is about a unit, and unit ids are immutable history while
  the working tree is live (ADR 0030).
- `New` — whether this is about a line the change touched. Pre-existing
  findings are why an unscoped analyser layer is worthless (ADR 0043), and that
  is a property of the item, not of the query.

**`Directive` is stored, not generated at export.** A reviewer who rewrites
what an agent should do has done the most valuable work in the review, and an
export-time renderer would throw it away on the next run. Renderers may still
override it for their consumer; the stored one is the default and the record.

**Three severities, not five.** `info` appears in the Phase 2 spec only to be
suppressed by default, and a level whose defined behaviour is "hidden" is a
filter rather than a level. `critical` sits above "I would not merge this
without dealing with it", which does not leave it an action of its own. The
existing three are defined by what the reviewer should do (ADR 0024) and
`Normalise` already maps every analyser's own vocabulary onto them.

**`Verdict` stays; `State` is added beside it.** ADR 0030 said only
`dismissed` changes anything, and that remains true of a *reading*: confirmed
and unruled are both still to deal with. But `pushed` and `fixed` are not
opinions about a finding, they are facts about the world — an item was handed
to an agent at a time, in a batch, and either survived the next pass or did
not. Those belong in a different field from what the reviewer thought.

**Not a rewrite in one commit.** `Item` is added and written first; `Finding`,
`Reported` and `Note` are converted one at a time behind it, and files already
on disk keep being read as what they are. A single commit that moved all three
would land the schema, the store, the presenters, the MCP tools and the web
surface at once, and nothing about it could be reviewed.

## Consequences

- The planner and msr cannot disagree about the schema, because there is one
  declaration and both compile against it.
- `Item` is wider than any of the three types it replaces, and most items will
  leave most of it empty. That is the price of one table instead of three, and
  it is visible in every JSON line written.
- Three severities means an analyser's `critical` arrives as `high`. The rule
  id is still verbatim on the item, so nothing about the original is lost —
  only the claim that msr has a fourth thing to say about it.
- The conversion is several commits during which two shapes exist. Anything
  added to a finding in that window has to be added in two places, which is the
  exact failure this ADR is about, so the window should be short.
- `contract` being dependency-free is a constraint on what may be put in it: no
  ULID generation, no git, no time formatting beyond the standard library.
