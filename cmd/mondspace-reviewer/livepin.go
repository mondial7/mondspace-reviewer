package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/web"
	gitsnap "github.com/mondial7/mondspace-reviewer/internal/adapter/snapshot/git"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
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

// runAnalysis runs one audit over one target and stores what it found.
//
// The model call is per audit and shares nothing with the others: each is a
// fresh question about the same diff, which is the point of having more than
// one (ADR 0024).
func runAnalysis(pool *agentPool, model string) web.RunAnalysisFunc {
	return func(ctx context.Context, targetID string, kind domain.AnalysisKind) (err error) {
		started := time.Now()
		// Every way of failing gets recorded, not only the model call. A card
		// that goes quiet with nothing in the log is indistinguishable from one
		// nobody ran, and for a security card that is the worst way to be wrong.
		defer func() {
			if err != nil {
				handlerRef().Record(web.AuditEntry{
					SessionID: targetID, Action: string(kind), Model: model,
					Millis: time.Since(started).Milliseconds(),
					Failed: true, Detail: err.Error(),
				})
			}
		}()

		audit, known := usecase.AuditFor(kind)
		if !known {
			return fmt.Errorf("no such analysis %q", kind)
		}
		entry, found := lookupTarget(targetID)
		if !found {
			return fmt.Errorf("no such review %q", targetID)
		}

		units, diffs, err := unitsFor(ctx, entry)
		if err != nil {
			return err
		}

		// What this audit said last time, so it can be asked about the
		// difference rather than about the whole change again (ADR 0038). A
		// rerun would otherwise produce fresh findings that no longer match the
		// old ones by text, and take the reviewer's dismissals with them
		// (ADR 0030).
		store := jsonl.New(entry.out)
		earlier, _ := store.LoadAnalysis(targetID, kind)

		// An audit is a judgement about a change, so it goes where the routing
		// table sends judgement (ADR 0039).
		reader := pool.For(domain.Narration)
		result, err := usecase.RunAuditIncremental(ctx, reader,
			audit, targetID, units, diffs, earlier)
		if err != nil {
			return err
		}
		result.Model = model
		result.Engine, result.Fallback = answeredBy(reader)
		result = usecase.CarryJudgements(result, earlier)

		how := "read in full"
		if result.Partial() {
			how = fmt.Sprintf("re-read %d of %d files", result.Read, result.Of)
		}
		if result.Fallback {
			how += ", on the fallback engine"
		}
		handlerRef().Record(web.AuditEntry{
			SessionID: targetID, Action: string(kind), Model: model,
			Millis: time.Since(started).Milliseconds(),
			Detail: fmt.Sprintf("%s: %d finding(s), %s — %s",
				audit.Title, len(result.Findings), how, usecase.Brief(result.Verdict, 60)),
		})

		if err := store.SaveAnalysis(result); err != nil {
			// It ran and the reviewer will see it; it simply will not survive a
			// restart.
			return fmt.Errorf("ran, but could not be saved: %w", err)
		}
		return nil
	}
}

// answeredBy names the engine behind a result, so a card can say where it came
// from and whether that was the engine the job was routed to (ADR 0039).
//
// A summarizer that does not account for itself reports nothing, which the page
// reads as "unattributed" rather than as a claim about either engine.
func answeredBy(sum port.Summarizer) (domain.Engine, bool) {
	reporter, ok := sum.(port.EngineReporter)
	if !ok {
		return "", false
	}
	engine, fellBack, _ := reporter.Answered()
	return engine, fellBack
}

// analysisOf reads back what an audit found. A store that cannot answer reads
// as "never run", which invites running it rather than showing nothing.
//
// The result for the diff on screen is preferred, and the most recent result is
// the fallback. That order is the whole of ADR 0037: a reviewer who audited a
// review, left, and came back to it unchanged is shown the answer they already
// paid for, and a reviewer whose code has moved since is shown the old answer
// with the card saying so.
func analysisOf() web.AnalysisOf {
	return func(targetID string, kind domain.AnalysisKind, print string) domain.Analysis {
		entry, known := lookupTarget(targetID)
		if !known {
			return domain.Analysis{}
		}
		store := jsonl.New(entry.out)
		if got, err := store.LoadAnalysisAt(targetID, kind, print); err == nil && got.Done() {
			return got
		}
		got, err := store.LoadAnalysis(targetID, kind)
		if err != nil {
			return domain.Analysis{}
		}
		return got
	}
}

