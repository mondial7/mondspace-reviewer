# 0054 — One home for a dismissal

- **Status:** proposed
- **Date:** 2026-09-10

## Context

ADR 0030 says a dismissal has to survive the next run or it is not a dismissal.
ADR 0045 moved findings into a store keyed by branch so that it survives the
*session* as well. Both are true, and 7.1 shipped them side by side rather than
one on top of the other.

So there were two places a reviewer's "not a problem" could land. Dismiss a
finding on the page and it went to the target's rulings file, keyed by the
finding's hash, in the session store. Dismiss the same finding with `msr
findings dismiss` and it went to the findings store, on the item. Neither read
the other, so the same finding could be settled in one and outstanding in the
other — and the CLI's copy, the one that outlives the session, was the one the
page ignored.

## Decision

**A ruling made on the page is written to both**, and a ruling made on the
command line is read by the page. The two are keyed the same way already —
producer, rule, path, message, hashed (`contract.Item.Key`) — because both come
out of the same decoder, so the bridge is a lookup rather than a translation.

**The page's own file wins where they disagree.** It is the one being written
while somebody watches, and a disagreement can only come from a store the page
has not caught up with.

**Not a migration, yet.** The rulings file stays, and stays authoritative for
the page. Deleting it means the cockpit reading its verdicts out of the findings
store on every render, which is a change to the read path of the page people
actually use, and it wants to be its own commit with its own way of being
wrong.

## Consequences

- Dismiss a finding in either place and it stays dismissed in both.
- Two writes per dismissal, one of them a file rewrite. It happens when a human
  presses a button.
- A ruling on a finding the store has never seen — a target reviewed before the
  store existed, or one nothing has scanned — is written to the rulings file
  alone, which is exactly what used to happen to all of them.
- The duplication is now visible in one place instead of two, which is the
  precondition for removing it.
