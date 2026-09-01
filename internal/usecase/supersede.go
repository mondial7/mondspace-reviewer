package usecase

import "github.com/mondial7/mondspace-reviewer/internal/domain"

// PlaceNotes puts a set of notes back where they belong against a freshly built
// unit list, then marks the ones a later change has overtaken.
//
// The re-anchoring covers two things. Unit ids used to be positional, so every
// note written before ids were derived from the path is pointing at whatever
// happened to be in that slot. And a review rebuilt from a different baseline —
// a different target over the same files — numbers its units differently again.
// A note carries the file it was about (ADR 0030), which is enough to find it a
// home in either case.
//
// It never guesses: a note whose file matches nothing, or matches more than one
// unit, is left exactly where it was rather than moved somewhere plausible.
func PlaceNotes(units []domain.Unit, notes []domain.Note) []domain.Note {
	known := map[string]bool{}
	byFile := map[string][]string{}
	for _, u := range units {
		known[u.ID] = true
		for _, f := range u.Files {
			byFile[f] = append(byFile[f], u.ID)
		}
	}

	out := make([]domain.Note, len(notes))
	copy(out, notes)
	for i := range out {
		if known[out[i].UnitID] || out[i].File == "" {
			continue
		}
		if ids := byFile[out[i].File]; len(ids) == 1 {
			out[i].UnitID = ids[0]
		}
	}
	return MarkSuperseded(units, out)
}

// MarkSuperseded flags a note as superseded when a later unit touches the same
// file as the annotated unit. It is a pure function: it never deletes a note and
// never auto-resolves it — supersession is surfaced, not silently applied.
func MarkSuperseded(units []domain.Unit, notes []domain.Note) []domain.Note {
	index := map[string]int{}
	for i, u := range units {
		index[u.ID] = i
	}

	out := make([]domain.Note, len(notes))
	copy(out, notes)
	for i := range out {
		pos, ok := index[out[i].UnitID]
		if !ok {
			continue
		}
		if later := firstLaterOverlap(units, pos); later != "" {
			out[i].SupersededBy = later
		}
	}
	return out
}

// firstLaterOverlap returns the ID of the earliest unit after pos that shares a
// file with the unit at pos, or "" if none does.
func firstLaterOverlap(units []domain.Unit, pos int) string {
	touched := map[string]bool{}
	for _, f := range units[pos].Files {
		touched[f] = true
	}
	for j := pos + 1; j < len(units); j++ {
		for _, f := range units[j].Files {
			if touched[f] {
				return units[j].ID
			}
		}
	}
	return ""
}
