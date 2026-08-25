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

func TestGroupChangesCollectsFilesThatChangedTogether(t *testing.T) {
	// Five files added under one package is one act of work, not five. Reviewing
	// them as five entries buries the thing that actually happened.
	units := []domain.Unit{
		{ID: "u1", Files: []string{"internal/store/pg/pg.go"}},
		{ID: "u2", Files: []string{"internal/store/pg/pg_test.go"}},
		{ID: "u3", Files: []string{"internal/store/pg/schema.go"}},
		{ID: "u4", Files: []string{"README.md"}},
	}
	diffs := map[string]domain.Diff{
		"u1": {Text: "@@\n+a\n+b\n"},
		"u2": {Text: "@@\n+c\n"},
		"u3": {Text: "@@\n+d\n-e\n"},
		"u4": {Text: "@@\n+f\n"},
	}

	got := usecase.GroupChanges(units, diffs)

	if len(got) != 2 {
		t.Fatalf("got %d groups, want the package and the loose file: %+v", len(got), got)
	}
	pkg := got[0]
	if pkg.Dir != "internal/store/pg" {
		t.Errorf("Dir = %q, want the shared directory", pkg.Dir)
	}
	if len(pkg.Units) != 3 {
		t.Errorf("group holds %d files, want 3", len(pkg.Units))
	}
	// The group carries the combined churn, so it can be read without expanding.
	if pkg.Added != 4 || pkg.Removed != 1 {
		t.Errorf("group churn = +%d -%d, want +4 -1", pkg.Added, pkg.Removed)
	}
	// A file with no companions stands alone rather than being forced into a group.
	if last := got[1]; last.Dir != "." || len(last.Units) != 1 {
		t.Errorf("the loose file should stand alone, got %+v", last)
	}
}

func TestGroupChangesKeepsEveryFileExactlyOnce(t *testing.T) {
	units := []domain.Unit{
		{ID: "a", Files: []string{"x/one.go"}},
		{ID: "b", Files: []string{"x/two.go"}},
		{ID: "c", Files: []string{"y/three.go"}},
		{ID: "d", Files: []string{"y/four.go"}},
		{ID: "e", Files: []string{"lonely.go"}},
	}

	got := usecase.GroupChanges(units, nil)

	seen := map[string]int{}
	for _, g := range got {
		for _, u := range g.Units {
			seen[u.ID]++
		}
	}
	for _, u := range units {
		if seen[u.ID] != 1 {
			t.Errorf("unit %s appears %d times, want exactly once", u.ID, seen[u.ID])
		}
	}
}

func TestGroupChangesIsStableInInputOrder(t *testing.T) {
	// The changes column is ordered newest-first by its caller; grouping must not
	// reshuffle that, or the reader loses the thread.
	units := []domain.Unit{
		{ID: "a", Files: []string{"z/one.go"}},
		{ID: "b", Files: []string{"a/two.go"}},
		{ID: "c", Files: []string{"z/three.go"}},
	}

	got := usecase.GroupChanges(units, nil)

	if len(got) != 2 || got[0].Dir != "z" || got[1].Dir != "a" {
		t.Errorf("groups = %+v, want z first because its first file came first", got)
	}
}

func TestGroupIDIsStableSoADescriptionCanBeStored(t *testing.T) {
	units := []domain.Unit{
		{ID: "u1", Files: []string{"pkg/a.go"}},
		{ID: "u2", Files: []string{"pkg/b.go"}},
	}

	first := usecase.GroupChanges(units, nil)
	second := usecase.GroupChanges(units, nil)

	if first[0].ID == "" || first[0].ID != second[0].ID {
		t.Errorf("group id = %q then %q, want a stable non-empty id", first[0].ID, second[0].ID)
	}
}

// describer answers a per-group prompt and records what it was asked.
type describer struct {
	reply  string
	err    error
	asked  []string
	schema []port.JSONSchema
}

func (d *describer) Answer(_ context.Context, q string, _ domain.AskContext) (string, error) {
	d.asked = append(d.asked, q)
	return d.reply, d.err
}

func (d *describer) AnswerSchema(ctx context.Context, q string, c domain.AskContext, s port.JSONSchema) (string, error) {
	d.schema = append(d.schema, s)
	return d.Answer(ctx, q, c)
}

