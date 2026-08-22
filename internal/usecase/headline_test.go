package usecase_test

import (
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

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
