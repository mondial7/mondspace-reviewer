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
			func(_ string, k domain.AnalysisKind, _ string) domain.Analysis {
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
			func(string, domain.AnalysisKind, string) domain.Analysis { return domain.Analysis{} }),
		"/cockpit").Body.String()

	if !strings.Contains(strings.ToLower(body), "nothing to review") {
		t.Errorf("the card should say there is nothing here:\n%s", firstLines(body, 8))
	}
	for _, offered := range []string{
		"start review",       // there is nothing to read
		"MARK THIS REVIEWED", // nobody can review nothing
		"take the log",       // an empty log is not worth exporting
		`action="/analysis/`, // and an audit of nothing is a wasted model call
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

func TestThePickerSaysWhenEachPointWas(t *testing.T) {
	// Choosing two points to compare is a question about time — "what changed
	// since this morning" — and the list gave a hash, a kind and a subject with
	// nothing to place any of them in.
	when := time.Date(2026, 8, 30, 9, 41, 0, 0, time.UTC)
	h := web.NewServer(testSession(), nil).WithTargets([]web.TargetSummary{
		{ID: "t1", Ref: "abc12345", Kind: domain.TargetCommit, Title: "a commit",
			Repo: "demo", TS: when},
	})

	body := get(t, h, "/cockpit").Body.String()

	stamp := when.Local().Format("2 Jan 15:04")
	if !strings.Contains(body, stamp) {
		t.Errorf("the picker should date each point (%q):\n%s", stamp, firstLines(body, 6))
	}
}

func TestTheBriefWearsTheModelsEmojiWhenThereAreAny(t *testing.T) {
	sess := testSession()
	sess.Narrative = domain.Narrative{
		Title: "a change", Source: domain.NarrativeModel, Model: "m",
		Emoji: []string{"🔒", "🧪", "🚀"},
	}

	body := get(t, web.NewServer(sess, nil), "/cockpit").Body.String()

	for _, want := range []string{"🔒", "🧪", "🚀"} {
		if !strings.Contains(body, want) {
			t.Errorf("the brief should wear %q", want)
		}
	}
}

func TestNoEmojiIsNoRow(t *testing.T) {
	// Filler on a card read at a glance is worse than an empty card.
	sess := testSession()
	sess.Narrative = domain.Narrative{Title: "a change", Source: domain.NarrativeModel}

	if strings.Contains(get(t, web.NewServer(sess, nil), "/cockpit").Body.String(), "brief__emoji") {
		t.Error("an empty list should render nothing at all")
	}
}

func TestTwoPointsPickedInTheHistoryCompareWithoutJavaScript(t *testing.T) {
	// The history is the one place the checkpoints are listed, so it is the one
	// place to pick them. Picking is two checkboxes and a submit, which has to
	// work as a plain form — the script only decides when the button lights up.
	var gotFrom, gotTo string
	h := wiredServer(t).WithCompare(
		func(_ context.Context, _, from, to string) (string, error) {
			gotFrom, gotTo = from, to
			return "range-1", nil
		})

	rec := get(t, h, "/compare?repo=demo&pick=newer&pick=older")

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /compare = %d, want a redirect to the range", rec.Code)
	}
	// The history is newest first, so the second one picked is the earlier
	// point — and a range runs from the earlier one.
	if gotFrom != "older" || gotTo != "newer" {
		t.Errorf("compared %q…%q, want older…newer", gotFrom, gotTo)
	}
}

func TestOnePointPickedIsNotARange(t *testing.T) {
	h := wiredServer(t).WithCompare(
		func(context.Context, string, string, string) (string, error) {
			t.Error("one point is not something to compare")
			return "", nil
		})

	if rec := get(t, h, "/compare?repo=demo&pick=only"); rec.Code != http.StatusSeeOther {
		t.Errorf("GET /compare = %d, want it turned away", rec.Code)
	}
}

func TestTheBriefNeverSaysTheSameThingTwice(t *testing.T) {
	// A target's own name and the title of the story written about it are the
	// same sentence far more often than not — always, for the live target,
	// whose name *is* "Live · uncommitted work". Printing both is not a summary.
	sess := testSession()
	sess.Prompt = "Live · uncommitted work"
	sess.Target = domain.Target{Kind: domain.TargetLive, Title: "Live · uncommitted work"}
	sess.Narrative = domain.Narrative{
		Title:  "Live · uncommitted work",
		Intro:  "Live · uncommitted work",
		Source: domain.NarrativeMechanical,
	}

	body := get(t, web.NewServer(sess, nil), "/cockpit").Body.String()

	// Inside the card, not the whole page: the browser tab is entitled to say
	// it as well.
	card := body[strings.Index(body, `<section class="brief">`):]
	card = card[:strings.Index(card, "</section>")]
	if n := strings.Count(card, "Live · uncommitted work"); n != 1 {
		t.Errorf("the card says it %d times, want once:\n%s", n, card)
	}
}