func TestDescribeGroupsExplainsWhatEachChangeMeans(t *testing.T) {
	// "edited jsonl.go" tells a reviewer nothing they cannot see from the
	// filename. What they need is what the change is *for*.
	d := &describer{reply: `{"meaning":"Persists a session's story so it survives a restart."}`}
	groups := usecase.GroupChanges([]domain.Unit{
		{ID: "u1", Files: []string{"internal/store/jsonl/jsonl.go"}},
		{ID: "u2", Files: []string{"internal/store/jsonl/jsonl_test.go"}},
	}, map[string]domain.Diff{
		"u1": {Text: "@@\n+func SaveNarrative()\n"},
		"u2": {Text: "@@\n+func TestNarrative()\n"},
	})

	got := usecase.DescribeGroups(context.Background(), d, domain.Session{Prompt: "cache the story"}, groups, nil)

	if len(got) != 1 {
		t.Fatalf("described %d groups, want 1", len(got))
	}
	if got[groups[0].ID] != "Persists a session's story so it survives a restart." {
		t.Errorf("meaning = %q", got[groups[0].ID])
	}
	// The schema is used where available, so the reply is JSON by construction.
	if len(d.schema) != 1 {
		t.Errorf("asked with %d schemas, want the description constrained", len(d.schema))
	}
	// The prompt must carry enough to say something meaningful: the files, and
	// some of what changed in them.
	if !strings.Contains(d.asked[0], "jsonl.go") || !strings.Contains(d.asked[0], "SaveNarrative") {
		t.Errorf("prompt should carry files and diff content:\n%s", d.asked[0])
	}
}

func TestDescribeGroupsSkipsWhatItCannotDescribe(t *testing.T) {
	// A model that fails must leave the group undescribed rather than filling it
	// with a mechanical sentence dressed up as meaning.
	d := &describer{err: errors.New("offline")}
	groups := usecase.GroupChanges([]domain.Unit{{ID: "u1", Files: []string{"a/x.go"}}}, nil)

	got := usecase.DescribeGroups(context.Background(), d, domain.Session{}, groups, nil)

	if len(got) != 0 {
		t.Errorf("got %v, want nothing described when the model is unavailable", got)
	}
}

func TestDescribeGroupsIsBounded(t *testing.T) {
	// One model call per group is fine for twenty groups and not for four
	// hundred; the cost has to have a ceiling.
	var units []domain.Unit
	for i := 0; i < 200; i++ {
		units = append(units, domain.Unit{
			ID: fmt.Sprintf("u%03d", i), Files: []string{fmt.Sprintf("pkg%03d/a.go", i)},
		})
	}
	d := &describer{reply: `{"meaning":"x"}`}

	usecase.DescribeGroups(context.Background(), d, domain.Session{}, usecase.GroupChanges(units, nil), nil)

	if len(d.asked) > 32 {
		t.Errorf("made %d calls, want a bounded number", len(d.asked))
	}
}

func TestFileTreeIndentsFilesUnderTheirFolders(t *testing.T) {
	units := []domain.Unit{
		{ID: "u1", Files: []string{"internal/store/pg.go"}},
		{ID: "u2", Files: []string{"internal/store/pg_test.go"}},
		{ID: "u3", Files: []string{"internal/web/web.go"}},
		{ID: "u4", Files: []string{"README.md"}},
	}
	diffs := map[string]domain.Diff{"u1": {Text: "@@\n+a\n-b\n"}}

	got := usecase.FileTree(units, diffs)

	// A directory appears once, before its contents, with the depth to indent by.
	var lines []string
	for _, n := range got {
		kind := "file"
		if n.IsDir {
			kind = "dir"
		}
		lines = append(lines, fmt.Sprintf("%d:%s:%s", n.Depth, kind, n.Name))
	}
	// Paths sort as a file browser lists them, so README.md leads: 'R' sorts
	// before 'i'. Directories are emitted once, immediately before their contents.
	want := []string{
		"0:file:README.md",
		"0:dir:internal", "1:dir:store", "2:file:pg.go", "2:file:pg_test.go",
		"1:dir:web", "2:file:web.go",
	}
	if len(lines) != len(want) {
		t.Fatalf("tree =\n  %s\nwant\n  %s", strings.Join(lines, "\n  "), strings.Join(want, "\n  "))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}

	// A file row carries what the compact view shows: churn and its unit.
	for _, n := range got {
		if n.Name == "pg.go" {
			if n.Added != 1 || n.Removed != 1 || n.UnitID != "u1" {
				t.Errorf("pg.go row = %+v, want +1 -1 on u1", n)
			}
		}
	}
}

func TestFileTreeHandlesRootOnlyFiles(t *testing.T) {
	got := usecase.FileTree([]domain.Unit{{ID: "a", Files: []string{"go.mod"}}}, nil)

	if len(got) != 1 || got[0].IsDir || got[0].Depth != 0 || got[0].Name != "go.mod" {
		t.Errorf("tree = %+v, want a single root-level file", got)
	}
}

