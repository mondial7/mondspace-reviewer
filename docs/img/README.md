# Screenshots

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
| `cockpit-log.png` | the history card, with a colleague's commit incoming |

To recapture: run `msr web`, narrate a target, then screenshot at 1600×1000.
Headless Chrome works:

```sh
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  --headless=new --hide-scrollbars --window-size=1600,1000 \
  --screenshot=out.png http://127.0.0.1:7777/
```

The page follows the viewer's theme, so pass `data-theme="dark"` on `<html>` (or
run with a dark system setting) to match the ones committed here.
