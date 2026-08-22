# mondspace-reviewer — Build Spec (Phase 1)

A terminal review companion that runs alongside an autonomous coding agent. It
narrates what the agent is doing, answers questions about it, and captures your
annotations into a durable review log.

**It is a review tool, not a control harness. Phase 1 never writes to the agent.**

---

## 1. Purpose

While a coding agent works in auto mode, the human has no cheap way to stay
oriented. Reading the raw agent stream is high cognitive load; waiting for the
final diff means all feedback arrives too late to be cheap.

`mondspace-reviewer` turns the agent's activity into a **reviewable queue of
change units**, each collapsible to one scannable line, expandable on demand,
questionable in natural language, and annotatable in one keystroke.

The **review log is the product.** Narration and interrogation exist to help you
produce annotations.

## 2. Non-goals (Phase 1)

Do not build, do not scaffold "for later", do not add config flags for:

- Steering, blocking, or injecting anything into the agent
- Multi-agent / multi-session aggregation
- Cross-session history or trend analysis
- Web UI, PR integration, team features
- Auth, multi-user, telemetry

Watching **one agent, one session, one repo** must be excellent before anything
else exists.

## 3. Stack

- **Go 1.23+**, single binary, no CGO
- **TUI:** bubbletea + lipgloss (behind a `Presenter` port — swappable)
- **Store:** append-only JSONL on disk (see §6). No database in phase 1.
- **Summarizer:** OpenAI-compatible HTTP client, defaulting to the local
  LM Studio server (`http://192.168.101.99:1234/v1`, model `qwen/qwen3.5-9b`).
  Configurable; must degrade gracefully when unreachable (see §9).

Everything except the git, HTTP, and terminal adapters must be pure and testable
without a running agent.

## 4. Architecture — ports and adapters

Domain owns the types and the rules. Adapters own the I/O. Dependencies point
inward only; the domain imports nothing from `internal/adapter/...`.

```
internal/
  domain/          Event, Unit, Note, Session — types + invariants, zero I/O
  usecase/         Ingest, Cluster, Flag, Summarize, Ask, Annotate, Export
  port/            interfaces below
  adapter/
    source/hooks/     tails events.jsonl written by agent hooks
    source/replay/    replays a recorded log from testdata — the test source
    snapshot/git/     git snapshot refs (§7)
    summarizer/openai/  LM Studio / any OpenAI-compatible endpoint
    summarizer/null/    passthrough, used until M3 and when offline
    store/jsonl/
    presenter/tui/
    presenter/plain/  line-oriented output — makes the app scriptable and
                      end-to-end testable without a terminal
cmd/
  mondspace-reviewer/
```

### Ports

```go
type EventSource interface {
    // Events emits until ctx is cancelled. Must never block the agent.
    Events(ctx context.Context) (<-chan domain.Event, error)
}

type Snapshotter interface {
    Snapshot(ctx context.Context, label string) (domain.SnapshotRef, error)
    Diff(ctx context.Context, from, to domain.SnapshotRef, paths []string) (domain.Diff, error)
}

type Summarizer interface {
    Headline(ctx context.Context, u domain.Unit, d domain.Diff) (domain.Headline, error)
    Answer(ctx context.Context, q string, c domain.AskContext) (string, error)
}

type Store interface {
    AppendEvent(domain.Event) error
    AppendNote(domain.Note) error
    Load(sessionID string) (domain.Session, error)
}

type Flagger interface { // deterministic, no model
    Flags(u domain.Unit, d domain.Diff) []domain.Flag
}
```

## 5. Core concepts

**Event** — one observed agent action. Immutable. Comes from a hook.

**Unit** — a cluster of consecutive events forming one unit of meaning. This is
what the human reviews. Immutable once sealed.

**Note** — a human annotation attached to a *Unit ID*.

> **Annotations anchor to Unit IDs, never to file/line.** The working tree is
> live; line anchors rot within seconds. Unit IDs are immutable history.

## 6. Data model & storage

All state lives under `.mondspace-reviewer/<session-id>/` in the watched repo
(add to `.gitignore`). Three append-only JSONL files: `events.jsonl`,
`units.jsonl`, `notes.jsonl`. Append-only means crash-safe, tail-able,
trivially diffable in tests, and inspectable with `jq`.

