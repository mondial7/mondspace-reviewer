# 0008 — `msr export --format=slack`: a single concise mrkdwn message

- **Status:** accepted
- **Date:** 2026-08-23

## Context

`msr export` already produces `md` (a full review document) and `json` (the
raw `domain.Report`). Neither fits posting a review's status into a Slack
channel: Markdown headings and task lists render as literal `#`/`- [ ]` text
in Slack, and the full report is far too long to post as one message. A third
format that renders Slack's own markup (`mrkdwn`) and stays within a single
message is a separate concern from either existing exporter, not a variant of
one.

`domain.Report`/`ReportItem` carried headline and note text, but never a
unit's deterministic flags (`domain.Flag`, e.g. `no-test`, `large`) — those
live only on `domain.Unit`. A Slack summary that is supposed to surface "the
top few flagged/risky items" needs that signal, so `ReportItem` gained a
`Flags []domain.Flag` field, populated in `BuildReport` from the session's
units alongside the headline it already carried. This is additive (JSON
`omitempty`) and does not change any existing exporter's output.

## Decision

Add `usecase.ExportSlack(domain.Report) string`, a pure formatter over the
same `domain.Report` the other two exporters consume, wired to `msr export
--format=slack`.

Output is Slack `mrkdwn` only — `*bold*`, `_italic_`, `• ` bullets, `\n` — with
no Markdown headings and no tables, since Slack renders neither as intended:

1. **Headline** (one line): counts of files reviewed, flagged, open
   questions/objections, and debt items — e.g. `*Review — s*: 3 files
   reviewed, 1 flagged, 1 question / 1 objection open, 2 debt items`.
2. **Flagged** (optional section, omitted when nothing is flagged): the
   flagged/risky items, each unit once, with its flags named.
3. **Open agenda** (optional section, omitted when empty): the same
   objection/question directives `ExportMarkdown` already phrases (`Address
   the objection on …` / `Answer the question on …`), reusing `directive()`
   so the two exports never drift on how an open concern reads.

Both item lists are capped to the top 5, and the whole message is capped to
~3000 characters (Slack's own message limit). Either cap, when it bites,
announces itself rather than dropping content silently: a truncated list ends
with `…and N more`; a message cut to fit ends with an explicit `_(truncated
to fit Slack's message limit)_` notice, budgeted inside the same cap so the
notice itself is never the thing that gets cut off.

## Consequences

- One Slack message is enough to post a review's status without a human
  hand-editing anything — no headings/tables to strip, no length to trim.
- `ReportItem.Flags` makes flag information available to any future exporter
  too (currently unused by `ExportMarkdown`/`ExportJSON`, which is fine — it's
  additive).
- Cost: a second place (`ExportSlack` vs `ExportMarkdown`) that phrases open
  concerns as directives, mitigated by both calling the same `directive()`
  helper. The counts in the headline are a coarse summary — a unit annotated
  under two different note kinds is counted once per kind context (e.g. both
  "reviewed" and possibly present in two groups), which is fine for a
  headline and documented in code rather than hidden.
