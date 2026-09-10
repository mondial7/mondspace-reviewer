package domain

import "time"

// GraphCommit is one commit as the network graph needs it: where it sits in
// history, and what points at it.
//
// It is separate from Commit because the graph needs the parents and Commit
// does not: everything else in msr reads history as a list, and only this reads
// it as a shape.
type GraphCommit struct {
	Hash   string
	Short  string
	Parent []string
	// Refs is what points here — branch names, without the remote — so a lane
	// can be labelled with the branch whose tip it is.
	Refs    []string
	Subject string
	Author  string
	TS      time.Time
}