func TestGroupIDSurvivesTheReviewGrowing(t *testing.T) {
	// Unit ids are positional, so adding one file renumbers every unit after it.
	// A group id derived from those ids would change too, orphaning the
	// description written for it — and on a live review that rebuild happens
	// every fifteen seconds, so a description could never survive to be read.
	before := usecase.GroupChanges([]domain.Unit{
		{ID: "t-f001", Files: []string{"internal/tui/model.go"}},
		{ID: "t-f002", Files: []string{"internal/tui/view.go"}},
	}, nil)

	// A new file lands earlier in the walk; everything after it renumbers.
	after := usecase.GroupChanges([]domain.Unit{
		{ID: "t-f001", Files: []string{"docs/index.md"}},
		{ID: "t-f002", Files: []string{"internal/tui/model.go"}},
		{ID: "t-f003", Files: []string{"internal/tui/view.go"}},
	}, nil)

	var beforeTUI, afterTUI string
	for _, g := range before {
		if g.Dir == "internal/tui" {
			beforeTUI = g.ID
		}
	}
	for _, g := range after {
		if g.Dir == "internal/tui" {
			afterTUI = g.ID
		}
	}
	if beforeTUI == "" || afterTUI == "" {
		t.Fatalf("missing the tui group: %q %q", beforeTUI, afterTUI)
	}
	if beforeTUI != afterTUI {
		t.Errorf("group id changed from %q to %q because an unrelated file was added",
			beforeTUI, afterTUI)
	}
}

func TestGroupIDChangesWhenTheGroupItselfDoes(t *testing.T) {
	// Stability must not become staleness: a description written for two files
	// should not silently carry over to three.
	two := usecase.GroupChanges([]domain.Unit{
		{ID: "a", Files: []string{"pkg/one.go"}},
		{ID: "b", Files: []string{"pkg/two.go"}},
	}, nil)
	three := usecase.GroupChanges([]domain.Unit{
		{ID: "a", Files: []string{"pkg/one.go"}},
		{ID: "b", Files: []string{"pkg/two.go"}},
		{ID: "c", Files: []string{"pkg/three.go"}},
	}, nil)

	if two[0].ID == three[0].ID {
		t.Error("a group that gained a file should not keep its old description")
	}
}

func TestGroupIDIgnoresTheOrderFilesArriveIn(t *testing.T) {
	a := usecase.GroupChanges([]domain.Unit{
		{ID: "x", Files: []string{"pkg/one.go"}},
		{ID: "y", Files: []string{"pkg/two.go"}},
	}, nil)
	b := usecase.GroupChanges([]domain.Unit{
		{ID: "y", Files: []string{"pkg/two.go"}},
		{ID: "x", Files: []string{"pkg/one.go"}},
	}, nil)

	if a[0].ID != b[0].ID {
		t.Errorf("group id depends on walk order: %q vs %q", a[0].ID, b[0].ID)
	}
}

func TestDescribeFileExplainsOneFileOnItsOwn(t *testing.T) {
	// Reading a folder's summary is where a reviewer starts; the next question is
	// always "and what happened to this one".
	d := &describer{reply: `{"meaning":"Adds a validator interface so the JWT library can be swapped."}`}
	unit := domain.Unit{ID: "u1", Files: []string{"auth/token.go"}}
	diff := domain.Diff{Text: "@@\n+type TokenValidator interface{}\n"}

	got, _, err := usecase.DescribeFile(context.Background(), d,
		domain.Session{Prompt: "add auth"}, unit, diff)
	if err != nil {
		t.Fatalf("DescribeFile: %v", err)
	}
	if got != "Adds a validator interface so the JWT library can be swapped." {
		t.Errorf("meaning = %q", got)
	}
	// The prompt must carry the file and what changed in it, or the answer is a
	// guess from the filename.
	if !strings.Contains(d.asked[0], "auth/token.go") || !strings.Contains(d.asked[0], "TokenValidator") {
		t.Errorf("prompt should carry the file and its diff:\n%s", d.asked[0])
	}
	if len(d.schema) != 1 {
		t.Errorf("the description should be schema-constrained, got %d schemas", len(d.schema))
	}
}

func TestDescribeFileReportsWhenItCannot(t *testing.T) {
	// Silence would be indistinguishable from "this file means nothing".
	d := &describer{err: errors.New("summarizer offline")}

	_, _, err := usecase.DescribeFile(context.Background(), d, domain.Session{},
		domain.Unit{ID: "u1", Files: []string{"a.go"}}, domain.Diff{})

	if err == nil {
		t.Error("a failure should be reported, not swallowed")
	}
}

func TestFileKeyIsStableAndDistinctFromAGroup(t *testing.T) {
	// A file's description lives in the same map as a group's, so the two must
	// not collide — and a file's key must survive renumbering just as a group's
	// does.
	a := usecase.FileKey(domain.Unit{ID: "t-f001", Files: []string{"auth/token.go"}})
	b := usecase.FileKey(domain.Unit{ID: "t-f009", Files: []string{"auth/token.go"}})
	other := usecase.FileKey(domain.Unit{ID: "t-f001", Files: []string{"auth/other.go"}})

	if a != b {
		t.Errorf("file key changed with the unit number: %q vs %q", a, b)
	}
	if a == other {
		t.Error("two files must not share a key")
	}
	if a == "" {
		t.Error("a file needs a key")
	}
}

