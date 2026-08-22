// Package plain is a line-oriented Presenter. It makes the app scriptable and
// end-to-end testable without a terminal.
package plain

import (
	"fmt"
	"io"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

type Presenter struct {
	w       io.Writer
	verbose bool
}

func New(w io.Writer) *Presenter {
	return &Presenter{w: w}
}

// Verbose makes Present also list each member event and the snapshot refs
// bracketing the unit.
func (p *Presenter) Verbose() *Presenter {
	p.verbose = true
	return p
}

// Present renders a unit as fixed slots so the eye scans instead of reads. In
// verbose mode it appends the events clustered into the unit.
func (p *Presenter) Present(u domain.Unit, events []domain.Event) error {
	if _, err := fmt.Fprintf(p.w, "[%s] %s\nWHAT  %s\nWHY   %s\nFLAG  %s\n",
		u.ID,
		strings.Join(u.Files, ", "),
		u.Headline.Text,
		renderWhy(u.Headline),
		renderFlags(u.Flags),
	); err != nil {
		return err
	}
	if p.verbose {
		if err := p.renderDetail(u, events); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(p.w)
	return err
}

// renderDetail lists the member events and the snapshot bracket.
func (p *Presenter) renderDetail(u domain.Unit, events []domain.Event) error {
	if ref := snapshotLine(u); ref != "" {
		if _, err := fmt.Fprintf(p.w, "SNAP  %s\n", ref); err != nil {
			return err
		}
	}
	for _, e := range events {
		if _, err := fmt.Fprintf(p.w, "  · %-9s %s\n", string(e.Kind), eventDetail(e)); err != nil {
			return err
		}
	}
	return nil
}

func eventDetail(e domain.Event) string {
	var b strings.Builder
	if len(e.Files) > 0 {
		b.WriteString(strings.Join(e.Files, ", "))
	} else if e.Tool != "" {
		b.WriteString("[" + e.Tool + "]")
	}
	if e.StatedIntent != "" {
		b.WriteString(` — "` + e.StatedIntent + `"`)
	}
	if e.Failed {
		b.WriteString(" [failed]")
	}
	return strings.TrimSpace(b.String())
}

func snapshotLine(u domain.Unit) string {
	if u.From.Commit == "" && u.To.Commit == "" {
		return ""
	}
	return short(u.From.Commit) + ".." + short(u.To.Commit)
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
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
