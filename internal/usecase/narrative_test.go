package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func narrativeUnits() []domain.Unit {
	return []domain.Unit{
		{ID: "s-f001", Files: []string{"auth/token.go"}, Headline: domain.Headline{Text: "edited token.go"}},
		{ID: "s-f002", Files: []string{"auth/token_test.go"}, Headline: domain.Headline{Text: "added token_test.go"}},
		{ID: "s-f003", Files: []string{"http/middleware.go"}, Headline: domain.Headline{Text: "edited middleware.go"}},
		{ID: "s-f004", Files: []string{"README.md"}, Headline: domain.Headline{Text: "edited README.md"}},
	}
}

// narrator returns a canned model reply, or an error.
type narrator struct {
	reply string
	err   error
	asked string
}

func (n *narrator) Answer(_ context.Context, question string, _ domain.AskContext) (string, error) {
	n.asked = question
	return n.reply, n.err
}

func TestGroupByPathClustersRelatedFiles(t *testing.T) {
	chapters := usecase.GroupByPath(narrativeUnits())

	// auth/* forms one chapter, http/* another, and the root file its own.
	titles := map[string][]string{}
	for _, c := range chapters {
		titles[c.Title] = c.UnitIDs
	}
	if got := titles["auth"]; len(got) != 2 {
		t.Errorf("auth chapter = %v, want both auth files", got)
	}
	if got := titles["http"]; len(got) != 1 {
		t.Errorf("http chapter = %v, want one file", got)
	}
	if len(chapters) != 3 {
		t.Fatalf("got %d chapters, want 3", len(chapters))
	}
	// Every unit is placed exactly once — a story may not silently lose changes.
	seen := map[string]int{}
	for _, c := range chapters {
		for _, id := range c.UnitIDs {
			seen[id]++
		}
	}
	for _, u := range narrativeUnits() {
		if seen[u.ID] != 1 {
			t.Errorf("unit %s placed %d times, want exactly 1", u.ID, seen[u.ID])
		}
	}
}

func TestNarrateFallsBackWhenModelUnavailable(t *testing.T) {
	n := &narrator{err: errors.New("summarizer offline")}

	got, _ := usecase.Narrate(context.Background(), n, domain.Session{ID: "s", Prompt: "add auth"}, narrativeUnits())

	if got.Source != domain.NarrativeMechanical {
		t.Errorf("Source = %q, want mechanical when the model is unavailable", got.Source)
	}
	if len(got.Chapters) != 3 {
		t.Errorf("got %d chapters, want the deterministic grouping", len(got.Chapters))
	}
	if got.Title == "" {
		t.Error("a fallback narrative still needs a title")
	}
}

func TestNarrateUsesModelChapters(t *testing.T) {
	n := &narrator{reply: `Sure! Here is the story:
	{"title":"Locking down authentication",
	 "intro":"The session added token validation and wired it in.",
	 "chapters":[
	   {"title":"Token validation","prose":"A TokenValidator interface was extracted and tested.","unit_ids":["s-f001","s-f002"]},
	   {"title":"Request path","prose":"Middleware now guards every route.","unit_ids":["s-f003"]}
	 ]}`}

	got, _ := usecase.Narrate(context.Background(), n, domain.Session{ID: "s", Prompt: "add auth"}, narrativeUnits())

	if got.Source != domain.NarrativeModel {
		t.Fatalf("Source = %q, want model", got.Source)
	}
	if got.Title != "Locking down authentication" {
		t.Errorf("Title = %q", got.Title)
	}
	if len(got.Chapters) < 2 || got.Chapters[0].Prose == "" {
		t.Fatalf("chapters = %+v, want the model's prose", got.Chapters)
	}
	// The model forgot s-f004; it must still appear, not vanish from the story.
	var all []string
	for _, c := range got.Chapters {
		all = append(all, c.UnitIDs...)
	}
	if !contains(all, "s-f004") {
		t.Errorf("a unit the model omitted was lost: %v", all)
	}
	// The prompt describes the session by area, so it stays small however many
	// files changed; it must not enumerate every unit.
	if !strings.Contains(n.asked, "[auth]") || !strings.Contains(n.asked, "auth/token.go") {
		t.Errorf("prompt should describe the real areas and example files:\n%s", n.asked)
	}
}

func TestNarrativePromptStaysBoundedForHugeSessions(t *testing.T) {
	var units []domain.Unit
	for i := 0; i < 400; i++ {
		units = append(units, domain.Unit{
			ID:       fmt.Sprintf("s-f%03d", i),
			Files:    []string{fmt.Sprintf("internal/pkg%d/file.go", i%3)},
			Headline: domain.Headline{Text: "edited file.go"},
		})
	}
	n := &narrator{err: errors.New("offline")}

	_, _ = usecase.Narrate(context.Background(), n, domain.Session{ID: "s"}, units)

	// A 400-file session must not produce a 400-line prompt.
	if lines := strings.Count(n.asked, "\n"); lines > 60 {
		t.Errorf("prompt has %d lines; it must summarise by area, not enumerate units", lines)
	}
	if strings.Contains(n.asked, "s-f399") {
		t.Errorf("prompt should not enumerate every unit id")
	}
}

