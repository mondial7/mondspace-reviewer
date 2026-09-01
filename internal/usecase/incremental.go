package usecase

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// What a run of a model over a change knew, file by file, so the next run can
// tell how much of it is still true (ADR 0038).
//
// A whole-review fingerprint answers one question — has anything moved — and it
// is the right question for "is this card stale". It is the wrong one for "what
// do I have to do about it": the answer to that is almost always "two of these
// fourteen files", and re-reading the other twelve is a model call nobody
// needed and a dismissal quietly lost.

// FilePrints fingerprints what each file in a review currently says.
//
// Keyed by path rather than by unit id, because that is what a finding names
// and what a chapter is ultimately made of. A unit covering several files gives
// each of them the same print: they changed together and there is no finer
// answer to be had from a single diff.
func FilePrints(units []domain.Unit, diffs map[string]domain.Diff) map[string]string {
	out := make(map[string]string, len(units))
	for _, u := range units {
		sum := sha256.Sum256([]byte(diffs[u.ID].Text))
		print := hex.EncodeToString(sum[:12])
		for _, f := range u.Files {
			out[f] = print
		}
	}
	return out
}

// MovedFiles compares two sets of prints: which files read differently now
// (including ones that were not there before), and which have left the review
// altogether.
//
// The two are kept apart because they mean different things to a finding. A
// finding about a file that moved is out of date and can be derived again; a
// finding about a file that is gone is about nothing, and re-deriving it is not
// possible because there is nothing left to read.
func MovedFiles(before, now map[string]string) (moved, gone []string) {
	for path, print := range now {
		if was, known := before[path]; !known || was != print {
			moved = append(moved, path)
		}
	}
	for path := range before {
		if _, still := now[path]; !still {
			gone = append(gone, path)
		}
	}
	sort.Strings(moved)
	sort.Strings(gone)
	return moved, gone
}

// Touched is a set membership test over a list of paths, which is what every
// caller of MovedFiles actually wants.
func Touched(paths ...[]string) map[string]bool {
	out := map[string]bool{}
	for _, list := range paths {
		for _, p := range list {
			out[p] = true
		}
	}
	return out
}

// UnitsTouching is the units that hold at least one of these files, in the order
// the review lists them. This is how a set of moved paths becomes something a
// model can be asked about.
func UnitsTouching(units []domain.Unit, touched map[string]bool) []domain.Unit {
	var out []domain.Unit
	for _, u := range units {
		for _, f := range u.Files {
			if touched[f] {
				out = append(out, u)
				break
			}
		}
	}
	return out
}

// DiffsOf narrows a diff map to the given units, so a partial re-reading is
// handed exactly what it is being asked about and nothing else.
func DiffsOf(units []domain.Unit, diffs map[string]domain.Diff) map[string]domain.Diff {
	out := make(map[string]domain.Diff, len(units))
	for _, u := range units {
		if d, ok := diffs[u.ID]; ok {
			out[u.ID] = d
		}
	}
	return out
}
