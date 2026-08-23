package usecase_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestFingerprintIsStableForTheSameReview(t *testing.T) {
	// Narration costs several model calls. Reusing a stored story is only safe
	// if the fingerprint says the review is unchanged, so it must be stable
	// across runs — not a hash of anything that varies per process.
	a := usecase.Fingerprint(narrativeUnits())
	b := usecase.Fingerprint(narrativeUnits())

	if a != b {
		t.Errorf("Fingerprint is not stable: %q vs %q", a, b)
	}
	if a == "" {
		t.Error("Fingerprint must not be empty")
	}
}

func TestFingerprintChangesWhenTheReviewDoes(t *testing.T) {
	base := usecase.Fingerprint(narrativeUnits())

	cases := []struct {
		name  string
		units []domain.Unit
	}{
		{"a file changed", func() []domain.Unit {
			u := narrativeUnits()
			u[0].Files = []string{"auth/session.go"}
			return u
		}()},
		{"a file was added", append(narrativeUnits(),
			domain.Unit{ID: "s-f005", Files: []string{"db/pool.go"}})},
		{"a file was removed", narrativeUnits()[:3]},
		{"the content changed", func() []domain.Unit {
			u := narrativeUnits()
			u[1].To = domain.SnapshotRef{Commit: "deadbeef"}
			return u
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := usecase.Fingerprint(tc.units); got == base {
				t.Errorf("Fingerprint did not change when %s", tc.name)
			}
		})
	}
}

func TestFingerprintIgnoresOrder(t *testing.T) {
	// The same files reviewed in a different order are the same review; a story
	// written for them is still valid.
	units := narrativeUnits()
	shuffled := []domain.Unit{units[2], units[0], units[3], units[1]}

	if usecase.Fingerprint(units) != usecase.Fingerprint(shuffled) {
		t.Error("Fingerprint should not depend on unit order")
	}
}
