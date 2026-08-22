package usecase

import (
	"context"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/port"
)

// Summarize asks the model for a better headline while enforcing WhySrc
// discipline: a stated rationale (taken verbatim from the agent) is never
// overwritten by the model. The model contributes the WHAT text and, only when
// nothing was stated, an inferred WHY. On any error it degrades to the unit's
// mechanical headline.
func Summarize(ctx context.Context, sum port.Summarizer, u domain.Unit, d domain.Diff) domain.Headline {
	model, err := sum.Headline(ctx, u, d)
	if err != nil {
		return u.Headline
	}

	out := domain.Headline{Text: model.Text}

	// The stated intent is load-bearing: keep it verbatim, and let the model
	// only sharpen the WHAT text.
	out.Why = u.Headline.Why
	out.WhySrc = u.Headline.WhySrc
	return out
}
