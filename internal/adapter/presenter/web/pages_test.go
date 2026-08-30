package web_test

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/web"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// Structural tests over the rendered pages.
//
// Capturing screenshots for the documentation once found five bugs that the
// whole suite had missed — a card reporting "8/4 described", a banner rendering
// at the bottom of the page, one file listed twice, a hash broken across three
// lines, and files at the repository root grouped under a lone ".". Every one
// was about what the page *looked like*, and nothing here could see that.
//
// These do not replace looking. They pin the invariants that were violated, so
// the same class of mistake fails a test rather than waiting for someone to
// take a screenshot (issue #19).

// wiredServer is a server with everything attached, which is the configuration
// the cockpit is actually used in and the one with the most to go wrong.
func wiredServer(t *testing.T) *web.Server {
	t.Helper()
	sess := testSession()
	sess.Notes = []domain.Note{
		{ID: "n1", UnitID: "s-f001", Kind: domain.NoteOK, Text: "fine"},
		{ID: "n2", UnitID: "s-f002", Kind: domain.NoteQuestion, Anchor: "+package http", Text: "why here?"},
	}
	sess.Target = domain.Target{Kind: domain.TargetCommit, Ref: "abc12345",
		Title: "a commit", Subtitle: "abc12345 · Someone"}

	return web.NewServer(sess, nil).
		WithStats(domain.SessionStats{Files: 2, Added: 9, Removed: 3, Commits: 1}).
		WithTargets([]web.TargetSummary{
			{ID: "s", Ref: "abc12345", Kind: domain.TargetCommit, Title: "a commit", Repo: "demo"},
			{ID: "t2", Ref: "v1.0.0", Kind: domain.TargetTag, Title: "v1.0.0", Repo: "demo", Reviewed: true},
		}).
		WithAgent(web.AgentStatus{Model: "m", Endpoint: "http://e/v1", Online: true}).
		WithAnalyses(
			func(context.Context, string, domain.AnalysisKind) error { return nil },
			func(_ string, k domain.AnalysisKind) domain.Analysis {
				return domain.Analysis{TargetID: "s", Kind: k, At: time.Now(),
					Verdict: "one thing", Print: usecase.Fingerprint(testSession().Units),
					Findings: []domain.Finding{{File: "a.go", Note: "look", Severity: domain.SeverityMedium}}}
			}).
		WithSignoff(
			func(context.Context, domain.Signoff) error { return nil },
			func(string) domain.Signoff { return domain.Signoff{} }).
		WithLog(func(string, string) web.LogView {
			return web.LogView{
				Entries: []usecase.LogEntry{{Commit: domain.Commit{Hash: "abcdef123456",
					Subject: "a commit", Author: "Someone", TS: time.Now()},
					Ref: "abcdef12", Ago: "1h ago"}},
				Remote: domain.RemoteState{Branch: "main", Upstream: "origin/main", Behind: 1},
			}
		}).
		WithBranches(func(string) web.BranchView {
			return web.BranchView{Base: "origin/main", Branches: []domain.Branch{
				{Name: "origin/x", Short: "x", Subject: "s", Author: "a", TS: time.Now(), Ahead: 1}}}
		}).
		WithRemoteWatch(func() (bool, time.Duration) { return true, time.Minute }, nil).
		WithAsk(func(context.Context, string, string, []web.Exchange) (string, error) {
			return "", nil
		})
}

func TestEveryPageRendersWhateverIsWired(t *testing.T) {
	// A template referring to a field that is not there is a 500 on that page
	// alone, which is exactly the kind of thing nobody notices until they open
	// it. Both configurations, because most of these are conditional on wiring.
	pages := []string{"/", "/settings", "/activity", "/tutorial", "/branches"}

	for _, name := range []string{"bare", "wired"} {
		h := web.NewServer(testSession(), nil)
		expect := map[string]int{"/branches": http.StatusNotFound}
		if name == "wired" {
			h = wiredServer(t)
			expect = nil
		}

		for _, page := range pages {
			want := http.StatusOK
			if code, special := expect[page]; special {
				want = code
			}
			rec := get(t, h, page)
			if rec.Code != want {
				t.Errorf("%s: GET %s = %d, want %d\n%s", name, page, rec.Code, want,
					firstLines(rec.Body.String(), 6))
			}
		}
	}
}

func TestAProgressCountNeverExceedsItsTotal(t *testing.T) {
	// The "8/4 described" bug: group descriptions and per-file descriptions
	// share one map, and the count counted the map. A ratio above one is
	// impossible by definition, so it is worth asserting as a shape rather than
	// as one case.
	sess := testSession()
	sess.Narrative = domain.Narrative{
		Source:   domain.NarrativeModel,
		Chapters: []domain.Chapter{{Title: "auth", UnitIDs: []string{"s-f001"}}},
		Meanings: map[string]string{
			"auth": "a group", "http": "another group",
			"file:auth/token.go": "a file", "file:http/middleware.go": "another file",
		},
	}

	body := get(t, web.NewServer(sess, nil), "/").Body.String()

	ratio := regexp.MustCompile(`(\d+)/(\d+) described`)
	for _, m := range ratio.FindAllStringSubmatch(body, -1) {
		done, _ := strconv.Atoi(m[1])
		total, _ := strconv.Atoi(m[2])
		if done > total {
			t.Errorf("%q: a count cannot exceed its total", m[0])
		}
	}
}

