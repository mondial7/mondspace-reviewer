# 0028 — Notes on lines, anchored to what they were about

- **Status:** accepted
- **Date:** 2026-08-29
- **Part of:** [issue #19](https://github.com/mondial7/mondspace-reviewer/issues/19)
- **Builds on:** [ADR 0002](0002-net-change-per-file.md)

## Context

A note attached to a file. Real review happens on lines, and this was the widest
gap between msr and what a reviewer expects — "this retries forever" is a
comment about one line, and filing it against the whole file makes the reader
find it again.

## Decision

**A note may carry an anchor: the diff line it was written about.**

### The line's text, not its number

A line number is the obvious anchor and the wrong one. A diff grows above the
line you commented on constantly — a rebase, another edit, a wider hunk — and a
number would drift quietly onto a different line. **A wrong anchor that never
looks wrong is worse than an obviously broken one.**

So the anchor is the line verbatim. It survives everything above it moving,
which is the case that actually happens.

Identical lines are told apart by which occurrence they are. Closing braces,
blank lines and `return nil` are everywhere, and text alone would put every note
on the first one. When the occurrence no longer exists — the line is still
there, just fewer times — the note falls back to the first: somewhere true beats
orphaning a judgement that still applies.

### A line that has gone is reported, not dropped

If the anchor text is nowhere in the diff, the code it was about is gone. The
note is shown anyway, marked as no longer anchored. This is the same discipline
as a stale sign-off (ADR 0021): a judgement about code that no longer exists
must neither vanish nor read as current.

### One form, told what it is about

Clicking a line does not open a second kind of note form. It fills hidden fields
on the file's existing one and focuses it. Submitting without clicking a line
leaves them empty, which is a note about the file — exactly what every note was
before this existed, and still the right shape for plenty of what a reviewer
says.

Lines are focusable and Enter does what a click does, so this is reachable
without a mouse like everything else (ADR 0022).

## Consequences

- A line-level note renders on its line and is left out of the file's note list,
  so one judgement is not shown twice.
- Compacting a long diff (ADR 0013) can drop the line a note was on. That note
  becomes orphaned rather than lost — visible, and visibly not current.
- The anchor is stored on the note, so it travels with the review, survives a
  restart, and appears in the export like any other.
- Anchoring is a pure function over a diff and a set of notes, so it is tested
  without a browser, a server or a repository.
