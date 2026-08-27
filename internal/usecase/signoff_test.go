package usecase_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestAnUnreviewedTargetSaysSo(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	got := usecase.SignoffState(domain.Signoff{}, "print-a", 4, now)

	if got.Done {
		t.Errorf("got %+v, want not yet reviewed", got)
	}
	if got.Sentence != "not reviewed yet" {
		t.Errorf("sentence = %q", got.Sentence)
	}
}

func TestASignedOffTargetSaysWhen(t *testing.T) {
	// Coming back to a review you finished is the case this exists for: the
	// first thing you need to know is that you are done with it.
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	s := domain.Signoff{
		TargetID: "t1", At: now.Add(-2 * time.Hour),
		Print: "print-a", Files: 4,
	}

	got := usecase.SignoffState(s, "print-a", 4, now)

	if !got.Done {
		t.Fatalf("got %+v, want reviewed", got)
	}
	if got.Moved {
		t.Error("nothing has changed, so it has not moved")
	}
	if !strings.HasPrefix(got.Sentence, "reviewed 2h ago") {
		t.Errorf("sentence = %q, want it to lead with when", got.Sentence)
	}
}

func TestASignOffIsQualifiedWhenTheCodeMovedUnderIt(t *testing.T) {
	// A sign-off is a judgement about a specific state of the code. Saying
	// "reviewed" about something that has changed since would be the same lie
	// the pending banner exists to prevent, one level up.
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	s := domain.Signoff{
		TargetID: "t1", At: now.Add(-30 * time.Minute),
		Print: "print-a", Files: 4,
	}

	got := usecase.SignoffState(s, "print-b", 7, now)

	if !got.Done {
		t.Error("it was still reviewed; that is a fact about the past")
	}
	if !got.Moved {
		t.Fatalf("got %+v, want it flagged as moved", got)
	}
	want := "reviewed 30m ago, but it has changed since — 4 files then, 7 now"
	if got.Sentence != want {
		t.Errorf("sentence = %q,\n    want %q", got.Sentence, want)
	}
}

func TestAChangeThatKeepsTheFileCountIsStillAChange(t *testing.T) {
	// Editing a line in an already-changed file leaves the count identical.
	// The fingerprint is what actually knows.
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	s := domain.Signoff{At: now.Add(-time.Hour), Print: "print-a", Files: 4}

	got := usecase.SignoffState(s, "print-b", 4, now)

	if !got.Moved {
		t.Error("the fingerprint differs, so it has moved")
	}
	if !strings.Contains(got.Sentence, "changed since") {
		t.Errorf("sentence = %q, want it to say so", got.Sentence)
	}
	// With the same count either side, quoting both numbers says nothing.
	if strings.Contains(got.Sentence, "4 files then") {
		t.Errorf("sentence = %q, should not quote identical counts", got.Sentence)
	}
}

func TestTheCommentComesBackWithTheSignOff(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	s := domain.Signoff{At: now, Comment: "happy with this; the retry loop needs a follow-up"}

	got := usecase.SignoffState(s, "", 0, now)

	if got.Comment != "happy with this; the retry loop needs a follow-up" {
		t.Errorf("comment = %q", got.Comment)
	}
}

func TestAFreshSignOffReadsAsJustNow(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	got := usecase.SignoffState(domain.Signoff{At: now.Add(-5 * time.Second)}, "", 0, now)

	if !strings.HasPrefix(got.Sentence, "reviewed just now") {
		t.Errorf("sentence = %q", got.Sentence)
	}
}
