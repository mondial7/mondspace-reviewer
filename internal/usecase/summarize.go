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

	if u.Headline.WhySrc == domain.WhyStated {
		// The stated intent is load-bearing: keep it verbatim; the model only
		// sharpens the WHAT text.
		out.Why = u.Headline.Why
		out.WhySrc = domain.WhyStated
	} else {
		// Nothing was stated, so the model's rationale is a guess — mark it so.
		out.Why = model.Why
		out.WhySrc = domain.WhyInferred
	}
	return out
}
