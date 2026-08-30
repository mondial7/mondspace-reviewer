# 0033 — Settings as a place, one pane at a time

- **Status:** accepted
- **Date:** 2026-08-30

## Context

`/status` had become two columns of six panels: the reviewer's model and the
form that changes it, the remote watch, token usage, live assistant activity,
the repositories in the workspace, and every recorded review.

Half of that is configuration you set once. The other half changes while you are
looking at it. Putting them in one scroll meant passing the settled half every
time you wanted to know whether the model was answering — and rendering all of
it on every request, including the workspace-wide review list, which is the
expensive one.

The name was wrong too. "Status" describes one of the six panels.

## Decision

**It is Settings, and it renders one pane at a time**: overview, model, remote,
repositories, reviews, usage. A sidebar names them with a line each. A pane you
are not looking at is not built, which is both the performance answer and the
attention answer.

**Overview is the door.** Settings is a place you arrive at without a question
in mind as often as with one, so the first thing has to answer "is this
working": whether the model is answering and which one, whether msr is fetching,
how many repositories and reviews the workspace holds, what it has spent, and
what is running this second. Every line links to the pane that changes it.

**Sections are addresses, not tabs.** `/settings?s=model` is a link you can
bookmark, send, and land on from a form submission — each form returns to the
pane it was submitted from. An unknown section lands on the overview: a stale
bookmark or a typo is not an error worth a page of its own.

**Old links keep working.** `/status` and `/sessions` are in the tutorial, in the
README, on the branches page and in bookmarks. Renaming a page is not a reason
to break a link, so both redirect — `/sessions` to the pane that now holds
reviews, rather than to the top of a page they are no longer on.

**Reviews are a table.** They were six values run together on one line with
nothing saying which was which.

## Consequences

- The expensive pane costs nothing until you ask for it.
- One more place a link can point at, and one more thing to keep consistent:
  the sidebar is a list in Go, not six copies in templates.
- The nav rail says "settings", so the six hand-copied rails had to agree. They
  had already drifted — `/search` offered no way to search and `/activity`
  offered neither branches nor search — and a test now holds them to each other.
