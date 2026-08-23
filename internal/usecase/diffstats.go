package usecase

import (
	"path/filepath"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// DiffHeadline is a mechanical, file-aware summary of a change, used until the
// model provides a richer storyline. The rationale is always inferred.
func DiffHeadline(file string, d domain.Diff) domain.Headline {
	verb := "edited"
	switch {
	case strings.Contains(d.Text, "new file mode"):
		verb = "added"
	case strings.Contains(d.Text, "deleted file mode"):
		verb = "removed"
	}
	return domain.Headline{Text: verb + " " + filepath.Base(file), WhySrc: domain.WhyInferred}
}

// DiffStats counts added and removed content lines in a unified diff, ignoring
// the +++/--- file headers.
func DiffStats(d domain.Diff) (added, removed int) {
	for _, line := range strings.Split(d.Text, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}
