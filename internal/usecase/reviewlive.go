package usecase

import (
	"context"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

// ReviewLive drives a live session: it consumes events from the source, seals
// units incrementally, and snapshots the tree at each seal so every unit records
// the refs bracketing it. From is the prior snapshot, To the one taken when the
// unit sealed.
//
// It does not persist events: the agent's ingest hooks already own events.jsonl,
// which the source is tailing. Re-appending would feed the tail its own output.
func ReviewLive(ctx context.Context, src port.EventSource, snap port.Snapshotter, store port.Store, pres port.Presenter) error {
	ch, err := src.Events(ctx)
	if err != nil {
		return err
	}

	prev, err := snap.Snapshot(ctx, "start")
	if err != nil {
		return err
	}

	c := NewClusterer("")
	byID := map[string]domain.Event{}

	finalize := func(u domain.Unit) error {
		to, err := snap.Snapshot(ctx, u.ID)
		if err != nil {
			return err
		}
		u.From, u.To = prev, to
		prev = to

		diff, err := snap.Diff(ctx, u.From, u.To, u.Files)
		if err != nil {
			return err
		}
		u.Flags = Flags(u, diff)

		members := make([]domain.Event, 0, len(u.EventIDs))
		for _, id := range u.EventIDs {
			members = append(members, byID[id])
		}
		u.Headline = MechanicalHeadline(members)

		if err := store.AppendUnit(u); err != nil {
			return err
		}
		return pres.Present(u)
	}

	for e := range ch {
		byID[e.ID] = e
		if c.sessionID == "" {
			c.sessionID = e.SessionID
		}
		if u, ok := c.Push(e); ok {
			if err := finalize(u); err != nil {
				return err
			}
		}
	}
	if u, ok := c.Flush(); ok {
		if err := finalize(u); err != nil {
			return err
		}
	}
	return nil
}
