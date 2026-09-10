# 0051 — An asset a browser has to ask about

- **Status:** proposed
- **Date:** 2026-09-10

## Context

The stylesheet and the scripts are embedded in the binary and served with
`http.FileServer`. An embedded file has no modification time, so those responses
went out with no `Last-Modified`, no `ETag` and no `Cache-Control` — nothing a
browser can revalidate against. Browsers then do what the spec allows: cache
heuristically and reuse without asking.

That is invisible until msr is upgraded. Then the page is this build's markup
and the stylesheet is last build's, and the result is a page with rules missing
from it — a control in the wrong corner, text that does not wrap, a disclosure
triangle nobody styled away. It looks exactly like a bug in the new build, it is
reported as one, and whoever wrote the new build cannot reproduce it, because
their browser fetched both halves at the same time.

## Decision

**Every asset goes out with an ETag and `Cache-Control: no-cache`.** Not
`no-store`: the browser keeps its copy and asks whether it is still good, so the
normal answer is a 304 with no body. On loopback that is a fraction of a
millisecond, and it is the difference between "cached until something evicts it"
and "cached until the program changes".

**The tag identifies the build, not the file.** Assets are embedded, so every
process running one binary serves identical bytes; what has to change the tag is
a different binary. It is a hash of the executable's path and its modification
time — the cheapest thing that has that property, computed once at start-up.

Hashing each file's contents would be more precise and buys nothing: the files
cannot change without the binary changing.

## Consequences

- Upgrading msr and reloading gets the new stylesheet, without anybody being
  told to hard-reload.
- One conditional request per asset per page load, answered 304. Six assets, all
  local.
- A build served from a rebuilt binary with identical contents gets a new tag
  and re-sends its assets once. That is a rebuild, not a reload, and it is a few
  hundred kilobytes.
- The reverse case — a browser holding an asset from a *newer* build than the
  page — is not addressed, because it cannot happen: one process serves both.
