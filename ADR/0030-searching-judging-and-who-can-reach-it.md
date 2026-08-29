# 0030 — Searching, judging, and who can reach it

- **Status:** accepted
- **Date:** 2026-08-29
- **Part of:** [issue #19](https://github.com/mondial7/mondspace-reviewer/issues/19)

## Context

Three smaller gaps from the assessment, each about the review log being a real
artefact rather than a display.

## Decision

### Search, because the log is the product

`⌘K` searched the files in the review on screen. "Where did I write that about
the retry loop" had no answer at all, across a workspace whose entire purpose is
accumulating that writing.

`/search` looks through every note, question, answer and finding in every review
in the workspace. Every word must match, not any: two words is how someone
narrows a search, and treating it as "either" makes adding a word return *more*,
which is the opposite of what they asked for. Hits are grouped by review,
because several hits in one place are one place to go back to.

It reads the stores on each search rather than keeping an index. A review log is
small, searches are rare, and an index is one more thing that can be wrong about
what is on disk.

**A note now records the file it was about.** A unit is derived from git rather
than stored, so for a commit or a tag nothing on disk could say which file a
note concerned — and remembering the file is at least as common as remembering
the wording.

### Judging a finding, because a rerun repeats itself

An audit run twice produces the same findings from the same diff. A finding the
reviewer had already decided was not a problem came back identically every time,
and the only way to stop seeing it was to stop running the audit.

A finding can now be dismissed. It **stays on the card**, greyed and struck
through: removing it would invite the next run to raise the same thing as though
it were new. Everything that counts or colours is measured on what still
*stands*, so dismissing the last one leaves a clean card.

Judgements are carried onto a rerun, matched on the file **and the claim**. If
the model now says something different about the same file, that is a new claim
and nobody has ruled on it — inheriting the dismissal would hide it.

Only "dismissed" changes anything. A finding nobody has ruled on and one
somebody confirmed are both still to deal with, and a third colour for that
difference would be decoration.

### Refusing to serve the repository to a network

`--addr` defaulted to loopback and would happily bind `0.0.0.0`, serving source,
diffs and review notes over plain HTTP with no authentication, silently.

Non-loopback now requires `--allow-remote`. Refusing rather than warning,
because a warning printed above a working server is a warning nobody reads. The
message says why and what to pass, so the person who meant it is not blocked —
only the person who did not.

A malformed address is passed through untouched: `net.Listen` explains it better
than this could.

## Consequences

- Per-repository configuration, also on the list, turned out to be mostly done:
  `.msrignore` is per-repository by construction (ADR 0027). What remains global
  is the model endpoint, which is a property of the machine rather than of a
  repository, so it is left alone.
- Search covers what was *written*, not the code. Finding code is what the
  editor is for.
