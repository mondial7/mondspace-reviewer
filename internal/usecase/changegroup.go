package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

// GroupChanges collects files that changed together in the same directory, so a
// reviewer sees one act of work rather than a list of files. A file with no
// companions stands alone rather than being forced into a group of one.
//
// Input order is preserved: the caller has already ordered the changes (newest
// first), and reshuffling would lose the thread.
func GroupChanges(units []domain.Unit, diffs map[string]domain.Diff) []domain.ChangeGroup {
	order := []string{}
	byDir := map[string][]domain.Unit{}

	for _, u := range units {
		d := dirOf(u)
		if _, seen := byDir[d]; !seen {
			order = append(order, d)
		}
		byDir[d] = append(byDir[d], u)
	}

	groups := make([]domain.ChangeGroup, 0, len(order))
	for _, dir := range order {
		members := byDir[dir]
		g := domain.ChangeGroup{ID: groupID(members), Dir: dir, Units: members}
		var sample strings.Builder
		for _, u := range members {
			added, removed := countChangedLines(diffs[u.ID])
			g.Added += added
			g.Removed += removed
			if sample.Len() < 2000 {
				compact, _ := CompactDiff(diffs[u.ID], diffLinesPerGroup/max(len(members), 1)+2)
				sample.WriteString(compact.Text)
			}
		}
		g.Sample = sample.String()
		groups = append(groups, g)
	}
	return groups
}

// dirOf is the directory a unit's file lives in, or "." for a file at the root.
func dirOf(u domain.Unit) string {
	if len(u.Files) == 0 {
		return "."
	}
	return filepath.ToSlash(filepath.Dir(filepath.ToSlash(u.Files[0])))
}

// groupID identifies a group by its members, so a description written for it can
// be stored and matched back to it on a later run.
func groupID(units []domain.Unit) string {
	ids := make([]string, 0, len(units))
	for _, u := range units {
		ids = append(ids, u.ID)
	}
	sum := sha256.Sum256([]byte(strings.Join(ids, "\x00")))
	return hex.EncodeToString(sum[:6])
}

const (
	// maxDescribedGroups bounds the model cost. One call per group is fine for
	// twenty groups and not for four hundred.
	maxDescribedGroups = 24
	// diffLinesPerGroup is how much of a group's change the model is shown. It
	// needs enough to say what the code is for, not the whole file.
	diffLinesPerGroup = 24
	meaningChars      = 160
)

// DescribeGroups asks the model what each group of changes is *for*. A file's
// mechanical headline ("edited jsonl.go") tells a reviewer nothing they cannot
// read off the filename; this is the sentence that earns its place.
//
// One bounded call per group, schema-constrained where the endpoint supports it.
// A group the model could not describe is left undescribed rather than filled
// with a mechanical sentence dressed up as meaning. Returns groupID → meaning.
func DescribeGroups(ctx context.Context, n Narrator, sess domain.Session, groups []domain.ChangeGroup, onProgress func(map[string]string)) map[string]string {
	meanings := map[string]string{}
	if n == nil {
		return meanings
	}

	for i, g := range groups {
		if i == maxDescribedGroups {
			break
		}
		reply, err := ask(ctx, n, describePrompt(sess, g), meaningSchema())
		if err != nil {
			continue
		}
		var m struct {
			Meaning string `json:"meaning"`
		}
		if start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}"); start >= 0 && end > start {
			_ = json.Unmarshal([]byte(reply[start:end+1]), &m)
		}
		if text := Brief(m.Meaning, meaningChars); text != "" {
			meanings[g.ID] = text
			if onProgress != nil {
				onProgress(meanings)
			}
		}
	}
	return meanings
}

func meaningSchema() port.JSONSchema {
	return port.JSONSchema{
		Name: "change_meaning",
		Schema: object(map[string]any{
			"meaning": map[string]any{"type": "string", "maxLength": meaningChars},
		}, "meaning"),
	}
}

// describePrompt shows the model the files and a bounded slice of what changed
// in them — enough to say what the change is for, not the whole diff.
func describePrompt(sess domain.Session, g domain.ChangeGroup) string {
	var b strings.Builder
	b.WriteString("You explain what a code change is for, to a reviewer.\n")
	if sess.Prompt != "" {
		b.WriteString("The developer asked the agent to: " + sess.Prompt + "\n")
	}
	b.WriteString(fmt.Sprintf("\n%d file(s) changed in %s:\n", len(g.Units), g.Dir))
	for _, u := range g.Units {
		b.WriteString("- " + strings.Join(u.Files, ", ") + "\n")
	}
	if g.Sample != "" {
		b.WriteString("\nWhat changed:\n" + g.Sample + "\n")
	}
	b.WriteString(`
In one sentence of at most 160 characters, say what this change is FOR — the
behaviour or intent, not the file names. Do not restate the filenames. JSON only:
{"meaning":".."}`)
	return b.String()
}
