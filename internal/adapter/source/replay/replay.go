// Package replay is an EventSource that replays a recorded JSONL log from
// testdata. It is the test source: the whole app is exercisable through it with
// no agent, terminal, or network.
package replay

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

// Events opens the recorded log and streams its events in order, closing the
// channel when the file is exhausted.
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
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				continue
			}
			select {
			case out <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
