# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **A fourth reading, and it is not a model.** msr detects the deterministic
  analysers already on your `PATH` — golangci-lint, go vet, staticcheck, gosec,
  semgrep, gitleaks, osv-scanner, ruff, eslint — runs them over the files a
  change touched, and shows what they found against the file it is about. It
  ships none of them and installs none of them; a machine with none sees no
  mention of any of it. `reported` is a third class beside `stated` and
  `inferred`, with its own colour: it names its tool and its rule and is the
  only one of the three you can act on without checking it first
  ([ADR 0043](ADR/0043-a-fourth-reading-that-is-not-a-model.md)).
- **`.msr.toml`** — a repository says which analysers to run over it. `[[extra]]`
  adds one to the defaults, `off = [...]` turns one off, `[[analyser]]` replaces
  the set. SARIF is the primary format, so a tool emitting it works with
  configuration alone.
- **New findings, not every finding.** Scoped to the lines a change added, with
  the pre-existing ones counted and folded away. `compare against the base` is
  the accurate answer, on demand: the same tools over the code as it was, via
  `git archive` into a temp directory — never a registered worktree.
- **`reported_findings`** over MCP, beside `model_findings`, so an agent can
  tell a linter's output from a model's guess.
- **One routing table.** `internal/domain/routing.go` decides where every job
  goes: the story and the two audits to the Claude Code CLI when it is
  installed, the per-file and per-group descriptions to the local model. A
  result from the fallback engine says so on the card, and the settings page
  reports each engine separately with its own spend
  ([ADR 0039](ADR/0039-one-table-decides-where-a-question-goes.md)).
- **A designed first-run state**, and `?still=1` for screenshots.

### Changed

- **The review polls every five seconds instead of fifteen**, does nothing at
  all while no page is open, and asks a cheap probe before doing any diff work
  ([ADR 0036](ADR/0036-a-probe-before-the-poll.md)).
- **An analysis card is in one of five states** — absent, running, fresh, stale,
  failed — with what it found as a separate axis, and results stored per diff so
  coming back to an unchanged review re-runs nothing
  ([ADR 0037](ADR/0037-four-things-a-card-can-be.md)).
- **A rerun reads only what moved.** Audits, chapters and group descriptions
  each record what every file said when they ran; findings on untouched files
  carry forward with their dismissals intact
  ([ADR 0038](ADR/0038-read-what-moved.md)).
- **The log gets the middle column.** The story is a rail that folds; the idle
  isometric field is a strip; `start review` and `mark this reviewed` are real
  buttons and everything else is demoted
  ([ADR 0040](ADR/0040-the-log-gets-the-middle.md)).
- **Narration is set in a proportional face** and everything measured stays
  monospace. Flags are neutral except the two that mean stop, with a per-review
  tally. One rule for heading case. Shortcuts sit beside what they move
  ([ADR 0041](ADR/0041-narration-reads-like-prose.md)).
- **Branches, search and the tour sit below a line** in the rail, and the
  settings page counts what actually gets used
  ([ADR 0042](ADR/0042-below-the-line.md)).

### Fixed

- An agent writing files never reached an open page: the server broadcast
  `units` and `stats` and the page listened for neither.
- An audit of a live review could never go stale, because it was fingerprinted
  on snapshot refs that do not move while a review diffs against the working
  tree.
- Unit ids were positional, so a file arriving at the top of the list renumbered
  every unit under it — and every note anchored to one. They are derived from
  the path now, and a note whose file is still in the review is re-anchored.
- A verdict was stored already cut to the card's width, so the `…` on the card
  expanded to nothing on the report.
- Commit subjects in the picker were cut mid-word, on the one list a reviewer
  chooses their next target from.
- The documented way to screenshot msr hung rather than failing.

## [6.2.0] — 2026-08-30

### Added

- **Five themes** — auto, light, dark and Solarized both ways. Every colour
  lives in a theme and a test refuses one written anywhere else; the starfield
  and the isometric field re-read the palette when it changes, which they never
  did before ([ADR 0032](ADR/0032-five-themes-and-colours-that-belong-to-one.md)).
- **A sidebar instead of a top rail**, foldable to a strip of icons, with the
  cockpit's instrument panel moved to the right. ⌘Z hides both; it lost its
  button and kept its shortcut.
- **Settings** (was `/status`), one pane at a time — overview, model, remote,
  repositories, reviews, usage. A pane you are not on is not built
  ([ADR 0033](ADR/0033-settings-as-a-place.md)).
- **The whole diff.** A compacted diff now says how many lines it left out and
  offers them: expanded in place, or as that file's own page without JavaScript.
