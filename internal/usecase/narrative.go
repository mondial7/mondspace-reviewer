package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

// Narrator answers a prompt. Declared where it is consumed so the usecase layer
// depends on no adapter (ADR 0001); any Summarizer satisfies it.
type Narrator interface {
	Answer(ctx context.Context, question string, c domain.AskContext) (string, error)
}

// ask puts a question to the narrator, constraining the reply to schema when the
// narrator can enforce one. An endpoint that compiles the schema into a grammar
// returns valid JSON by construction; one that cannot returns prose, which the
// callers still parse defensively.
//
// The AskContext is empty on purpose: the prompt already carries everything, and
// the summarizer would otherwise append the whole session a second time.
func ask(ctx context.Context, n Narrator, question string, schema port.JSONSchema) (string, error) {
	if sa, ok := n.(port.SchemaAnswerer); ok {
		return sa.AnswerSchema(ctx, question, domain.AskContext{}, schema)
	}
	return n.Answer(ctx, question, domain.AskContext{})
}

// narrativeSchema describes the whole-session reply. Area names are an enum of
// the real areas, so a model that cannot name a fictional one cannot invent one —
// the check in reconcileChapters becomes a backstop rather than the only defence.
func narrativeSchema(groups []domain.Chapter) port.JSONSchema {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Title)
	}
	return port.JSONSchema{
		Name: "session_narrative",
		Schema: object(map[string]any{
			"title": map[string]any{"type": "string"},
			"intro": map[string]any{"type": "string"},
			"chapters": map[string]any{
				"type": "array",
				"items": object(map[string]any{
					"title": map[string]any{"type": "string"},
					"prose": map[string]any{"type": "string"},
					"groups": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string", "enum": names},
					},
				}, "title", "prose", "groups"),
			},
		}, "title", "intro", "chapters"),
	}
}

// chapterSchema describes a single narrated area.
func chapterSchema() port.JSONSchema {
	return port.JSONSchema{
		Name: "chapter",
		Schema: object(map[string]any{
			"title": map[string]any{"type": "string"},
			"prose": map[string]any{"type": "string"},
		}, "title", "prose"),
	}
}

