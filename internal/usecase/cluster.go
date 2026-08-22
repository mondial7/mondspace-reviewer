package usecase

import "github.com/marcomondini/mondspace-reviewer/internal/domain"

// Cluster groups a session's events into reviewable units. It is a pure
// function over the event log.
func Cluster(sessionID string, events []domain.Event) []domain.Unit {
	return nil
}
