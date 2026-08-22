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

## Adding a new event source or presenter

Implement the relevant port (`port.EventSource`, `port.Presenter`, …) in a new
package under `internal/adapter/…` and wire it in `cmd/mondspace-reviewer`. The
`replay` source and `plain` presenter are the reference implementations, and let
you test the whole pipeline with no agent, terminal, or network.

## Reporting bugs / requesting features

Open an [issue](https://github.com/mondial7/mondspace-reviewer/issues). A recorded
`events.jsonl` that reproduces the problem is the most useful thing you can attach.
