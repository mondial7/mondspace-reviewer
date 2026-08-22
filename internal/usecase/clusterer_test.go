package usecase_test

import (
	"testing"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

func TestClustererEmitsAtSealNotBeforeFlush(t *testing.T) {
	c := usecase.NewClusterer("s")

	// Two actions, no seal yet.
	if _, ok := c.Push(ev("e1", domain.KindEdit, "a.go")); ok {
		t.Fatal("push e1 sealed prematurely")
	}
	if _, ok := c.Push(ev("e2", domain.KindWrite, "b.go")); ok {
		t.Fatal("push e2 sealed prematurely")
	}

	// batch_end seals the open unit.
	u, ok := c.Push(ev("e3", domain.KindBatchEnd))
	if !ok {
		t.Fatal("batch_end did not seal")
	}
	if u.ID != "s-u001" {
		t.Errorf("unit ID = %q, want s-u001", u.ID)
	}
	if len(u.EventIDs) != 2 || u.EventIDs[0] != "e1" || u.EventIDs[1] != "e2" {
		t.Errorf("EventIDs = %v, want [e1 e2]", u.EventIDs)
	}

	// One more action, then Flush seals the trailing unit.
	if _, ok := c.Push(ev("e4", domain.KindEdit, "c.go")); ok {
		t.Fatal("push e4 sealed prematurely")
	}
	if _, ok := c.Flush(); !ok {
		t.Fatal("Flush did not seal the trailing unit")
	}
	// A second flush yields nothing.
	if _, ok := c.Flush(); ok {
		t.Error("second Flush unexpectedly sealed")
	}
}
