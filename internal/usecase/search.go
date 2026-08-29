package usecase

import (
	"sort"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// Searchable is one thing a reviewer wrote or was told, with enough about where
// it came from to go back to it (ADR 0030).
type Searchable struct {
	TargetID    string
	TargetRef   string
	TargetTitle string
	// Kind is what it is: note, question, answer, finding.
	Kind string
	// Where is the file it concerns, when it concerns one.
	Where string
	Text  string
}

// Search finds what a reviewer wrote, across every review in the workspace.
//
// "Where did I write that about the retry loop" had no answer, and the review
// log is what this tool exists to produce.
//
// Every word must match, not any: two words is how someone narrows a search,
// and treating it as "either" makes adding a word return *more*, which is the
// opposite of what they asked for.
func Search(query string, in []Searchable) []Searchable {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		// An empty box is not a query. Returning everything would be a dump.
		return nil
	}

	var hits []Searchable
	for _, s := range in {
		hay := strings.ToLower(strings.Join([]string{s.Text, s.Where, s.TargetTitle, s.Kind}, " "))
		matched := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				matched = false
				break
			}
		}
		if matched {
			hits = append(hits, s)
		}
	}

	// Grouped by review: two hits in one place are one place to go back to.
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].TargetID != hits[j].TargetID {
			return hits[i].TargetID < hits[j].TargetID
		}
		return hits[i].Kind < hits[j].Kind
	})
	return hits
}

// SearchableReview turns one review's written record into things that can be
// found again.
func SearchableReview(targetID, ref, title string, notes []domain.Note,
	exchanges []domain.Exchange, analyses []domain.Analysis, unitFile func(string) string) []Searchable {

	var out []Searchable
	add := func(kind, where, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		out = append(out, Searchable{
			TargetID: targetID, TargetRef: ref, TargetTitle: title,
			Kind: kind, Where: where, Text: text,
		})
	}

	for _, n := range notes {
		// The note's own record of what it was about, falling back to the review
		// for notes written before that was kept.
		where := n.File
		if where == "" && unitFile != nil {
			where = unitFile(n.UnitID)
		}
		add(string(n.Kind), where, n.Text)
	}
	for _, e := range exchanges {
		add("question", "", e.Question)
		add("answer", "", e.Answer)
	}
	for _, a := range analyses {
		for _, f := range a.Findings {
			add(string(a.Kind), f.File, f.Note)
		}
	}
	return out
}
