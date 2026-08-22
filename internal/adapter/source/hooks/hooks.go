// Package hooks is an EventSource that tails the events.jsonl an agent's hooks
// append to. It catches up from the start of the log, then follows appends.
package hooks

import (
	"bufio"
	"context"
	"encoding/json"
	"os"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

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

		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			var e domain.Event
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			select {
			case out <- e:
			case <-ctx.Done():
				return
			}
		}
		// Follow: stay open until cancelled.
		<-ctx.Done()
	}()
	return out, nil
}
