package domain_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

func TestOnlyWhatIsStillToBeDealtWithIsActionable(t *testing.T) {
	// An agent's context is scarce. Handing it every note a human ever wrote
	// wastes it, and handing it approvals as though they were work is worse
	// (ADR 0031).
	tests := []struct {
		note domain.Note
		want bool
		why  string
	}{
		{domain.Note{Kind: domain.NoteObjection}, true, "an objection is a thing to change"},
		{domain.Note{Kind: domain.NoteQuestion}, true, "a question wants answering"},
		{domain.Note{Kind: domain.NoteDebt}, true, "debt is a thing to remember"},
		{domain.Note{Kind: domain.NoteOK}, false, "an approval is not work"},
		{domain.Note{Kind: domain.NoteNote}, false, "a remark is the reviewer thinking aloud"},
		{domain.Note{Kind: domain.NoteObjection, SupersededBy: "n2"}, false,
			"a superseded note has been dealt with"},
	}

	for _, tt := range tests {
		if got := tt.note.Actionable(); got != tt.want {
			t.Errorf("%+v Actionable = %v, want %v — %s", tt.note, got, tt.want, tt.why)
		}
	}
}
