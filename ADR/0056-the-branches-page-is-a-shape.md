# 0056 — The branches page is a shape

- **Status:** proposed
- **Date:** 2026-09-10

## Context

The branches page was a column of cards down the middle of a wide screen. Each
card said a name, a subject, who and when, how far ahead and behind, and offered
to open a review. Everything on it was true and none of it answered the question
the page exists for: who forked from where, and how far apart is everyone.

That question has an answer that is a picture. A list can tell you
`rate-limit-the-api` is one ahead and one behind; only a shape tells you it and
`retire-the-memory-store` left from the same commit and neither has come back.

Three things were also wrong rather than merely thin. The whole card was one
`<a>` with no styling of its own, so the name, the subject, the author and the
drift all rendered as underlined blue body text — a card that looks like a
paragraph of links. The header said "refreshed every 30s" on a page that
refreshed nothing, because no region on it was in the live-update list. And the
watcher behind that promise ran `git` — and, with watching on, `git fetch`
against somebody else's server — every fifteen seconds for as long as msr was
up, whether or not a single page was open.

## Decision

**A fifth of the width is the list; the rest is the graph.** The list stays,
because a name is how you find a branch and a shape is not searchable. It is an
index into the picture rather than the content of the page.

**The layout is computed on the server, in `usecase`.** Which lane a commit
belongs to and where the line between two commits goes is a pure function of the
commits — the only interesting part of drawing a graph, and the part worth
testing. Everything downstream is coordinates in an SVG. A page that needs a
script to show you a picture shows you nothing while the script is loading, and
msr's pages have always rendered without one.

**A line holds its own lane until the last half row.** The naive shape — turn
immediately, then run down the lane you are joining — is shorter and unreadable:
two branches off the same commit become one thick line, and every branch line
spends most of its length lying on top of the mainline.

**Edges are drawn after every commit has a lane, not while assigning them.**
Two branches can each reserve a lane for the same parent, and the parent is
drawn in only one of them. Drawing as you go gives a line that ends at the right
height beside the wrong lane, which is the one mistake a graph is not allowed to
make: a line that ends nowhere. There is a test that says every line ends on a
dot.

**A branch name labels one commit: the newest one carrying it.** `origin/x` and
`x` are the same branch said twice, and a checkout sitting behind what it tracks
puts the name on two different commits. Two pills reading `main` on two dots
asks the reader to work out which one is meant, and two elements cannot share
the id the list links to.

**The card is a card again.** The link keeps the card's shape and colour, and
the branch name is the only thing that underlines on hover. The subject is
clamped to two lines: a commit message longer than that is one you open, not one
you skim in a column an eighth of the page wide.

**The page live-updates, because it says it does.** `.branches` and the header
meta joined the regions the event stream swaps.

**The remote watcher stands down while nobody has a page open.** This is the
gate `refreshReview` has always used, applied to the one poller that never had
it. Nothing is lost: the branches page reads git in the request that renders it,
so a returning reader gets a fresh answer from their own page load. Only the
toast is skipped, and a toast about a push nobody was there to see is not news.

## Consequences

- The page answers its own question at a glance, and the list is still there for
  the reader who wants a name.
- msr stops fetching from a remote on behalf of a browser tab that was closed an
  hour ago. On a laptop left running overnight with watching on, that is the
  difference between four hundred network round trips and none.
- The graph covers the last eighty commits across all branches. A branch whose
  tip is older than that has a card in the list and no dot to link to. The
  alternative is a page that is a mile of SVG nobody scrolls to the end of.
- Below 1000px the picture goes under the list rather than beside it, which is
  the honest thing to do with a two-column layout at phone width.
