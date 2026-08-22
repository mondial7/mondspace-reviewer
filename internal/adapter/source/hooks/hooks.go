// Package hooks is an EventSource that tails the events.jsonl an agent's hooks
// append to. It catches up from the start of the log, then follows appends via
// fsnotify with a polling fallback, so a missed notification never stalls it.
package hooks

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// pollInterval bounds how long a follow can wait when fsnotify misses a write.
const pollInterval = 50 * time.Millisecond

type Source struct {
	path string
}

func New(path string) *Source {
	return &Source{path: path}
}

func (s *Source) Events(ctx context.Context) (<-chan domain.Event, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}

	out := make(chan domain.Event)
	go func() {
		defer close(out)
		defer f.Close()
		s.tail(ctx, f, out)
	}()
	return out, nil
}

// tail reads complete lines, decoding each into an event. At EOF it waits for a
// write notification or a poll tick, then resumes, carrying any partial line.
func (s *Source) tail(ctx context.Context, f *os.File, out chan<- domain.Event) {
	wake := s.watch(ctx)
	reader := bufio.NewReader(f)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var pending string
	for {
		chunk, err := reader.ReadString('\n')
		switch err {
		case nil:
			line := strings.TrimRight(pending+chunk, "\n")
			pending = ""
			if e, ok := decode(line); ok {
				select {
				case out <- e:
				case <-ctx.Done():
					return
				}
			}
		case io.EOF:
			pending += chunk
			select {
			case <-ctx.Done():
				return
			case <-wake:
			case <-ticker.C:
			}
		default:
			return
		}
	}
}

// watch returns a channel that pulses on write events for the file. If a
// watcher cannot be created, follow still works through the poll ticker.
func (s *Source) watch(ctx context.Context) <-chan struct{} {
	wake := make(chan struct{}, 1)
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return wake
	}
	if err := w.Add(s.path); err != nil {
		w.Close()
		return wake
	}
	go func() {
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-w.Events:
				if !ok {
					return
				}
				select {
				case wake <- struct{}{}:
				default:
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return wake
}

func decode(line string) (domain.Event, bool) {
	if strings.TrimSpace(line) == "" {
		return domain.Event{}, false
	}
	var e domain.Event
	if json.Unmarshal([]byte(line), &e) != nil {
		return domain.Event{}, false
	}
	return e, true
}