func TestModelChaptersResolveFromAreaNames(t *testing.T) {
	// The model answers with area names rather than unit ids.
	n := &narrator{reply: `{"title":"T","intro":"I","chapters":[
	   {"title":"Auth work","prose":"p","groups":["auth"]},
	   {"title":"The rest","prose":"q","groups":["http","root"]}]}`}

	got, _ := usecase.Narrate(context.Background(), n, domain.Session{ID: "s"}, narrativeUnits())

	if got.Source != domain.NarrativeModel {
		t.Fatalf("Source = %q, want model", got.Source)
	}
	if len(got.Chapters) != 2 {
		t.Fatalf("got %d chapters, want 2", len(got.Chapters))
	}
	if len(got.Chapters[0].UnitIDs) != 2 {
		t.Errorf("the auth area should resolve to both auth units, got %v", got.Chapters[0].UnitIDs)
	}
	if len(got.Chapters[1].UnitIDs) != 2 {
		t.Errorf("http+root should resolve to two units, got %v", got.Chapters[1].UnitIDs)
	}
}

func TestNarrateDropsHallucinatedUnitIDs(t *testing.T) {
	n := &narrator{reply: `{"title":"T","intro":"I","chapters":[
	   {"title":"Real and invented","prose":"p","unit_ids":["s-f001","s-f999","nonsense"]}]}`}

	got, _ := usecase.Narrate(context.Background(), n, domain.Session{ID: "s"}, narrativeUnits())

	for _, c := range got.Chapters {
		for _, id := range c.UnitIDs {
			if id == "s-f999" || id == "nonsense" {
				t.Errorf("invented unit id %q was kept", id)
			}
		}
	}
}

// perChapterNarrator refuses the whole-session prompt (as a small-context model
// does) but answers a single-chapter prompt.
type perChapterNarrator struct {
	calls int
}

func (n *perChapterNarrator) Answer(_ context.Context, question string, _ domain.AskContext) (string, error) {
	n.calls++
	if strings.Contains(question, "chapters") {
		return "", errors.New("Context size has been exceeded")
	}
	return `{"title":"Auth hardening","prose":"Validation moved behind an interface."}`, nil
}