```go
type Event struct {
    ID        string    `json:"id"`         // ULID
    SessionID string    `json:"session_id"`
    TS        time.Time `json:"ts"`
    Source    string    `json:"source"`     // "claude-code" | "opencode" | "replay"
    Kind      string    `json:"kind"`       // "edit" | "write" | "bash" | "prompt" | "batch_end"
    Tool      string    `json:"tool"`
    Files     []string  `json:"files"`
    StatedIntent string `json:"stated_intent"` // verbatim from agent, may be ""
    Raw       json.RawMessage `json:"raw"`
}

type Unit struct {
    ID        string   `json:"id"`
    SessionID string   `json:"session_id"`
    EventIDs  []string `json:"event_ids"`
    Files     []string `json:"files"`
    From, To  SnapshotRef `json:"from","to"`
    Headline  Headline `json:"headline"`
    Flags     []Flag   `json:"flags"`
    Sealed    bool     `json:"sealed"`
}

type Headline struct {
    Text   string `json:"text"`
    Why    string `json:"why"`
    WhySrc string `json:"why_src"` // "stated" | "inferred"  — REQUIRED
}

type Note struct {
    ID     string    `json:"id"`
    UnitID string    `json:"unit_id"`
    Kind   NoteKind  `json:"kind"`  // ok | question | objection | debt | note
    Text   string    `json:"text"`  // optional
    TS     time.Time `json:"ts"`
}
```

### `WhySrc` is load-bearing

`stated` = taken verbatim from the agent's own words. `inferred` = the summarizer
guessed it from the diff. These must be visually distinct in every view (different
colour *and* different label word). A single confabulated rationale presented as
fact destroys trust in the whole feed. When in doubt, mark `inferred`.

## 7. Snapshots

The tree moves while you review. Every sealed unit records the snapshot refs
bracketing it, so a unit's diff is stable forever.

Snapshot without touching the user's HEAD, index, or working tree:

```sh
export GIT_INDEX_FILE=$(mktemp)
git add -A                                  # builds a throwaway index, honours .gitignore
TREE=$(git write-tree)
COMMIT=$(git commit-tree "$TREE" ${PREV:+-p $PREV} -m "msr snapshot $N")
git update-ref refs/mondspace/review/<session-id> "$COMMIT"
```

Diff a unit with `git diff <from> <to> -- <files>`. Cheap: git dedupes blobs.

Snapshot on every `batch_end`, and on a 5s debounce if no batch boundary arrives.

`msr gc` deletes `refs/mondspace/review/*` for closed sessions.

## 8. Ingestion

Hooks are short-lived external processes. **The hook must never block the agent.**

> Do **not** use a FIFO: a FIFO write blocks until a reader attaches, so the
> agent stalls whenever the reviewer isn't running.

Instead the hook does an atomic `O_APPEND` write of one JSON line to
`.mondspace-reviewer/<session>/events.jsonl` and exits 0 immediately. The
reviewer tails that file (fsnotify + poll fallback). Agent runs fine with no
reviewer attached; attaching later replays the whole log.

Ship `msr install-hooks` writing `.claude/settings.json`:

- `UserPromptSubmit` → `kind:"prompt"`. **Required** — the task prompt is what
  makes "has it drifted from what I asked?" answerable.
- `PostToolUse` matching `Write|Edit|MultiEdit` → `kind:"edit"`
- `PostToolUseFailure` → same, flagged failed
- `PostToolBatch` → `kind:"batch_end"` — the clustering and snapshot clock

Each hook shells to `msr ingest --kind=... ` reading the hook JSON on stdin. All
hooks exit 0 always; a broken reviewer must never affect the agent.

OpenCode adapter is a second `EventSource` emitting the same `Event`. **The
domain must not know which agent it is watching.**

## 9. Clustering, flags, narration

**Clustering.** Seal a unit at `batch_end`, or on 5s inactivity, or at 12 events.
Prefer under-segmenting: a unit dismissed in one keystroke is cheap; 200
micro-edits is unusable. Tool-call granularity is explicitly wrong.

**Flags** (deterministic, no model, run before any LLM call):

| Flag | Rule |
|---|---|
| `no-test` | unit touches non-test source, no `*_test.*` in same unit |
| `solo-iface` | new interface declared with < 2 implementations in repo |
| `new-dep` | import/require/go.mod addition |
| `swallowed-err` | `_ =` on an error-returning call, or empty catch |
| `large` | > 150 changed lines in one unit |
| `todo` | TODO/FIXME/XXX added |
| `public-api` | exported identifier changed or removed |

Flags are the highest-value-per-effort feature: they are what make you stop and
look, and they work with no model, offline, instantly. **Ship these before
headlines.**

