// Package null is a passthrough Summarizer: it returns the unit's mechanical
// headline unchanged. It is the offline default and the fallback whenever the
// real summarizer is unreachable.
package null

import (
	"context"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

type Summarizer struct{}

func New() *Summarizer { return &Summarizer{} }

func (Summarizer) Headline(_ context.Context, u domain.Unit, _ domain.Diff) (domain.Headline, error) {
	return u.Headline, nil
}
