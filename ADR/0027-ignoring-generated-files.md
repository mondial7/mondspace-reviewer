# 0027 — Leaving files out, visibly

- **Status:** accepted
- **Date:** 2026-08-29
- **Part of:** [issue #19](https://github.com/mondial7/mondspace-reviewer/issues/19)

## Context

An agent's diff is full of things nobody reviews: `go.sum`, `package-lock.json`,
protobuf output, generated mocks, `vendor/`. Each one became a review unit,
spent a model call being described, inflated every statistic, and diluted the
story around the two files that actually changed.

## Decision

**`.msrignore` at the repository root, in gitignore syntax.**

### git applies the rules

msr does not implement gitignore matching. The syntax has enough corners —
anchoring, directory-only matches, negation, `**` — that a second
implementation would quietly differ from the one every developer already knows,
and the difference would show up as files mysteriously present or absent.

So the rules go to `git check-ignore` with `core.excludesFile` pointed at
`.msrignore`. `--no-index` is what makes it work at all: to git a *tracked* file
is never "ignored", so without it every path in a review comes back clean. One
process handles the whole file list, and git reports **which pattern matched
which path** — which is what lets the page explain itself.

### Nothing is hidden unless asked for

There are **no built-in defaults**. A review tool that hides files by default is
a review tool you cannot trust, and the specific case makes it concrete: hiding
lockfiles would mask exactly the supply-chain change msr already has a
`new-dep` flag to draw attention to. Defaults would have contradicted an
existing feature.

### And what is hidden is always said

This is the safety argument the whole feature rests on. When rules are active
the page says **how many files were left out, which ones, and the pattern that
did it**, with one link to show them anyway. A surprising absence can always be
traced to a line in a file rather than to the tool.

A unit is only set aside when *every* file in it matched. Units can cover
several files, and dropping one because a single generated file was in it would
take the reviewer's own work with it.

## Consequences

- The rules apply to recorded sessions too. An agent's run is precisely where
  generated files pile up.
- Turning the rules off has to invalidate the loaded-review cache: a review
  built under the old rules would otherwise answer for one built under the new.
  `Server.Forget` exists for that, and rebuilds the open review as well as
  clearing the cache — it is the field itself, not a cache entry, so it would
  otherwise have been the one thing still showing the old rules.
- `msr` still never writes to your repository, so it cannot create a
  `.msrignore` for you. The README carries one worth starting from.
