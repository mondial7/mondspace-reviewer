package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/mcp"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// runMCP serves the review over MCP on stdin/stdout, so a coding agent can pull
// what msr knows when it wants it (ADR 0031).
//
// It reads the store and nothing else: no git, no model, no port. That is what
// makes it safe to leave configured in an agent's client — the worst it can do
// is report what a human wrote.
func runMCP(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	// Errors must not reach stdout: stdout is the protocol channel, and one
	// line of usage text on it desynchronises the client for the whole session.
	fs.SetOutput(os.Stderr)
	out := fs.String("out", ".mondspace-reviewer", "store root directory (jsonl store)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	root := *out
	if !filepath.IsAbs(root) {
		abs, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		root = abs
	}

	server := mcp.NewServer("mondspace-reviewer", released(), mcp.Tools(storedReviews{root: root}))
	return server.Serve(ctx, stdin, stdout)
}

// openReview is what msr web leaves behind about the review a human has in
// front of them, so a separate process can find it without asking.
type openReview struct {
	TargetID string    `json:"target_id"`
	Title    string    `json:"title,omitempty"`
	Ref      string    `json:"ref,omitempty"`
	Repo     string    `json:"repo,omitempty"`
	At       time.Time `json:"at"`
}

// openFile is the pointer to the current review; seenFile is every review the
// reviewer has opened, so the workspace tools can name them.
//
// Two files because they answer different questions and change at different
// rates: one is rewritten, the other appended, which is the same split the
// store already makes between a narrative and its notes.
const (
	openFile = "open.json"
	seenFile = "reviews.jsonl"
)

// markOpen records which review is open. Failing to write it is not worth
// failing a page load over — the MCP server falls back to guessing.
func markOpen(root string, r openReview) error {
	if r.At.IsZero() {
		r.At = time.Now()
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}

	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, openFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(root, openFile)); err != nil {
		return err
	}

	// Also remembered, so a review that was open last week still has a name
	// when the workspace tools list it.
	f, err := os.OpenFile(filepath.Join(root, seenFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(body, '\n'))
	return err
}

// whatIsOpen is the review to answer about.
//
// The pointer when there is one, and otherwise the review most recently written
// to. Guessing beats refusing: the store is on disk whether or not msr web has
// ever run, and an agent asking "what does the reviewer want" is better served
// by the likeliest answer than by an error about a missing file.
func whatIsOpen(root string) (openReview, bool) {
	if body, err := os.ReadFile(filepath.Join(root, openFile)); err == nil {
		var r openReview
		if json.Unmarshal(body, &r) == nil && r.TargetID != "" {
			return r, true
		}
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return openReview{}, false
	}
	names := knownReviews(root)
	best, newest := openReview{}, time.Time{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		at, ok := lastWritten(filepath.Join(root, e.Name()))
		if !ok || !at.After(newest) {
			continue
		}
		newest = at
		best = names[e.Name()]
		best.TargetID, best.At = e.Name(), at
	}
	return best, best.TargetID != ""
}

// lastWritten is when a review directory was last added to, or false when it
// holds no review at all.
func lastWritten(dir string) (time.Time, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, false
	}
	var newest time.Time
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !holdsReview(e.Name()) {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest, !newest.IsZero()
}

// holdsReview says whether a file in a review directory is part of the review
// record. Events and units are the change itself, which the agent already has.
func holdsReview(name string) bool {
	switch name {
	case "notes.jsonl", "ask.jsonl", "signoff.json":
		return true
	}
	return strings.HasPrefix(name, "analysis-")
}

// knownReviews is every review msr web has opened, by id, last mention winning.
func knownReviews(root string) map[string]openReview {
	out := map[string]openReview{}
	body, err := os.ReadFile(filepath.Join(root, seenFile))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(body), "\n") {
		var r openReview
		if line == "" || json.Unmarshal([]byte(line), &r) != nil || r.TargetID == "" {
			continue
		}
		out[r.TargetID] = r
	}
	return out
}

// storedReviews reads reviews straight off disk. It is the whole of the MCP
// server's access to the world.
type storedReviews struct{ root string }

func (s storedReviews) Open() (mcp.Review, error) {
	open, ok := whatIsOpen(s.root)
	if !ok {
		return mcp.Review{}, fmt.Errorf(
			"no review is open, and nothing has been reviewed under %s yet — "+
				"start one with `msr web` in the repository you are working in", s.root)
	}
	return s.read(open), nil
}

func (s storedReviews) All() ([]mcp.Review, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("no review store at %s", s.root)
	}
	names := knownReviews(s.root)

	var out []mcp.Review
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, holds := lastWritten(filepath.Join(s.root, e.Name())); !holds {
			// A directory with events and no review is a recorded run nobody
			// has read yet. There is nothing here to tell an agent.
			continue
		}
		known := names[e.Name()]
		known.TargetID = e.Name()
		out = append(out, s.read(known))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// read assembles one review from the store.
func (s storedReviews) read(open openReview) mcp.Review {
	store := jsonl.New(s.root)
	review := mcp.Review{
		ID:    open.TargetID,
		Title: open.Title,
		Ref:   open.Ref,
		Repo:  open.Repo,
		Files: map[string]string{},
	}

	if loaded, err := store.Load(open.TargetID); err == nil {
		review.Notes, review.Exchanges = loaded.Notes, loaded.Exchanges
		for _, u := range loaded.Units {
			if len(u.Files) > 0 {
				review.Files[u.ID] = u.Files[0]
			}
		}
		if review.Title == "" {
			// A recorded run names itself by what it was asked to do.
			review.Title = loaded.Prompt
		}
	}
	if v, err := store.LoadSignoff(open.TargetID); err == nil {
		review.Signoff = v
	}
	for _, a := range usecase.Audits() {
		if got, err := store.LoadAnalysis(open.TargetID, a.Kind); err == nil && got.Done() {
			review.Analyses = append(review.Analyses, got)
		}
	}
	return review
}

// Compile-time proof that the store satisfies what the tools need.
var _ mcp.Workspace = storedReviews{}

// lastOpened is the review the pointer was last written for. Every SSE refresh
// re-loads the same target, and rewriting an identical pointer a few times a
// second would be churn for nothing.
var lastOpened struct {
	sync.Mutex
	id string
}

// recordOpen leaves the trace the MCP server follows.
//
// Best effort: a store that cannot be written to is a real problem, but it is
// not this one's to report — failing a page load because an agent-facing
// pointer could not be updated would be the wrong trade.
func recordOpen(entry targetEntry) {
	lastOpened.Lock()
	unchanged := lastOpened.id == entry.target.ID
	lastOpened.id = entry.target.ID
	lastOpened.Unlock()
	if unchanged {
		return
	}
	_ = markOpen(entry.out, openReview{
		TargetID: entry.target.ID,
		Title:    entry.target.Title,
		Ref:      entry.target.Ref,
		Repo:     filepath.Base(mustAbs(entry.repo)),
	})
}
