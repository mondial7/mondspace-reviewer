// Package null is a passthrough Summarizer: it returns the unit's mechanical
// headline unchanged. It is the offline default and the fallback whenever the
// real summarizer is unreachable.
package null

import (
	"context"
	"errors"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// ErrOffline is returned by the null summarizer when asked a question: there is
// no model to answer it.
var ErrOffline = errors.New("summarizer offline — cannot answer questions")

type Summarizer struct{}

func New() *Summarizer { return &Summarizer{} }

func (Summarizer) Headline(_ context.Context, u domain.Unit, _ domain.Diff) (domain.Headline, error) {
	return u.Headline, nil
}

func (Summarizer) Answer(context.Context, string, domain.AskContext) (string, error) {
	return "", ErrOffline
}
