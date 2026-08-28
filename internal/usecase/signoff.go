package usecase

import (
	"fmt"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// SignoffView is a sign-off as the page needs to read it.
type SignoffView struct {
	Done bool
	// Moved reports that the code has changed since it was signed off, which
	// makes the judgement about a state that no longer exists.
	Moved    bool
	Comment  string
	Sentence string
}

// SignoffState turns a stored sign-off into the sentence shown above a review.
//
// The qualification is the point. A sign-off is a judgement about a specific
// state of the code, so reporting a bare "reviewed" for something that has
// changed since would be the same lie the pending banner exists to prevent,
// one level up (ADR 0020, ADR 0021).
func SignoffState(s domain.Signoff, print string, files int, now time.Time) SignoffView {
	if !s.Done() {
		return SignoffView{Sentence: "not reviewed yet"}
	}

	view := SignoffView{Done: true, Comment: s.Comment}
	view.Sentence = "reviewed " + since(now.Sub(s.At)) + " ago"

	// An empty stored print means a sign-off from before the review could be
	// fingerprinted; claiming it moved would be a guess.
	if s.Print == "" || s.Print == print {
		return view
	}

	view.Moved = true
	view.Sentence += ", but it has changed since"
	// Quoting both counts only helps when they differ. A line edited inside an
	// already-changed file leaves the count identical, and "4 files then, 4
	// now" reads as though nothing happened.
	if files != s.Files {
		view.Sentence += fmt.Sprintf(" — %d files then, %d now", s.Files, files)
	}
	return view
}

// Since renders an age the way a reviewer would say it out loud. Exported
// because the audit cards say the same thing about their own age, and two
// spellings of "an hour ago" on one page would look like two different facts.
func Since(d time.Duration) string { return since(d) }

// since renders an age the way a reviewer would say it out loud.
func since(d time.Duration) string {
	switch {
	case d < 30*time.Second:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
