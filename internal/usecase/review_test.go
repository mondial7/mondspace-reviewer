package usecase_test

import (
	"context"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

// fakeSource replays a fixed slice of events, honouring context cancellation.
type fakeSource struct{ events []domain.Event }

func (f *fakeSource) Events(ctx context.Context) (<-chan domain.Event, error) {
	ch := make(chan domain.Event)
	go func() {
		defer close(ch)
		for _, e := range f.events {
			select {
			case ch <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// fakeStore records everything it is told to persist.
type fakeStore struct {
	events []domain.Event
	units  []domain.Unit
}

func (s *fakeStore) AppendEvent(e domain.Event) error { s.events = append(s.events, e); return nil }
func (s *fakeStore) AppendUnit(u domain.Unit) error   { s.units = append(s.units, u); return nil }
func (s *fakeStore) Load(string) (domain.Session, error) {
	return domain.Session{}, nil
}

// fakePresenter records the units it is shown, in order.
type fakePresenter struct{ units []domain.Unit }

func (p *fakePresenter) Present(u domain.Unit) error { p.units = append(p.units, u); return nil }

func TestReviewStoresEveryEventAndUnit(t *testing.T) {
	events := []domain.Event{
		ev("e1", domain.KindEdit, "a.go"),
		ev("e2", domain.KindWrite, "b.go"),
		ev("e3", domain.KindBatchEnd),
		ev("e4", domain.KindEdit, "c.go"),
		ev("e5", domain.KindBatchEnd),
	}
	src := &fakeSource{events: events}
	store := &fakeStore{}
	pres := &fakePresenter{}

	if err := usecase.Review(context.Background(), src, store, pres); err != nil {
		t.Fatalf("Review: %v", err)
	}

	if len(store.events) != 5 {
		t.Errorf("store got %d events, want 5", len(store.events))
	}
	if len(store.units) != 2 {
		t.Errorf("store got %d units, want 2", len(store.units))
	}
	if len(pres.units) != 2 {
		t.Errorf("presenter got %d units, want 2", len(pres.units))
	}
}