func TestDescribeFileAlsoCallsOutTheLinesWorthReading(t *testing.T) {
	// The description says what the change is for; these say where to look. They
	// come from the same call so they cannot disagree with it.
	d := &describer{reply: `{"meaning":"Adds a validator interface so the JWT library can be swapped.",
		"key_lines":["+type TokenValidator interface{}","+func Validate(tok string) error {"]}`}
	unit := domain.Unit{ID: "u1", Files: []string{"auth/token.go"}}
	diff := domain.Diff{Text: "@@\n+type TokenValidator interface{}\n+func Validate(tok string) error {\n"}

	meaning, lines, err := usecase.DescribeFile(context.Background(), d,
		domain.Session{Prompt: "add auth"}, unit, diff)
	if err != nil {
		t.Fatalf("DescribeFile: %v", err)
	}
	if meaning == "" {
		t.Error("the description is still the point")
	}
	if len(lines) != 2 || lines[0] != "+type TokenValidator interface{}" {
		t.Errorf("key lines = %v, want the two the model chose", lines)
	}
}

func TestKeyLinesAreCappedAtThree(t *testing.T) {
	// A highlight that covers half the diff is not a highlight.
	d := &describer{reply: `{"meaning":"m","key_lines":["a","b","c","d","e"]}`}

	_, lines, err := usecase.DescribeFile(context.Background(), d, domain.Session{},
		domain.Unit{ID: "u", Files: []string{"a.go"}}, domain.Diff{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Errorf("got %d key lines, want at most 3", len(lines))
	}
}

func TestADescriptionWithoutKeyLinesIsStillADescription(t *testing.T) {
	// A model that answers the first half of the question has still answered
	// something useful, and losing it would be worse.
	d := &describer{reply: `{"meaning":"Adds retry to the pool."}`}

	meaning, lines, err := usecase.DescribeFile(context.Background(), d, domain.Session{},
		domain.Unit{ID: "u", Files: []string{"a.go"}}, domain.Diff{})
	if err != nil {
		t.Fatalf("DescribeFile: %v", err)
	}
	if meaning != "Adds retry to the pool." {
		t.Errorf("meaning = %q", meaning)
	}
	if len(lines) != 0 {
		t.Errorf("key lines = %v, want none", lines)
	}
}

func TestALongMeaningIsCutAtAWordNotMidWord(t *testing.T) {
	// A grammar stops the model at exactly its maxLength, which lands mid-word:
	// "…identify units within a specific active session. It also re". The schema
	// therefore allows more than the column shows, and the trim happens here,
	// where it can respect a word boundary.
	long := "This change improves the reliability of session management and navigation " +
		"by ensuring that unit lookups correctly resolve sessions from incoming " +
		"requests and maintaining the intended destination during redirects."
	d := &describer{reply: `{"meaning":"` + long + `"}`}

	got, _, err := usecase.DescribeFile(context.Background(), d, domain.Session{},
		domain.Unit{ID: "u1", Files: []string{"web.go"}}, domain.Diff{Text: "@@\n+x\n"})
	if err != nil {
		t.Fatalf("DescribeFile: %v", err)
	}

	if !strings.HasSuffix(got, "…") {
		t.Errorf("a trimmed meaning should say it was trimmed, got %q", got)
	}
	last := strings.TrimSuffix(got, "…")
	if !strings.HasPrefix(long, last) {
		t.Errorf("the trim should be a prefix of what the model wrote, got %q", last)
	}
	// The cut must land where a word ended: the next character in the original is
	// a space, which is exactly what "not mid-word" means.
	if rest := long[len(last):]; rest != "" && !strings.HasPrefix(rest, " ") {
		t.Errorf("cut mid-word: %q | %q", last[max(0, len(last)-30):], rest[:min(20, len(rest))])
	}
}

func TestTheSchemaLeavesRoomToFinishTheSentence(t *testing.T) {
	// If the schema's cap equalled the display width the model would be stopped
	// exactly at the truncation point and there would be nothing left to trim.
	d := &describer{reply: `{"meaning":"short"}`}
	if _, _, err := usecase.DescribeFile(context.Background(), d, domain.Session{},
		domain.Unit{ID: "u1", Files: []string{"a.go"}}, domain.Diff{}); err != nil {
		t.Fatalf("DescribeFile: %v", err)
	}

	props := d.schema[0].Schema["properties"].(map[string]any)
	cap := props["meaning"].(map[string]any)["maxLength"].(int)
	if cap <= 160 {
		t.Errorf("the schema should allow more than the 160 shown, got %d", cap)
	}
}
