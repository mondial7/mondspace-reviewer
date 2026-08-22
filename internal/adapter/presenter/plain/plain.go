// Package plain is a line-oriented Presenter. It makes the app scriptable and
// end-to-end testable without a terminal.
package plain

import (
	"fmt"
	"io"
	"strings"

	"github.com/marcomondini/mondspace-reviewer/internal/domain"
)

type Presenter struct {
	w io.Writer
}

func New(w io.Writer) *Presenter {
	return &Presenter{w: w}
}

// Present renders a unit as fixed slots so the eye scans instead of reads.
func (p *Presenter) Present(u domain.Unit) error {
	_, err := fmt.Fprintf(p.w, "[%s] %s\nWHAT  %s\nWHY   %s\nFLAG  %s\n\n",
		u.ID,
		strings.Join(u.Files, ", "),
		u.Headline.Text,
		renderWhy(u.Headline),
		renderFlags(u.Flags),
	)
	return err
}

// renderFlags joins the flags with a middot, or an em dash when there are none.
func renderFlags(flags []domain.Flag) string {
	if len(flags) == 0 {
		return "—"
	}
	names := make([]string, len(flags))
	for i, f := range flags {
		names[i] = string(f)
	}
	return strings.Join(names, " · ")
}

// renderWhy keeps stated and inferred rationale visually distinct: a different
// label word and, for inferred with no text, an explicit placeholder.
func renderWhy(h domain.Headline) string {
	if h.WhySrc == domain.WhyStated {
		return "stated: " + h.Why
	}
	if h.Why == "" {
		return "inferred: (none stated)"
	}
	return "inferred: " + h.Why
}
