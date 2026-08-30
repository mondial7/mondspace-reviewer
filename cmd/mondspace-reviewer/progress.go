package main

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// progress is what the terminal shows while msr is thinking.
//
// A local model call is seconds to minutes, and a terminal that prints nothing
// for a minute looks broken rather than busy. This is the difference between
// waiting and wondering.
//
// It writes to stderr and nowhere else — stdout belongs to whatever is reading
// it — and when nothing is watching it writes nothing at all. Not fewer frames:
// none. A spinner redirected to a file is a few hundred carriage returns in a
// log.
type progress struct {
	w    io.Writer
	live bool

	mu   sync.Mutex
	stop chan struct{}
	done sync.WaitGroup
}

// Braille, unlike the wordmark, which is plain ASCII. The banner persists in
// scrollback and in pasted bug reports; a spinner frame is erased a tenth of a
// second later, so a terminal that renders it badly loses nothing.
var frames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	eraseLine = "\x1b[2K\r"
	tick      = 90 * time.Millisecond
)

func newProgress(w io.Writer, live bool) *progress {
	return &progress{w: w, live: live}
}

// terminalProgress is the one the commands use: stderr, when a person is there
// to read it.
func terminalProgress() *progress {
	return newProgress(os.Stderr, os.Getenv("MSR_NO_BANNER") == "" && isTerminal(os.Stderr))
}

// step announces something that takes a while and returns the function that
// ends it. Call the returned function with the error, or nil.
func (p *progress) step(label string) func(error) {
	if !p.live {
		return func(error) {}
	}

	started := time.Now()
	p.mu.Lock()
	p.stop = make(chan struct{})
	stop := p.stop
	p.mu.Unlock()

	p.done.Add(1)
	go func() {
		defer p.done.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			fmt.Fprintf(p.w, "%s%s%s%s %s", eraseLine, tint, frames[i%len(frames)], reset, label)
			select {
			case <-stop:
				return
			case <-time.After(tick):
			}
		}
	}()

	return func(err error) {
		close(stop)
		p.done.Wait()
		if err != nil {
			fmt.Fprintf(p.w, "%s%s — %s%s\n", eraseLine, label, err, reset)
			return
		}
		fmt.Fprintf(p.w, "%s%s%s %s %s%s\n",
			eraseLine, tint, "·", label, took(started), reset)
	}
}

// count replaces the line with "3/7 described". It is for work that arrives in
// pieces, where the number is more use than a spinner.
func (p *progress) count(label string, done, total int) {
	if !p.live {
		return
	}
	fmt.Fprintf(p.w, "%s%s%d/%d%s %s", eraseLine, tint, done, total, reset, label)
	if done >= total {
		fmt.Fprintln(p.w)
	}
}

// took renders a duration the way someone waiting for it would say it.
func took(since time.Time) string {
	d := time.Since(since)
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
