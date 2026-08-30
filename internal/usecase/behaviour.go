package usecase

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

// How much of the change one narration is shown. A chapter has to be written
// from what the diff says, so the model has to see the diff — and a small local
// context has to be able to hold it, so both ends are bounded.
const (
	behaviourFiles     = 24
	behaviourDiffLines = 26
	behaviourChapters  = 5
)

// behaviourPrompt asks what the change *does*, with the change in front of it.
//
// The older prompt listed area names and a few filenames, so the best a model
// could answer with was the name of a directory — and a directory gets more
// generic the more files land in it, which is the opposite of what a story is
// for. A reviewer does not need to be told that cmd/ changed. They need to be
// told that tags now resolve to the commit they point at.
func behaviourPrompt(sess domain.Session, units []domain.Unit, diffs map[string]domain.Diff) string {
	var b strings.Builder
	b.WriteString("You are explaining to a reviewer what a change actually does.\n")
	if sess.Prompt != "" {
		b.WriteString("It was made in response to: " + sess.Prompt + "\n")
	}

	b.WriteString(fmt.Sprintf("\n%s changed. Here is each one, with the important "+
		"part of its diff:\n", count(len(units), "file")))

	shown := 0
	for _, u := range units {
		if shown == behaviourFiles {
			b.WriteString(fmt.Sprintf("\n…and %d more files\n", len(units)-shown))
			break
		}
		compact, hidden := CompactDiff(diffs[u.ID], behaviourDiffLines)
		b.WriteString("\n--- " + strings.Join(u.Files, ", ") + "\n")
		if text := strings.TrimSpace(compact.Text); text != "" {
			b.WriteString(text + "\n")
		} else {
			b.WriteString("(no textual change)\n")
		}
		if hidden > 0 {
			b.WriteString(fmt.Sprintf("… %s of this file not shown\n", count(hidden, "line")))
		}
		shown++
	}

	b.WriteString(`
Work out what changed in behaviour: what the code now does that it did not,
which rule or default moved, what was added, removed or renamed, and what that
means for whoever calls it.

Group that into 2 to ` + fmt.Sprint(behaviourChapters) + ` chapters. A chapter is
one thing that happened, not one folder. Two files in different directories that
do one thing belong in one chapter; one directory doing three unrelated things
is three chapters.

Title each chapter as the change itself, in the present tense, under 70
characters — "Tokens expire after an hour, not a day", never "auth" or
"changes to token.go". A title that is a path or a filename is wrong.

Then 1 to 2 sentences saying what it does and what it means for someone using
this code.

Say only what the diff shows. Where you cannot tell why something changed, say
what changed and stop: a reason nobody gave is worse than no reason.

List the files each chapter covers, using the paths exactly as given. Every file
belongs to exactly one chapter, and every file must appear.

Answer with JSON only, no explanation:
{"title":"..","intro":"..","emoji":["..",".."],"chapters":[{"title":"..","prose":"..","files":["path"]}]}`)
	return b.String()
}

// behaviourSchema constrains the files a chapter may name to the files that
// actually changed. A model that cannot name a file which is not there cannot
// write a chapter about one.
func behaviourSchema(units []domain.Unit) port.JSONSchema {
	paths := make([]string, 0, len(units))
	seen := map[string]bool{}
	for _, u := range units {
		for _, f := range u.Files {
			if !seen[f] {
				seen[f] = true
				paths = append(paths, f)
			}
		}
	}
	sort.Strings(paths)

	return port.JSONSchema{
		Name: "session_behaviour",
		Schema: object(map[string]any{
			"title": map[string]any{"type": "string"},
			"intro": map[string]any{"type": "string"},
			"emoji": map[string]any{
				"type":     "array",
				"maxItems": maxEmoji,
				"items":    map[string]any{"type": "string", "maxLength": 8},
			},
			"chapters": map[string]any{
				"type":     "array",
				"maxItems": behaviourChapters,
				"items": object(map[string]any{
					"title": map[string]any{"type": "string"},
					"prose": map[string]any{"type": "string"},
					"files": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string", "enum": paths},
					},
				}, "title", "prose", "files"),
			},
		}, "title", "intro", "chapters"),
	}
}

// unitsByFile indexes the units by every path they cover, which is the
// vocabulary a chapter answers in.
func unitsByFile(units []domain.Unit) map[string]string {
	out := map[string]string{}
	for _, u := range units {
		for _, f := range u.Files {
			out[f] = u.ID
		}
	}
	return out
}

// resolveFiles turns the paths a chapter named into the units they belong to.
// A path nobody recognises is dropped rather than guessed at; reconcileChapters
// then puts whatever was left out into a chapter of its own, so a file is never
// silently lost from the story.
func (c modelChapter) resolveFiles(byFile map[string]string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, f := range c.Files {
		id, ok := byFile[strings.TrimSpace(f)]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}
