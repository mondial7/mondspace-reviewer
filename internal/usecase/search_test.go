package usecase_test

import (
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func corpus() []usecase.Searchable {
	return []usecase.Searchable{
		{TargetID: "t1", TargetRef: "abc12345", TargetTitle: "Add the retry loop",
			Kind: "note", Text: "this retries forever on a 500", Where: "http/client.go"},
		{TargetID: "t1", TargetRef: "abc12345", TargetTitle: "Add the retry loop",
			Kind: "question", Text: "why is the backoff constant?"},
		{TargetID: "t2", TargetRef: "v1.0.0", TargetTitle: "v1.0.0",
			Kind: "finding", Text: "hardcoded secret in the signer", Where: "auth/token.go"},
		{TargetID: "t3", TargetRef: "def67890", TargetTitle: "Tidy the parser",
			Kind: "note", Text: "fine", Where: "parse/lex.go"},
	}
}

func TestSearchFindsWhatWasWrittenAcrossEveryReview(t *testing.T) {
	// "Where did I write that about the retry loop" had no answer, and notes
	// are supposedly the product (ADR 0030).
	got := usecase.Search("retry", corpus())

	if len(got) == 0 {
		t.Fatal("nothing found for a word that is there")
	}
	for _, h := range got {
		if !strings.Contains(strings.ToLower(h.Text+h.TargetTitle), "retr") {
			t.Errorf("%+v does not match the query", h)
		}
	}
}

func TestSearchIgnoresCase(t *testing.T) {
	if len(usecase.Search("BACKOFF", corpus())) != 1 {
		t.Error("a search should not care about case")
	}
}

func TestSearchMatchesEveryWordNotJustOne(t *testing.T) {
	// Two words is how someone narrows a search. Treating it as "either" makes
	// adding a word return more, which is the opposite of what they wanted.
	if got := usecase.Search("hardcoded secret", corpus()); len(got) != 1 {
		t.Errorf("got %d hits, want the one matching both words", len(got))
	}
	if got := usecase.Search("hardcoded parser", corpus()); len(got) != 0 {
		t.Errorf("got %d hits, want none — no entry has both", len(got))
	}
}

func TestSearchLooksAtWhereAsWellAsWhat(t *testing.T) {
	// A reviewer remembers the file at least as often as the wording.
	if got := usecase.Search("token.go", corpus()); len(got) != 1 {
		t.Errorf("got %d hits, want the finding on auth/token.go", len(got))
	}
}

func TestAnEmptyQueryFindsNothingRatherThanEverything(t *testing.T) {
	// Returning the whole corpus for an empty box is not a search result, it is
	// a dump.
	for _, q := range []string{"", "   "} {
		if got := usecase.Search(q, corpus()); len(got) != 0 {
			t.Errorf("Search(%q) returned %d hits", q, len(got))
		}
	}
}

func TestHitsAreGroupedByReviewInAStableOrder(t *testing.T) {
	// Two hits in one review read as one place to go back to, not two.
	got := usecase.Search("e", corpus())
	if len(got) < 2 {
		t.Fatalf("expected several hits, got %d", len(got))
	}
	seen := map[string]int{}
	last := ""
	for _, h := range got {
		if h.TargetID != last {
			if seen[h.TargetID] > 0 {
				t.Errorf("%s appears in two separate runs; hits should be grouped", h.TargetID)
			}
			last = h.TargetID
		}
		seen[h.TargetID]++
	}
}
