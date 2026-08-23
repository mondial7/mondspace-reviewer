# 0016 — One page for review, and a shell shared by all of them

- **Status:** accepted
- **Date:** 2026-08-23
- **Supersedes parts of:** [ADR 0012](0012-focus-mode.md), [ADR 0013](0013-narrative-story-view.md), [ADR 0015](0015-cockpit-view.md)

## Context

The web app had grown four ways to look at one session — a review queue, a
story, a cockpit, an activity log — and three of them showed the same changes
in different clothes. Moving between them lost your place, and the story could
not be read next to the diffs it described, which is the one arrangement a
reviewer actually wants.

## Decision

**The cockpit is the whole review**, at `/`. Three columns:

1. a fixed instrument panel — the model's reading of the session, the isometric
   field, and the numbers;
2. the story, as prose;
3. the changes, with diffs, notes, annotation and re-analysis.

`/review` and `/story` are gone, and permanently redirect to `/` so links and
bookmarks do not rot.

### The two reading columns are magnet-scrolled

Each chapter is anchored to the first unit it covers, so scrolling either column
brings the other alongside and lights the matching pair. The correspondence is
by unit id, not by proportion: a three-line chapter and a four-hundred-line diff
are the same chapter, and proportional scrolling would drift within a screen.

### Diffs are compacted inline and fetched in full on demand

A 97-file session with every full diff inlined is megabytes of HTML that almost
nobody opens. Each file shows a compacted diff; `GET /units/{id}/diff` serves the
rest when a reader asks for it.

### A session has a model-written title and brief

Narration already produced both; they now lead the panel. The brief is capped at
200 characters in the JSON schema *and* trimmed on the way in — a grammar cannot
be talked out of a limit, but a model can ignore an instruction. It is labelled
`inferred` and names the model that wrote it; the numbers beneath it are not
inferred, and the developer's own prompt is shown beside it unaltered.

### Files carry their history

A net-change-per-file review deliberately collapses the agent's back-and-forth
(ADR 0002). Each file now says how many times it was touched and when, and opens
into a per-file log — the collapse is visible rather than silent. Units built
from a git range carry no event ids, so touches are matched by path, tolerating
the absolute paths hooks record against the repo-relative paths units use.

### One shell, on every page

A spatial backdrop and one nav rail, identical everywhere. **Zen** (`⌘Z`, `Esc`
to leave) hides both, leaving only the work; it replaces ADR 0012's focus mode
and its per-page toggles. Because zen hides the rail, navigation cannot depend on
it: **`⌘K`** opens a command palette over the pages *and* over every changed file
in the session.

## Consequences

- One address to share, one place to work, and the story finally sits beside the
  diffs it describes.
- `⌘Z` is only intercepted outside text fields — inside one it is undo, and
  stealing it would be hostile.
- The backdrop is a 2D canvas, deliberately: it must never compete with the
  isometric field for a WebGL context.
- Cost: the cockpit template is now the largest in the project, and one page
  renders every unit. Compaction and on-demand diffs are what keep that viable.