- **One list of checkpoints.** Commits, the tags on them and recorded runs, in
  time order, with the working tree at the top. Tick two to compare. The
  separate picker is gone.
- **A report page per audit.** The card keeps a verdict and a tally; the
  findings are read on their own page, with what is being reviewed above them.
- **Three to five emoji** for a change, chosen by the model and filtered to
  actual pictographs.
- **`air`** for development, and **`msr mcp`** gained `--target`/the open review
  across the CLI.

### Fixed

- The review card painted over its own text at 1200px, and below 1000px the
  cockpit was silently three columns wide and overflowed the window.
- An annotated tag resolved to the tag object rather than the commit, in three
  separate places — which is why a tag's date came back as its message and a
  compared range could not be marked in the history.
- A range named newest-first reported "0 commits" over a real diff.
- Notes on lines a compacted diff had dropped were reported as orphaned, as
  though the code had moved under them.
- Live-update toasts pushed the page up from the bottom instead of floating over
  it; the icon sprite pushed the whole shell below the fold. Both were the same
  rule quietly putting a fixed child back into a two-column grid.
- The brief said the same sentence up to three times.
- `msr review --since` ignored `.msrignore`, and `msr ask` did not keep what it
  asked.
- Controls that could do nothing looked like they could: "open" on the review
  you are reading, "run" while it is running, "ask" with an empty box.

## [6.1.0] — 2026-08-29

### Added

- **`msr mcp` — your coding agent can read the review back.** An MCP server on
  stdin/stdout that a coding agent's client can be pointed at, so the review
  reaches the thing it is about without anyone retyping it. It never speaks
  first: no hook, no injected message, no file it watches. The agent asks when
  it wants to know ([ADR 0031](ADR/0031-an-agent-pulls-the-review.md)).
- **What a human wrote and what a model inferred come through separate calls.**
  `review_status`, `review_feedback` and `review_file` serve only what a person
  typed; `model_findings` serves the audits. The judge msr runs is a small local
  model — right often enough to be worth reading, wrong often enough that acting
  on it unverified is a mistake — so the warning travels in every payload rather
  than only in the tool description, and each finding names the model that
  produced it. A finding you dismissed is still shown, marked settled, so an
  agent does not raise it again.
- **A second, expensive tier.** `workspace_feedback` and `workspace_search` read
  every stored review, and say `EXPENSIVE` in their descriptions so the choice
  to spend it is made knowingly.
- A step in the in-app tour, and a section on the project page, about handing the
  review back.

### Notes

- The MCP server reads the store and nothing else — no git, no model, no network
  — so it is safe to leave configured in an agent's client. It cannot write to
  the review either: a review log the agent can edit is not a review of the
  agent.
- Opening a review in `msr web` now records which one is open, in the store, so
  the separate MCP process can follow it.

## [6.0.0] — 2026-08-29

Five releases of v5 built the git-first review model. v6 is the one where the
review itself became a real artefact rather than a display.

### Breaking

- **`--addr` no longer binds beyond loopback without `--allow-remote`.** msr
  serves your source, your diffs and your review notes with no authentication,
  so putting it on a network is now a decision rather than a typo.
- Notes anchored to a target reviewed under a different range do not carry over,
  as [ADR 0017](ADR/0017-git-first-review.md) has always said. Everything stored
  by v5 is read by v6 unchanged.

### Added

- **`.msrignore`** — generated files kept out of a review, with **no defaults**:
  a review tool that hides files by default is one you cannot trust, and hiding
  a lockfile would mask the one dependency change worth seeing. What is hidden
  is always named, with the rule that hid it
  ([ADR 0027](ADR/0027-ignoring-generated-files.md)).
- **Notes on individual lines**, anchored to the line's *text* rather than its
  number, plus an occurrence index so identical lines — closing braces, `return
  nil` — are told apart. A diff grows above the line you commented on
  constantly, and a number would drift onto something else without ever looking
  wrong ([ADR 0028](ADR/0028-notes-on-lines.md)).
- **Export from the app.** Markdown, JSON and Slack links on the review card, of
  whatever review is being read, arriving as a downloadable file. "The review log
  is the product" was stated in three places and the only way to get it out was a
  CLI invocation against a session id.
- **Workspace search** (`/search`) — every note, question, answer and finding in
  every review. Every word must match, not any: two words is how someone narrows
  a search, and treating it as "either" makes adding a word return *more*. It
  reads the stores each time rather than keeping an index
  ([ADR 0030](ADR/0030-searching-judging-and-who-can-reach-it.md)).
- **A note now records the file it was about.** A unit is derived from git rather
  than stored, so for a commit or a tag nothing on disk could say which file a
  note concerned.
