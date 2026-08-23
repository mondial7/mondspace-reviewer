# 0011 — `solo-iface` as a pure diff heuristic, not a repo scan

- **Status:** accepted
- **Date:** 2026-08-23

## Context

SPEC §9 asks for a flag on "new interface declared with < 2 implementations
in repo". The literal reading needs to enumerate every type in the repository
and check its method set against the interface — a repo-wide scan. That was
previously deferred specifically because `usecase.Flags(u domain.Unit, d
domain.Diff)` is a pure function with no I/O (ADR 0001): `domain`, `usecase`,
and `port` import nothing from `internal/adapter/...`, and a repo scan needs
a filesystem or a git adapter reaching back into `usecase`, which is exactly
the dependency direction ADR 0001 forbids. Adding it would also mean `Flags`
stops being safely callable from `BuildFileUnits`'s tight per-file loop
without also plumbing a repo handle through every caller.

## Decision

Implement only the diff-local heuristic, not the repo-wide count. `Flags`
gains `hasSoloIface(d.Text)` (`internal/usecase/flag.go`): it flags a unit
when its diff has an *added* line matching `type X interface {`, collects
that interface's declared method names from the added lines between the
declaration and its closing `}`, and then flags **unless** the same diff also
adds a method with a receiver (`func (recv T) Name(...)`) whose name matches
one of those declared methods. It never inspects any file outside the diff
already being flagged, so it stays a pure function and keeps `Flags`'s
signature and callers (both `ReviewLive` and `BuildFileUnits`) unchanged.

This is explicitly a heuristic, not the spec's literal "< 2 implementations
in repo":

- **Over-flags:** an interface implemented in a *different* unit or diff (a
  common split when the interface and its implementation land in separate
  commits/units) still reads as solo, because this unit's diff alone has no
  matching method.
- **Under-flags:** any same-diff type that happens to add a method with a
  matching *name* — even an unrelated one on an unrelated receiver — is
  treated as "implemented" and the flag is suppressed. The heuristic checks
  name equality only, not that the receiver type actually satisfies the full
  interface.
- A removed interface, or one only present in unchanged context, is not
  "new" and is never flagged — the heuristic only looks at added lines.

Over/under-flagging is the accepted tradeoff (explicitly allowed by the
issue) for keeping the flag deterministic, instant, and free of any adapter
dependency.

## Consequences

- `solo-iface` joins the deterministic flag set with no new dependency and no
  change to `Flags`'s pure signature — `BuildFileUnits` and `ReviewLive` both
  get it for free.
- The flag's accuracy is bounded by what's visible in one diff. A reviewer
  should read `solo-iface` as "this diff alone doesn't show an implementation
  nearby", not as a repo-wide guarantee that the interface truly has fewer
  than two implementers.
- The flag order in `usecase.Flags` is `no-test, large, todo, new-dep,
  swallowed-err, public-api, solo-iface` — `solo-iface` is appended last. The
  order-pinning test (`TestFlagsOrderedAndCleanUnit`) still passes unchanged
  because its fixture diff declares no interface; a new
  `TestFlagsOrderedWithSoloIfaceLast` test pins the new tail position
  deliberately.
- A true repo-scanning "< 2 implementations" flag remains future work and
  would need a new adapter-backed port, not a change to this pure function.
