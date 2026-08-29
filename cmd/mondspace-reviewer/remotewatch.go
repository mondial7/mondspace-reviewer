package main

import (
	"sync"
	"time"
)

// minFetchEvery is the floor on how often msr will fetch.
//
// This is a network call against somebody else's server, and the interval
// arrives from a form field. A typo must not turn msr into a hammer.
const minFetchEvery = 15 * time.Second

// remoteWatch is whether msr is fetching, and how often, as a value that can be
// changed while it runs (ADR 0026).
//
// The watcher used to capture the flag at start-up, which meant changing it
// needed a restart — and a setting you can only change by restarting is not a
// setting you can put on a status page.
type remoteWatch struct {
	mu    sync.RWMutex
	on    bool
	every time.Duration
}

func newRemoteWatch(on bool, every time.Duration) *remoteWatch {
	w := &remoteWatch{}
	w.Set(on, every)
	return w
}

// Set changes what the watcher does from its next tick.
func (w *remoteWatch) Set(on bool, every time.Duration) {
	if every < minFetchEvery {
		every = minFetchEvery
	}
	w.mu.Lock()
	w.on, w.every = on, every
	w.mu.Unlock()
}

// Get is what the watcher should do now.
func (w *remoteWatch) Get() (bool, time.Duration) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.on, w.every
}
