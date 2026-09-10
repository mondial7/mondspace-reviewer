# 0052 — The end of the row is where you leave

- **Status:** proposed
- **Date:** 2026-09-10

## Context

The bottom row of the review card carried, from left to right: the words "not
reviewed yet", a button offering to mark it reviewed, a download icon, the words
"take the log:", and three links — markdown, json, slack.

Every part of that is something you do once, at the end, and it was laid out as
though you would read it on the way in. The label said what the button beside it
already said. The log had five elements across the widest row on the card, for a
file most people take once per review if at all. And the button claimed you had
reviewed something whether or not anything had read it.

## Decision

**The verdict goes at the end of the row**, hard against the right edge, with
the log immediately to its left. That is where the control that finishes a job
belongs, and it puts the two things you do on your way out next to each other
rather than at opposite ends of a line.

**The log is an icon.** One button, opening a menu with markdown and json. It
opens upwards, because this row is the bottom of the card and a menu opening
downwards would hang over whatever is under it.

**Slack goes from the page.** It is one paste of one message, which is a thing
you do from a terminal on the rare occasion you do it at all; `msr export
--format=slack` still writes it and is not going anywhere.

**"Not reviewed yet" goes.** It sits beside an enabled button offering to mark
it reviewed, which is the button read twice. The sentence comes back the moment
it says something the button does not: *reviewed 20 minutes ago*, and *reviewed
20 minutes ago, but it has changed since*.

**The button is called what it does.** "Mark as reviewed" once a model has read
the change; **"skip review"** when nothing has. Signing off on a change nothing
has looked at is skipping the review, not finishing one, and a button that says
otherwise is the page lying about its own record. Already signed off, it offers
to review it again.

## Consequences

- The row is three things instead of eight, and two of them are icons.
- The log is one click further away. It is the right trade for something taken
  once at the end, and the icon is next to the button people are already
  reaching for at that moment.
- "Skip review" is a slightly uncomfortable thing to press, which is the point:
  the record it writes is the same either way, and the reviewer should know
  which one they are making.
- Two tests now assert what the row *does* rather than what it is called — the
  label depends on state, and a test that owns the wording turns every copy edit
  into a failure.
