# Contributing

Thanks for your interest! This is a small, deliberately-scoped tool. Contributions
are welcome — bug fixes, new event sources, flags, or presenter improvements.

## Ground rules

The project is built **strictly test-first**, one behaviour at a time. If you add
or change behaviour, add the failing test first, then the minimum code to pass.

- **Ports and adapters, dependencies inward only.** `domain`, `usecase`, and
  `port` must never import `internal/adapter/...`. This is enforced by
  [`arch/arch_test.go`](arch/arch_test.go) — if it fails, your change inverted a
  dependency.
- **All I/O behind a port.** `usecase` takes port interfaces, never concrete
  adapters. Clustering, flagging, supersession, summarization discipline, and
  export are pure functions over the log.
- **Table-driven tests, hand-written fakes.** No mocking framework.
- **Keep the domain agent-agnostic.** It must not know which agent it is watching.

## Developing

```sh
go test ./...          # must be green
go test -race ./...
go vet ./...            # must be clean
gofmt -l .             # must print nothing
```

Please keep each commit focused and name the behaviour it introduces.

### Working on the web app

The templates and the stylesheet are compiled into the binary with `go:embed`,
so editing one is a rebuild rather than a refresh. [air](https://github.com/air-verse/air)
does the rebuilding:

```sh
go install github.com/air-verse/air@latest
air                    # serves http://127.0.0.1:7777, rebuilds as you edit
```

`.air.toml` watches `go`, `html`, `css` and `js`, and deliberately does **not**
watch `.mondspace-reviewer/` — msr writes to its own store on every page load,
so watching it would mean every page view rebuilt the binary and restarted the
server underneath the page that caused it. A failed build leaves the last
working server running rather than dropping you onto a dead port.

Reload the page once air says it has restarted; there is no live-reload
injection, and there will not be — msr serves your source, and a development
convenience that opens a socket into the page is not worth it.

## Adding a new event source or presenter

Implement the relevant port (`port.EventSource`, `port.Presenter`, …) in a new
package under `internal/adapter/…` and wire it in `cmd/mondspace-reviewer`. The
`replay` source and `plain` presenter are the reference implementations, and let
you test the whole pipeline with no agent, terminal, or network.

## Reporting bugs / requesting features

Open an [issue](https://github.com/mondial7/mondspace-reviewer/issues). A recorded
`events.jsonl` that reproduces the problem is the most useful thing you can attach.
