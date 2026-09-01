package domain

import (
	"fmt"
	"time"
)

// PendingFile is one file that has changed since the reviewer opened what they
// are reading.
//
// The two flags are what turn a notification into a decision. A file nobody has
// looked at is just more work arriving; a file already on screen means the page
// no longer matches the disk; a file the reviewer has already ruled on means
// their judgement was made against a version that no longer exists.
type PendingFile struct {
	Path           string
	Added, Removed int
	// InReview: this file is part of the review currently open.
	InReview bool
	// Annotated: ...and it already carries a note that has not been superseded.
	Annotated bool
}

// Pending is the work that has arrived since a review was opened, and the range
// that would review it on its own (ADR 0020).
type Pending struct {
	// Target is the review this is about. A live review is the only kind that
	// can have work arrive under it — a commit does not move — and the banner
	// offers to fold that work in, which on any other review would do something
	// nobody asked for. So it names what it belongs to and the page checks.
	Target string
	// From is what the reviewer is reading up to; To is where the repository is
	// now. Together they are an ordinary range, which is what makes "review only
	// what is new" a target like any other.
	From, To SnapshotRef
	Files    []PendingFile
	Since    time.Time
}

// About reports whether this is work waiting on a particular review. Unnamed
// pending work is about whatever is open, which is how it behaved before it
// carried a target at all.
func (p Pending) About(target string) bool {
	return p.Target == "" || p.Target == target
}

// Empty reports whether there is nothing to say. Silence is the normal answer:
// this is checked every couple of seconds and almost always finds nothing.
func (p Pending) Empty() bool { return len(p.Files) == 0 }

// Count totals what is waiting.
func (p Pending) Count() (files, added, removed int) {
	for _, f := range p.Files {
		added += f.Added
		removed += f.Removed
	}
	return len(p.Files), added, removed
}

// Stale is the files the reviewer has already annotated. These are the ones
// worth interrupting for: a note written against a version that no longer
// exists is worse than no note, because it reads as current.
func (p Pending) Stale() []PendingFile {
	var out []PendingFile
	for _, f := range p.Files {
		if f.Annotated {
			out = append(out, f)
		}
	}
	return out
}

// Headline is the one sentence shown above the choice. It leads with the count
// and adds the judgement clause only when there is one, because that clause is
// the whole reason to stop reading and decide.
func (p Pending) Headline() string {
	files, _, _ := p.Count()
	if files == 0 {
		return ""
	}
	noun := "files"
	if files == 1 {
		noun = "file"
	}
	head := fmt.Sprintf("%d %s changed since you opened this review", files, noun)

	stale := len(p.Stale())
	if stale == 0 {
		return head
	}
	return fmt.Sprintf("%s — %d you had already annotated", head, stale)
}
