package usecase_test

import (
	"context"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

// fakeSummarizer returns a canned headline, or an error.
type fakeSummarizer struct {
	head domain.Headline
	err  error
}

func (s fakeSummarizer) Headline(context.Context, domain.Unit, domain.Diff) (domain.Headline, error) {
	return s.head, s.err
}

func TestSummarizePreservesStatedWhy(t *testing.T) {
	unit := domain.Unit{Headline: domain.Headline{
		Text: "2 edits across 1 file", Why: "swap the JWT lib later", WhySrc: domain.WhyStated,
	}}
	model := fakeSummarizer{head: domain.Headline{
		Text: "extracted validation behind a TokenValidator interface",
		Why:  "the model's own guess", WhySrc: domain.WhyInferred,
	}}

	got := usecase.Summarize(context.Background(), model, unit, domain.Diff{})

	if got.Text != "extracted validation behind a TokenValidator interface" {
		t.Errorf("Text = %q, want the model's WHAT text", got.Text)
	}
	if got.Why != "swap the JWT lib later" || got.WhySrc != domain.WhyStated {
		t.Errorf("Why/WhySrc = %q/%q, want the stated intent preserved verbatim", got.Why, got.WhySrc)
	}
}
