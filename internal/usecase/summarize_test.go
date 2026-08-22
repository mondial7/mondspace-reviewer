package usecase_test

import (
	"context"
	"errors"
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

func TestSummarizeNeverLetsModelFabricateStated(t *testing.T) {
	// The unit stated nothing, but a misbehaving model claims a stated rationale.
	unit := domain.Unit{Headline: domain.Headline{Text: "1 edit", Why: "", WhySrc: domain.WhyInferred}}
	rogue := fakeSummarizer{head: domain.Headline{
		Text: "renamed the field", Why: "the user asked for this", WhySrc: domain.WhyStated,
	}}

	got := usecase.Summarize(context.Background(), rogue, unit, domain.Diff{})

	if got.WhySrc == domain.WhyStated {
		t.Error("model must never be able to assert a stated rationale")
	}
	if got.WhySrc != domain.WhyInferred {
		t.Errorf("WhySrc = %q, want inferred", got.WhySrc)
	}
}

func TestSummarizeDegradesOnError(t *testing.T) {
	mechanical := domain.Headline{Text: "2 edits across 1 file", Why: "stated thing", WhySrc: domain.WhyStated}
	unit := domain.Unit{Headline: mechanical}
	model := fakeSummarizer{err: errors.New("connection refused")}

	got := usecase.Summarize(context.Background(), model, unit, domain.Diff{})

	if got != mechanical {
		t.Errorf("on error, headline = %+v, want the mechanical headline %+v", got, mechanical)
	}
}

func TestSummarizeInfersWhyWhenNoneStated(t *testing.T) {
	unit := domain.Unit{Headline: domain.Headline{
		Text: "3 edits across 2 files", Why: "", WhySrc: domain.WhyInferred,
	}}
	model := fakeSummarizer{head: domain.Headline{
		Text: "added retry with backoff to the HTTP client",
		Why:  "to survive transient network failures", WhySrc: domain.WhyInferred,
	}}

	got := usecase.Summarize(context.Background(), model, unit, domain.Diff{})

	if got.Text != "added retry with backoff to the HTTP client" {
		t.Errorf("Text = %q, want the model's WHAT", got.Text)
	}
	if got.Why != "to survive transient network failures" || got.WhySrc != domain.WhyInferred {
		t.Errorf("Why/WhySrc = %q/%q, want the model's inferred rationale", got.Why, got.WhySrc)
	}
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
