package usecase_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestSuppressCoveredNoTest(t *testing.T) {
	units := []domain.Unit{
		// TDD: the test is written first, in its own unit…
		{ID: "u1", Files: []string{"internal/plain/plain_test.go"}},
		// …then the implementation lands in a follow-up unit (false positive).
		{ID: "u2", Files: []string{"internal/plain/plain.go"}, Flags: []domain.Flag{domain.FlagNoTest}},
		// Genuinely untested source: no matching *_test file anywhere.
		{ID: "u3", Files: []string{"internal/port/port.go"}, Flags: []domain.Flag{domain.FlagNoTest}},
		// Covered, but other flags must survive.
		{ID: "u4", Files: []string{"internal/plain/plain.go"}, Flags: []domain.Flag{domain.FlagNoTest, domain.FlagLarge}},
	}

	got := usecase.SuppressCoveredNoTest(units)

	if hasFlag(got[1].Flags, domain.FlagNoTest) {
		t.Error("u2: no-test should be suppressed — plain_test.go is in the session")
	}
	if !hasFlag(got[2].Flags, domain.FlagNoTest) {
		t.Error("u3: no-test should remain — port.go has no matching test")
	}
	if hasFlag(got[3].Flags, domain.FlagNoTest) {
		t.Error("u4: no-test should be suppressed")
	}
	if !hasFlag(got[3].Flags, domain.FlagLarge) {
		t.Error("u4: the large flag must survive suppression")
	}
	// Order must not matter: put the impl before its test.
	swapped := usecase.SuppressCoveredNoTest([]domain.Unit{
		{ID: "a", Files: []string{"pkg/a.go"}, Flags: []domain.Flag{domain.FlagNoTest}},
		{ID: "b", Files: []string{"pkg/a_test.go"}},
	})
	if hasFlag(swapped[0].Flags, domain.FlagNoTest) {
		t.Error("suppression must be order-independent (test-after should still count)")
	}
}