func TestTheBannersComeBeforeWhatTheyAreAbout(t *testing.T) {
	// The pending banner auto-placed into an implicit grid row and rendered at
	// the *bottom* of the page. Document order is what a screen reader and a
	// no-CSS render follow, so it is the thing to pin.
	h := wiredServer(t)
	h.SetPending([]domain.FileStat{{Path: "new.go", Added: 3}},
		domain.SnapshotRef{Commit: "pin"}, domain.SnapshotRef{Commit: "now"}, time.Now())

	body := get(t, h, "/").Body.String()

	pending := strings.Index(body, `id="pending"`)
	card := strings.Index(body, `id="reviewcard"`)
	story := strings.Index(body, `id="story-col"`)

	if pending < 0 || card < 0 || story < 0 {
		t.Fatalf("missing regions: pending=%d card=%d story=%d", pending, card, story)
	}
	if pending > card {
		t.Error("work that arrived should be announced above the review card, not below it")
	}
	if card > story {
		t.Error("the review card should come before the story it is about")
	}
}

func TestNoFileIsListedTwiceInOneReview(t *testing.T) {
	// One unchanged file arrived twice with opposite signs, because a snapshot
	// diff counted untracked files once from git and once from a scan.
	h := wiredServer(t)
	body := get(t, h, "/").Body.String()

	// Only the entries that *are* the file in the review. `data-file` also
	// appears on the history button inside one, which is not a second listing —
	// and is exactly the distinction the command palette makes when it selects
	// `.post[data-file]`.
	seen := map[string]int{}
	entry := regexp.MustCompile(`<details class="post" id="[^"]*" data-file="([^"]+)"`)
	for _, m := range entry.FindAllStringSubmatch(body, -1) {
		seen[m[1]]++
	}
	for file, n := range seen {
		if n > 1 {
			t.Errorf("%s appears %d times in one review", file, n)
		}
	}
	if len(seen) == 0 {
		t.Error("the review should list its files")
	}
}

func TestNoRegionRendersEmptyWhenItShouldHaveContent(t *testing.T) {
	// A region that renders but is empty reads as broken, and is how a missing
	// field looked before templates were buffered.
	body := get(t, wiredServer(t), "/").Body.String()

	for _, region := range []string{"cockpit__story", "cockpit__changes", "cockpit__stats", "analyses"} {
		i := strings.Index(body, region)
		if i < 0 {
			t.Errorf("%s is missing from the page", region)
			continue
		}
		// Enough content after the opening tag to be more than an empty box.
		if len(strings.TrimSpace(body[i:min(i+400, len(body))])) < 120 {
			t.Errorf("%s renders essentially empty", region)
		}
	}
}

func TestAGroupIsNeverNamedWithPunctuationAlone(t *testing.T) {
	// Files at the repository root grouped under ".", which beside real
	// directory names reads as a rendering fault rather than as a place.
	sess := testSession()
	sess.Units = []domain.Unit{{ID: "s-f001", Files: []string{"go.mod"},
		Headline: domain.Headline{Text: "added a dependency"}}}
	sess.Diffs = map[string]domain.Diff{"s-f001": {Text: "@@\n+require x\n"}}

	body := get(t, web.NewServer(sess, nil), "/").Body.String()

	for _, m := range regexp.MustCompile(`class="group__dir">([^<]*)<`).FindAllStringSubmatch(body, -1) {
		if strings.Trim(m[1], ". \t") == "" {
			t.Errorf("a group is named %q, which reads as a rendering fault", m[1])
		}
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func TestAReviewWithNothingInItSaysSoAndOffersNothing(t *testing.T) {
	// Opening the live target with a clean tree is the common way to land here,
	// and the card offered to read a change that does not exist, to export a log
	// with nothing in it, and to mark as reviewed something nobody could review.
	empty := testSession()
	empty.Units, empty.Diffs = nil, nil

	body := get(t, web.NewServer(empty, nil).
		WithAgent(web.AgentStatus{Model: "m", Online: true}).
		WithNarrate(func(context.Context, string) {}).
		WithAnalyses(
			func(context.Context, string, domain.AnalysisKind) error { return nil },
			func(string, domain.AnalysisKind) domain.Analysis { return domain.Analysis{} }),
		"/cockpit").Body.String()

	if !strings.Contains(strings.ToLower(body), "nothing to review") {
		t.Errorf("the card should say there is nothing here:\n%s", firstLines(body, 8))
	}
	for _, offered := range []string{
		"ask it to read this", // there is nothing to read
		"MARK THIS REVIEWED",  // nobody can review nothing
		"take the log",        // an empty log is not worth exporting
		`action="/analysis/`,  // and an audit of nothing is a wasted model call
	} {
		if strings.Contains(body, offered) {
			t.Errorf("an empty review should not offer %q:\n%s", offered, firstLines(body, 8))
		}
	}
}

func TestAReviewWithSomethingInItStillOffersEverything(t *testing.T) {
	// The guard above must not switch the page off for a real review.
	body := get(t, wiredServer(t).WithNarrate(func(context.Context, string) {}), "/cockpit").Body.String()

	for _, want := range []string{"mark this reviewed", "take the log", `action="/analysis/`} {
		if !strings.Contains(body, want) {
			t.Errorf("a review with changes in it should still offer %q", want)
		}
	}
}
