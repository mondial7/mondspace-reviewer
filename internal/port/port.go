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

// Store persists the append-only session log.
type Store interface {
	AppendEvent(domain.Event) error
	AppendUnit(domain.Unit) error
	Load(sessionID string) (domain.Session, error)
}

// Presenter renders sealed units for review.
type Presenter interface {
	Present(domain.Unit) error
}
