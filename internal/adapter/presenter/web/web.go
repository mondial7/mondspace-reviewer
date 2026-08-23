// Package web serves the review as a localhost web application (ADR 0004).
//
// It renders server-side HTML with html/template — no build step, no client
// framework, and so far no JavaScript at all (expansion uses native <details>).
// The review semantics come entirely from the usecase layer; this package only
// presents them.
package web

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

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

// SessionSummary is one row of the workspace: a review that exists, wherever it
// came from. Sessions span repos and agents (issue #8).
type SessionSummary struct {
	ID      string
	Repo    string
	Agent   string
	Prompt  string
	Files   int
	Flags   int
	Open    int // unresolved questions and objections
	Started time.Time
}

// Exchange is one question and its answer. Review is iterative dialogue, so the
// thread is kept and handed back to the assistant as context (issue #12).
type Exchange struct {
	Question string
	Answer   string
	TS       time.Time
}

// AskFunc answers a question given everything already discussed.
type AskFunc func(ctx context.Context, question string, history []Exchange) (string, error)

// ReanalyseFunc re-summarises one unit, returning the headline and the model
// that produced it. Re-running with a better model is cheap because the diff is
// stable (issue #10).
type ReanalyseFunc func(ctx context.Context, u domain.Unit) (domain.Headline, string, error)

// AuditEntry records one reviewer interaction. Once reviews are shared, these
// are records, not a cache: who did what, when (issue #11).
type AuditEntry struct {
	TS        time.Time
	SessionID string
	UnitID    string
	Action    string // annotate | ask | reanalyse
	Detail    string
}

// AuditLog persists interactions. Declared where consumed, so this adapter
// depends on no other adapter.
type AuditLog interface {
	Append(AuditEntry) error
}

// Annotator persists a reviewer's annotation. It is declared where it is
// consumed so this adapter depends on no other adapter.
type Annotator interface {
	AppendNote(domain.Note) error
}

// Server renders and serves one review session. Handlers run concurrently, so
// session state is guarded.
type Server struct {
	mux   *http.ServeMux
	tmpl  *template.Template
	notes Annotator

	mu        sync.RWMutex
	sess      Session
	workspace []SessionSummary
	thread    []Exchange
	narrative domain.Narrative
	models    map[string]string // unit ID -> model that produced its headline
	ask       AskFunc
	reanalyse ReanalyseFunc
	audit     AuditLog

	newID func() string
	now   func() time.Time
}

// NewServer builds the HTTP handler for a session. notes may be nil, in which
// case annotations are held in memory only.
func NewServer(sess Session, notes Annotator) *Server {
	s := &Server{
		mux:    http.NewServeMux(),
		tmpl:   template.Must(template.New("").Funcs(funcs()).ParseFS(templates, "templates/*.html")),
		sess:   sess,
		notes:  notes,
		models: map[string]string{},
		newID:  func() string { return ulid.Make().String() },
		now:    func() time.Time { return time.Now().UTC() },
	}
	s.routes()
	return s
}

// WithClock overrides id and time generation for deterministic tests.
func (s *Server) WithClock(newID func() string, now func() time.Time) *Server {
	s.newID, s.now = newID, now
	return s
}

// WithAsk wires the reviewer-assistant. Without it, the ask panel is not shown.
func (s *Server) WithAsk(fn AskFunc) *Server {
	s.ask = fn
	return s
}

// WithReanalyse wires per-unit re-analysis; without it, the button is not shown.
func (s *Server) WithReanalyse(fn ReanalyseFunc) *Server {
	s.reanalyse = fn
	return s
}

// WithAudit records every reviewer interaction.
func (s *Server) WithAudit(a AuditLog) *Server {
	s.audit = a
	return s
}

// WithNarrative supplies the session's story, shown at /story.
func (s *Server) WithNarrative(n domain.Narrative) *Server {
	s.narrative = n
	return s
}

// SetNarrative replaces the story while the server is running, so a slow model
// can upgrade a mechanical narrative without the page ever waiting on it.
func (s *Server) SetNarrative(n domain.Narrative) {
	s.mu.Lock()
	s.narrative = n
	s.mu.Unlock()
}

// WithWorkspace supplies the sessions listed at /sessions.
func (s *Server) WithWorkspace(sessions []SessionSummary) *Server {
	s.workspace = sessions
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
	s.mux.HandleFunc("POST /units/{id}/notes", s.handleAnnotate)
	s.mux.HandleFunc("GET /sessions", s.handleWorkspace)
	s.mux.HandleFunc("POST /ask", s.handleAsk)
	s.mux.HandleFunc("POST /units/{id}/reanalyse", s.handleReanalyse)
	s.mux.HandleFunc("GET /story", s.handleStory)
}

// noteKinds are the annotations a reviewer may attach (SPEC §11).
var noteKinds = map[string]domain.NoteKind{
	"ok":        domain.NoteOK,
	"question":  domain.NoteQuestion,
	"objection": domain.NoteObjection,
	"debt":      domain.NoteDebt,
	"note":      domain.NoteNote,
}

