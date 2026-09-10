package usecase

import (
	"strconv"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// Laying history out as a picture (ADR 0056).
//
// A list of commits answers "what happened lately". It does not answer "who is
// working on what, and how far apart are they" — which is the question the
// branches page exists for, and the one a shape answers in a glance and a
// column of cards never does.
//
// The layout is here rather than in the template or in a script because it is
// the only interesting part: which lane a commit belongs in, and where the
// lines between them go. Everything downstream is coordinates.

// Lane geometry, in the units the SVG is drawn in. They are here rather than in
// the stylesheet because the paths are computed, and a path computed against
// one number and drawn against another is a graph that misses its own dots.
const (
	// LaneWidth is the distance between two lanes.
	LaneWidth = 18
	// RowHeight is the distance between two commits.
	RowHeight = 34
	// GraphPad is the margin around the drawing, so a dot on lane zero is not
	// cut in half by the edge.
	GraphPad = 12
)

// GraphNode is one commit, placed.
type GraphNode struct {
	domain.GraphCommit
	// Lane is which column it sits in, and X and Y are where that puts it.
	Lane int
	X, Y int
	// Merge says this commit brought two histories together, which is worth
	// drawing differently: it is the one kind of commit a reader is looking for
	// when they look at a shape rather than a list.
	Merge bool
	// Tip is the branch names that point here, if any.
	Tip []string
	// Anchor is the one of those names this commit answers to, so the list
	// beside the graph has somewhere to link. A branch pushed but not merged
	// decorates two commits with the same name — the remote's tip and the local
	// one behind it — and two elements cannot share an id, so the newest wins.
	Anchor string
	// Ago is how long ago it landed, filled in by whoever is showing it: how
	// long ago something was is a presentation question.
	Ago string
}

// link is a commit and one of its parents, by row, before either has been
// placed.
type link struct{ from, to int }

// GraphEdge is a line from a commit to one of its parents, as an SVG path.
type GraphEdge struct {
	D string
	// Lane is the parent's lane, which is what colours the line: an edge
	// belongs to the history it is going into.
	Lane int
}

// GraphView is a whole picture, ready to draw.
type GraphView struct {
	Nodes  []GraphNode
	Edges  []GraphEdge
	Lanes  int
	Width  int
	Height int
	// anchors is which branch names have a dot to point at. History is
	// bounded, so a branch whose tip is older than the window has a card in
	// the list and nothing to link to.
	anchors map[string]bool
}

// Any reports whether there is anything to draw.
func (v GraphView) Any() bool { return len(v.Nodes) > 0 }

// Has reports whether this branch name is drawn, so a list beside the picture
// can offer a link only where there is somewhere to go.
func (v GraphView) Has(branch string) bool { return v.anchors[branch] }

// LayOutGraph places commits into lanes and works out the lines between them.
//
// The rule is the one every git graph uses: a commit takes the lane its first
// child left for it, its first parent inherits that lane, and every other
// parent starts a lane of its own. What makes it readable is that the mainline
// keeps a single lane all the way down, because each commit hands its lane to
// the parent it came from.
//
// Commits arrive newest first and are drawn in that order. A parent msr did not
// fetch — history is bounded, so the oldest commits here have parents nobody
// asked for — ends its lane rather than dangling a line into the margin.
func LayOutGraph(commits []domain.GraphCommit) GraphView {
	if len(commits) == 0 {
		return GraphView{}
	}

	row := make(map[string]int, len(commits))
	for i, c := range commits {
		row[c.Hash] = i
	}

	// lanes[i] is the hash the lane is waiting for, or "" when it is free.
	var lanes []string
	// links are the edges, by row, until every commit has a lane.
	var links []link
	view := GraphView{Nodes: make([]GraphNode, 0, len(commits))}

	// take is the lane already waiting for this hash, or a free one.
	take := func(hash string) int {
		for i, want := range lanes {
			if want == hash {
				return i
			}
		}
		for i, want := range lanes {
			if want == "" {
				lanes[i] = hash
				return i
			}
		}
		lanes = append(lanes, hash)
		return len(lanes) - 1
	}

	for i, c := range commits {
		lane := take(c.Hash)

		// A merge is drawn where two lanes arrive at one commit, so every other
		// lane waiting for it is done.
		for j, want := range lanes {
			if j != lane && want == c.Hash {
				lanes[j] = ""
			}
		}

		node := GraphNode{
			GraphCommit: c,
			Lane:        lane,
			X:           GraphPad + lane*LaneWidth,
			Y:           GraphPad + i*RowHeight,
			Merge:       len(c.Parent) > 1,
			Tip:         c.Refs,
		}

		// The first parent keeps this lane; the rest start their own.
		//
		// Where the line actually arrives is not known yet: two branches can
		// reserve a lane each for the same parent, and the parent is drawn in
		// only one of them. So the edges are remembered by row and drawn once
		// every commit has a lane — the alternative is a line that ends in the
		// right place vertically and the wrong one sideways, which is the one
		// mistake a graph is not allowed to make.
		lanes[lane] = ""
		for n, parent := range c.Parent {
			at, known := row[parent]
			if !known {
				continue // outside the window msr fetched
			}
			if n == 0 {
				lanes[lane] = parent
			} else {
				take(parent)
			}
			links = append(links, link{from: i, to: at})
		}

		view.Nodes = append(view.Nodes, node)
	}

	// A name belongs to one commit: the newest one carrying it. A checkout
	// sitting behind what it tracks puts the same name on two commits once the
	// remote prefix is off, and a graph with two dots both labelled `main` is
	// asking its reader to work out which one is meant. It is also two elements
	// with one id, which is not a thing a page can link into.
	claimed := map[string]bool{}
	for i := range view.Nodes {
		// A fresh slice: Tip aliases the refs the caller handed over, and
		// filtering in place would edit their copy.
		kept := make([]string, 0, len(view.Nodes[i].Tip))
		for _, tip := range view.Nodes[i].Tip {
			if claimed[tip] {
				continue
			}
			claimed[tip] = true
			kept = append(kept, tip)
		}
		view.Nodes[i].Tip = nil
		if len(kept) > 0 {
			view.Nodes[i].Tip = kept
			view.Nodes[i].Anchor = kept[0]
		}
	}

	for _, l := range links {
		from, to := view.Nodes[l.from], view.Nodes[l.to]
		view.Edges = append(view.Edges, GraphEdge{
			D:    edgePath(from.X, from.Y, to.X, to.Y),
			Lane: to.Lane,
		})
	}

	view.anchors = claimed
	view.Lanes = len(lanes)
	view.Width = GraphPad*2 + max(1, view.Lanes)*LaneWidth
	view.Height = GraphPad*2 + len(commits)*RowHeight
	return view
}

// edgePath is the line from a commit to a parent.
//
// Straight down when they share a lane. Otherwise it holds the commit's own
// lane all the way down and turns into the parent's only in the last half row.
// Turning early would be shorter and wrong: the line would spend most of its
// length lying on top of the lane it is joining, and two branches off the same
// commit would be one thick line rather than two.
func edgePath(x1, y1, x2, y2 int) string {
	if x1 == x2 {
		return "M " + itoa(x1) + " " + itoa(y1) + " L " + itoa(x2) + " " + itoa(y2)
	}
	// Where the turn begins. Adjacent rows leave only the gap between them,
	// which is enough for the curve and nothing else.
	bend := y2 - RowHeight/2
	if bend < y1 {
		bend = y1
	}
	return "M " + itoa(x1) + " " + itoa(y1) +
		" L " + itoa(x1) + " " + itoa(bend) +
		" C " + itoa(x1) + " " + itoa(y2) + " " + itoa(x2) + " " + itoa(bend) + " " +
		itoa(x2) + " " + itoa(y2)
}

func itoa(n int) string { return strconv.Itoa(n) }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
