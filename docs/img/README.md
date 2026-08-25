# Screenshots

`plain-review.png` is current — it matches the output of

```sh
msr review --source=replay --file=testdata/sessions/basic.jsonl --plain
```

`tui-review.png`, `ask.png` and `export.png` are of the **terminal UI**, which is
unmaintained (v5.0.0). They are kept for history and are not referenced by the
README or the site: illustrating a web-first product with terminal shots
misrepresents it.

## What is still missing

Shots of the **cockpit**, which is the product. To capture them:

```sh
cd ~/some-project-with-history
msr web
```

Then, at a window around **1600×1000** so all three columns are visible:

| file | what to capture |
| --- | --- |
| `cockpit.png` | the whole page on a target with a few described groups — the panel, the story, and the changes side by side. **The lead image.** |
| `cockpit-changes.png` | one group expanded: its sentence, then a file open with its diff and annotation row |
| `cockpit-status.png` | `/status` with the assistant mid-call, so the activity card shows something running |

Two things worth doing before capturing:

- Press **review this** and let it finish, so the groups have their descriptions
  rather than reading "not yet described" throughout.
- Pick a target with real substance — a tag, or a commit that touched several
  directories — so the grouping has something to show.

Dark theme is the default and the one the palette was designed against; `⌘J`
switches if a light shot is wanted too.
