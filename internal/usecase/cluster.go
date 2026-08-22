package usecase

import (
	"fmt"
	"time"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

// inactivityGap seals an open unit when the agent pauses this long between
// consecutive events.
const inactivityGap = 5 * time.Second

// maxUnitEvents caps a unit's size so a long uninterrupted run still segments.
const maxUnitEvents = 12

// Clusterer seals events into units incrementally, so a live review can act on
// each unit the moment it is sealed. It is pure: no I/O, no clock — sealing
// decisions come from the events' own timestamps.
type Clusterer struct {
	sessionID string
	open      []domain.Event
	sealed    int
}

func NewClusterer(sessionID string) *Clusterer {
	return &Clusterer{sessionID: sessionID}
}

// Push feeds one event. It returns a sealed unit when this event triggers a
// boundary (batch_end/prompt), a 5s inactivity gap, or the 12-event cap.
func (c *Clusterer) Push(e domain.Event) (domain.Unit, bool) {
	// batch_end is a boundary; prompt is a task statement, not a change. Both
	// seal the open unit without joining one.
	if e.Kind == domain.KindBatchEnd || e.Kind == domain.KindPrompt {
		return c.seal()
	}

	if len(c.open) > 0 && e.TS.Sub(c.open[len(c.open)-1].TS) >= inactivityGap {
		u, ok := c.seal()
		c.open = append(c.open, e)
		return u, ok
	}

	c.open = append(c.open, e)
	if len(c.open) == maxUnitEvents {
		return c.seal()
	}
	return domain.Unit{}, false
}

// Flush seals any trailing open unit at end of log.
func (c *Clusterer) Flush() (domain.Unit, bool) {
	return c.seal()
}

func (c *Clusterer) seal() (domain.Unit, bool) {
	if len(c.open) == 0 {
		return domain.Unit{}, false
	}
	c.sealed++
	u := newUnit(c.sessionID, c.sealed, c.open)
	c.open = nil
	return u, true
}

// Cluster groups a session's events into reviewable units. It is a pure
// function over the event log, built on the incremental Clusterer.
func Cluster(sessionID string, events []domain.Event) []domain.Unit {
	c := NewClusterer(sessionID)
	var units []domain.Unit
	for _, e := range events {
		if u, ok := c.Push(e); ok {
			units = append(units, u)
		}
	}
	if u, ok := c.Flush(); ok {
		units = append(units, u)
	}
	return units
}

func newUnit(sessionID string, ordinal int, members []domain.Event) domain.Unit {
	ids := make([]string, len(members))
	var files []string
	seen := map[string]bool{}
	for i, e := range members {
		ids[i] = e.ID
		for _, f := range e.Files {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	return domain.Unit{
		ID:        fmt.Sprintf("%s-u%03d", sessionID, ordinal),
		SessionID: sessionID,
		EventIDs:  ids,
		Files:     files,
		Sealed:    true,
	}
}
