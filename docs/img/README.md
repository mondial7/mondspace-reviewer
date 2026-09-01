# Screenshots

Captured from a real `msr` against a real repository, with a real local model —
never mocked. The demo repo is a small Go service (`auth`, `api`, `store`) whose
second commit adds bearer auth and Postgres-backed sessions. That change really
does concatenate a token into a SQL string and really does alter an exported
signature, which is why the security pass and the breaking-change pass both
have something to say — and why golangci-lint does too.

| file | what it shows |
| --- | --- |
| `cockpit.png` | the whole page: story rail, changes, panel |
| `cockpit-changes.png` | a file opened — diff, flags, annotation |
| `cockpit-analyses.png` | the three analysis cards, with real findings |
| `cockpit-reported.png` | what the deterministic analysers found, against the file |
| `report.png` | one audit in full, with the control to dismiss a finding |
| `cockpit-pending.png` | work that arrived mid-review, and the three ways out |
| `cockpit-status.png` | both engines, what each reads and what each has spent |
| `analysers.png` | which analysers are on this machine, and what was not found |
| `cockpit-log.png` | the history card, with a colleague's commit incoming |
| `branches.png` | every branch on the remote, with what there is to review |
| `tutorial.png` | the built-in tour at `/tutorial` |

## Retaking them

Run `msr web` against the demo repo, open the second commit, narrate it and run
both audits, then capture at 1600×1000 with a 2× scale factor. Headless Chrome
works, with three flags that are not optional:

```sh
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  --headless=new --no-first-run --disable-component-update \
  --hide-scrollbars --window-size=1600,1000 --force-device-scale-factor=2 \
  --virtual-time-budget=5000 \
  --screenshot=out.png "http://127.0.0.1:7777/?still=1"
```

`?still=1` serves the page without its live stream. The cockpit holds a
server-sent-events connection open for as long as it is on screen, which is an
HTTP request that never finishes — and a headless browser waits for it forever.
Without it this command hangs rather than failing, which is how it went unnoticed.

`--no-first-run --disable-component-update` stops Chrome running its updater
instead of the browser on a machine where it has not been launched headless
before.

Shots of one band of the page are taken by sizing the window to that band rather
than by cropping — `--window-size=1600,380` is the analyses row. `cockpit-log.png`
is a column and has to be cut out; `sips` measures its crop offsets from the
centre and is easy to get wrong, so a five-line Go program using `image/png` is
the more reliable way.

The page follows the viewer's theme, so pass `data-theme="dark"` on `<html>` (or
run with a dark system setting) to match the ones committed here.
