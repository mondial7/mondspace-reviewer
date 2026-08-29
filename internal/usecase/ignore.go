package usecase

import (
	"sort"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// Hidden is one file kept out of a review, and the rule that kept it out.
//
// The reason travels with it because a review tool that hides files has to be
// able to say which and why. Silently dropping them is the failure this feature
// must not become (ADR 0027).
type Hidden struct {
	Path    string
	Pattern string
}

// SplitIgnored separates the units a reviewer asked not to see from the rest.
//
// A unit is only set aside when *every* file in it matched. Units can cover
// more than one file, and hiding one because a single generated file was in it
// would take the reviewer's own work with it.
func SplitIgnored(units []domain.Unit, rules map[string]string) ([]domain.Unit, []Hidden) {
	if len(rules) == 0 {
		return units, nil
	}

	shown := make([]domain.Unit, 0, len(units))
	var hidden []Hidden

	for _, u := range units {
		all := len(u.Files) > 0
		for _, f := range u.Files {
			if _, matched := rules[f]; !matched {
				all = false
				break
			}
		}
		if !all {
			shown = append(shown, u)
			continue
		}
		for _, f := range u.Files {
			hidden = append(hidden, Hidden{Path: f, Pattern: rules[f]})
		}
	}

	// Path order, so the list reads like a directory rather than like the order
	// git happened to produce.
	sort.Slice(hidden, func(i, j int) bool { return hidden[i].Path < hidden[j].Path })
	return shown, hidden
}