- **Dismissing a finding.** An audit run twice produces the same findings from
  the same diff, so a finding you had already ruled out came back identically
  every time. Dismissals carry across reruns, matched on the file *and* the
  claim, and the finding stays on the card rather than being deleted — deleting
  it would invite the next run to raise it as though it were new.
- **Structural tests for the pages**, pinning invariants that only screenshots
  had caught, and **tests for the prompts against a real model**, which found and
  fixed two real weaknesses: a verdict saying "nothing found" alongside findings,
  and the breaking-changes pass reporting newly-added functions as breaking.

### Changed

- **A 600-file review loads in half a second instead of twenty-eight.** Not by
  bounding anything: the plan proposed capping units, paginating the column and
  truncating diffs, and measuring showed all three were wrong. The review was
  rebuilt from git on every request — the cache was discarded each time because
  the "show ignored files" answer was applied unconditionally, and each file was
  diffed in its own git process ([ADR 0029](ADR/0029-large-reviews.md)).

### Fixed

- **Annotations on anything other than a recorded session were write-only.**
  Stored correctly under the target's own id and never read back, because the
  loader only reconstructed a session when the target *was* one — and
  `handleAnnotate` appended every note to whichever review msr started with. A
  note on a commit vanished on the redirect and turned up on something unrelated.
- **Each review now has its own conversation.** Questions asked about one review
  were shown under another and handed to the model as history for it.
- **The history card and branches page follow the repository being reviewed.**
  Both captured the repository msr was started in, so opening a target elsewhere
  showed the first repository's commits under the second's name — worse than
  showing nothing, because it looks right.

## [5.8.0] — 2026-08-29

### Added

- **`/branches` — every branch on the remote**: how far each has drifted from the
  mainline, who last pushed to it and when. *Ahead* is the number that matters,
  because it is how much there would be to review, and the row opens exactly
  that. A colleague's branch is the range `main..their-branch`, an ordinary
  comparison, so what opens is a normal review — narrated, annotated, signed off,
  audited like anything else ([ADR 0026](ADR/0026-the-wider-view.md)).
- Merged branches stay listed but dimmed. Removing them would lie about what
  exists; letting them compete would bury the ones that matter.

### Changed

- **The fetch switch moved out of the command line.** `/status` says whether msr
  is fetching and how often, and turns it on or off while it runs — no restart.
  That fact was previously knowable only from how the process was started, which
  is the wrong place for something a program is doing to your repository and your
  network.

## [5.7.0] — 2026-08-29

### Added

- **A history card** showing where you stand against everything that has landed:
  the commit you are reviewing, the ones you have signed off, the ones that have
  not left your machine, and the ones a colleague has pushed that are not here
  yet. Every row opens that commit
  ([ADR 0025](ADR/0025-watching-the-remote.md)).
- **A toast that names who pushed** — "3 new commits on origin/main · Alice" —
  because "you are 3 behind" and "Alice moved main" are different facts and both
  fit on one line.
- **Watching the remote is opt-in** (`--fetch`), because fetching talks to the
  network and writes remote-tracking refs, and msr otherwise does neither.
  Without it the card still works from whatever your own last fetch brought in.
  With it, HEAD, your index and your working tree are still never touched —
  there is a test for exactly that.
- **Severity on findings.** Three levels, a schema enum so the model cannot
  invent a fourth, sorted worst-first, and still labelled inferred — including
  how bad.

### Fixed

- "just now ago".

## [5.6.0] — 2026-08-28

### Added

- **Three readings of a change, not one.** The story is now one analysis card
  among three; beside it sit a **security pass** and a **breaking-change check**,
  each run when you ask and never before
  ([ADR 0024](ADR/0024-analyses-as-independent-cards.md)).
- They share nothing: no card is shown what another found, and running one never
  triggers another. A model given three questions at once answers the first well
  and the rest as an afterthought, and one shown a previous finding anchors on
  it.
- They are short by construction. The caps live in the schema rather than in a
  politely-worded prompt: at most five findings, each a file and one sentence.
- **A card is never ambiguous about which it means.** "Nobody has run this", "it
  could not run" and "it ran and found nothing" all look different. On a security
  card that is the difference between information and a false sense of safety.

### Fixed

- **An action naming an unresolvable target fell back to whatever review was
  open**, so an audit could land under something nobody was looking at. Rendering
  still falls back — a stale link should not be a dead end — but acting refuses.

## [5.5.0] — 2026-08-28

### Added

- **An actual install guide**: two things to install, the second optional, and
  what you lose without it.
- **The project page rebuilt** in the application's own palette, with real
  screenshots captured from a real msr against a real repository and a real local
  model.
