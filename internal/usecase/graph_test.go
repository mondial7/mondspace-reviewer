package usecase_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// commits are written newest first, the order git hands them over in.
func commit(hash string, parents ...string) domain.GraphCommit {
	return domain.GraphCommit{Hash: hash, Short: hash, Parent: parents, TS: time.Now()}
}

func laneOf(t *testing.T, view usecase.GraphView, hash string) int {
	t.Helper()
	for _, n := range view.Nodes {
		if n.Hash == hash {
			return n.Lane
		}
	}
	t.Fatalf("%s is not in the graph", hash)
	return -1
}

// The mainline keeps one lane all the way down. If it wanders, the picture is
// harder to read than the list it replaced.
func TestAStraightHistoryIsOneLane(t *testing.T) {
	view := usecase.LayOutGraph([]domain.GraphCommit{
		commit("c", "b"), commit("b", "a"), commit("a"),
	})

	if view.Lanes != 1 {
		t.Errorf("laid out %d lanes for a straight history", view.Lanes)
	}
	for _, n := range view.Nodes {
		if n.Lane != 0 {
			t.Errorf("%s is in lane %d", n.Hash, n.Lane)
		}
	}
}

// A branch is a second lane that starts where it diverged and ends where it
// rejoins.
func TestABranchTakesALaneOfItsOwn(t *testing.T) {
	// m2 merges the branch b1 into the mainline m1 → base.
	view := usecase.LayOutGraph([]domain.GraphCommit{
		commit("m2", "m1", "b1"),
		commit("m1", "base"),
		commit("b1", "base"),
		commit("base"),
	})

	if laneOf(t, view, "m2") != laneOf(t, view, "m1") {
		t.Error("a merge and its first parent should share a lane")
	}
	if laneOf(t, view, "b1") == laneOf(t, view, "m1") {
		t.Error("the merged branch should have a lane of its own")
	}
	if laneOf(t, view, "base") != laneOf(t, view, "m1") {
		t.Error("the branch's lane should be free again once it has rejoined")
	}
}

// A merge is worth drawing differently: it is what a reader is looking for when
// they look at a shape rather than a list.
func TestAMergeIsMarked(t *testing.T) {
	view := usecase.LayOutGraph([]domain.GraphCommit{
		commit("m", "a", "b"), commit("a"), commit("b"),
	})

	for _, n := range view.Nodes {
		if n.Hash == "m" && !n.Merge {
			t.Error("a commit with two parents is a merge")
		}
		if n.Hash == "a" && n.Merge {
			t.Error("a commit with one parent is not")
		}
	}
}

// History is bounded, so the oldest commits on screen have parents nobody
// fetched. A line into the margin is a line to nowhere.
func TestAParentOutsideTheWindowEndsItsLine(t *testing.T) {
	view := usecase.LayOutGraph([]domain.GraphCommit{commit("a", "older-than-anything")})

	if len(view.Edges) != 0 {
		t.Errorf("drew %d edge(s) to a commit that is not on screen", len(view.Edges))
	}
}

// Every dot has to sit inside the box the SVG reserves, or half of them are cut
// off by it.
func TestTheDrawingFitsItsBox(t *testing.T) {
	view := usecase.LayOutGraph([]domain.GraphCommit{
		commit("m", "a", "b"), commit("a", "base"), commit("b", "base"), commit("base"),
	})

	for _, n := range view.Nodes {
		if n.X < 0 || n.X > view.Width || n.Y < 0 || n.Y > view.Height {
			t.Errorf("%s is at %d,%d, outside %dx%d", n.Hash, n.X, n.Y, view.Width, view.Height)
		}
	}
}

func TestAnEmptyHistoryDrawsNothing(t *testing.T) {
	if usecase.LayOutGraph(nil).Any() {
		t.Error("an empty history has something to draw")
	}
}

// Two branches off one commit is the shape the branches page exists to show,
// and it is where naive lane assignment goes wrong: both branches reserve a
// lane for the same parent, the parent is drawn in one of them, and the other
// line ends at the right height beside the wrong lane.
func TestEveryLineEndsOnADot(t *testing.T) {
	view := usecase.LayOutGraph([]domain.GraphCommit{
		commit("tip", "base"),
		commit("one", "base"),
		commit("two", "base"),
		commit("base", "old"),
		commit("old"),
	})

	ends := map[string]bool{}
	for _, n := range view.Nodes {
		ends[itoa(n.X)+","+itoa(n.Y)] = true
	}
	for _, e := range view.Edges {
		fields := strings.Fields(e.D)
		last := fields[len(fields)-2] + "," + fields[len(fields)-1]
		if !ends[last] {
			t.Errorf("edge %q ends at %s, where there is no commit", e.D, last)
		}
	}
	if len(view.Edges) != 4 {
		t.Errorf("drew %d lines for 4 parent links", len(view.Edges))
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// A pushed branch decorates two commits with the same name — the remote's tip
// and whatever the local checkout is still sitting at. Both cannot carry the
// anchor the list beside the graph links to.
func TestABranchNameAnchorsOnce(t *testing.T) {
	ahead := commit("b", "a")
	ahead.Refs = []string{"main"}
	behind := commit("a")
	behind.Refs = []string{"main"}

	view := usecase.LayOutGraph([]domain.GraphCommit{ahead, behind})

	anchors := 0
	for _, n := range view.Nodes {
		if n.Anchor != "" {
			anchors++
		}
		if n.Hash == "a" && (n.Anchor == "main" || len(n.Tip) != 0) {
			t.Error("main is still on the commit the branch has moved past")
		}
	}
	if anchors != 1 {
		t.Errorf("%d commits answer to main; only one element can hold an id", anchors)
	}
}
