# 0012 — A cinematic storyline view, with focus mode as the escape hatch

- **Status:** accepted
- **Date:** 2026-08-23
- **Amends:** 0004 (which said the web app would ship no client framework and no JavaScript)

## Context

ADR 0004 chose server-rendered HTML with no JavaScript. That keeps the review
surface fast and dependency-free, and it is still the right default for reading
diffs.

But the product is a *storyline*: what an agent did to a repository, in order,
with weight and consequence. A flat list conveys sequence poorly and conveys
significance not at all. A spatial, animated presentation can carry both — depth
for time, position for relationship, motion for attention — and makes the tool
something a developer actually wants to open.

The risk is obvious: spectacle that gets in the way of work. Reviewing a 400-line
diff inside an animated scene would be worse than a plain list, and animation is
hostile to anyone with vestibular sensitivity.

## Decision

Ship both, with the reviewer in control.

- **Storyline (default).** A WebGL scene rendered with **Three.js r160**, vendored
  locally (MIT) so the app works offline and the version is auditable. Units are
  nodes in space along the session's timeline; flags and change size drive their
  appearance; parallax starfield layers and eased camera motion give depth.
- **Focus mode.** One keypress (`f`) drops all of it and renders the same review
  as plain, dense HTML — the fastest path to the essence. The toggle persists in
  `localStorage`, so a reviewer who prefers plain gets it on every visit.

Focus mode is not a degraded fallback: it is a first-class view, and the one the
"immediately get the essence" use case is measured against.

Accessibility and robustness are part of the decision, not an afterthought:

- `prefers-reduced-motion` forces focus mode automatically.
- If WebGL is unavailable or Three.js fails to load, the app renders focus mode
  rather than an empty canvas.
- All review content is in the server-rendered DOM either way; the scene reads
  from that DOM. **Nothing is visible only inside the canvas**, so the review
  remains usable, selectable, and searchable without WebGL.

## Consequences

- The tool gains a distinctive, engaging front door without compromising the
  dense review path.
- Cost: a 655 KB vendored dependency, a WebGL code path to maintain, and a second
  presentation of the same data that must not drift from the DOM.
- The no-JavaScript rule of ADR 0004 no longer holds; the no-build-step and
  no-CSS-framework rules still do. There is still no bundler, no npm, and no CDN.
