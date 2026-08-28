# 0023 — Teaching the page

- **Status:** accepted
- **Date:** 2026-08-28

## Context

`msr web` opens on three columns, a picker, six kinds of flag, a story written
by a model, five annotation verbs and a dozen keyboard bindings. Every one of
them is defensible on its own and the whole thing is a lot to meet at once.

The README explained the ideas well and the setup badly: installing the binary
was step one of one, and the model server — which is most of the value — was
four hundred lines further down under a heading about configuration. Someone
setting up a new machine had to read the whole document to find out that a
second install existed.

The project page was the Jekyll Cayman theme: a grey banner and a wall of
markdown that looked like nothing the application looks like.

And the screenshots were from v2, showing a terminal UI that is no longer
developed.

## Decision

**Three places, one story, told at three lengths.**

- The **project page** is the pitch and the setup: what it is, three install
  steps, and the first five minutes. Hand-written HTML using the application's
  own palette and type, so someone who arrives there and then runs `msr`
  recognises the second from the first. No Jekyll — `.nojekyll` and a single
  self-contained file, because a theme that has to be fought is worse than no
  theme.
- The **README** is the reference: everything the page says, plus the parts
  only someone already running it needs.
- **`/tutorial`** is the tour, inside the app, where someone confused by the
  page actually is. Reachable from every page's rail, from `⌘K`, and from the
  `?` cheatsheet.

The install section leads with the fact that there are **two** things to
install, and says plainly that the second is optional and what you lose without
it. A reader who stops after step one still has a working tool.

### Screenshots are captured, never mocked

The images are a real `msr` against a real repository with a real local model,
taken with headless Chrome against the running server. Mocked screenshots drift
from the product and, worse, they cannot fail — a mockup will happily show a
layout the code cannot produce.

This one earned its keep immediately: capturing the first set found five bugs,
including a review card reporting "8/4 described" and a notification banner
rendering at the bottom of the page instead of the top. None had shown up in a
test, because all five were about what the page *looked like*.

## Consequences

- `docs/` is now `.nojekyll` plus one HTML file and the images. There is no
  build step and nothing to keep in sync with a theme.
- The page duplicates some README prose. That is deliberate: someone deciding
  whether to install should not have to read a reference manual, and someone
  looking up a flag should not have to scroll past a pitch.
- Screenshots have to be recaptured when the UI changes materially.
  `docs/img/README.md` records how, so it is a command rather than an
  archaeology exercise.