- **A tour inside the app** at `/tutorial` — reachable from every page, from the
  command palette and from the `?` sheet
  ([ADR 0023](ADR/0023-teaching-the-page.md)).

### Fixed

Taking the screenshots found five bugs no test had, because all five were about
what the page *looked* like:

- The review card could report "8/4 described": group and per-file descriptions
  share one map, and the count counted the map.
- The mid-review banner rendered at the bottom of the page, having auto-placed
  into an implicit grid row.
- Unchanged files were listed twice, once as a phantom deletion — diffing a
  snapshot needs the working tree compared as a tree rather than by scanning
  untracked files.
- The fifteen-second refresher was still rebuilding a pinned review against the
  working tree, so the page held still and then jumped.
- A commit hash broke across three lines to make room for an author name nobody
  reads in a tile that size, and files at the repository root grouped under ".".

## [5.4.0] — 2026-08-28

### Added

- **The live review holds still while you read it.** It is pinned to a snapshot
  taken when you opened it, and work the agent does afterwards queues in a banner
  that names the files — flagging any you have already annotated, because a note
  written against a version that no longer exists is the thing worth interrupting
  for. Then you choose: **keep reading**, **include them**, or **review just the
  new work** as a range of its own
  ([ADR 0020](ADR/0020-pin-the-review-queue-the-rest.md)).
- **Finishing a review.** Mark a target reviewed, leave a closing comment on the
  change as a whole, and reopening it tomorrow says so. Signed-off targets are
  ticked in the picker. If the code moved after you signed off, it says that
  rather than reading as current
  ([ADR 0021](ADR/0021-finishing-a-review.md)).
- **Keyboard navigation**: `j`/`k` between files, `o` to open, `[` and `]`
  between reviews, `{` and `}` between repositories, `/` for the picker, `a` to
  ask, `r` to sign off, `?` for the list
  ([ADR 0022](ADR/0022-keyboard-navigation.md)).

### Changed

- **msr talks to llama.cpp's `llama-server` by default**, with a model per
  workload if you want one ([ADR 0019](ADR/0019-llama-server-and-a-model-per-workload.md)).
- **Answers are no longer read out of the reasoning channel.** An empty `content`
  is a fault now, and says which server flag causes it.

## [5.3.0] — 2026-08-25

### Added

- **A live target** — the working tree against HEAD, updating as the agent works.
  Unlike the old worktree target it is always offered, and it keeps its identity
  when a commit lands, so notes and the story survive the commit
  ([ADR 0018](ADR/0018-live-target-and-pulses.md)).
- **A toast when the repository moves** — a commit, a tag, files changing —
  wherever you are. Clicking it opens that target, which msr has already
  discovered.
- **The model is configurable from `/status`** and from a config file, taking
  effect at once rather than on the next launch. Settings are refused, and not
  saved, if the endpoint does not answer. `switchable.Summarizer` forwards the
  optional capabilities deliberately: a wrapper that dropped `SchemaAnswerer`
  would silently turn schema-enforced narration back into parsing JSON out of
  prose, and one that answered nil to `Ping` would light the status page green
  for something that answers nothing.
- **One searchable input for every point in history.** Choosing what to review
  and choosing the two ends of a comparison are the same question, and they were
  three widgets. They are one now: a native `datalist`, searchable as you type,
  working with no JavaScript at all. A target carries a `Ref` — a tag name, a
  short hash, `#42` — so `/?target=v5.2.0` opens a release rather than a hex id
  nobody can recognise.
- **The status page reports what share of the model's output was reasoning**, and
  says plainly when `enable_thinking: false` is being ignored. "Only some chat
  templates honour this" was accurate and useless; this is measurable, because
  reasoning tokens were already counted.

### Changed

- **The stats box answers what the target can answer.** It showed the same six
  numbers whatever was being reviewed, so a single commit reported "1 commit" and
  "0 PRs".
- **`new-dep` fires on dependency manifests, not on every import.** It fired on
  any added `import` line, so every source file that gained an internal import
  carried it. It asks a supply-chain question, so only files that can answer one
  trip it — a manifest with a line naming a dependency, or any addition to a lock
  file. Bumping `go 1.25` no longer claims to be taking on a dependency.

### Fixed

- **Every action now acts on the review being read.** Every form posts with its
  target, every handler resolves the review from it, and the redirect returns to
  the review that was being read. Describing a group answered "no such group in
  this review"; annotating and re-analysing a switched target failed the same way
  silently; and a question asked while reading one target was answered from
  another.