// handleAnnotate attaches a note to a unit and persists it. Annotations anchor
// to unit ids, never file/line — the working tree moves, unit ids do not.
func (s *Server) handleAnnotate(w http.ResponseWriter, r *http.Request) {
	unit, ok := s.unit(r.PathValue("id"))
	if !ok {
		http.Error(w, "unknown unit", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	kind, ok := noteKinds[r.FormValue("kind")]
	if !ok {
		http.Error(w, "unknown note kind", http.StatusBadRequest)
		return
	}

	note := domain.Note{
		ID:        s.newID(),
		SessionID: unit.SessionID,
		UnitID:    unit.ID,
		Kind:      kind,
		Text:      strings.TrimSpace(r.FormValue("text")),
		TS:        s.now(),
	}
	if s.notes != nil {
		if err := s.notes.AppendNote(note); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.mu.Lock()
	s.sess.Notes = append(s.sess.Notes, note)
	s.mu.Unlock()
	s.record(AuditEntry{SessionID: unit.SessionID, UnitID: unit.ID,
		Action: "annotate", Detail: string(note.Kind) + ": " + note.Text})

	http.Redirect(w, r, "/#unit-"+unit.ID, http.StatusSeeOther)
}

func (s *Server) unit(id string) (domain.Unit, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.sess.Units {
		if u.ID == id {
			return u, true
		}
	}
	return domain.Unit{}, false
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
	Model    string
}

type diffLine struct {
	Kind string // add | del | hunk | ctx
	Text string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	data := struct {
		Session   Session
		Units     []unitView
		Thread    []Exchange
		HasAsk    bool
		HasReanal bool
	}{Session: s.sess, Units: s.views(), Thread: s.thread,
		HasAsk: s.ask != nil, HasReanal: s.reanalyse != nil}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleAsk answers a question in the running conversation. An assistant that
// is offline or failing becomes a visible notice, never a 500.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	question := strings.TrimSpace(r.FormValue("question"))
	if question == "" {
		http.Redirect(w, r, "/#ask", http.StatusSeeOther)
		return
	}

	answer := "(no assistant configured)"
	if s.ask != nil {
		s.mu.RLock()
		history := append([]Exchange(nil), s.thread...)
		s.mu.RUnlock()

		got, err := s.ask(r.Context(), question, history)
		if err != nil {
			answer = "(" + err.Error() + ")"
		} else {
			answer = got
		}
	}

	s.mu.Lock()
	s.thread = append(s.thread, Exchange{Question: question, Answer: answer, TS: s.now()})
	sessionID := s.sess.ID
	s.mu.Unlock()
	s.record(AuditEntry{SessionID: sessionID, Action: "ask", Detail: question})

	http.Redirect(w, r, "/#ask", http.StatusSeeOther)
}

// handleReanalyse re-summarises one unit with the configured model, replacing
// its headline and recording which model produced it.
func (s *Server) handleReanalyse(w http.ResponseWriter, r *http.Request) {
	unit, ok := s.unit(r.PathValue("id"))
	if !ok {
		http.Error(w, "unknown unit", http.StatusNotFound)
		return
	}
	if s.reanalyse == nil {
		http.Error(w, "no analyser configured", http.StatusBadRequest)
		return
	}

	headline, model, err := s.reanalyse(r.Context(), unit)
	if err != nil {
		// A failing model must not lose the existing headline.
		s.record(AuditEntry{SessionID: unit.SessionID, UnitID: unit.ID,
			Action: "reanalyse", Detail: "failed: " + err.Error()})
		http.Redirect(w, r, "/#unit-"+unit.ID, http.StatusSeeOther)
		return
	}

	s.mu.Lock()
	for i := range s.sess.Units {
		if s.sess.Units[i].ID == unit.ID {
			s.sess.Units[i].Headline = headline
			break
		}
	}
	s.models[unit.ID] = model
	s.mu.Unlock()
	s.record(AuditEntry{SessionID: unit.SessionID, UnitID: unit.ID,
		Action: "reanalyse", Detail: model})

	http.Redirect(w, r, "/#unit-"+unit.ID, http.StatusSeeOther)
}

// record appends to the audit log. A log that cannot be written must not break
// the review, so the error is swallowed after a best-effort attempt.
func (s *Server) record(e AuditEntry) {
	if s.audit == nil {
		return
	}
	e.TS = s.now()
	_ = s.audit.Append(e)
}

// chapterView pairs a chapter's prose with the real units it covers, so the
// page can show model narration beside facts that come from git.
type chapterView struct {
	Title string
	Prose string
	Units []unitView
}

// handleStory renders the session as a long-form, chaptered read (ADR 0013).
func (s *Server) handleStory(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	byID := map[string]unitView{}
	for _, v := range s.views() {
		byID[v.ID] = v
	}
	chapters := make([]chapterView, 0, len(s.narrative.Chapters))
	for _, c := range s.narrative.Chapters {
		cv := chapterView{Title: c.Title, Prose: c.Prose}
		for _, id := range c.UnitIDs {
			if v, ok := byID[id]; ok {
				cv.Units = append(cv.Units, v)
			}
		}
		chapters = append(chapters, cv)
	}
	data := struct {
		Session   Session
		Narrative domain.Narrative
		Chapters  []chapterView
	}{Session: s.sess, Narrative: s.narrative, Chapters: chapters}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "story.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleWorkspace lists every known review, across repos and agents.
func (s *Server) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	data := struct{ Sessions []SessionSummary }{Sessions: s.workspace}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "sessions.html", data); err != nil {
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
		Model:    s.models[u.ID],
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
		"add":  func(a, b int) int { return a + b },
	}
}
