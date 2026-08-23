package usecase

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// FileHistories reconstructs, per unit, how its file was reached: every recorded
// touch, in the order it happened. Keyed by unit id.
//
// A unit built from a git baseline carries no event ids (ADR 0002 diffs the tree,
// it does not replay the log), so touches are matched by path instead. That also
// makes this work identically for live and retroactive review.
func FileHistories(events []domain.Event, units []domain.Unit) map[string]domain.FileHistory {
	histories := make(map[string]domain.FileHistory, len(units))

	for _, u := range units {
		var edits []domain.Edit
		for _, e := range events {
			if !touches(e, u.Files) {
				continue
			}
			edits = append(edits, domain.Edit{
				TS: e.TS, Tool: e.Tool, Intent: e.StatedIntent, Failed: e.Failed,
			})
		}
		sort.SliceStable(edits, func(i, j int) bool { return edits[i].TS.Before(edits[j].TS) })

		h := domain.FileHistory{Count: len(edits), Edits: edits}
		if len(edits) > 0 {
			h.First = edits[0].TS
			h.Last = edits[len(edits)-1].TS
		}
		histories[u.ID] = h
	}
	return histories
}

// touches reports whether an event changed one of these files. An event with no
// files changed no file — a prompt, or a shell command — and counting it would
// inflate every number on the page.
func touches(e domain.Event, files []string) bool {
	for _, ef := range e.Files {
		for _, uf := range files {
			if samePath(ef, uf) {
				return true
			}
		}
	}
	return false
}

// samePath compares paths recorded by different parties. Hooks report whatever
// path the agent used — often absolute, sometimes "./"-prefixed — while units are
// named relative to the repository root. Requiring an exact match would silently
// report "never edited" for every file.
func samePath(a, b string) bool {
	a = filepath.ToSlash(filepath.Clean(a))
	b = filepath.ToSlash(filepath.Clean(b))
	if a == b {
		return true
	}
	// One is a repo-relative tail of the other. The separator is required so
	// "auth/token.go" cannot match "other/auth/token.go" by accident... which it
	// legitimately would, so anchor on a full segment boundary only.
	return strings.HasSuffix(a, "/"+b) || strings.HasSuffix(b, "/"+a)
}
