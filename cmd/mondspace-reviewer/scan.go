package main

import (
	"context"
	"sync"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/config"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/web"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/scanner/local"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// The fourth reading, wired up (ADR 0043).
//
// It is the one reading that is neither asked for nor paid for: it runs itself,
// off the same movement the poll already detects, and costs nothing when
// nothing moved. Everything here is arranged around that not becoming a
// nuisance — it never blocks a page, never interrupts a review, and never runs
// while an agent is mid-write.

// scanQuiet is how long the repository has to hold still before the analysers
// are run over it.
//
// Not on the poll tick. An agent writing a file writes it several times in a
// row, and a linter over a half-written file is a syntax error reported as a
// finding; worse, it is a syntax error reported as a finding four times in
// twenty seconds. Two seconds of quiet is the difference between a fourth
// reading and a fourth nuisance.
const scanQuiet = 2 * time.Second

var (
	scannersMu sync.Mutex
	// scanners are one per repository, because detection and the answer cache
	// belong to the machine and the tools rather than to a review.
	scanners = map[string]*local.Scanner{}
	// scanned is the last result per target, and the change it was about.
	scanned = map[string]scanResult{}
)

// scanResult is what the analysers last said about one review.
type scanResult struct {
	// print is the change this was about. A result for a different one is
	// stale, and stale deterministic findings are worse than none: the whole
	// claim of this layer is that it is exactly right.
	print   string
	at      time.Time
	reports []domain.Reported
}

// scannerFor is the analyser set for one repository, built once.
//
// A `.msr.toml` that cannot be read leaves the repository with no analysers at
// all rather than with the defaults. The reviewer wrote a file saying which
// tools to run; quietly running different ones is worse than running none, and
// the settings page says the file is broken.
func scannerFor(repoDir string) *local.Scanner {
	scannersMu.Lock()
	defer scannersMu.Unlock()

	if have, ok := scanners[repoDir]; ok {
		return have
	}
	analysers, err := config.LoadAnalysers(repoDir)
	if err != nil {
		analysers = nil
	}
	s := local.New(repoDir, analysers)
	scanners[repoDir] = s
	return s
}

// scanTarget runs the installed analysers over what one review changed, and
// remembers what they said.
//
// Everything expensive is behind two gates already: the caller only reaches
// here after the repository has held still, and the scanner itself does not
// re-run a tool over files that have not moved.
func scanTarget(ctx context.Context, targetID string) {
	entry, known := lookupTarget(targetID)
	if !known {
		return
	}
	units, diffs, err := unitsFor(ctx, entry)
	if err != nil || len(units) == 0 {
		return
	}

	print := usecase.ChangeFingerprint(units, diffs)
	scannersMu.Lock()
	already := scanned[targetID].print == print
	scannersMu.Unlock()
	if already {
		return
	}

	found := scannerFor(entry.repo).Look(ctx, pathsOf(units),
		usecase.FilePrints(units, diffs), entry.target.From.Commit)

	// Which of these this change actually caused, and which were already here.
	found = usecase.MarkNew(found, units, diffs)
	if rulings, err := jsonl.New(entry.out).LoadDismissals(targetID); err == nil {
		found = usecase.ApplyDismissals(found, rulings)
	}

	scannersMu.Lock()
	scanned[targetID] = scanResult{print: print, at: time.Now(), reports: found}
	scannersMu.Unlock()

	if handler := handlerRef(); handler != nil {
		handler.Broadcast("reported")
	}
}

// reportedOf hands the page whatever the analysers last said.
//
// It never runs anything and never waits: a review that has not been scanned
// yet, or whose tools are not installed, has nothing reported — which is the
// same shape as a review with nothing wrong with it, and the settings page is
// where the difference is stated.
func reportedOf() web.ReportedOf {
	return func(targetID string) []domain.Reported {
		scannersMu.Lock()
		defer scannersMu.Unlock()
		return scanned[targetID].reports
	}
}

// dismissReported records a ruling on one deterministic finding and applies it
// to what is already on the page, so the redirect lands on a page that has
// listened.
func dismissReported() web.DismissFunc {
	return func(_ context.Context, targetID, key string, verdict domain.Verdict) error {
		entry, known := lookupTarget(targetID)
		if !known {
			return nil
		}
		store := jsonl.New(entry.out)
		rulings, err := store.LoadDismissals(targetID)
		if err != nil {
			rulings = map[string]domain.Verdict{}
		}
		rulings[key] = verdict

		scannersMu.Lock()
		if have, ok := scanned[targetID]; ok {
			have.reports = usecase.ApplyDismissals(have.reports, rulings)
			scanned[targetID] = have
		}
		scannersMu.Unlock()

		return store.SaveDismissals(targetID, rulings)
	}
}

// toolsOf is what the settings page says about the analysers: what was looked
// for, what is here, and what broke (ADR 0043).
//
// Every repository in the workspace, deduplicated by tool name, because "is
// gosec installed" is a question about the machine and not about the review
// that happens to be open.
func toolsOf() web.ToolsFunc {
	return func() []web.ToolStatus {
		scannersMu.Lock()
		each := make([]*local.Scanner, 0, len(scanners))
		for _, s := range scanners {
			each = append(each, s)
		}
		scannersMu.Unlock()

		seen := map[string]bool{}
		var out []web.ToolStatus
		for _, s := range each {
			for _, t := range s.Report() {
				if seen[t.Name] {
					continue
				}
				seen[t.Name] = true
				out = append(out, web.ToolStatus{
					Name: t.Name, Why: t.Why, Present: t.Present,
					Version: t.Version, Absent: t.Absent, Failed: t.Failed,
				})
			}
		}
		return out
	}
}