- **Two spinners that lied.** The review card said "reading this review…" until
  the page was reloaded by hand, never having been in the live-update list; the
  assistant's "thinking…" showed with nothing running, because a class that sets
  `display` outranks the browser's own `[hidden]` rule.
- The panel sizes itself: it declared four grid rows and had six boxes in it, so
  adding one silently changed which box was allowed to grow.
- Targets opened by ref were rebuilt on every request; the target index was an
  unguarded global written by a request handler; the working-tree target sorted
  below every commit ever made; the watcher counted msr's own store as the
  reviewer's work; and a schema cap equal to the display width cut model
  descriptions off mid-word.

## [5.2.0] — 2026-08-25

### Added

- **A review card across the top of the cockpit** — has the assistant read this,
  how long ago, which model, how many groups it described, and the button to
  read it again. Switching target is exactly when a reviewer needs all four, and
  they were spread across three pages.

### Fixed

- **Describing a group failed with "no such group in this review".** The page
  rendered groups from the units it had loaded while the command recomputed them
  from git, and on a repository being worked in those drift apart within seconds.
  The page now passes the units it is actually showing rather than an id to look
  up again.
- **Failed descriptions were silent.** `DescribeGroups` skipped anything it could
  not describe without a word, so "1 of 6 described" was visible but
  unexplainable. The count of failures and the first reason are recorded in the
  audit log.
- **Stats no longer overflow their cards.** They reflow instead of forcing three
  across, and numbers wrap rather than ellipsing — a clipped `+10301` reads as
  `+103…`, which is not a truncated number but a wrong one.
- A read review still offers a re-read. It goes stale as soon as the code moves,
  and it stays a button rather than anything automatic.

## [5.1.1] — 2026-08-25

### Fixed

- **macOS refused to run the installed binary.** `msr` is not signed with an
  Apple Developer ID, so anything downloaded — a release tarball or the binary
  inside the Homebrew cask — arrives quarantined, and Gatekeeper kills it with
  "Apple could not verify … is free of malware", offering only *Move to Bin*.
  The cask now strips the attribute on install. Verified against the released
  v5.1.0 binary: quarantined it is killed silently with no output; with the
  attribute removed it runs.

  Stripping it **at install time, before the first run**, is what matters: once
  macOS has refused a binary it caches that decision for the path, and removing
  the attribute afterwards does not lift it. Diagnosed on a real install — the
  quarantined copy stayed blocked after the attribute was removed, while the
  same bytes copied elsewhere ran immediately.

  This skips Gatekeeper's check for that one file, which is the standard remedy
  for an unsigned open-source CLI and still a real trade-off. The proper fix is
  a Developer ID signature and notarisation, which needs a paid Apple account
  and is not done. The README says all of this, including how to verify the
  checksum first, and how to do it by hand for a tarball.

- Homebrew now installs the command as **both** `mondspace-reviewer` and `msr`,
  so the documented name works without writing an alias.

## [5.1.0] — 2026-08-25

### Fixed

- **Descriptions were being generated and then thrown away.** A group's id was
  derived from its units' ids, which are positional (`-f001`, `-f002`) — so
  adding one file renumbered everything after it and orphaned every description
  beyond it. On a live review the units rebuild every fifteen seconds, which is
  why a folder could sit at "not yet described" no matter how often it was
  asked. Ids now come from the file paths, which do not move.
- **A panicking model adapter took the server down**, and on the way out left
  the page claiming to still be thinking. It is recovered, recorded as a
  failure, and the work is registered before the goroutine starts so the page
  never shows a call it has not begun.
- Work that runs past a ceiling is shown as **stalled** rather than running: a
  spinner that has been spinning for half an hour is telling the reviewer
  something untrue.

### Added

- **Per-file descriptions.** A folder's summary is where a reviewer starts; "and
  what happened to this one" is always next. Each file can be described on its
  own, from the same control the folder has.
- **Compare any two points** — a tag against a tag, a branch against a commit,
  anything against the working tree. The result is a target like any other, so
  it narrates and annotates identically.
- **The picker is two steps**: repository, then what in it. One combined list
  became unreadable past a few repositories.
- **Repositories can be unwatched** from `/status`. Nothing on disk is touched —
  it closes a window, and the reviews and notes stay where they are.

## [5.0.0] — 2026-08-24

The web app is the product.

### Changed

- **The documentation leads with the web app.** README and the site now open on
  install-and-run, what the tool is for, and how to use the cockpit. The CLI is a
  secondary section for scripting; everything else was reorganised around the
  question a new reader actually has, which is "what does this do and why would
  I want it".
- **The terminal UI is unmaintained.** It still works and is still shipped, but
  it will not gain anything added since v3 — the cockpit, git-first review,
  workspaces, grouped changes and the assistant are web only. `msr review --tui`
  now says so on start-up, and `msr help review` says so in its flags.