func TestTheBriefKeepsWhatIsActuallyDifferent(t *testing.T) {
	// The guard must not eat the commit subject, which is a different sentence
	// from the title a model wrote about it and is the reason the line exists.
	sess := testSession()
	sess.Prompt = "Serve the review to an agent with msr mcp"
	sess.Narrative = domain.Narrative{
		Title: "msr mcp store review", Source: domain.NarrativeModel, Model: "m",
		Intro: "The agent configured msr mcp to serve review output.",
	}

	body := get(t, web.NewServer(sess, nil), "/cockpit").Body.String()

	for _, want := range []string{
		"msr mcp store review",
		"Serve the review to an agent with msr mcp",
		"The agent configured msr mcp to serve review output.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the card should still carry %q", want)
		}
	}
}

func TestALongPathKeepsBothItsEnds(t *testing.T) {
	// A chapter is often a directory, and a directory is often long. Cutting
	// the end off loses the filename, which is the half you were looking for;
	// the middle is the part nobody reads.
	sess := testSession()
	sess.Narrative = domain.Narrative{
		Source: domain.NarrativeModel, Model: "m", Title: "a change",
		Chapters: []domain.Chapter{{
			Title:   "internal/adapter/presenter/web/assets/app.css",
			Prose:   "styles",
			UnitIDs: []string{"s-f001"},
		}},
	}

	body := get(t, web.NewServer(sess, nil), "/cockpit").Body.String()

	// Split into a head that may be trimmed and a tail that may not, so the
	// last segment survives whatever the column width turns out to be.
	if !strings.Contains(body, `<span class="path__head">internal/adapter/presenter/web/assets/</span>`) {
		t.Errorf("the head should be the part that can be trimmed:\n%s", firstLines(body, 6))
	}
	if !strings.Contains(body, `<span class="path__tail">app.css</span>`) {
		t.Error("the last segment should be kept whole")
	}
}

func TestATitleThatIsNotAPathIsLeftAlone(t *testing.T) {
	sess := testSession()
	sess.Narrative = domain.Narrative{
		Source: domain.NarrativeModel, Model: "m", Title: "a change",
		Chapters: []domain.Chapter{{Title: "the retry loop", UnitIDs: []string{"s-f001"}}},
	}

	body := get(t, web.NewServer(sess, nil), "/cockpit").Body.String()

	if strings.Contains(body, "path__head") {
		t.Error("a sentence is not a path and should not be split like one")
	}
	if !strings.Contains(body, "the retry loop") {
		t.Error("and it should still be shown")
	}
}

func TestTheLiveTargetIsTheTopOfTheHistory(t *testing.T) {
	// Choosing what to review happens in one place, and that place is the list
	// of checkpoints. The working tree is a checkpoint like any other — the
	// newest one — so it sits at the top of the list rather than in a control
	// of its own beside it.
	h := wiredServer(t)

	body := get(t, h, "/cockpit").Body.String()

	if !strings.Contains(body, `href="/?target=live"`) {
		t.Errorf("the history should lead to the live target:\n%s", firstLines(body, 6))
	}
	// And the control that used to do it is gone.
	if strings.Contains(body, `class="picker"`) {
		t.Error("the separate picker should be gone")
	}
}

func TestTheKeyboardCanStillWalkEveryPoint(t *testing.T) {
	// `[`, `]`, `{` and `}` read the option list to move between reviews and
	// between repositories. Removing the visible picker must not remove what
	// they walk — a workspace spans repositories, and the history card only
	// knows about one.
	h := wiredServer(t)

	body := get(t, h, "/cockpit").Body.String()

	if !strings.Contains(body, `<datalist id="refs"`) {
		t.Errorf("the list of every point should still be in the page:\n%s", firstLines(body, 6))
	}
	if !strings.Contains(body, `data-repo="demo"`) {
		t.Error("and each one should still name its repository")
	}
}

func TestATruncatedTitleStillCountsAsTheSameSentence(t *testing.T) {
	// Before a model has read anything the title is the commit subject, cut to
	// fit — so it is never *equal* to the subject printed under it, and an
	// equality test let the card say the same thing twice with an ellipsis
	// between the two copies.
	subject := "Put tags and recorded runs in the history, and colour a column by what it did"
	sess := testSession()
	sess.Prompt = subject
	sess.Target = domain.Target{Kind: domain.TargetCommit, Ref: "34938765", Subtitle: "34938765 · Someone"}
	sess.Narrative = domain.Narrative{
		Title:  "Put tags and recorded runs in the history, and colour a column by…",
		Source: domain.NarrativeMechanical,
	}

	body := get(t, web.NewServer(sess, nil), "/cockpit").Body.String()
	card := body[strings.Index(body, `<section class="brief">`):]
	card = card[:strings.Index(card, "</section>")]

	if strings.Contains(card, subject) {
		t.Errorf("the card repeats the subject it already truncated at the top:\n%s", card)
	}
}
