# 0040 — The log gets the middle

- **Status:** accepted
- **Date:** 2026-09-01

## Context

msr's claim is that the reviewable log is the product: the diffs, and what a
human wrote on them. The cockpit did not lay out as though that were true.

The story took the first column at `0.9fr` and the diffs the second at `1.1fr`
— near enough equal for narration, which is the part a model guessed at. The
instrument panel took a third column whose largest single object was an
isometric field that, once a session went idle, was a picture of something that
had stopped happening, with the history it could have made room for squeezed
underneath it.

The actions had the same problem in miniature. Every control on the page was
the same outlined chip: `start review`, `run again`, `re-describe`,
`read the report`, `re-analyse`. When everything has the same weight nothing
leads, and the two that actually matter — start the review, say you have
reviewed it — did not. `mark this reviewed` was worse than equal: it was small
text behind a `▸`, which is where you put a footnote, not the verdict the whole
page exists to record.

And the state before any of it, the one a new reviewer meets first, was four
words and a button.

## Decision

**The middle column is the log, and everything else is sized around it.** The
story becomes a rail: `17rem` open, `2.1rem` folded to a vertical strip that
still says what it is and how many chapters are in it. The fold is remembered
like every other shell preference, next to the sidebar's.

The rail carries the reviewer assistant with it. Asking a question about the
change is the same kind of act as reading the story about it, and both are
things you open when you want them.

**Idle, the isometric field is a strip.** Three and a half rem: enough to see it
is there and that nothing is moving. The height it gives up goes to the history,
which is the one thing in that column that can always use more. While work is
landing it is full height again, because then it is showing something.

**Three weights, and a control gets exactly one.**

- *primary* — filled, one per card at most: `start review`, `mark this reviewed`.
- *secondary* — outlined: running an audit nobody has run, re-running one whose
  code has moved.
- *quiet* — reads as a link: `review again`, `run again`, `read the report`,
  `re-describe`, `re-analyse`.

`mark this reviewed` keeps its `<details>`, because the form under it works with
no JavaScript. Only the disclosure triangle goes.

**The first run is designed.** Before anything has been read, the card says what
is already true: the diffs below are git's, the flags are mechanical, annotation
works, and none of it is waiting on a model. Then what `start review` will
actually do, that the audits are separate and asked for one at a time, and which
two keys move the file list.

## Consequences

- The diffs get roughly half the width again on a normal screen, and all of it
  when the rail is folded.
- The story is one click further away. That is the point, and the click is
  remembered — but a reviewer who folds it and forgets will not see a new
  chapter appear.
- A weight scale is a thing to maintain. A new control now has to be assigned
  one, and the failure mode is a page that slowly returns to all-chips.
- The first-run block is prose that has to stay true. It names `j`, `k` and `?`,
  and it will be wrong the day those change.