### Note

No behaviour changed in this release beyond that notice. It is a major version
because standing a supported interface down is the kind of thing a version
number should say out loud rather than leave in a changelog footnote.

## [4.1.0] — 2026-08-24

### Fixed

- **`msr web` no longer needs a session to start.** Opening repositories that
  had never been recorded failed with "no reviews found — run `msr review`
  first", which contradicted the whole point of v4: a repository with years of
  history and no recorded runs is a perfectly good thing to review. The bootstrap
  now opens the newest **target**, and a session is not special.
- Open repositories were counted from the session index, so a repository with
  history and no recorded runs looked absent on `/status`.

### Changed

- **Repositories are chosen in the app, not at launch.** The prompt is gone: the
  first few discovered are opened and the rest are listed on `/status` under
  *found nearby*, one click from being watched. Choosing belongs where it can be
  changed without a restart, and a script never has to answer a question.
- Annotations are written to the store of whatever they were made against. One
  store cannot serve a workspace: a note on a commit in another repository
  belongs in that repository's store.

### Added

- **The assistant's work is visible from everywhere.** While any model call is in
  flight the nav rail carries a spinner on every page, and `/status` gains an
  *assistant activity* card: what is running now, what was just asked, how long
  each took, why any failed, and a control to run it again. Narration, group
  description, questions and re-analysis all register.

## [4.0.0] — 2026-08-23

Git is the subject of a review. A session is a lens on it.

### Changed

- **A `Target` is what gets reviewed** — a range of history with a name (ADR
  0017). Git supplies most of them: every recent **commit** as `parent..commit`,
  every **tag** as the range since the tag before it, every **pull request** as
  the commits referencing it, and the **working tree** when it is dirty. A root
  commit diffs against the empty tree, which is the only honest way to say
  everything here is new.
- **A recorded session is one kind of target, not the index.** It still answers
  what an agent run did and still holds the stated intent nothing else has. Every
  other target lists the sessions overlapping it, so the intent behind a commit
  is one click away — the run enriching the commit rather than containing it.
- **`--since`/`--until` stop being a special case.** They name a range like any
  other target.
- **Identity is derived, not stored**: a target's id is a hash of its repository
  and range, so the same commit or tag always reviews to the same id across
  restarts, machines and clones — and nothing needs migrating when a session is
  deleted.
- Net change per file is untouched (ADR 0002 stands). `BuildFileUnits` always
  took two refs; only what supplies them changed. Its first parameter was called
  `sessionID` and was only ever the unit-id prefix — it is `reviewID` now.

### Added

- A target picker in place of the session switcher. Against this repository it
  offers 48 things to review — 40 commits, 4 tags, 3 recorded runs and the
  uncommitted work — where before it offered 3 sessions.
- Any target can be narrated and have its groups described, not only the one the
  server started with.

### Breaking

- **Notes keyed to unit ids derived from a session id do not carry over** to the
  same files reviewed as a commit. The old ids still resolve for the session
  targets that produced them, but a note attaches to a review, and the same code
  under a different range is a different review.
- `?session=` still resolves — a session is still a target — but `?target=` is
  what the application now links to.

## [3.1.0] — 2026-08-23

### Added

- **`msr help`** — every command with a one-line summary, and `msr help <command>`
  for one command's flags. Typing `msr` with no arguments now shows it rather
  than an error: someone typing the bare name is asking what it does. **`msr
  version`** reports the build, stamped at release time.
- **Homebrew**: `brew install mondial7/tap/mondspace-reviewer`, published to
  `mondial7/homebrew-tap` on each release. Publishing needs a token with write
  access to the tap; without it the cask is skipped rather than failing the
  release.
- **The reviewer assistant keeps its conversation.** Exchanges are stored in
  both the JSONL and Postgres stores and reloaded, so a thread can be picked up
  tomorrow. Answers render as markdown, and a question is a mode: the story
  steps aside, the field grows, and the wait is explicit.
- **Groups can be described on demand.** The automatic pass is bounded, so most
  groups in a large session read "not yet described"; each now carries a control
  to ask, and the result is saved with the story.

### Fixed

- **The assistant was showing its own thinking as the answer.** A reasoning
  model that runs out of budget mid-thought leaves `content` empty and a
  monologue in `reasoning_content`, and the fallback added for schema replies
  took it. That fallback now applies only when a schema was in force; otherwise
  an empty answer is reported as one, naming the finish reason and the reasoning
  tokens spent. Questions also get a roomier token budget than headlines.
