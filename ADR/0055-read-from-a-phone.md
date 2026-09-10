# 0055 — Read from a phone

- **Status:** proposed
- **Date:** 2026-09-10

## Context

msr runs on the machine the work is happening on. The reviewer is not always at
that machine — they are on the sofa, or in a meeting, and the agent is still
writing. The review is a web page on the local network, and a phone is the
screen they have.

It did not work. The cockpit is a fixed-height shell holding three columns that
scroll inside themselves, which on a phone is three scroll traps in a window
that cannot move; the rail took a column of a 390px screen; cards ran off the
right edge; and every affordance that appears on hover appears never.

## Decision

**One breakpoint, at 760px.** Two would mean a width where the rail is a bottom
bar and the cockpit still thinks it is three columns.

**The rail becomes a bottom bar.** Fixed, thumb-height, icons with their words
back — six unlabelled icons is a puzzle, and there is room for both at this
size. The brand, the "also" divider and the fold control are navigation about
navigation, and the bar has room for destinations instead.

**The cockpit is one column that scrolls with the page**, in the order the work
happens: what this change is and what to do about it, then the diffs, then the
story, then the instruments. The panel is last because it is the thing you
glance at, and on a phone glancing costs a scroll either way.

**A diff scrolls sideways inside its own card, and the page never does.**
Horizontal scroll on a phone is how a page stops being usable with one hand.

**Touch is a media query of its own** (`pointer: coarse`), because a tablet is
wide and still a thumb. Controls get a 2.5rem minimum, and anything that was
only visible on hover is simply visible.

**`:7777` now needs `--allow-remote`, like `0.0.0.0:7777` always did.** They
reach the same listener. Refusing one spelling and allowing the other is a
guard that can be got around by typing less, and the code said so in a comment
directly above the line that allowed it. The default is `127.0.0.1`, so nobody
arrives at this by accident.

**With `--allow-remote`, msr prints the addresses another device can reach it
on.** Somebody who passed that flag is about to go and look up their laptop's
IP; it is two lines to save them the trip. Loopback and link-local are left out
— one is the address they already have and the other is not routable from a
phone.

## Consequences

- The review is readable and annotatable from a phone on the same network,
  which is the case msr was built for and could not serve.
- Anybody running `msr web --addr=:7777` today gets an error naming the flag.
  That is the point: it was serving their source to the network with no
  authentication and nothing said so.
- Two more layouts to keep working. The cockpit is the only page that needed
  real work; the rest were already columns of cards.
- The `?` sheet is unreachable on a phone, which is fine — it lists keys a
  phone does not have, and the tour is in the bar.
