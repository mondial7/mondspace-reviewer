package web

import (
	"fmt"
	"time"
)

// Work is one thing the reviewer's model was asked to do. Every call takes
// seconds to minutes on a local model, so a page that shows nothing while one
// runs is indistinguishable from a broken one — this is what makes the waiting
// visible, wherever the reviewer happens to be looking.
type Work struct {
	ID      string
	Kind    string // narrate | describe | ask | reanalyse
	Target  string // which review it belongs to, so it can be retriggered
	Detail  string
	Started time.Time
	Millis  int64
	Done    bool
	Err     string
}

// Running reports whether this call is still in flight, and still plausibly so.
func (w Work) Running() bool { return !w.Done && !w.Stalled() }

// Stalled reports a call that has run past any reasonable ceiling. It is shown
// as stalled rather than running: the difference matters to a reviewer deciding
// whether to wait or ask again.
func (w Work) Stalled() bool {
	return !w.Done && time.Since(w.Started) > workCeiling
}

// Failed reports whether it finished badly, or gave up waiting.
func (w Work) Failed() bool { return (w.Done && w.Err != "") || w.Stalled() }

// Took renders how long it ran, for a finished call.
func (w Work) Took() string {
	if !w.Done {
		return ""
	}
	return fmt.Sprintf("%.1fs", float64(w.Millis)/1000)
}

// Why is what to show for a call that did not succeed.
func (w Work) Why() string {
	if w.Err != "" {
		return w.Err
	}
	if w.Stalled() {
		return "still running after " + workCeiling.String() + " — it may be stuck"
	}
	return ""
}

// Retry is where to send a reviewer who wants this done again. Only the kinds
// that can be re-triggered from a link return one.
func (w Work) Retry() string {
	switch w.Kind {
	case "narrate":
		return "/story/narrate?target=" + w.Target
	default:
		return ""
	}
}

// workCeiling is how long a call may run before the page stops claiming it is
// still thinking. Nothing is cancelled — the call may yet return — but a spinner
// that has been spinning for half an hour is telling the reviewer a lie, and a
// stuck one is indistinguishable from a slow one without it.
const workCeiling = 25 * time.Minute

// recentWork bounds the history kept in memory. A long session must not grow an
// unbounded list, or a status page that takes a second to render.
const recentWork = 24

// BeginWork records that the model has been asked something, and returns the
// function to call when it answers. The returned function is safe to call once.
func (s *Server) BeginWork(kind, target, detail string) func(error) {
	item := Work{
		ID: s.newID(), Kind: kind, Target: target,
		Detail: detail, Started: s.now(),
	}

	s.mu.Lock()
	s.work = append(s.work, item)
	if len(s.work) > recentWork {
		s.work = s.work[len(s.work)-recentWork:]
	}
	s.mu.Unlock()
	s.broadcast("work")

	return func(err error) {
		s.mu.Lock()
		for i := range s.work {
			if s.work[i].ID != item.ID {
				continue
			}
			s.work[i].Done = true
			s.work[i].Millis = s.now().Sub(item.Started).Milliseconds()
			if err != nil {
				s.work[i].Err = err.Error()
			}
			break
		}
		s.mu.Unlock()
		s.broadcast("work")
	}
}

// Work is the recent history, newest first.
func (s *Server) Work() []Work {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Work, 0, len(s.work))
	for i := len(s.work) - 1; i >= 0; i-- {
		out = append(out, s.work[i])
	}
	return out
}

// InFlight is how many calls are running now. It drives the indicator the rail
// shows on every page.
func (s *Server) InFlight() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := 0
	for _, w := range s.work {
		if w.Running() {
			n++
		}
	}
	return n
}
