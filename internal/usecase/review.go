package usecase

import (
	"context"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

// Review drains the event source into the store, clusters the log into units,
// attaches a mechanical headline to each, and persists and presents them in
// seal order. It never waits on a model.
func Review(ctx context.Context, src port.EventSource, store port.Store, pres port.Presenter) error {
	ch, err := src.Events(ctx)
	if err != nil {
		return err
	}

	var events []domain.Event
	byID := map[string]domain.Event{}
	sessionID := ""
	for e := range ch {
		if err := store.AppendEvent(e); err != nil {
			return err
		}
		events = append(events, e)
		byID[e.ID] = e
		if sessionID == "" {
			sessionID = e.SessionID
		}
	}

	for _, u := range Cluster(sessionID, events) {
		members := make([]domain.Event, 0, len(u.EventIDs))
		for _, id := range u.EventIDs {
			members = append(members, byID[id])
		}
		u.Headline = MechanicalHeadline(members)

		if err := store.AppendUnit(u); err != nil {
			return err
		}
		if err := pres.Present(u, members); err != nil {
			return err
		}
	}
	return nil
}
