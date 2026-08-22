package null_test

import (
	"context"
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/summarizer/null"
	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

func TestNullReturnsMechanicalHeadlineUnchanged(t *testing.T) {
	mechanical := domain.Headline{Text: "2 edits across 1 file", Why: "", WhySrc: domain.WhyInferred}
	u := domain.Unit{ID: "u1", Headline: mechanical}

	got, err := null.New().Headline(context.Background(), u, domain.Diff{})
	if err != nil {
		t.Fatalf("Headline: %v", err)
	}

	if got != mechanical {
		t.Errorf("null Headline = %+v, want the mechanical headline %+v", got, mechanical)
	}
}
