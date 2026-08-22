package usecase

import "github.com/marcomondini/mondspace-reviewer/internal/domain"

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
