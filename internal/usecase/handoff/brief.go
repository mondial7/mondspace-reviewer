// Package handoff assembles what a push sends: an ordered, self-contained
// brief rather than a list of findings in the order a linter emitted them
// (ADR 0046).
package handoff

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
)

// What a push actually sends (ADR 0046).
//
// Not a list of findings in the order a linter emitted them: an ordered brief
// that an agent with nothing loaded can work down a file at a time, with the
// two-directives-on-one-line case caught before it is sent rather than
// resolved silently by whoever reads it.

// Brief is one batch of work handed to an implementation agent.
type Brief struct {
	// Batch is the id that makes a push idempotent. Sending the same batch
	// twice sends nothing the second time.
	Batch  string
	Branch string
	At     time.Time
	// Items are in the order they should be worked: by file, then by line.
	Items []contract.Item
	// Conflicts are pairs of items whose line ranges overlap. They are reported
	// rather than resolved — which of two directives is right is not something
	// msr can know.
	Conflicts []Conflict
}

// Conflict is two directives touching the same lines.
type Conflict struct {
	Path string
	A    contract.Item
	B    contract.Item
}

// Empty reports whether there is anything to send. A push with nothing in it is
// not an error; it is the normal answer to "send everything accepted" when
// everything accepted has already gone.
func (b Brief) Empty() bool { return len(b.Items) == 0 }

// Assemble orders a selection into a brief and finds the overlaps.
//
// Anything that may not be pushed is dropped here rather than refused: a
// selection of twelve findings, two of which were dismissed this morning,
// should send ten rather than fail.
func Assemble(batch, branch string, at time.Time, selection []contract.Item) Brief {
	var items []contract.Item
	for _, item := range selection {
		if item.Pushable() {
			items = append(items, item)
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].Location, items[j].Location
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return items[i].ID < items[j].ID
	})

	return Brief{
		Batch:     batch,
		Branch:    branch,
		At:        at,
		Items:     items,
		Conflicts: overlaps(items),
	}
}

// overlaps finds directives that touch the same lines.
//
// File-wide items — line zero — overlap nothing. "This file has no test" and
// "line 40 swallows an error" are not two instructions about one place, and
// flagging them would make the warning meaningless by making it constant.
func overlaps(items []contract.Item) []Conflict {
	var found []Conflict
	for i := 0; i < len(items); i++ {
		a := items[i]
		if a.Location.StartLine == 0 {
			continue
		}
		for j := i + 1; j < len(items); j++ {
			b := items[j]
			if b.Location.Path != a.Location.Path {
				break // sorted by path: nothing after this is in the same file
			}
			if b.Location.StartLine == 0 {
				continue
			}
			if b.Location.StartLine <= end(a) && a.Location.StartLine <= end(b) {
				found = append(found, Conflict{Path: a.Location.Path, A: a, B: b})
			}
		}
	}
	return found
}

// end is an item's last line, for an item whose end was never set.
func end(item contract.Item) int {
	if item.Location.EndLine > item.Location.StartLine {
		return item.Location.EndLine
	}
	return item.Location.StartLine
}

// MarkPushed is what the store records once a brief has gone out.
//
// The transition is what stops the same finding being sent twice and what lets
// the next post-mortem pass ask whether the agent actually did it (ADR 0046).
func MarkPushed(b Brief, by string) []contract.Item {
	out := make([]contract.Item, 0, len(b.Items))
	for _, item := range b.Items {
		at := b.At
		item.State = contract.StatePushed
		item.PushedAt = &at
		item.PushBatch = b.Batch
		item.PushedBy = by
		item.LastSeen = b.At
		out = append(out, item)
	}
	return out
}

// AlreadySent reports whether this batch has been delivered before.
//
// Idempotence is per batch id and it is checked against the store rather than
// remembered in the process, because the retry is usually a second command
// after the first one failed halfway.
func AlreadySent(stored []contract.Item, batch string) bool {
	for _, item := range stored {
		if item.PushBatch == batch {
			return true
		}
	}
	return false
}

// Render writes the brief an agent is handed.
//
// Markdown because every agent reads it and a person can check it before it
// goes. The item id is on every entry so that what comes back can be matched to
// what went out.
func Render(b Brief) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Review brief %s\n\n", b.Batch)
	fmt.Fprintf(&out, "%d item(s) on `%s`, %s.\n", len(b.Items), b.Branch, b.At.UTC().Format(time.RFC3339))
	out.WriteString("Work them in the order given. Leave anything you disagree with undone and say so.\n")

	if len(b.Conflicts) > 0 {
		out.WriteString("\n## Overlapping directives\n\n")
		out.WriteString("These touch the same lines. Read both before changing either.\n\n")
		for _, c := range b.Conflicts {
			fmt.Fprintf(&out, "- `%s` — %s and %s\n", c.Path, c.A.ID, c.B.ID)
		}
	}

	path := ""
	for _, item := range b.Items {
		if item.Location.Path != path {
			path = item.Location.Path
			fmt.Fprintf(&out, "\n## %s\n", path)
		}
		out.WriteString("\n### " + item.Where() + "\n\n")
		fmt.Fprintf(&out, "- **do:** %s\n", item.Directive)
		fmt.Fprintf(&out, "- **why:** %s\n", severityReason(item))
		fmt.Fprintf(&out, "- **id:** `%s`\n", item.ID)
		if item.Snippet != "" {
			out.WriteString("\n```\n" + strings.TrimRight(item.Snippet, "\n") + "\n```\n")
		}
	}
	return out.String()
}

// severityReason is the one line that says who is asking and how much it
// matters, so an agent weighing two directives has something to weigh.
func severityReason(item contract.Item) string {
	who := string(item.Source)
	if item.Producer != "" {
		who = item.Producer
	}
	if item.RuleID != "" {
		who += " " + item.RuleID
	}
	return fmt.Sprintf("%s, severity %s", who, item.Severity.Normalise())
}

// Delivery is how a brief reaches an agent (ADR 0046).
//
// A port rather than a switch statement because agents run in other terminals
// and every one of their CLIs is different. The thing msr can always do is
// write a file; everything else is convenience over the same brief.
type Delivery interface {
	// Name is what the reviewer asked for: "file", "stdout", "clipboard".
	Name() string
	// Send delivers the brief, or says why it could not. It is called once per
	// batch, after the brief has been assembled and before the store records
	// the push — a delivery that fails must leave nothing marked as sent.
	Send(b Brief) error
}
