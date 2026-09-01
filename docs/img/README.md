# Screenshots

> **These are from 6.2 and predate the current build.** The cockpit's columns
> are in a different order, the story is a rail that folds, the buttons have
> weights, narration is set in a proportional face, and there is a fourth
> reading — the deterministic analysers — that none of these show. Retaking them
> needs the demo repo below and a running local model.

Captured from a real `msr` against a real repository, with a real local model —
never mocked. The demo repo is a small Go service (`auth`, `api`, `store`) whose
second commit adds bearer auth and Postgres-backed sessions, which is enough to
produce four groups, several flags and a story worth reading.

| file | what it shows |
| --- | --- |
| `cockpit.png` | the whole page: panel, story, changes |
| `cockpit-changes.png` | a file opened — diff, flags, key lines, annotation |
| `cockpit-pending.png` | work that arrived mid-review, and the three ways out |
| `cockpit-status.png` | the model, what it has cost, and the workspace |
| `tutorial.png` | the built-in tour at `/tutorial` |
| `cockpit-analyses.png` | the three analysis cards, with real findings |
| `cockpit-reported.png` | what the deterministic analysers found, against the file |
| `cockpit-log.png` | the history card, with a colleague's commit incoming |
| `branches.png` | every branch on the remote, with what there is to review |

To recapture: run `msr web`, narrate a target, then screenshot at 1600×1000.
Headless Chrome works, with two flags that are not optional:

```sh
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  --headless=new --no-first-run --disable-component-update \
  --hide-scrollbars --window-size=1600,1000 --virtual-time-budget=5000 \
  --screenshot=out.png "http://127.0.0.1:7777/?still=1"
```

`?still=1` serves the page without its live stream. The cockpit holds a
server-sent-events connection open for as long as it is on screen, which is an
HTTP request that never finishes — and a headless browser waits for it forever.
Without it this command hangs rather than failing, which is how it went unnoticed.

`--no-first-run --disable-component-update` stops Chrome running its updater
instead of the browser on a machine where it has not been launched headless
before.

The page follows the viewer's theme, so pass `data-theme="dark"` on `<html>` (or
run with a dark system setting) to match the ones committed here.
