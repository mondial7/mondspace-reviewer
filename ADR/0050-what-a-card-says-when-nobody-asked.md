# 0050 — What a card says when nobody asked

- **Status:** proposed
- **Date:** 2026-09-10

## Context

Three things on the cockpit were taking room in proportion to how often they
were needed, which is backwards.

The review card carried a paragraph explaining what you can do before a model
has read anything — true, useful once, and the widest band on the card every
time the page opened. The field, the isometric picture of what changed, had a
card of its own; when nothing was landing it collapsed to a 3.4rem strip, so it
spent most of its life as a picture too small to read taking a whole row to be
too small in. And the tour ran to a thousand words in six numbered steps, under
a masthead promising four.

None of that is a styling problem. It is the same question three times: what
should a card say when nobody has asked it anything?

## Decision

**Orientation goes behind an icon.** The paragraph moves into a dialog opened
from an info icon in the card's corner — the shape already used for the field's
legend beside it. It opens in the sheet the command palette uses, because a
page with two kinds of dialog in it is a page nobody trusts.

A `<details>` element opens it with no JavaScript, which is the point of using
one. Closing it takes a little: nobody expects to have to find the icon again
to dismiss something covering the page, so escape and a click on the scrim do
it too.

**The field moves into the card that says what range this is.** A 6.5rem square
in the corner, floated, so the sentence closes underneath it rather than living
in whatever column is left — in a panel that narrow, a column left about eleven
characters a line.

It is the same size whether or not work is landing. A picture that shrinks when
nothing is happening is telling you about msr rather than about the code, and
the history below it — the one thing in that column that can always use more
room — gets the height back either way.

**The tour is four steps and four hundred words**, each with a sketch of the
region it is about and a link to the page it describes. What went is the second
paragraph of everything: the fetch policy, the analyser catalogue, the shape of
the incoming-work banner. All of it is discoverable by clicking the thing it
describes, which is what a tour is for.

**The sketches are drawn, not captured.** A screenshot of this app is stale the
next time anybody moves a card, it cannot follow the theme — there are five —
and it would have to be fetched or embedded, which offline software should not
do for decoration. A dozen rectangles painted from the page's own tokens change
colour with the page and cost nothing.

## Consequences

- The review card is a row shorter and the panel column has one less thing
  dividing its height.
- Two dialogs on the cockpit now open the same way, and both close on escape.
  Anything else that grows an explanation should use the same pair.
- A first-time reader has to find an icon to be told what they can do. That is
  the trade: the fifth time you open the page, the card is quiet.
- The tour no longer explains everything msr does. It gets somebody working,
  and the pages it links to carry the rest.
- A sketch cannot show type, colour rendering or real content, which a
  screenshot can. If the README ever needs those, it needs real captures — this
  is a decision about the tour, not about documentation generally.
