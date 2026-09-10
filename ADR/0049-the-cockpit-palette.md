# 0049 — The cockpit palette

- **Status:** proposed
- **Date:** 2026-09-10

## Context

msr's page was a deep-space hull: near-black with a violet cast, two nebulae
behind everything, a violet accent, and every word on it set in monospace
because most of what it says is git talking (ADR 0012, ADR 0041).

It reads well and it is not what the reviewer wants to look at all day. The
same author's other tool — a jobs cockpit built beside this one — settled on a
different answer to the same problem: a slate hull, one mint accent, three
steps of grey text, and monospace kept for the things that are actually
mechanical. Sitting side by side, the second one is the one that reads as an
instrument and the first is the one that reads as a poster.

This adopts that language here. It is a restyle, not a rebuild: msr's tokens,
themes and BEM classes were already the right shape for it, which is why this
is mostly a palette and four primitives.

## Decision

**The default dark theme becomes the cockpit palette.** A slate hull —
`#0d1117` — with two surfaces above it a few points apart, `#262d38` for the
rules that separate regions and `#1d232c` for the ones that separate rows.
Mint at `#4dd4ac` for the accent, warm amber and hot pink as the only other two
signals, and blue for anything that is a reference to code.

Not a sixth theme. A theme is a preference; this is what msr looks like, and
shipping it as an option would leave the thing everybody sees unchanged.

**Three steps of text, not two.** What you read, what labels what you read, and
what is there only when you look for it. Two steps in an interface this dense
forces every label to choose between shouting and vanishing.

**The interface is sans; the work is mono.** Everything that is git talking — a
path, a sha, a rule id, a diff, a log line — stays monospace, because those are
things you compare, count or copy. Everything that is msr talking is prose, and
prose set in monospace reads as program output rather than as an interface.
This is the one part of the restyle that is not just colour, and it is the part
that changes how the page feels most.

**Four primitives, defined once.** A control (input, select, textarea) is a
soft-cornered box on the ground colour with a rule around it and a mint ring
when focused. A button is one of three weights. A link has a colour — there was
no base rule for links before, so anything nobody had styled rendered in the
browser's own violet, which belonged to no theme and answered to nothing. And
two radii: a large one for surfaces, a small one for controls, replacing nine
different numbers that made buttons look like cards.

**The primary action is tinted, not filled.** A solid mint slab is the
brightest thing on a slate page by a distance, and with one on every card the
cockpit became a row of buttons with some text between them. Tint, mint type
and a mint edge still make it unmistakably the loudest control in its card,
which is the only comparison that matters.

**The backdrop is all but extinguished.** Two nebulae on near-black were a
different room from this one; on a slate ground a visible cloud reads as a
smudge on the screen. What is left is a few points of shift, enough that the
page is not a flat fill.

**Every other theme gets the new tokens, in its own colours.** The light theme
takes a deeper mint that clears contrast on white; both Solarized themes keep
their canonical accents and gain the greys, tints and signals from their own
palettes. The stylesheet already had a test that fails when a theme forgets a
token another theme defines, and it is what made this safe to do at all.

## Consequences

- The app looks like the tool beside it, which is the point: two instruments on
  one desk should not argue about what a border is.
- Monospace is now a signal rather than the medium. A path in a sentence is
  visibly a path.
- Five themes to look at instead of one, every time a colour moves. The token
  test catches a missing one; it cannot catch an ugly one.
- The screenshots in the README and in `docs/` are of the old palette until
  `docs/img/demo.sh` is run again, which needs a model endpoint and is not part
  of this change.
- The violet is gone from the default theme and survives in both Solarized
  themes, where it is canonical.
