// Package plain is a line-oriented Presenter. It stays scriptable and end-to-end
// testable without a terminal: colour is applied only when the output is a TTY
// (via a lipgloss renderer bound to the writer), so piped or captured output is
// plain text.
package plain

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

type Presenter struct {
	w       io.Writer
	verbose bool
	base    string // relativise absolute file paths against this dir

	idStyle    lipgloss.Style
	timeStyle  lipgloss.Style
	labelStyle lipgloss.Style
	stated     lipgloss.Style
	inferred   lipgloss.Style
	flag       lipgloss.Style
	event      lipgloss.Style
}

func New(w io.Writer) *Presenter {
	r := lipgloss.NewRenderer(w)
	return &Presenter{
		w:          w,
		idStyle:    r.NewStyle().Bold(true).Foreground(lipgloss.Color("6")), // cyan
		timeStyle:  r.NewStyle().Faint(true),
		labelStyle: r.NewStyle().Faint(true),
		stated:     r.NewStyle().Foreground(lipgloss.Color("2")), // green
		inferred:   r.NewStyle().Foreground(lipgloss.Color("3")), // yellow
		flag:       r.NewStyle().Foreground(lipgloss.Color("1")), // red
		event:      r.NewStyle().Faint(true),
	}
}

// Verbose makes Present also list each member event and the snapshot refs.
func (p *Presenter) Verbose() *Presenter {
	p.verbose = true
	return p
}

// RelativeTo displays absolute file paths relative to base (e.g. the repo root).
func (p *Presenter) RelativeTo(base string) *Presenter {
	if abs, err := filepath.Abs(base); err == nil {
		p.base = abs
	} else {
		p.base = base
	}
	return p
}

// Present renders a unit as fixed slots so the eye scans instead of reads. In
// verbose mode it appends the events clustered into the unit.
func (p *Presenter) Present(u domain.Unit, events []domain.Event) error {
	header := p.idStyle.Render("[" + u.ID + "]")
	if t := unitTime(events); t != "" {
		header += "  " + p.timeStyle.Render(t)
	}
	if files := p.relFiles(u.Files); files != "" {
		header += "  " + files
	}

	if _, err := fmt.Fprintln(p.w, header); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(p.w, "%s  %s\n", p.labelStyle.Render("WHAT"), u.Headline.Text); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(p.w, "%s   %s\n", p.labelStyle.Render("WHY"), p.renderWhy(u.Headline)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(p.w, "%s  %s\n", p.labelStyle.Render("FLAG"), p.renderFlags(u.Flags)); err != nil {
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
		if _, err := fmt.Fprintf(p.w, "%s  %s\n", p.labelStyle.Render("SNAP"), ref); err != nil {
			return err
		}
	}
	for _, e := range events {
		line := fmt.Sprintf("  · %-9s %s", string(e.Kind), p.eventDetail(e))
		if _, err := fmt.Fprintln(p.w, p.event.Render(line)); err != nil {
			return err
		}
	}
	return nil
}

func (p *Presenter) eventDetail(e domain.Event) string {
	var b strings.Builder
	if len(e.Files) > 0 {
		b.WriteString(p.relFiles(e.Files))
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

// unitTime is the wall-clock (UTC) of the last event that sealed the unit.
func unitTime(events []domain.Event) string {
	for i := len(events) - 1; i >= 0; i-- {
		if !events[i].TS.IsZero() {
			return events[i].TS.UTC().Format("15:04:05")
		}
	}
	return ""
}

func (p *Presenter) relFiles(files []string) string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = p.rel(f)
	}
	return strings.Join(out, ", ")
}

func (p *Presenter) rel(f string) string {
	if p.base == "" || !filepath.IsAbs(f) {
		return f
	}
	r, err := filepath.Rel(p.base, f)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return f // outside the base; keep the absolute path
	}
	return r
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

// renderFlags joins the flags with a middot (in red on a TTY), or an em dash.
func (p *Presenter) renderFlags(flags []domain.Flag) string {
	if len(flags) == 0 {
		return "—"
	}
	names := make([]string, len(flags))
	for i, f := range flags {
		names[i] = string(f)
	}
	return p.flag.Render(strings.Join(names, " · "))
}

// renderWhy keeps stated and inferred rationale visually distinct: a different
// label word and a different colour on a TTY.
func (p *Presenter) renderWhy(h domain.Headline) string {
	switch {
	case h.WhySrc == domain.WhyStated:
		return p.stated.Render("stated: " + h.Why)
	case h.Why == "":
		return p.inferred.Render("inferred: (none stated)")
	default:
		return p.inferred.Render("inferred: " + h.Why)
	}
}
