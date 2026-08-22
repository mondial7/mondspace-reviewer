package usecase

import "github.com/marcomondini/mondspace-reviewer/internal/domain"

// BuildAskContext assembles the bounded context for a question. Unit scope is
// narrow — the current unit, its diff, and its notes. Session scope is broad but
// still bounded — every unit's headline and all notes, no diffs.
func BuildAskContext(scope domain.AskScope, sess domain.Session, current domain.Unit, diff domain.Diff) domain.AskContext {
	ctx := domain.AskContext{Scope: scope, Prompt: sess.Prompt}

	if scope == domain.AskUnit {
		ctx.Units = []domain.Unit{current}
		ctx.Diff = diff
		ctx.Notes = notesFor(sess.Notes, current.ID)
		ctx.HasStated = current.Headline.WhySrc == domain.WhyStated
		return ctx
	}

	ctx.Units = sess.Units
	ctx.Notes = sess.Notes
	return ctx
}

func notesFor(notes []domain.Note, unitID string) []domain.Note {
	var out []domain.Note
	for _, n := range notes {
		if n.UnitID == unitID {
			out = append(out, n)
		}
	}
	return out
}