- **A session-scoped question carried no diffs**, so the assistant correctly
  answered that it could not say what changed. It now receives a bounded digest
  of the actual changes, capped per file, built from the units the page shows
  rather than whatever the store happened to hold.

## [3.0.0] — 2026-08-23

One page to review a session, across as many repositories as you like.

### Added

- **The cockpit** (`/`) — the whole review on one screen (ADR 0015, ADR 0016):
  a fixed panel carrying a model-written title and brief for the session; the
  story as prose; and the changes with diffs, notes, annotation and re-analysis.
  Clicking a chapter brings its files alongside it.
- **An isometric field that means something.** One block per changed file:
  height is lines changed (log-scaled), colour is growth / deletion / flagged,
  depth is recency, and it moves only while the session is live. A still field
  means nothing is happening.
- **A workspace of repositories.** `--repo` is repeatable; with none given `msr`
  opens the checkout it is in, or offers the checkouts one level below it. Past
  five it lists them and asks rather than opening forty. Sessions load on demand
  and each remembers which repository owns it. Repositories can also be opened
  while the app runs, from `/status`.
- **`/status`** — is the reviewer's model online (re-probed every 20s), what it
  has spent split into prompt / completion / *of which reasoning*, the open
  repositories, and every session. **`/activity`** — every model call and every
  change to the review, across the whole workspace.
- **Schema-enforced model output.** Where the endpoint supports it the story is
  requested as `json_schema` with the real area names as an `enum`, so an
  invented area cannot be emitted at all (ADR 0014). Measured on a 32k context:
  unconstrained the model spent all 299 completion tokens reasoning and returned
  nothing in 107s; schema-enforced it returned valid JSON in 54 tokens and 2.2s.
- **Per-file history.** How many times a file was touched and when, opening into
  a per-file log, and a full-diff overlay that steps through that file's git
  history with the arrow keys.
- **Grouped changes.** Files changed together in a directory are shown together,
  with one model-written sentence about what the change is *for*.
- **A shared shell**: a spatial backdrop and one nav rail on every page, **zen
  mode** (`⌘Z`, `Esc` to leave), a **command palette** (`⌘K`) over pages and
  files, and a **theme switch** (`⌘J`) — dark, light, or follow the system.
- **Token accounting.** The adapter records what every call cost; a cap
  (`max_tokens`) is sent on every request, which is LM Studio's own remedy for a
  model stuck inside an unclosed structure.
- A **deep-universe palette**, measured rather than eyeballed: every token clears
  WCAG AA against both the background and the panel.

### Changed

- **`/review` and `/story` are gone**, folded into the cockpit; both permanently
  redirect, so links and bookmarks still work.
- **`msr web` needs no `--session`** — it opens the newest review it can find.
- **Narration runs once per review, not once per launch.** The story is stored
  with a fingerprint of the review it describes and reused while that matches;
  re-opening the page costs nothing. A fallback is stored too, so a failure is
  retried by pressing a button rather than by navigating. While a session is
  still moving the review refreshes every 15s from one `git diff --numstat`
  call, and re-narration is bounded to once every five minutes.
- **Mechanical headlines are no longer shown.** "edited jsonl.go" above a row
  labelled `jsonl.go` said nothing; a per-file headline now appears only when a
  model wrote one. Diffs are folded by default and open on the file name.
- **Zen mode replaces focus mode**, and applies to every page rather than one.

### Fixed

- **A schema-constrained reply arrives in `reasoning_content` with `content`
  empty** — the grammar constrains sampling inside the chat template's thinking
  block. Reading only `content` is what made narration fall back silently.
- **An endpoint that rejects a schema** is retried unconstrained, and if that
  also fails the error names the rejection rather than only the second failure.
- **Listeners bound inside the changes column** were lost whenever a live update
  replaced it, which killed the history overlay, the tree links and the view
  switch within seconds on an active session. All are delegated now.
- **`--out` was both the pattern for finding a store and the resolved path**, so
  opening a repository at runtime silently found nothing.
- **`/dev/null` counted as a terminal**, so a script redirecting from it was
  shown a prompt nobody could answer.
- Postgres remembers a session's story too; it was JSONL-only, so the
  once-per-review rule silently did not apply to the store the web app is meant
  for.

## [2.0.0] — 2026-08-23

The web app becomes the primary interface, and review becomes the *net change* of
a session rather than a log of every keystroke.

### Added