func TestNarrateFallsBackToPerChapterNarration(t *testing.T) {
	n := &perChapterNarrator{}

	got, err := usecase.Narrate(context.Background(), n, domain.Session{ID: "s"}, narrativeUnits())
	if err != nil {
		t.Fatalf("per-chapter narration should succeed: %v", err)
	}

	if got.Source != domain.NarrativeModel {
		t.Errorf("Source = %q, want model — the prose was model-written", got.Source)
	}
	// The deterministic grouping is kept, but each chapter is narrated.
	if len(got.Chapters) != 3 {
		t.Fatalf("got %d chapters, want the 3 deterministic areas", len(got.Chapters))
	}
	for _, c := range got.Chapters {
		if c.Prose != "Validation moved behind an interface." {
			t.Errorf("chapter %q prose = %q, want the model's prose", c.Title, c.Prose)
		}
		if c.Title != "Auth hardening" {
			t.Errorf("chapter title = %q, want the model's title", c.Title)
		}
	}
	// Every unit is still placed exactly once.
	seen := map[string]bool{}
	for _, c := range got.Chapters {
		for _, id := range c.UnitIDs {
			if seen[id] {
				t.Errorf("unit %s placed twice", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 4 {
		t.Errorf("placed %d units, want all 4", len(seen))
	}
}

func TestPerChapterNarrationKeepsAreaOnPartialFailure(t *testing.T) {
	// A narrator that fails everything: the story still stands, mechanically.
	n := &narrator{err: errors.New("Context size has been exceeded")}

	got, err := usecase.Narrate(context.Background(), n, domain.Session{ID: "s"}, narrativeUnits())

	if err == nil {
		t.Error("a total failure should be reported, not hidden")
	}
	if got.Source != domain.NarrativeMechanical || len(got.Chapters) != 3 {
		t.Errorf("want the mechanical grouping, got %q with %d chapters", got.Source, len(got.Chapters))
	}
	// The reason must be actionable, naming the context window.
	if !strings.Contains(err.Error(), "context window") {
		t.Errorf("error should explain the small context window: %v", err)
	}
}

// unmatchedThenPerChapter answers the whole-session prompt with well-formed JSON
// that names nothing real, then answers per-chapter prompts properly.
type unmatchedThenPerChapter struct{}

func (unmatchedThenPerChapter) Answer(_ context.Context, question string, _ domain.AskContext) (string, error) {
	if strings.Contains(question, "chapters") {
		return `{"title":"T","intro":"I","chapters":[{"title":"Ghost","prose":"p","groups":["nowhere"]}]}`, nil
	}
	return `{"title":"Real chapter","prose":"Real prose."}`, nil
}

func TestNarrateRejectsAStoryThatMatchedNothing(t *testing.T) {
	got, err := usecase.Narrate(context.Background(), unmatchedThenPerChapter{}, domain.Session{ID: "s"}, narrativeUnits())
	if err != nil {
		t.Fatalf("should have recovered per-chapter: %v", err)
	}

	// A model story whose chapters match no real unit is worthless: it must not
	// be shown as a one-chapter catch-all.
	if len(got.Chapters) == 1 && strings.HasPrefix(got.Chapters[0].Title, "Also in this session") {
		t.Fatalf("a story that matched nothing was accepted: %+v", got.Chapters)
	}
	for _, c := range got.Chapters {
		if c.Title == "Ghost" {
			t.Errorf("a chapter naming no real area was kept")
		}
	}
	if got.Chapters[0].Prose != "Real prose." {
		t.Errorf("expected the per-chapter narration to take over, got %+v", got.Chapters[0])
	}
}

// schemaNarrator implements the optional port.SchemaAnswerer capability and
// records the schema it was handed.
type schemaNarrator struct {
	narrator
	schemas []port.JSONSchema
}

func (n *schemaNarrator) AnswerSchema(ctx context.Context, question string, c domain.AskContext, s port.JSONSchema) (string, error) {
	n.schemas = append(n.schemas, s)
	return n.Answer(ctx, question, c)
}

func TestNarrateConstrainsTheReplyWhenTheNarratorCan(t *testing.T) {
	n := &schemaNarrator{narrator: narrator{
		reply: `{"title":"T","intro":"I","chapters":[{"title":"Auth","prose":"p","groups":["auth"]}]}`,
	}}

	got, err := usecase.Narrate(context.Background(), n, domain.Session{ID: "s"}, narrativeUnits())
	if err != nil {
		t.Fatalf("Narrate: %v", err)
	}
	if got.Source != domain.NarrativeModel {
		t.Fatalf("Source = %q, want model", got.Source)
	}

	if len(n.schemas) != 1 {
		t.Fatalf("asked with %d schemas, want the whole-session call to be constrained", len(n.schemas))
	}
	props, ok := n.schemas[0].Schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties: %v", n.schemas[0].Schema)
	}
	for _, field := range []string{"title", "intro", "chapters"} {
		if _, present := props[field]; !present {
			t.Errorf("schema is missing the %q field: %v", field, props)
		}
	}
}

func TestNarrativeSchemaAllowsOnlyRealAreaNames(t *testing.T) {
	// The schema is compiled into a grammar server-side, so restricting the area
	// names to an enum makes a hallucinated area impossible to emit rather than
	// merely something to detect afterwards.
	n := &schemaNarrator{narrator: narrator{
		reply: `{"title":"T","intro":"I","chapters":[{"title":"Auth","prose":"p","groups":["auth"]}]}`,
	}}

	if _, err := usecase.Narrate(context.Background(), n, domain.Session{ID: "s"}, narrativeUnits()); err != nil {
		t.Fatalf("Narrate: %v", err)
	}

	got := groupEnum(t, n.schemas[0].Schema)
	want := map[string]bool{"auth": true, "http": true, "root": true}
	if len(got) != len(want) {
		t.Fatalf("groups enum = %v, want the three real areas", got)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("groups enum offers %q, which is not a real area", name)
		}
	}
}

// groupEnum digs the enum of allowed area names out of the narrative schema.
func groupEnum(t *testing.T, schema map[string]any) []string {
	t.Helper()
	dive := func(m map[string]any, key string) map[string]any {
		next, ok := m[key].(map[string]any)
		if !ok {
			t.Fatalf("schema has no %q: %v", key, m)
		}
		return next
	}
	chapters := dive(dive(schema, "properties"), "chapters")
	groups := dive(dive(dive(chapters, "items"), "properties"), "groups")
	values, ok := dive(groups, "items")["enum"].([]string)
	if !ok {
		t.Fatalf("groups items carry no string enum: %v", groups)
	}
	return values
}

func TestPerChapterNarrationIsAlsoConstrained(t *testing.T) {
	// The per-chapter fallback is exactly where structure matters most: it runs
	// because the model is short of room, which is when it rambles.
	n := &schemaNarrator{narrator: narrator{reply: `not json at all`}}

	_, _ = usecase.Narrate(context.Background(), n, domain.Session{ID: "s"}, narrativeUnits())

	if len(n.schemas) != 4 {
		t.Fatalf("asked with %d schemas, want 1 whole-session + 3 per-chapter", len(n.schemas))
	}
	props, _ := n.schemas[1].Schema["properties"].(map[string]any)
	if _, present := props["prose"]; !present {
		t.Errorf("per-chapter schema should require prose: %v", n.schemas[1].Schema)
	}
	if _, present := props["chapters"]; present {
		t.Errorf("per-chapter schema should describe one chapter, not many: %v", props)
	}
}

func TestNarrateFallsBackOnUnparseableReply(t *testing.T) {
	n := &narrator{reply: "I think the session was mostly about authentication, honestly."}

	got, _ := usecase.Narrate(context.Background(), n, domain.Session{ID: "s"}, narrativeUnits())

	if got.Source != domain.NarrativeMechanical {
		t.Errorf("Source = %q, want mechanical for an unparseable reply", got.Source)
	}
	if len(got.Chapters) == 0 {
		t.Error("fallback must still produce chapters")
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
