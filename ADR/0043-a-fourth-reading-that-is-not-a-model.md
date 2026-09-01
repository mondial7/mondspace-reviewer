# 0043 — A fourth reading that is not a model

- **Status:** accepted
- **Date:** 2026-09-01

## Context

msr had three readings of a change and all three were a model: the story, the
security pass, the breaking-change pass. Everything on the page was therefore
either `stated` — the agent's own words — or `inferred`, and `inferred` means
"check this before you believe it".

It also had a fourth reading nobody called one. `no-test`, `swallowed-err`,
`public-api` are a hand-rolled static analyser: deterministic, rule-named,
reproducible, every property that makes a finding trustworthy. They were
rendered as chips beside a filename and could not be dismissed, counted, or
rolled up.

Meanwhile every reviewer already has real analysers installed, and msr was
ignoring them.

## Decision — not SonarQube

Recorded so it is not re-opened. The scanner has no local-only mode: it is a
client reporting to a server that stores and processes results, so it is a JVM
and a server process beside a 4 GB model. The free Community Build analyses a
single branch; branch and pull-request analysis — which is exactly diff-scoped
review — is a paid edition. And it runs at CI cadence, not inside a five-second
poll.

The one idea worth taking is "new code": findings scoped to what changed. That
is below.

## Decision

**`reported` is a third class beside `stated` and `inferred`** (ADR 0003). It
names its tool and its rule, it is the same answer every time it is asked, and
it is the only one of the three a reviewer can act on without checking it first.
It has its own colour in every theme, because a distinction carried only by a
word is a distinction nobody reads.

**msr ships no analysers and installs none.** An analyser is a name, a command
that proves it is installed, a command that runs it, and how to read what comes
back — so adding one is a block of `.msr.toml` rather than a commit. Nine
defaults for tools people actually have; each used only if its detect command
exits zero. Detection is that command exiting zero and not a `PATH` lookup: a
broken install, a wrapper pointing at a deleted virtualenv, and a binary for the
wrong architecture all pass a lookup and all fail this.

**Two decoders, and only two.** SARIF, because it is what the analyser world
settled on and it is what makes configuration-alone work. And `file:line:col:
message`, because everything that does not emit SARIF emits that.

**Findings are scoped to what the change caused.** Two answers to the same
question:

- The cheap one, on every scan: intersect the finding's line with the lines the
  change added. Fast enough for a five-second poll and right most of the time.
- The accurate one, on demand: `git archive` the base into a temp directory, run
  the same tools over the same files as they were, set-diff. This catches the
  case the cheap one cannot — an import that is now unused is not on an added
  line and is entirely this change's doing.

`git archive`, not `git worktree add`: a worktree registers itself under `.git`
and leaves one behind on every crash, and msr's claim is that it never writes to
the repository.

Pre-existing findings are counted and folded away, never dropped. Silently
having none and silently hiding four hundred look identical from the page, and
one of them means the tool is broken.

**It never interrupts anything.** Debounced two seconds after the last observed
change, because an agent writes a file several times in a row and a linter over
a half-written file reports a syntax error as a finding — four times in twenty
seconds. One tool gets thirty seconds and is then abandoned. A tool that breaks
is reported once, on the settings page, and never again.

**Cached on (tool, version, command, file contents).** A tool that was upgraded
has different opinions; a file that moved is the only reason to ask again. It
deliberately does not cover the tool's own config file — editing `.golangci.yml`
needs a restart, which is a smaller cost than stat'ing a guessed list of config
filenames on every tick and still missing the one nobody thought of.

**Not a fourth card.** The three model cards each answer a question about the
whole change; this answers "is anything mechanically wrong with the file I am
reading", so it rolls up against the file, with one summary line for the review
and a filter to the files that have something. A finding can be dismissed like a
model's and stays dismissed.

**msr's own flags are absorbed.** One producer, `Flags`, and two renderings. The
ones that mean stop — `swallowed-err`, `public-api`, `failed` — become findings
beside gosec's. The rest stay chips and a tally, because `no-test` is true of
half the files in a normal review and making it a finding would put "12 of 14
files have findings" on every page (ADR 0041).

**`reported_findings` is its own MCP tool**, beside `model_findings`. An agent
handed a model's guess and a linter's output in one list, in one voice, has no
way to tell which one it can act on.

## Consequences

- A reviewer who has golangci-lint gets its answers in the review, scoped to
  what they changed, without configuring anything.
- A reviewer who has nothing installed sees no mention of any of this. That is
  the default and it is silent.
- msr now starts subprocesses it did not write. They are capped, debounced,
  cached, and confined to the repository being reviewed, and every one of those
  properties has a test — but it is a larger surface than a review tool had
  before, and it is worth saying so plainly.
- The findings are written to the store as well as held in memory, which is a
  cache of something reproducible. The alternative was the MCP server starting
  nine linters to answer one question, in a process that may not have them.
- The accurate path is minutes on a large repository and is the one thing here a
  reviewer has to ask for.
- One dependency added: `github.com/BurntSushi/toml`, with no transitive ones.
