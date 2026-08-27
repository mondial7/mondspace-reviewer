package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/web"
	gitsnap "github.com/mondial7/mondspace-reviewer/internal/adapter/snapshot/git"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// A pin is where a live review stops (ADR 0020).
//
// The live target follows HEAD, and before this it also followed the working
// tree — so a reviewer's page changed under them whenever the agent saved a
// file. That is right for a status display and wrong for a review: you cannot
// form a judgement about something that is being edited while you read it.
//
// So the far end is pinned to a snapshot taken when the review was opened, and
// work arriving after that queues up visibly instead of arriving silently.
type pin struct {
	ref domain.SnapshotRef
	// head is the commit the pin was taken against. When HEAD moves the pin is
	// behind history rather than ahead of it, and means nothing any more.
	head string
	at   time.Time
}

var (
	pinsMu sync.RWMutex
	pins   = map[string]pin{}
)

// pinFor is where a live review currently stops, taking a snapshot the first
// time and whenever HEAD has moved out from under the previous one.
//
// Snapshotting costs a `git add -A` against a throwaway index, so it happens
// once per review rather than once per request: every page load and every live
// refresh after the first reuses the pin, which is also what makes the page
// hold still.
func pinFor(ctx context.Context, snap *gitsnap.Snapshotter, targetID, head string) (pin, error) {
	pinsMu.RLock()
	have, ok := pins[targetID]
	pinsMu.RUnlock()
	if ok && have.head == head {
		return have, nil
	}
	return repin(ctx, snap, targetID, head)
}

// repin moves a live review's far end to the working tree as it is now.
func repin(ctx context.Context, snap *gitsnap.Snapshotter, targetID, head string) (pin, error) {
	ref, err := snap.Snapshot(ctx, "review "+targetID)
	if err != nil {
		return pin{}, err
	}
	p := pin{ref: ref, head: head, at: time.Now()}

	pinsMu.Lock()
	pins[targetID] = p
	pinsMu.Unlock()
	return p, nil
}

// pinnedAt reports where a live review stops, without taking a snapshot. The
// watcher uses it: it must be able to ask "has anything arrived since" without
// moving the answer.
func pinnedAt(targetID string) (pin, bool) {
	pinsMu.RLock()
	defer pinsMu.RUnlock()
	p, ok := pins[targetID]
	return p, ok
}

// liveActions wires the two ways of resolving work that arrived mid-review.
// Carrying on reading is the third, and needs nothing from the server.
func liveActions() (web.IncludeFunc, web.SplitFunc) {
	include := func(ctx context.Context, targetID string) error {
		entry, known := lookupTarget(targetID)
		if !known {
			return fmt.Errorf("no such review %q", targetID)
		}
		snap := gitsnap.New(entry.repo, targetID)
		head, err := currentHead(ctx, snap)
		if err != nil {
			return err
		}
		// Moving the pin forward is the whole operation: the review is rebuilt
		// against it on the next load, and now covers the new work too.
		_, err = repin(ctx, snap, targetID, head)
		return err
	}

	split := func(ctx context.Context, targetID string) (string, error) {
		entry, known := lookupTarget(targetID)
		if !known {
			return "", fmt.Errorf("no such review %q", targetID)
		}
		was, ok := pinnedAt(targetID)
		if !ok {
			return "", fmt.Errorf("this review has no starting point to measure from")
		}

		snap := gitsnap.New(entry.repo, targetID)
		head, err := currentHead(ctx, snap)
		if err != nil {
			return "", err
		}
		now, err := repin(ctx, snap, targetID, head)
		if err != nil {
			return "", err
		}

		// Only what arrived is an ordinary range, which is what lets it be
		// opened, narrated and annotated like anything else.
		target := usecase.RangeTarget(entry.repo, "since you started reading", was.ref, now.ref)
		registerTarget(target.ID, targetEntry{target: target, repo: entry.repo, out: entry.out})
		return target.ID, nil
	}

	return include, split
}

// currentHead is the commit the working tree is measured against.
func currentHead(ctx context.Context, snap *gitsnap.Snapshotter) (string, error) {
	head, err := snap.RecentCommits(ctx, 1)
	if err != nil {
		return "", err
	}
	if len(head) == 0 {
		return "", nil // a repository with no commits yet
	}
	return head[0].Hash, nil
}

// saveSignoff records that a reviewer has finished with a target, in whichever
// store that target belongs to. A workspace spans repositories, and each keeps
// its own store, so the verdict follows the target rather than the process.
func saveSignoff() web.SignoffFunc {
	return func(_ context.Context, v domain.Signoff) error {
		entry, known := lookupTarget(v.TargetID)
		if !known {
			return fmt.Errorf("no such review %q", v.TargetID)
		}
		store, ok := any(jsonl.New(entry.out)).(signoffStore)
		if !ok {
			return fmt.Errorf("this store cannot remember a verdict")
		}
		return store.SaveSignoff(v)
	}
}

// loadSignoff reads back a target's verdict. A store that cannot answer reads
// as "not reviewed", which is the safe way to be wrong: it invites another look
// rather than claiming one happened.
func loadSignoff() web.SignoffOf {
	return func(targetID string) domain.Signoff {
		entry, known := lookupTarget(targetID)
		if !known {
			return domain.Signoff{}
		}
		store, ok := any(jsonl.New(entry.out)).(signoffStore)
		if !ok {
			return domain.Signoff{}
		}
		v, err := store.LoadSignoff(targetID)
		if err != nil {
			return domain.Signoff{}
		}
		return v
	}
}
