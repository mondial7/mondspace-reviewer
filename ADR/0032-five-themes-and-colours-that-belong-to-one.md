# 0032 — Five themes, and colours that belong to one

- **Status:** accepted
- **Date:** 2026-08-30

## Context

msr had two themes and a bug that neither of them could fix. Two nebula
gradients were written as literals into the `.page` rule, so they belonged to no
theme and no theme could override them: the light page carried a dark smear
across the top of it, and nothing inside the light block looked wrong.

That is the failure mode worth designing against. A theme is not a list of
colours; it is a promise that *every* colour on the page comes from one place.

## Decision

**Every colour lives in a theme block, and every theme answers for every token.**
Two tests hold it: one refuses any colour literal written outside a `:root`
block, and one holds each theme to the union of what the others override. The
first would have caught the nebulae the day they were written.

**Five themes**: auto, light, dark, Solarized light, Solarized dark. Following
the operating system stays a state of its own rather than the absence of a
choice — a two-state toggle silently overrides the OS forever after one click,
which is wrong for someone who switches at sunset.

**A menu, not a cycle.** Five is one too many to cycle through: four clicks to
get back where you started is not a control. The menu is built in `chrome.js`
rather than in the six templates that carry the nav, so adding a theme is one
list and not six edits.

### Solarized keeps its softness, not its unreadability

Solarized is deliberately low-contrast: base1 on base03 measures 5.6:1 where
msr's own dark theme measures 13.6:1. Softening the page is the point of
choosing it, so that is kept. What is not negotiable is that anything carrying
meaning stays readable, so body text sits one step off the canonical base and
each accent is pulled until it clears 4.5:1 on its own ground — the same hue,
enough of it to read.

### Painted and written are different jobs

The isometric field took its colours from the signal tokens, which are chosen to
be read as words. On a light theme those are darkened for contrast, and a colour
dark enough to read at 14px is mud as a 200px block: the field came out three
brown lumps on cream, legend matching, consistent and wrong.

Anything painted rather than written now has its own tokens. On a dark ground
the two coincide and the values repeat; on a light one they part company. It
also hands Solarized its canonical accents back for the geometry — they were
never meant to be read as words, and as material they are what Solarized
actually looks like.

Lighting follows the ground too, decided from the background the page really
has rather than from the name of the theme, so a theme msr has never heard of
is still lit correctly.

### A canvas does not read a stylesheet

The starfield and the isometric field are painted from these same custom
properties, read once at load. Changing theme left both drawings in whichever
palette the page happened to open in. They now re-read on a `msr:theme` event,
and the field is rebuilt, because its blocks hold their colour in a material
rather than in CSS.

## Consequences

- A colour that belongs to no theme fails a test rather than shipping.
- Adding a theme is one block of tokens and one line in a list.
- Solarized is recognisably Solarized and still legible, which is a compromise
  a purist will notice; the alternative was a palette that reads badly on the
  one page it exists to make comfortable.
- The theme is still per-browser, in `localStorage`. It is a preference about a
  screen, not about a review.