// object builds a closed JSON Schema object. Strict structured output requires
// additionalProperties:false and every property listed as required.
func object(props map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

// GroupByPath clusters units by their top-level path segment. It is the
// deterministic grouping the story view falls back to: pure, instant, offline,
// and guaranteed to place every unit exactly once (ADR 0013).
func GroupByPath(units []domain.Unit) []domain.Chapter {
	order := []string{}
	byGroup := map[string][]string{}

	for _, u := range units {
		g := topLevel(u)
		if _, seen := byGroup[g]; !seen {
			order = append(order, g)
		}
		byGroup[g] = append(byGroup[g], u.ID)
	}

	chapters := make([]domain.Chapter, 0, len(order))
	for _, g := range order {
		ids := byGroup[g]
		chapters = append(chapters, domain.Chapter{
			Title:   g,
			Prose:   mechanicalProse(g, len(ids)),
			UnitIDs: ids,
		})
	}
	return chapters
}

// topLevel is the first path segment of a unit's first file, or "root" for a
// file that sits at the top of the repository.
func topLevel(u domain.Unit) string {
	if len(u.Files) == 0 {
		return "root"
	}
	f := filepath.ToSlash(u.Files[0])
	if i := strings.Index(f, "/"); i > 0 {
		return f[:i]
	}
	return "root"
}

func mechanicalProse(group string, n int) string {
	return fmt.Sprintf("%d %s changed under %s.", n, plural("file", n), group)
}

// Narrate asks a model to group the session into chapters and write the prose,
// then validates that output against the real units. Anything the model invents
// is dropped and anything it forgets is appended, so the story can never lose or
// fabricate a change.
//
// It always returns a usable narrative. The error is non-nil when it fell back to
// the deterministic grouping, and says why — a silent fallback is impossible to
// diagnose when a model is involved.
func Narrate(ctx context.Context, n Narrator, sess domain.Session, units []domain.Unit) (domain.Narrative, error) {
	return NarrateProgressively(ctx, n, sess, units, nil)
}

// NarrateProgressively is Narrate with a callback invoked each time a chapter is
// narrated, so a caller can publish the story as it is written rather than after
// the last chapter. onProgress may be nil.
func NarrateProgressively(ctx context.Context, n Narrator, sess domain.Session, units []domain.Unit, onProgress func(domain.Narrative)) (domain.Narrative, error) {
	fallback := domain.Narrative{
		SessionID: sess.ID,
		Title:     fallbackTitle(sess),
		Intro:     fallbackIntro(sess, units),
		Chapters:  GroupByPath(units),
		Source:    domain.NarrativeMechanical,
	}
	if n == nil || len(units) == 0 {
		return fallback, fmt.Errorf("no narrator or no units")
	}

	groups := fallback.Chapters
	progress := func(partial []domain.Chapter) {
		if onProgress != nil {
			onProgress(domain.Narrative{
				SessionID: sess.ID, Title: fallback.Title, Intro: fallback.Intro,
				Chapters: partial, Source: domain.NarrativeModel,
			})
		}
	}

	// One call for the whole session is tried once: it gives the best grouping
	// when the model has room for it. A small local context usually cannot manage
	// it — the reply comes back truncated or empty — so rather than retry the
	// same shape, fall through to narrating one area at a time.
	var parsed modelNarrative
	shown := boundedGroups(groups)
	reply, err := ask(ctx, n, narrativePrompt(sess, units, shown, promptExamplesPerGroup), narrativeSchema(shown))
	lastErr := err
	if err == nil {
		if m, ok := parseNarrative(reply); ok {
			parsed, lastErr = m, nil
		} else {
			lastErr = fmt.Errorf("model reply was not the requested JSON: %.160q", reply)
		}
	}

	if lastErr != nil {
		// The whole-session call did not fit. Narrate each area on its own
		// instead: a much smaller prompt and answer, which a small local context
		// can manage. The grouping stays deterministic; the model supplies the
		// words, and each chapter is published as it is written.
		if chapters, ok := narrateEachChapter(ctx, n, sess, units, groups, progress); ok {
			return domain.Narrative{
				SessionID: sess.ID,
				Title:     fallback.Title,
				Intro:     fallback.Intro,
				Chapters:  chapters,
				Source:    domain.NarrativeModel,
			}, nil
		}
		return fallback, explainNarrationFailure(lastErr)
	}

	resolved := make([]domain.Chapter, 0, len(parsed.Chapters))
	for _, c := range parsed.Chapters {
		resolved = append(resolved, domain.Chapter{
			Title: c.Title, Prose: c.Prose, UnitIDs: c.resolve(groups),
		})
	}
	chapters, kept := reconcileChapters(resolved, units)
	if kept == 0 {
		// Well-formed JSON that named nothing real. Everything would land in the
		// catch-all chapter, which tells the reviewer nothing — narrate per area
		// instead.
		if perChapter, ok := narrateEachChapter(ctx, n, sess, units, groups, progress); ok {
			return domain.Narrative{
				SessionID: sess.ID, Title: fallback.Title, Intro: fallback.Intro,
				Chapters: perChapter, Source: domain.NarrativeModel,
			}, nil
		}
		return fallback, fmt.Errorf("model chapters matched no real areas")
	}

	out := domain.Narrative{
		SessionID: sess.ID,
		Title:     Brief(firstNonEmpty(parsed.Title, fallback.Title), briefTitleChars),
		Intro:     Brief(firstNonEmpty(parsed.Intro, fallback.Intro), briefChars),
		Chapters:  chapters,
		Source:    domain.NarrativeModel,
	}
	return out, nil
}

// narrateEachChapter asks the model to title and narrate one area at a time. It
// reports success if any chapter came back narrated; areas the model could not
// describe keep their mechanical prose, so the story is never left with a hole.
func narrateEachChapter(ctx context.Context, n Narrator, sess domain.Session, units []domain.Unit, groups []domain.Chapter, onProgress func([]domain.Chapter)) ([]domain.Chapter, bool) {
	byID := map[string]domain.Unit{}
	for _, u := range units {
		byID[u.ID] = u
	}

	out := make([]domain.Chapter, 0, len(groups))
	narrated := 0
	for _, g := range groups {
		c := g
		if reply, err := ask(ctx, n, chapterPrompt(sess, g, byID), chapterSchema()); err == nil {
			if m, ok := parseChapter(reply); ok {
				c.Title = firstNonEmpty(m.Title, g.Title)
				c.Prose = firstNonEmpty(m.Prose, g.Prose)
				narrated++
			}
		}
		out = append(out, c)

		if narrated > 0 && onProgress != nil {
			// Publish what exists so far, with the areas not yet reached still
			// carrying their mechanical prose.
			partial := append(append([]domain.Chapter{}, out...), groups[len(out):]...)
			onProgress(partial)
		}
	}
	return out, narrated > 0
}

// chapterPrompt describes one area only, so prompt and answer both stay small.
func chapterPrompt(sess domain.Session, g domain.Chapter, byID map[string]domain.Unit) string {
	var b strings.Builder
	b.WriteString("Describe one part of a coding session for a reviewer.\n")
	if sess.Prompt != "" {
		b.WriteString("The task was: " + sess.Prompt + "\n")
	}
	b.WriteString(fmt.Sprintf("Area %q, %d %s changed:\n", g.Title, len(g.UnitIDs), plural("file", len(g.UnitIDs))))
	for i, id := range g.UnitIDs {
		if i == promptExamplesPerGroup {
			b.WriteString(fmt.Sprintf("- and %d more\n", len(g.UnitIDs)-i))
			break
		}
		u := byID[id]
		b.WriteString(fmt.Sprintf("- %s: %s\n", strings.Join(u.Files, ", "), u.Headline.Text))
	}
	b.WriteString(`
Give a short chapter title and 1-2 sentences of prose. JSON only:
{"title":"..","prose":".."}`)
	return b.String()
}

// parseChapter reads a single-chapter reply.
func parseChapter(reply string) (modelChapter, bool) {
	start := strings.Index(reply, "{")
	end := strings.LastIndex(reply, "}")
	if start < 0 || end <= start {
		return modelChapter{}, false
	}
	var c modelChapter
	if err := json.Unmarshal([]byte(reply[start:end+1]), &c); err != nil {
		return modelChapter{}, false
	}
	if strings.TrimSpace(c.Prose) == "" && strings.TrimSpace(c.Title) == "" {
		return modelChapter{}, false
	}
	return c, true
}

// isContextExceeded reports whether the model refused for want of context.
func isContextExceeded(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context size") || strings.Contains(msg, "context length")
}

// explainNarrationFailure turns an opaque model failure into something the
// operator can act on. A small local context is the common cause: reasoning
// models spend most of their budget thinking before they emit any output.
func explainNarrationFailure(err error) error {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "context size") || strings.Contains(msg, "context length") ||
		strings.Contains(msg, "too many tokens") {
		return fmt.Errorf("%w — the model's context window is too small to narrate this "+
			"session; raise it in your server (LM Studio: load the model with a larger "+
			"context length) or use a model with a bigger window", err)
	}
	return err
}

