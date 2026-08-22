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

// Cluster groups a session's events into reviewable units. It is a pure
// function over the event log.
//
// A unit is sealed at a batch_end boundary. Boundary events are not members
// of any unit.
func Cluster(sessionID string, events []domain.Event) []domain.Unit {
	var units []domain.Unit
	var open []domain.Event

	seal := func() {
		if len(open) == 0 {
			return
		}
		units = append(units, newUnit(sessionID, len(units)+1, open))
		open = nil
	}

	for _, e := range events {
		// batch_end is a boundary; prompt is a task statement, not a change.
		// Both seal the open unit without joining one.
		if e.Kind == domain.KindBatchEnd || e.Kind == domain.KindPrompt {
			seal()
			continue
		}
		if len(open) > 0 && e.TS.Sub(open[len(open)-1].TS) >= inactivityGap {
			seal()
		}
		open = append(open, e)
		if len(open) == maxUnitEvents {
			seal()
		}
	}
	seal()

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
