// Package port declares the interfaces through which the usecase layer reaches
// the outside world. Adapters implement these; the usecase depends only on them.
package port

import (
	"context"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
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

// Summarizer turns a unit and its diff into a headline. It must degrade
// gracefully: callers fall back to the mechanical headline on any error.
type Summarizer interface {
	Headline(ctx context.Context, u domain.Unit, d domain.Diff) (domain.Headline, error)
}

// Store persists the append-only session log.
type Store interface {
	AppendEvent(domain.Event) error
	AppendUnit(domain.Unit) error
	AppendNote(domain.Note) error
	Load(sessionID string) (domain.Session, error)
}

// Presenter renders sealed units for review.
type Presenter interface {
	Present(domain.Unit) error
}
