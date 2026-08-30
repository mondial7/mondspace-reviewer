package usecase

import (
	"strings"
	"unicode"
)

// maxEmoji is how many a review gets. Three to five is a glance; nine is a
// sentence in a language nobody reads.
const maxEmoji = 5

// CleanEmoji keeps the entries that are actually emoji, in order, without
// repeats.
//
// A schema can ask for emoji and cannot insist. Asked for five, a small local
// model answers with a mix of real ones, the word "security", ":sparkles:", an
// arrow, and occasionally a letter — and every one of those on a card meant to
// be read at a glance is worse than the card having none.
//
// So the model proposes and this decides. What it cannot recognise it drops
// rather than passes through, and a review with nothing recognisable simply
// shows nothing: an inferred flourish is the last thing that should be filled
// in with a guess (ADR 0003).
func CleanEmoji(in []string) []string {
	var out []string
	seen := map[string]bool{}

	for _, candidate := range in {
		e := strings.TrimSpace(candidate)
		if e == "" || seen[e] || !isEmoji(e) {
			continue
		}
		seen[e] = true
		out = append(out, e)
		if len(out) == maxEmoji {
			break
		}
	}
	return out
}

// isEmoji reports whether a string is one pictograph — possibly several runes,
// because a skin tone, a flag and a family are each one emoji wearing several.
//
// The test is that it *starts* with a pictograph and carries nothing that would
// print as a letter, digit or punctuation. That admits sequences this list does
// not enumerate and rejects "security", ":sparkles:" and "→", which is the
// distinction that matters here.
func isEmoji(s string) bool {
	runes := []rune(s)
	if len(runes) == 0 || len(runes) > 8 || !pictograph(runes[0]) {
		return false
	}
	for _, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsPunct(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// pictograph reports whether a rune is in one of the blocks emoji come from.
// Regional indicators are included so a flag counts; the arrows and dingbats
// that a model reaches for when it runs out of ideas are not.
func pictograph(r rune) bool {
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF: // symbols, pictographs, extended
		return true
	case r >= 0x1F000 && r <= 0x1F0FF: // tiles and cards
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF: // regional indicators, for flags
		return true
	case r >= 0x2600 && r <= 0x27BF: // miscellaneous symbols and dingbats
		return true
	case r >= 0x2B00 && r <= 0x2BFF: // arrows and stars that are drawn, not typed
		return true
	}
	return false
}
