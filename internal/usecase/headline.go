package usecase

import (
	"fmt"
	"strings"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

// kindOrder fixes the order kinds appear in a mechanical headline so output is
// deterministic.
var kindOrder = []domain.Kind{domain.KindEdit, domain.KindWrite, domain.KindBash}

// MechanicalHeadline summarises a unit's member events by kind count and file
// count, with no model. It is the offline fallback the queue never waits on.
func MechanicalHeadline(events []domain.Event) domain.Headline {
	counts := map[domain.Kind]int{}
	seen := map[string]bool{}
	files := 0
	for _, e := range events {
		counts[e.Kind]++
		for _, f := range e.Files {
			if !seen[f] {
				seen[f] = true
				files++
			}
		}
	}

	var parts []string
	for _, k := range kindOrder {
		if n := counts[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, plural(string(k), n)))
		}
	}

	text := fmt.Sprintf("%s across %d %s", strings.Join(parts, ", "), files, plural("file", files))

	why, whySrc := "", domain.WhyInferred
	for _, e := range events {
		if e.StatedIntent != "" {
			why, whySrc = e.StatedIntent, domain.WhyStated
			break
		}
	}

	return domain.Headline{Text: text, Why: why, WhySrc: whySrc}
}

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