**Narration** renders fixed slots so the eye scans instead of reads:

```
[47] auth/token.go, auth/port.go                    2 batches ago  ?
WHAT  extracted validation behind a TokenValidator interface
WHY   stated: "so we can swap the JWT lib later"
FLAG  no-test · solo-iface
```

Never stream prose. This is an **unread queue with a cursor**, not a live feed.
The agent outruns the human by construction — design for being behind. Nothing
scrolls away, nothing auto-advances, the cursor only moves on keypress.

If the summarizer is unreachable or slow, render the unit immediately with a
mechanical headline (files + change counts) and fill in the model headline later
if it arrives. **The queue never waits on the model.**

## 10. Interrogation

Answers come from the event log, unit diffs, the task prompt, and existing notes
— **not** by re-reading the repo. Bounded context keeps it fast enough to be
worth using and cheap enough to run locally.

Two scopes:
- `a` — ask about the current unit (narrow, fast)
- `A` — ask about the session so far (log + prompt + all headlines)

Answers cite unit IDs. Answers must not invent stated intent; if the log doesn't
contain it, say so.

## 11. Annotation

Five kinds, one keystroke each, optional inline text:

| Key | Kind | Meaning |
|---|---|---|
| `o` | ok | reviewed and accepted — advances cursor, suppresses re-narration |
| `?` | question | unresolved |
| `x` | objection | wrong choice |
| `d` | debt | accept now, fix later |
| `n` | note | for yourself |

`ok` doubles as "mark read" — that is what keeps the queue moving.

**Supersession.** When a later unit touches the same file+symbol as an annotated
unit, mark the earlier note `superseded_by: <unit-id>`. Surface it, never
auto-resolve, never delete. *"You objected to this 3 units ago; it has been
rewritten since"* is among the most valuable things this tool can say.

### Keybindings

```
j/k     next/prev unit          enter   expand/collapse
g/G     top/bottom              /       filter (flag, file, note kind)
tab     toggle unread-only      a/A     ask (unit / session)
o ? x d n  annotate             e       export        q  quit
```

## 12. Export

`msr export --format=md|json` produces:

1. **Review report** — units grouped by note kind, headlines, your text
2. **`debt` items** — as a task list
3. **Open agenda** — unresolved `?` and `x`, phrased as the next agent prompt

## 13. Test strategy — TDD, outside-in

Non-negotiable: **write the failing test first, at every level.**

- **Golden event logs** in `testdata/sessions/*.jsonl`, recorded from real runs.
  The `replay` source plus the `plain` presenter gives full end-to-end coverage
  with no agent, no terminal, no network, no model.
- **Clustering, flagging, supersession, export** are pure functions over the log
  — table-driven unit tests, no fakes needed.
- **TUI** is tested at the `Update(msg, model) → model` level. Pure. No golden
  screenshots.
- **Summarizer** is faked via the port in all tests except one contract test
  behind `-tags=integration`.
- **Git snapshotter** gets a real integration test against a temp repo: verify
  HEAD, index, and working tree are *unchanged* after snapshotting.

Cover explicitly: reviewer starts mid-session and catches up; reviewer absent
entirely (agent unaffected, log intact); malformed/partial JSONL line; summarizer
timeout; file deleted between snapshot and diff.

## 14. Milestones

Each milestone ends green, demoable, and shippable on its own.

- **M0 — Walking skeleton.** `replay` source → cluster → store → `plain`
  presenter. Mechanical headlines only. No model, no git, no TUI.
- **M1 — Real ingestion.** `msr ingest`, `install-hooks`, tailing, git snapshots,
  real diffs. Now usable against a live Claude Code session.
- **M2 — Flags + TUI.** Deterministic flags, bubbletea queue, navigation,
  annotations, supersession. **This is the first genuinely useful build.**
- **M3 — Headlines.** LM Studio summarizer, `WhySrc` discipline, async fill-in,
  graceful offline degradation.
- **M4 — Interrogation.** `a` / `A`.
- **M5 — Export.**

## 15. Acceptance criteria

1. Agent runs normally whether or not the reviewer is attached — measurably no
   added latency in the hook path.
2. Attaching mid-session reconstructs full state from the log alone.
3. Every unit diff is stable and viewable after the file has been rewritten.
4. No annotation is ever silently lost or auto-resolved.
5. `stated` vs `inferred` rationale is unambiguous in every rendering.
6. Whole app is exercisable via `replay` + `plain` with no agent and no network.
7. Domain packages import no adapter package. Enforce with a test.