- **Web application** (`msr web`) — server-rendered Go templates, hand-written
  BEM CSS, no build step and no client framework (ADR 0004):
  - A **cinematic storyline** of the session rendered with Three.js (vendored
    locally, MIT, works offline), and **focus mode** (`f`) — a first-class plain,
    dense, motionless view. `prefers-reduced-motion` forces it; missing WebGL
    falls back to it. All review content is server-rendered, never canvas-only
    (ADR 0012).
  - Scrollable, colour-classified diffs; click-to-annotate; a **multi-session
    workspace** across repos and agents; a **reviewer-assistant chat** that keeps
    the conversation as context; **per-unit re-analysis** with model attribution;
    and an **audit log** of every interaction.
- **`--since=<ref>` / `--until=<ref>`** — review any commit/branch/tag range with
  no session at all (ADR 0005).
- **OpenCode event source** (`--source=opencode`), alongside Claude Code hooks
  (ADR 0006).
- **PostgreSQL storage**, opt-in via `MSR_POSTGRES_DSN`, always in a dedicated
  schema and never `public` (ADR 0007).
- **`msr gc`** to delete throwaway review refs (ADR 0009); **Slack export**
  (`--format=slack`) as one concise message (ADR 0008).
- New flags: `failed` (a failed tool call, ADR 0010) and `solo-iface` (pure diff
  heuristic, ADR 0011). Prebuilt binaries via goreleaser on tagged releases.
- Architecture decisions are now recorded in [`ADR/`](ADR).

### Changed

- **Retroactive review is the net change per file** against the commit just
  before the session, so back-and-forth edits collapse into one reviewable change
  with a real diff (ADR 0002). Previously every batch boundary produced a unit,
  and retroactive diffs came out empty.
- `no-test` is **session-aware**: the implementation chunk is no longer flagged
  when its test was written in another unit of the same session (TDD).
- The TUI was redesigned for readability (short handles, repo-relative paths,
  net `+/-` stats, diffs on expand) and now quits on `ctrl+c` and never renders
  a blank screen.
- The summarizer defaults to `http://localhost:1234/v1` and supports bearer-token
  auth via `MSR_API_KEY`.
- `install-hooks` writes the binary's **absolute path**, since hooks run under
  `/bin/sh` with no aliases and a bare PATH.
- Requires **Go 1.25+**.

### Security

- Session identifiers are validated before use as directory names (path traversal).
- The web app binds to localhost, holds no credentials, and vendors its one
  frontend dependency rather than loading it from a CDN.

## [1.0.0] — 2026-08-22

First public release. Watching one agent, one session, one repo.

### Added

- **Ingestion** — `msr ingest` (atomic append, always exits 0) and
  `msr install-hooks` (merges into `.claude/settings.json`); a `hooks` event
  source that tails `events.jsonl` with fsnotify and a poll fallback.
- **Clustering** — consecutive events sealed into review units at batch
  boundaries, a 5s inactivity gap, or a 12-event cap. Pure over the event log.
- **Git snapshots** — throwaway commits under `refs/mondspace/review/<session>`
  that never touch `HEAD`, the index, or the working tree, so every unit's diff
  is stable after the file is rewritten.
- **Deterministic flags** — `no-test`, `new-dep`, `swallowed-err`, `public-api`,
  `large`, `todo`. No model, offline, instant.
- **Interactive TUI** — a bubbletea unread-queue with navigation, filtering,
  expand/collapse, five annotation kinds, and file-level supersession.
- **Headlines** — LM Studio (any OpenAI-compatible) summarizer with strict
  `stated`/`inferred` discipline and async fill-in that never blocks the queue;
  graceful offline degradation to mechanical headlines.
- **Interrogation** — `a` / `A` in the TUI and a scriptable `msr ask`, answering
  from a bounded context (log, diffs, prompt, notes) and never re-reading the repo.
- **Export** — `msr export --format=md|json`: review report grouped by note kind,
  a debt task list, an open agenda phrased as the next agent prompt, plus
  superseded and unreviewed sections.
- **Scriptable core** — the `replay` source + `plain` presenter exercise the whole
  app with no agent, terminal, or network.

### Security

- Session identifiers are validated to prevent path traversal outside the store
  root.

[6.2.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v6.2.0
[6.1.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v6.1.0
[6.0.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v6.0.0
[5.8.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v5.8.0
[5.7.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v5.7.0
[5.6.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v5.6.0
[5.5.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v5.5.0
[5.4.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v5.4.0
[5.3.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v5.3.0
[5.2.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v5.2.0
[5.1.1]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v5.1.1
[5.1.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v5.1.0
[5.0.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v5.0.0
[4.1.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v4.1.0
[4.0.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v4.0.0
[3.1.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v3.1.0
[3.0.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v3.0.0
[2.0.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v2.0.0
[1.0.0]: https://github.com/mondial7/mondspace-reviewer/releases/tag/v1.0.0
