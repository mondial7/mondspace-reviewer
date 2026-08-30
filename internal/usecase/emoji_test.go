package usecase_test

import (
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestOnlyActualEmojiSurvive(t *testing.T) {
	// A small model asked for emoji answers with words, punctuation, ":sparkles:"
	// and occasionally a letter. None of those belong on a card that is meant to
	// be read at a glance, and there is no schema that can insist.
	for _, c := range []struct {
		name string
		in   []string
		want string
	}{
		{"real ones pass", []string{"🔒", "🧭", "🧪"}, "🔒 🧭 🧪"},
		{"words are dropped", []string{"🔒", "security", "🧪"}, "🔒 🧪"},
		{"shortcodes are dropped", []string{":sparkles:", "✨"}, "✨"},
		{"letters and punctuation are dropped", []string{"a", "!", "→", "🚀"}, "🚀"},
		{"duplicates are dropped", []string{"🚀", "🚀", "🔒"}, "🚀 🔒"},
		{"nothing usable is nothing", []string{"tests", "docs"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := strings.Join(usecase.CleanEmoji(c.in), " "); got != c.want {
				t.Errorf("CleanEmoji(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestAtMostFiveAndNeverAWallOfThem(t *testing.T) {
	// Three to five is a glance. Nine is a sentence in a language nobody reads.
	many := []string{"🔒", "🧭", "🧪", "🚀", "📦", "🎨", "🐛", "📝", "⚡"}

	if got := usecase.CleanEmoji(many); len(got) != 5 {
		t.Errorf("kept %d, want 5: %v", len(got), got)
	}
}

func TestSkinTonesAndFlagsStayWhole(t *testing.T) {
	// A multi-rune emoji is one emoji. Splitting it renders as two squares.
	got := usecase.CleanEmoji([]string{"👍🏽", "🇮🇹"})
	if len(got) != 2 {
		t.Fatalf("got %d: %q", len(got), got)
	}
	if got[0] != "👍🏽" || got[1] != "🇮🇹" {
		t.Errorf("got %q, want them whole", got)
	}
}
