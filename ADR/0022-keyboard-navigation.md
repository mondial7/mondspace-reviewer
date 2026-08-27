# 0022 — Reading a review without the mouse

- **Status:** accepted
- **Date:** 2026-08-28
- **Builds on:** [ADR 0016](0016-one-page-and-a-shared-shell.md)

## Context

The shell already had `⌘K`, `⌘Z` and `⌘J` — going somewhere, hiding the shell,
changing theme. All three are things you do occasionally.

The thing you do constantly had no binding at all. The cockpit is a reading
surface: a long column of files, a picker above it, and a workspace of other
reviews behind that. Moving down the files, opening one, moving to the next
review, switching repository — every one of those needed the mouse, and every
one of them happens dozens of times in a sitting.

## Decision

Single-key bindings for the reading motions, modifier keys left to the shell.

| | |
| --- | --- |
| `j` / `k` | next / previous file |
| `o` | open or close the file under the cursor |
| `g` / `G` | first / last file |
| `[` / `]` | previous / next review |
| `{` / `}` | previous / next repository |
| `/` | jump to the review picker |
| `a` | ask about these changes |
| `r` | mark this review done |
| `?` | the list of all of them |

Single keys are only safe because every one is ignored while something is being
typed, and while any modifier is held. That check is the load-bearing part of
the design, not a detail.

### Navigation reads the picker

Moving between reviews and repositories walks the picker's own `<option>` list,
which already holds every review in the workspace, in order, each carrying the
repository it belongs to. Building a second list for the keyboard would be two
sources of truth that drift; instead the options grew a `data-repo` attribute
and the keyboard reads what the page already shows.

Stepping between reviews stops at the ends rather than wrapping — wrapping from
the oldest commit to the newest is a jump, not a step. Switching repository does
wrap, because repositories are a small ring with no natural end, and it lands on
that repository's *newest* review: arriving at someone's oldest commit would be
a strange place to be put.

### The cursor is a reading position

`j` and `k` mark a file rather than selecting it, and the mark is stored in the
DOM (`data-cursor`) rather than in a variable. The live refresh replaces the
whole column underneath the reader (ADR 0018), and a remembered index would
survive that as a *wrong* index. Reading the cursor back out of the DOM means it
either survives correctly or is simply gone.

### `r` opens the form, it does not submit it

Finishing a review is a judgement (ADR 0021). A single keystroke may bring the
form up and put the caret in it; it may not record a verdict. The distance
between "I pressed r" and "I have signed this off" is deliberate.

### `?` and `⌘K` both find it

A shortcut nobody knows about is not a feature. `?` opens the list, and the same
list is a palette entry, so it is reachable by someone who has not yet learned
that `?` opens it.

## Consequences

- Escape inside the cheatsheet is handled on the dialog itself and stops there,
  so closing it does not also drop the reader out of zen mode. The shell's own
  Escape handling is untouched.
- The sign-off textarea needed its own class. It had been reusing the ask box's,
  and with `a` focusing the assistant and `r` focusing the sign-off, one
  selector matching both sends you to the wrong one.
- These are cockpit bindings and load only there. The shell's three work
  everywhere, as before.
