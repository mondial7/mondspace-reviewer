package usecase

import (
	"strings"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// Turning what a reading produced into what the store holds (ADR 0044).
//
// This is the one place a `Reported` or a `Note` becomes an `Item`, so it is
// the one place that has to know how a fingerprint is built and what an agent
// is told to do about a finding. Everything downstream — reconciliation, the
// store, the push, the exports — sees only items.

// SnippetRadius is how many lines of context travel with an item.
//
// Enough for an agent with no repository loaded to see what it is looking at,
// and not so much that a batch of twenty findings is a file dump. The
// fingerprint window is a different number for a different reason: this one is
// read by a person or a model, that one is hashed.
const SnippetRadius = 2

// Sighting is one pass's worth of context: which branch, which run, and how to
// read a file and mint an id.
//
// Reading files is a function rather than a port because the caller already has
// them — a live pass holds the working tree, a post-mortem pass holds a commit's
// contents — and neither should be made to write them somewhere first.
type Sighting struct {
	Branch    string
	SessionID string
	// Commit is the revision the locations were resolved against. A live pass
	// has none: the tree is uncommitted by definition.
	Commit string
	At     time.Time
	// Mint returns a new id. It is called once per sighting; a sighting that
	// turns out to be one msr has seen before keeps the id it was first given
	// and this one is discarded (see Reconcile).
	Mint func() string
	// Body returns a file's contents, or "" if it cannot be read. A file that
	// has been deleted since the tool ran is ordinary, not an error: the item
	// is still worth recording, it just gets no window and no snippet.
	Body func(path string) string
}

// Seen fills in what a decoder could not know.
//
// A SARIF reader knows the rule, the file and the sentence; it does not know
// which branch this is, what the code around the finding looks like, or what a
// downstream agent should do about it. That is this: the identity, the
// locality, and the directive, all in one place so there is one answer to each
// (ADR 0048).
func (s Sighting) Seen(found []contract.Item) []contract.Item {
	out := make([]contract.Item, 0, len(found))
	for _, item := range found {
		item.Location.Path = contract.NormalisePath(item.Location.Path)
		item.Location.Commit = s.Commit
		lines := s.lines(item.Location.Path)

		item.ID = s.Mint()
		item.Fingerprint = contract.Fingerprint(
			item.Location.Path,
			item.Ref(),
			contract.Window(lines, item.Location.StartLine, item.Location.EndLine),
		)
		item.Severity = item.Severity.Normalise()
		if item.Source == "" {
			item.Source = contract.SourceAnalyser
		}
		if item.Title == "" {
			item.Title = title(strings.TrimSuffix(item.Ref(), "/"), item.Message)
		}
		if item.Directive == "" {
			item.Directive = directiveFor(item)
		}
		if item.Snippet == "" {
			item.Snippet = snippet(lines, item.Location.StartLine)
		}
		if item.State == "" {
			item.State = contract.StateOpen
		}
		item.Branch = s.Branch
		if item.SessionID == "" {
			item.SessionID = s.SessionID
		}
		item.FirstSeen, item.LastSeen = s.At, s.At
		out = append(out, item)
	}
	return out
}

// FromNote finishes a reviewer's annotation into a stored item.
//
// A note arrives already carrying most of what an item needs — its own id, the
// unit, the kind, the anchor — so this fills in only what the lens could not
// know: the identity, the branch, and how much it should interrupt.
//
// Its fingerprint is built from the anchor for the same reason the anchor
// exists: the line it was written about is what identifies it, wherever that
// line has moved to (ADR 0028).
func (s Sighting) FromNote(n contract.Item) contract.Item {
	n.Location.Path = contract.NormalisePath(n.Location.Path)
	n.Location.Commit = s.Commit

	anchor := []string{n.Anchor}
	if n.Anchor == "" {
		// A note about the file as a whole. Its own id is the only thing that
		// makes it distinct from the next one, and that is enough: a human note
		// is written once and never re-raised by a later pass.
		anchor = []string{n.ID}
	}

	n.Fingerprint = contract.Fingerprint(n.Location.Path, "human:"+string(n.Kind), anchor)
	n.Source = contract.SourceHuman
	n.Producer = "human"
	n.Severity = noteSeverity(n.Kind)
	n.Title = title(string(n.Kind), n.Message)
	if n.Directive == "" {
		n.Directive = n.Message
	}
	if n.State == "" {
		n.State = contract.StateOpen
	}
	n.Branch = s.Branch
	if n.LastSeen.IsZero() {
		n.LastSeen = n.FirstSeen
	}
	return n
}

// lines splits a file, once, for whichever findings are in it.
func (s Sighting) lines(path string) []string {
	if s.Body == nil {
		return nil
	}
	body := s.Body(path)
	if body == "" {
		return nil
	}
	return strings.Split(body, "\n")
}

// directiveFor is what a downstream agent should do about a finding.
//
// An item without one is not actionable, so every producer path has to end with
// one rather than leave it empty (ADR 0044). This is the deterministic default:
// the tool's own sentence, addressed to whoever has to act on it. A reviewer
// who rewrites it is the point, and their version is what gets stored from then
// on.
func directiveFor(item contract.Item) string {
	rule := item.Producer
	if item.RuleID != "" {
		rule = item.Producer + "'s " + item.RuleID
	}
	return "Resolve " + rule + " at " + item.Where() + ": " + strings.TrimSpace(item.Message)
}

// noteSeverity maps what the reviewer meant onto how much it should interrupt.
//
// `objection` is somebody saying do not merge this, which is what high means.
// `question` and `debt` are worth answering and worth remembering, neither of
// which blocks. The kinds that are the reviewer thinking aloud do not become
// work at all, and are filtered before they reach here.
func noteSeverity(kind contract.Kind) contract.Severity {
	switch kind {
	case contract.KindObjection:
		return contract.SeverityHigh
	case contract.KindQuestion:
		return contract.SeverityMedium
	default:
		return contract.SeverityLow
	}
}

// title is the one line a list shows: what fired, and the first sentence of
// what it said.
func title(prefix, message string) string {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\n", " "))
	if cut := strings.IndexAny(message, ".\n"); cut > 0 {
		message = message[:cut]
	}
	const room = 72
	if len(message) > room {
		message = strings.TrimSpace(message[:room]) + "…"
	}
	if message == "" {
		return prefix
	}
	return prefix + " — " + message
}

// snippet is the code an agent is being asked to change, with a little either
// side so it can see where it is.
func snippet(lines []string, line int) string {
	if line <= 0 || len(lines) == 0 {
		return ""
	}
	from := line - 1 - SnippetRadius
	if from < 0 {
		from = 0
	}
	to := line + SnippetRadius
	if to > len(lines) {
		to = len(lines)
	}
	if from >= to {
		return ""
	}
	return strings.Join(lines[from:to], "\n")
}

// OnlyIn keeps the findings that are about files this change touched.
//
// It is what makes a whole-project source usable beside the diff-scoped ones: a
// server that has been analysing a repository for two years holds thousands of
// issues, and showing them next to a two-file change is the noise this whole
// layer is arranged to avoid (ADR 0043).
func OnlyIn(found []contract.Item, paths map[string]bool) []contract.Item {
	var out []contract.Item
	for _, r := range found {
		if paths[contract.NormalisePath(r.Location.Path)] {
			out = append(out, r)
		}
	}
	return out
}

// FromAnalysis converts one model reading — the security pass, the
// breaking-change pass — into items.
//
// A model's finding has no rule and no line: it is a sentence about a file
// (ADR 0024). So its identity is the file, the reading it came from, and the
// sentence itself, normalised. That has a cost worth stating plainly: a model
// that rephrases the same objection raises it again as a new item. The
// alternative — treating any two findings in one file as the same one — would
// merge two real problems into one record, and losing a finding is worse than
// seeing it twice.
func (s Sighting) FromAnalysis(a domain.Analysis) []contract.Item {
	producer := a.Model
	if producer == "" {
		producer = string(a.Engine)
	}

	out := make([]contract.Item, 0, len(a.Findings))
	for _, f := range a.Findings {
		f.Location.Path = contract.NormalisePath(f.Location.Path)
		f.Location.Commit = s.Commit
		f.ID = s.Mint()
		f.Source = contract.SourceLLM
		f.Producer = producer
		f.Fingerprint = contract.Fingerprint(
			f.Location.Path, "llm:"+string(a.Kind)+":"+said(f.Message), nil)
		f.Title = title(string(a.Kind), f.Message)
		if f.Directive == "" {
			f.Directive = f.Message
		}
		if f.State == "" {
			f.State = contract.StateOpen
		}
		f.Branch = s.Branch
		if f.SessionID == "" {
			f.SessionID = s.SessionID
		}
		f.FirstSeen, f.LastSeen = s.At, s.At
		out = append(out, f)
	}
	return out
}

// said normalises a model's sentence enough that the same objection, capitalised
// differently or with the whitespace rearranged, is the same objection.
func said(note string) string {
	return strings.ToLower(strings.Join(strings.Fields(note), " "))
}