// reconcileChapters keeps only unit ids that really exist, drops emptied
// chapters, and appends whatever the model left out so nothing is lost.
func reconcileChapters(chapters []domain.Chapter, units []domain.Unit) (out []domain.Chapter, kept int) {
	known := map[string]bool{}
	for _, u := range units {
		known[u.ID] = true
	}

	placed := map[string]bool{}
	out = make([]domain.Chapter, 0, len(chapters))
	for _, c := range chapters {
		var ids []string
		for _, id := range c.UnitIDs {
			if known[id] && !placed[id] {
				placed[id] = true
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 || strings.TrimSpace(c.Title) == "" {
			continue
		}
		c.UnitIDs = ids
		out = append(out, c)
		kept++
	}

	var missed []string
	for _, u := range units {
		if !placed[u.ID] {
			missed = append(missed, u.ID)
		}
	}
	if len(missed) > 0 {
		out = append(out, domain.Chapter{
			Title:   "Also in this session",
			Prose:   fmt.Sprintf("%d further %s changed.", len(missed), plural("file", len(missed))),
			UnitIDs: missed,
		})
	}
	return out, kept
}

// modelNarrative is the JSON contract asked of the model. Chapters reference
// area names; unit ids are also accepted, since a model sometimes answers that
// way regardless of what was asked.
type modelNarrative struct {
	Title    string         `json:"title"`
	Intro    string         `json:"intro"`
	Chapters []modelChapter `json:"chapters"`
}

type modelChapter struct {
	Title   string   `json:"title"`
	Prose   string   `json:"prose"`
	Groups  []string `json:"groups"`
	UnitIDs []string `json:"unit_ids"`
}

// resolve turns a model chapter into unit ids: the union of the areas it names,
// plus any unit ids it happened to give directly.
func (c modelChapter) resolve(groups []domain.Chapter) []string {
	byName := map[string][]string{}
	for _, g := range groups {
		byName[strings.ToLower(g.Title)] = g.UnitIDs
	}
	var ids []string
	for _, name := range c.Groups {
		ids = append(ids, byName[strings.ToLower(strings.TrimSpace(name))]...)
	}
	return append(ids, c.UnitIDs...)
}

// parseNarrative extracts the JSON object from a reply that may be wrapped in
// prose or a code fence — a small model rarely answers with bare JSON.
func parseNarrative(reply string) (modelNarrative, bool) {
	start := strings.Index(reply, "{")
	end := strings.LastIndex(reply, "}")
	if start < 0 || end <= start {
		return modelNarrative{}, false
	}
	var m modelNarrative
	if err := json.Unmarshal([]byte(reply[start:end+1]), &m); err != nil {
		return modelNarrative{}, false
	}
	if len(m.Chapters) == 0 {
		return modelNarrative{}, false
	}
	return m, true
}

// narrativePrompt describes the session as a handful of path groups rather than
// every unit, so the prompt stays small whether the session touched five files or
// five hundred. The model merges and renames those groups into chapters.
func narrativePrompt(sess domain.Session, units []domain.Unit, groups []domain.Chapter, examples int) string {
	var b strings.Builder
	b.WriteString("You are writing a short, readable story of one coding session for a reviewer.\n")
	if sess.Prompt != "" {
		b.WriteString("The developer asked the agent to: " + sess.Prompt + "\n")
	}
	b.WriteString(fmt.Sprintf("\n%d files changed, in these areas:\n", len(units)))

	byID := map[string]domain.Unit{}
	for _, u := range units {
		byID[u.ID] = u
	}
	for _, g := range groups {
		b.WriteString(fmt.Sprintf("[%s] %d %s", g.Title, len(g.UnitIDs), plural("file", len(g.UnitIDs))))
		if examples > 0 {
			var names []string
			for i, id := range g.UnitIDs {
				if i == examples {
					break
				}
				names = append(names, strings.Join(byID[id].Files, ", "))
			}
			b.WriteString(": " + strings.Join(names, ", "))
		}
		b.WriteString("\n")
	}

	b.WriteString(`
Give the session a short title (under 70 characters) and a one-sentence
description of under 200 characters. Then group these areas into 2-5 chapters
and write 1-2 short sentences of prose for each. Use only the area names in
brackets; do not invent names; cover every area.
Answer with JSON only, no explanation:
{"title":"..","intro":"..","chapters":[{"title":"..","prose":"..","groups":["area"]}]}`)
	return b.String()
}

// boundedGroups caps how many areas a prompt and its schema describe, so both
// stay small whether the session touched five files or five hundred.
func boundedGroups(groups []domain.Chapter) []domain.Chapter {
	if len(groups) > promptMaxGroups {
		return groups[:promptMaxGroups]
	}
	return groups
}

// A local model spends most of its budget "thinking": in measurement, a small
// prompt produced ~1400 reasoning tokens before any output. On a 4k context that
// leaves little room, and an over-long prompt comes back empty. So the prompt is
// kept deliberately small, and shrinks further on a retry.
const (
	promptExamplesPerGroup = 2
	promptMaxGroups        = 6

	// briefChars is how long the session description may be. It has to read at a
	// glance in a narrow column, beside the numbers.
	briefChars      = 200
	briefTitleChars = 70
)

// Brief trims a model-written title or description to fit the panel it lives in,
// cutting at a word boundary so it reads as a sentence rather than a truncation.
func Brief(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= max {
		return text
	}
	cut := text[:max]
	if i := strings.LastIndex(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

func fallbackTitle(sess domain.Session) string {
	if sess.Prompt != "" {
		// The prompt stands in until a model reads the session. It is arbitrary
		// user text, so it is trimmed to fit the panel it will appear in.
		return Brief(sess.Prompt, briefTitleChars)
	}
	return "Session " + sess.ID
}

func fallbackIntro(sess domain.Session, units []domain.Unit) string {
	return fmt.Sprintf("%d %s changed in this session.", len(units), plural("file", len(units)))
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// Fingerprint identifies a review by what it contains: which files changed and
// what they changed to. Narration costs several model calls, so a stored story
// is reused while the fingerprint matches and re-narrated only when the review
// itself has moved on — opening the page again is not a reason to pay again.
//
// Order does not affect it: the same files reviewed in a different order are the
// same review.
func Fingerprint(units []domain.Unit) string {
	lines := make([]string, 0, len(units))
	for _, u := range units {
		files := append([]string(nil), u.Files...)
		sort.Strings(files)
		lines = append(lines, strings.Join([]string{
			u.ID, strings.Join(files, ","), u.From.Commit, u.To.Commit,
		}, "\x00"))
	}
	sort.Strings(lines)

	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:16])
}
