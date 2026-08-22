package usecase_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestMechanicalHeadlineWhyIsFirstStatedIntent(t *testing.T) {
	events := []domain.Event{
		{ID: "e1", Kind: domain.KindEdit, Files: []string{"a.go"}, StatedIntent: ""},
		{ID: "e2", Kind: domain.KindEdit, Files: []string{"a.go"}, StatedIntent: "swap the JWT lib later"},
		{ID: "e3", Kind: domain.KindEdit, Files: []string{"a.go"}, StatedIntent: "second intent, ignored"},
	}

	got := usecase.MechanicalHeadline(events)

	if got.Why != "swap the JWT lib later" {
		t.Errorf("Why = %q, want first non-empty stated intent", got.Why)
	}
	if got.WhySrc != domain.WhyStated {
		t.Errorf("WhySrc = %q, want %q", got.WhySrc, domain.WhyStated)
	}
}

func TestMechanicalHeadlineWhyInferredWhenNoIntent(t *testing.T) {
	events := []domain.Event{
		{ID: "e1", Kind: domain.KindEdit, Files: []string{"a.go"}},
		{ID: "e2", Kind: domain.KindEdit, Files: []string{"a.go"}},
	}

	got := usecase.MechanicalHeadline(events)

	if got.Why != "" {
		t.Errorf("Why = %q, want empty", got.Why)
	}
	if got.WhySrc != domain.WhyInferred {
		t.Errorf("WhySrc = %q, want %q", got.WhySrc, domain.WhyInferred)
	}
}

func TestMechanicalHeadlineText(t *testing.T) {
	tests := []struct {
		name   string
		events []domain.Event
		want   string
	}{
		{
			name: "three edits two files",
			events: []domain.Event{
				ev("e1", domain.KindEdit, "http/middleware.go"),
				ev("e2", domain.KindEdit, "http/middleware.go"),
				ev("e3", domain.KindEdit, "http/routes.go"),
			},
			want: "3 edits across 2 files",
		},
		{
			name: "mixed kinds single file",
			events: []domain.Event{
				ev("e1", domain.KindEdit, "auth/token.go"),
				ev("e2", domain.KindWrite, "auth/token.go"),
			},
			want: "1 edit, 1 write across 1 file",
		},
		{
			name: "bash with no files",
			events: []domain.Event{
				ev("e1", domain.KindBash),
			},
			want: "1 bash across 0 files",
		},
		{
			name: "multiple bashes pluralize with es",
			events: []domain.Event{
				ev("e1", domain.KindBash),
				ev("e2", domain.KindBash),
			},
			want: "2 bashes across 0 files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := usecase.MechanicalHeadline(tt.events)
			if got.Text != tt.want {
				t.Errorf("Text = %q, want %q", got.Text, tt.want)
			}
		})
	}
}
