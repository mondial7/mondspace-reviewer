package usecase_test

import (
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func review() ([]domain.Unit, []domain.Note) {
	units := []domain.Unit{
		{ID: "u1", Files: []string{"auth/token.go"}},
		{ID: "u2", Files: []string{"api/handler.go"}},
	}
	notes := []domain.Note{
		{ID: "n1", UnitID: "u1", Kind: domain.NoteOK, Text: "looks right"},
	}
	return units, notes
}

func TestNothingWaitingIsTheNormalAnswer(t *testing.T) {
	units, notes := review()

	got := usecase.PendingWork(units, notes, nil, domain.SnapshotRef{}, domain.SnapshotRef{}, time.Time{})

	if !got.Empty() {
		t.Errorf("got %+v, want nothing waiting", got)
	}
	if got.Headline() != "" {
		t.Errorf("headline = %q, want silence", got.Headline())
	}
}

func TestAFileTheReviewerHasNotSeenIsSimplyNew(t *testing.T) {
	units, notes := review()
	changed := []domain.FileStat{{Path: "docs/readme.md", Added: 10, Removed: 0}}

	got := usecase.PendingWork(units, notes, changed, domain.SnapshotRef{}, domain.SnapshotRef{}, time.Time{})

	if len(got.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(got.Files))
	}
	f := got.Files[0]
	if f.InReview || f.Annotated {
		t.Errorf("%+v: a file not in the review is neither reviewed nor annotated", f)
	}
	if got.Headline() != "1 file changed since you opened this review" {
		t.Errorf("headline = %q", got.Headline())
	}
}

func TestAFileAlreadyInTheReviewIsMarkedAsSuch(t *testing.T) {
	// Changing a file that is part of what you are reading is different news
	// from a new file appearing: it means the thing on your screen is no longer
	// what is on disk.
	units, notes := review()
	changed := []domain.FileStat{{Path: "api/handler.go", Added: 4, Removed: 2}}

	got := usecase.PendingWork(units, notes, changed, domain.SnapshotRef{}, domain.SnapshotRef{}, time.Time{})

	if f := got.Files[0]; !f.InReview {
		t.Errorf("%+v: should be marked as part of the open review", f)
	}
	if f := got.Files[0]; f.Annotated {
		t.Errorf("%+v: nobody annotated this one", f)
	}
}

func TestAFileTheReviewerAlreadyJudgedIsTheHeadline(t *testing.T) {
	// This is the case the whole feature exists for. A reviewer who marked
	// auth/token.go "ok" made that judgement against a version that no longer
	// exists, and nothing else on the page would tell them.
	units, notes := review()
	changed := []domain.FileStat{
		{Path: "auth/token.go", Added: 6, Removed: 1},
		{Path: "docs/readme.md", Added: 2, Removed: 0},
	}

	got := usecase.PendingWork(units, notes, changed, domain.SnapshotRef{}, domain.SnapshotRef{}, time.Time{})

	stale := got.Stale()
	if len(stale) != 1 || stale[0].Path != "auth/token.go" {
		t.Fatalf("stale = %+v, want auth/token.go", stale)
	}
	want := "2 files changed since you opened this review — 1 you had already annotated"
	if got.Headline() != want {
		t.Errorf("headline = %q,\n    want %q", got.Headline(), want)
	}
}

func TestTheAlreadyJudgedFilesComeFirst(t *testing.T) {
	// Ordering is the cheapest way to make the important thing the first thing
	// read. A file you ruled on outranks a file you have never seen.
	units, notes := review()
	changed := []domain.FileStat{
		{Path: "zzz/new.go", Added: 1},
		{Path: "api/handler.go", Added: 1},
		{Path: "auth/token.go", Added: 1},
	}

	got := usecase.PendingWork(units, notes, changed, domain.SnapshotRef{}, domain.SnapshotRef{}, time.Time{})

	if got.Files[0].Path != "auth/token.go" {
		t.Errorf("first = %q, want the annotated one", got.Files[0].Path)
	}
	if got.Files[1].Path != "api/handler.go" {
		t.Errorf("second = %q, want the one already in the review", got.Files[1].Path)
	}
	if got.Files[2].Path != "zzz/new.go" {
		t.Errorf("third = %q, want the wholly new one", got.Files[2].Path)
	}
}

func TestASupersededNoteDoesNotCountAsAJudgement(t *testing.T) {
	// A note already marked superseded has been dealt with; counting it again
	// would keep warning about something the reviewer has moved past.
	units := []domain.Unit{{ID: "u1", Files: []string{"auth/token.go"}}}
	notes := []domain.Note{{ID: "n1", UnitID: "u1", Kind: domain.NoteOK, SupersededBy: "n2"}}
	changed := []domain.FileStat{{Path: "auth/token.go", Added: 1}}

	got := usecase.PendingWork(units, notes, changed, domain.SnapshotRef{}, domain.SnapshotRef{}, time.Time{})

	if len(got.Stale()) != 0 {
		t.Errorf("stale = %+v, want none — that note was already superseded", got.Stale())
	}
}

func TestTheTotalsAreTheSumOfWhatIsWaiting(t *testing.T) {
	units, notes := review()
	changed := []domain.FileStat{
		{Path: "a.go", Added: 10, Removed: 2},
		{Path: "b.go", Added: 5, Removed: 3},
	}

	got := usecase.PendingWork(units, notes, changed, domain.SnapshotRef{}, domain.SnapshotRef{}, time.Time{})

	files, added, removed := got.Count()
	if files != 2 || added != 15 || removed != 5 {
		t.Errorf("count = %d files +%d -%d, want 2 +15 -5", files, added, removed)
	}
}

func TestPendingCarriesTheRangeThatWouldReviewIt(t *testing.T) {
	// "Review only what is new" is an ordinary range target, so the pending
	// work has to name both of its ends.
	units, notes := review()
	from := domain.SnapshotRef{Commit: "aaa", Label: "what you are reading"}
	to := domain.SnapshotRef{Commit: "bbb", Label: "now"}

	got := usecase.PendingWork(units, notes,
		[]domain.FileStat{{Path: "a.go", Added: 1}}, from, to, time.Now())

	if got.From != from || got.To != to {
		t.Errorf("range = %v..%v, want %v..%v", got.From, got.To, from, to)
	}
}
