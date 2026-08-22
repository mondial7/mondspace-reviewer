package usecase_test

import (
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

func TestClusterEmptyLog(t *testing.T) {
	units := usecase.Cluster("sess-basic", nil)

	if len(units) != 0 {
		t.Errorf("Cluster(empty) = %d units, want 0", len(units))
	}
}
