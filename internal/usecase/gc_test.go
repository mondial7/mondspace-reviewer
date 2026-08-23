package usecase_test

import (
	"reflect"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestSessionsToGCReturnsRefsWithNoStoreDir(t *testing.T) {
	cases := []struct {
		name   string
		refs   []string
		stored []string
		want   []string
	}{
		{
			name:   "mixed",
			refs:   []string{"a", "b", "c"},
			stored: []string{"b"},
			want:   []string{"a", "c"},
		},
		{
			name:   "none gone",
			refs:   []string{"a", "b"},
			stored: []string{"a", "b"},
			want:   nil,
		},
		{
			name:   "all gone",
			refs:   []string{"a", "b"},
			stored: nil,
			want:   []string{"a", "b"},
		},
		{
			name:   "no refs",
			refs:   nil,
			stored: []string{"a"},
			want:   nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := usecase.SessionsToGC(c.refs, c.stored)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("SessionsToGC(%v, %v) = %v, want %v", c.refs, c.stored, got, c.want)
			}
		})
	}
}