// logLimit is how much history the card shows. Enough to see where you are in
// a morning's work, not a substitute for `git log`.
// The history card is the one place a checkpoint is chosen from, so it carries
// the history rather than the newest handful. The rows are small and the card
// scrolls; what costs is the git call, and one call for five hundred commits is
// the same call as one for thirty.
const logLimit = 500

// buildLog assembles the git log card for whichever review is open: recent
// history, which commits the upstream can already reach, and which have been
// signed off (issue #18).
func buildLog(fallback string) web.LogOf {
	signedOff := loadSignoff()

	return func(targetID, reviewingRef string) web.LogView {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// The repository the review belongs to, not the one msr was started
		// in: a workspace spans several and the picker moves between them.
		repo := fallback
		if entry, known := lookupTarget(targetID); known {
			repo = entry.repo
		}

		snap := gitsnap.New(repo, "log")
		state, _ := snap.Remote(ctx)

		// Both refs, so a colleague's commit appears above your own work rather
		// than only as a number saying you are behind.
		commits, err := snap.CommitsAcross(ctx, logLimit, "HEAD", state.Upstream)
		if err != nil || len(commits) == 0 {
			return web.LogView{}
		}

		onRemote := snap.ReachableFrom(ctx, state.Upstream, 2000)
		local := map[string]bool{}
		if state.Upstream != "" {
			local = snap.ReachableFrom(ctx, "HEAD", 2000)
		}

		// A commit's review is signed off under the target id derived from its
		// range, so the answer comes from the same index the picker uses.
		reviewed := map[string]bool{}
		for _, c := range commits {
			if id, ok := targetIDForCommit(c.Hash); ok && signedOff(id).Done() {
				reviewed[c.Hash] = true
			}
		}

		entries := usecase.BuildLogAcross(commits, reviewingRef, onRemote, reviewed, local, time.Now())

		// In compare mode the card has to say which two: a range is two commits
		// and everything between them, and "the one being reviewed" is one.
		if entry, known := lookupTarget(targetID); known && entry.target.Kind == domain.TargetRange {
			entries = usecase.MarkRange(entries, entry.target.From.Commit, entry.target.To.Commit)
		}

		return web.LogView{Entries: entries, Remote: state}
	}
}

// targetIDForCommit finds the review id for a commit, if that commit is one of
// the targets on offer.
func targetIDForCommit(hash string) (string, bool) {
	targetsMu.RLock()
	defer targetsMu.RUnlock()
	for id, entry := range targetIndex {
		if entry.target.Kind == domain.TargetCommit && entry.target.To.Commit == hash {
			return id, true
		}
	}
	return "", false
}

// branchesOf lists every remote branch with how far it has drifted, for the
// wider view (ADR 0026).
func branchesOf(fallback string) web.BranchesOf {
	return func(targetID string) web.BranchView {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		repo := fallback
		if entry, known := lookupTarget(targetID); known {
			repo = entry.repo
		}

		snap := gitsnap.New(repo, "branches")
		branches, err := snap.Branches(ctx, "")
		if err != nil || len(branches) == 0 {
			return web.BranchView{}
		}
		base := ""
		if len(branches) > 0 {
			base = branches[0].Base
		}
		return web.BranchView{Base: base, Branches: branches}
	}
}

// watchRemote keeps an eye on what the rest of the team is pushing.
//
// Fetching is the one thing msr does that talks to the network and writes to
// the repository, so it happens only when asked for. Without it this still
// works: it reports whatever the reviewer's own last `git fetch` or `git pull`
// brought in, which is honest, just as fresh as their last one (ADR 0025).
func watchRemote(ctx context.Context, handler *web.Server, repo string, watch *remoteWatch) {
	snap := gitsnap.New(repo, "remote")
	var prev domain.RemoteState

	for {
		fetch, every := watch.Get()
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}

		if fetch {
			// A failed fetch is not worth reporting: it is usually a laptop
			// that went to sleep, or a network that is not there. The next one
			// catches up.
			if err := snap.Fetch(ctx); err == nil {
				prev.Fetched = time.Now()
			}
		}

		state, err := snap.Remote(ctx)
		if err != nil {
			continue
		}
		state.Fetched = prev.Fetched

		// Silent on the first look, so opening a page never announces what was
		// already there.
		handler.Pulse(usecase.RemoteNews(prev, state))
		prev = state
	}
}

