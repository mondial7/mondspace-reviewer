// Package web serves the review as a localhost web application (ADR 0004).
//
// It renders server-side HTML with html/template — no build step, no client
// framework, and so far no JavaScript at all (expansion uses native <details>).
// The review semantics come entirely from the usecase layer; this package only
// presents them.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

//go:embed assets/*
var assets embed.FS

//go:embed templates/*.html
var templates embed.FS

// Session is everything the web app renders for one review.
type Session struct {
	ID     string
	Prompt string
	Repo   string
	Units  []domain.Unit
	Notes  []domain.Note
	Diffs  map[string]domain.Diff
}

// Annotator persists a reviewer's annotation. It is declared where it is
// consumed so this adapter depends on no other adapter.
type Annotator interface {
	AppendNote(domain.Note) error
}

// Server renders and serves one review session.
type Server struct {
	mux   *http.ServeMux
	tmpl  *template.Template
	sess  Session
	notes Annotator
}

// NewServer builds the HTTP handler for a session. notes may be nil, in which
// case annotations are not persisted.
func NewServer(sess Session, notes Annotator) *Server {
	s := &Server{
		mux:   http.NewServeMux(),
		tmpl:  template.Must(template.New("").Funcs(funcs()).ParseFS(templates, "templates/*.html")),
		sess:  sess,
		notes: notes,
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	static, err := fs.Sub(assets, "assets")
	if err != nil {
		panic("web: embedded assets missing: " + err.Error())
	}
	s.mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(static))))
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
}

// unitView is the presentation shape of a unit: everything the template needs,
// already computed, so templates stay logic-free.
type unitView struct {
	ID       string
	Handle   string
	File     string
	Headline string
	Why      string
	WhySrc   string
	Flags    []string
	Added    int
	Removed  int
	Diff     []diffLine
	Notes    []domain.Note
}

type diffLine struct {
	Kind string // add | del | hunk | ctx
	Text string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Session Session
		Units   []unitView
	}{Session: s.sess, Units: s.views()}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) views() []unitView {
	views := make([]unitView, 0, len(s.sess.Units))
	for _, u := range s.sess.Units {
		views = append(views, s.view(u))
	}
	return views
}

func (s *Server) view(u domain.Unit) unitView {
	d := s.sess.Diffs[u.ID]
	added, removed := usecase.DiffStats(d)

	flags := make([]string, len(u.Flags))
	for i, f := range u.Flags {
		flags[i] = string(f)
	}

	var notes []domain.Note
	for _, n := range s.sess.Notes {
		if n.UnitID == u.ID {
			notes = append(notes, n)
		}
	}

	why := u.Headline.Why
	if why == "" && u.Headline.WhySrc != domain.WhyStated {
		why = "(none stated)"
	}

	return unitView{
		ID:       u.ID,
		Handle:   shortHandle(u.ID),
		File:     strings.Join(u.Files, ", "),
		Headline: u.Headline.Text,
		Why:      why,
		WhySrc:   u.Headline.WhySrc,
		Flags:    flags,
		Added:    added,
		Removed:  removed,
		Diff:     splitDiff(d.Text),
		Notes:    notes,
	}
}

// splitDiff classifies each diff line so the template can style it without logic.
func splitDiff(text string) []diffLine {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var lines []diffLine
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		kind := "ctx"
		switch {
		case strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---"):
			kind = "meta"
		case strings.HasPrefix(l, "@@"):
			kind = "hunk"
		case strings.HasPrefix(l, "+"):
			kind = "add"
		case strings.HasPrefix(l, "-"):
			kind = "del"
		}
		lines = append(lines, diffLine{Kind: kind, Text: l})
	}
	return lines
}

func shortHandle(id string) string {
	if i := strings.LastIndex(id, "-"); i >= 0 && i+1 < len(id) {
		return id[i+1:]
	}
	return id
}

func funcs() template.FuncMap {
	return template.FuncMap{
		"base": filepath.Base,
	}
}
