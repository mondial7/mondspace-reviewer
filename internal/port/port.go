// Package port declares the interfaces through which the usecase layer reaches
// the outside world. Adapters implement these; the usecase depends only on them.
package port

import (
	"context"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// EventSource emits observed agent events until ctx is cancelled. It must never
// block the agent.
type EventSource interface {
	Events(ctx context.Context) (<-chan domain.Event, error)
}

// Snapshotter records tree states and diffs them without touching the user's
// HEAD, index, or working tree.
type Snapshotter interface {
	Snapshot(ctx context.Context, label string) (domain.SnapshotRef, error)
	Diff(ctx context.Context, from, to domain.SnapshotRef, paths []string) (domain.Diff, error)
}

// Summarizer turns a unit and its diff into a headline, and answers questions
// from a bounded context. It must degrade gracefully: callers fall back to the
// mechanical headline, or surface an offline notice, on any error.
type Summarizer interface {
	Headline(ctx context.Context, u domain.Unit, d domain.Diff) (domain.Headline, error)
	Answer(ctx context.Context, question string, c domain.AskContext) (string, error)
}

// JSONSchema is a named JSON Schema a model's reply must conform to. Schema is
// the schema document itself, as it would be written in JSON.
type JSONSchema struct {
	Name   string
	Schema map[string]any
}

// SchemaAnswerer is an optional capability of a Summarizer: an endpoint that can
// constrain the reply to a schema, so structure is guaranteed by the server
// rather than parsed hopefully from prose. Callers must type-assert for it and
// keep working without it — not every endpoint or model supports it.
type SchemaAnswerer interface {
	AnswerSchema(ctx context.Context, question string, c domain.AskContext, schema JSONSchema) (string, error)
}

// TokenUsage is what a summarizer has spent since the process started. Reasoning
// is broken out because on a thinking model it is most of the bill, and it is
// the number that decides whether a context window is big enough.
type TokenUsage struct {
	Calls      int
	Failures   int
	Prompt     int
	Completion int
	Reasoning  int
	Millis     int64
}

// UsageReporter is an optional capability of a Summarizer: reporting what it has
// spent. A summarizer without it simply has nothing to show.
type UsageReporter interface {
	Usage() TokenUsage
}

// EngineReporter is an optional capability of a Summarizer: naming what
// actually answered the most recent call, and whether that was the engine the
// job was routed to or the one standing behind it.
//
// It exists so a result can be labelled with the thing that produced it. An
// answer from a small local model presented with the same confidence as one
// from a much larger engine is worse than no answer, because the reviewer has
// no way to know how much of it to believe (ADR 0039).
type EngineReporter interface {
	Answered() (engine domain.Engine, fellBack bool, why string)
}

// Pinger is an optional capability of a Summarizer: answering, right now,
// whether its endpoint is reachable. Liveness is a live question — a probe at
// start-up says nothing about the model five minutes later.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Store persists the append-only session log.
type Store interface {
	AppendEvent(domain.Event) error
	AppendUnit(domain.Unit) error
	AppendNote(domain.Note) error
	Load(sessionID string) (domain.Session, error)
}

// Presenter renders sealed units for review. It receives the unit's member
// events so a verbose presenter can show what was clustered into it.
type Presenter interface {
	Present(u domain.Unit, events []domain.Event) error
}