// conversationsOf reads back what was asked about a review in an earlier run.
//
// A conversation is part of the review, not of the process that hosted it: it
// is written to the store the target belongs to, and comes back when you return
// to that review (issue #19).
func conversationsOf() web.ConversationsOf {
	return func(targetID string) []domain.Exchange {
		entry, known := lookupTarget(targetID)
		if !known {
			return nil
		}
		sess, err := jsonl.New(entry.out).Load(targetID)
		if err != nil {
			return nil
		}
		return sess.Exchanges
	}
}

// showAll is whether the reviewer has asked to see the files .msrignore keeps
// out. It is a session-wide switch rather than a per-request one because the
// review is built by a loader that the request does not reach into, and msr is
// one reviewer at one screen (ADR 0027).
var (
	showAllMu sync.RWMutex
	showAllOn bool
)

func setShowAll(on bool) {
	setShowAllWith(on, func() {
		if h := handlerRef(); h != nil {
			h.Forget()
		}
	})
}

// setShowAllWith is setShowAll with the rebuild injected, so the thing that
// matters about it can be tested: that it does nothing when nothing changed.
//
// The cockpit reports this on every request, so acting unconditionally threw
// away the review cache each time — a 600-file review was rebuilt from git on
// every page load and every live refresh, 31 seconds a piece.
func setShowAllWith(on bool, rebuild func()) {
	showAllMu.Lock()
	changed := showAllOn != on
	showAllOn = on
	showAllMu.Unlock()

	if !changed {
		return
	}
	rebuild()
}

func showingAll() bool {
	showAllMu.RLock()
	defer showAllMu.RUnlock()
	return showAllOn
}

// pathsOf is every file a set of units covers.
func pathsOf(units []domain.Unit) []string {
	var out []string
	for _, u := range units {
		out = append(out, u.Files...)
	}
	return out
}

// judgeFinding records what a reviewer made of one finding, and keeps it
// (ADR 0030).
func judgeFinding() web.JudgeFindingFunc {
	return func(_ context.Context, targetID string, kind domain.AnalysisKind,
		file, note string, verdict domain.Verdict) error {

		entry, known := lookupTarget(targetID)
		if !known {
			return fmt.Errorf("no such review %q", targetID)
		}
		store := jsonl.New(entry.out)

		got, err := store.LoadAnalysis(targetID, kind)
		if err != nil || !got.Done() {
			return fmt.Errorf("no %s analysis to judge", kind)
		}
		return store.SaveAnalysis(usecase.Judge(got, file, note, verdict))
	}
}

// searchWorkspace looks through everything written across every review.
//
// Read from the stores on each search rather than kept in memory: a review log
// is small, searches are rare, and an index would be one more thing that can be
// wrong about what is on disk (ADR 0030).
func searchWorkspace() web.SearchFunc {
	return func(query string) []usecase.Searchable {
		if strings.TrimSpace(query) == "" {
			return nil
		}

		targetsMu.RLock()
		entries := make(map[string]targetEntry, len(targetIndex))
		for id, e := range targetIndex {
			entries[id] = e
		}
		targetsMu.RUnlock()

		var corpus []usecase.Searchable
		for id, entry := range entries {
			store := jsonl.New(entry.out)

			review, err := store.Load(id)
			if err != nil {
				continue
			}
			var analyses []domain.Analysis
			for _, a := range usecase.Audits() {
				if got, err := store.LoadAnalysis(id, a.Kind); err == nil && got.Done() {
					analyses = append(analyses, got)
				}
			}
			if len(review.Notes) == 0 && len(review.Exchanges) == 0 && len(analyses) == 0 {
				continue
			}

			title := entry.target.Title
			if title == "" {
				title = id
			}
			corpus = append(corpus, usecase.SearchableReview(
				id, entry.target.Ref, title, review.Notes, review.Exchanges, analyses,
				func(unitID string) string { return unitFileIn(review, unitID) })...)
		}
		return usecase.Search(query, corpus)
	}
}

// unitFileIn is the file a note was written against, when the review still
// knows. A note carries a unit id, and a file is what a person remembers.
func unitFileIn(review domain.Session, unitID string) string {
	for _, u := range review.Units {
		if u.ID == unitID && len(u.Files) > 0 {
			return u.Files[0]
		}
	}
	return ""
}
