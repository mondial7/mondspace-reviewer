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

// groupID identifies a group by the files in it, so a description written for it
// can be found again on a later run.
//
// Deliberately not the unit ids: those are positional (`-f001`, `-f002`), so
// adding one file renumbers every unit after it and would change the id of every
// group beyond it. On a live review the units are rebuilt every fifteen seconds,
// which meant a description could never survive long enough to be read.
//
// Paths are sorted, so the order git happens to walk them in does not matter.
func groupID(units []domain.Unit) string {
	paths := make([]string, 0, len(units))
	for _, u := range units {
		paths = append(paths, strings.Join(u.Files, ","))
	}
	sort.Strings(paths)

	sum := sha256.Sum256([]byte(strings.Join(paths, "\x00")))
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
	meanings, _, _ := DescribeGroupsReporting(ctx, n, sess, groups, onProgress)
	return meanings
}

// DescribeGroupsReporting is DescribeGroups that also says how many it could not
// describe, and why the first one failed. Swallowing those silently is what made
// "1 of 6 described" impossible to act on: the page could show the shortfall but
// nothing could say what caused it.
func DescribeGroupsReporting(ctx context.Context, n Narrator, sess domain.Session, groups []domain.ChangeGroup, onProgress func(map[string]string)) (map[string]string, int, error) {
	meanings := map[string]string{}
	failed := 0
	var first error
	if n == nil {
		return meanings, len(groups), fmt.Errorf("no summarizer available")
	}

	note := func(err error) {
		failed++
		if first == nil {
			first = err
		}
	}

	for i, g := range groups {
		if i == maxDescribedGroups {
			break
		}
		reply, err := ask(ctx, n, describePrompt(sess, g), meaningSchema())
		if err != nil {
			note(err)
			continue
		}
		var m struct {
			Meaning string `json:"meaning"`
		}
		if start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}"); start >= 0 && end > start {
			_ = json.Unmarshal([]byte(reply[start:end+1]), &m)
		}
		text := Brief(m.Meaning, meaningChars)
		if text == "" {
			note(fmt.Errorf("the model replied without a description: %.120q", reply))
			continue
		}
		meanings[g.ID] = text
		if onProgress != nil {
			onProgress(meanings)
		}
	}
	return meanings, failed, first
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

// FileTree lays the changed files out as an indented directory listing — the
// compact view, where the shape of the change is the folder structure rather
// than the diffs. Directories are emitted once, before their contents.
//
// Paths are sorted so the tree reads like a file browser; the grouped view keeps
// recency order, and the two answer different questions.
func FileTree(units []domain.Unit, diffs map[string]domain.Diff) []domain.TreeNode {
	type entry struct {
		parts []string
		unit  domain.Unit
	}
	entries := make([]entry, 0, len(units))
	for _, u := range units {
		if len(u.Files) == 0 {
			continue
		}
		entries = append(entries, entry{strings.Split(filepath.ToSlash(u.Files[0]), "/"), u})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.Join(entries[i].parts, "/") < strings.Join(entries[j].parts, "/")
	})

	var nodes []domain.TreeNode
	var open []string // the directory path currently emitted

	for _, e := range entries {
		dirs := e.parts[:len(e.parts)-1]

		// Emit only the directories that differ from the ones already open.
		shared := 0
		for shared < len(open) && shared < len(dirs) && open[shared] == dirs[shared] {
			shared++
		}
		for d := shared; d < len(dirs); d++ {
			nodes = append(nodes, domain.TreeNode{Depth: d, Name: dirs[d], IsDir: true})
		}
		open = dirs

		added, removed := countChangedLines(diffs[e.unit.ID])
		flags := make([]string, 0, len(e.unit.Flags))
		for _, f := range e.unit.Flags {
			flags = append(flags, string(f))
		}
		nodes = append(nodes, domain.TreeNode{
			Depth: len(dirs), Name: e.parts[len(e.parts)-1],
			UnitID: e.unit.ID, Added: added, Removed: removed, Flags: flags,
		})
	}
	return nodes
}

// FileKey identifies one file's description. Like a group's id it is derived
// from the path rather than the unit id, which is positional and shifts whenever
// the file set changes. Prefixed so a file and a group can share one map without
// colliding.
func FileKey(u domain.Unit) string {
	sum := sha256.Sum256([]byte(strings.Join(u.Files, ",")))
	return "f:" + hex.EncodeToString(sum[:6])
}

// DescribeFile says what one file's change is for. A folder's summary is where a
// reviewer starts; the next question is always "and what happened to this one".
//
// Unlike DescribeGroups this is never run in bulk: it is one file, asked for
// deliberately, so there is no budget to bound — only the reviewer's patience.
func DescribeFile(ctx context.Context, n Narrator, sess domain.Session, u domain.Unit, d domain.Diff) (string, error) {
	if n == nil {
		return "", fmt.Errorf("no summarizer available")
	}

	reply, err := ask(ctx, n, filePrompt(sess, u, d), meaningSchema())
	if err != nil {
		return "", err
	}

	var m struct {
		Meaning string `json:"meaning"`
	}
	if start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}"); start >= 0 && end > start {
		_ = json.Unmarshal([]byte(reply[start:end+1]), &m)
	}
	text := Brief(m.Meaning, meaningChars)
	if text == "" {
		return "", fmt.Errorf("the model did not describe this file")
	}
	return text, nil
}

// filePrompt shows the model one file and a bounded slice of what changed in it.
func filePrompt(sess domain.Session, u domain.Unit, d domain.Diff) string {
	var b strings.Builder
	b.WriteString("You explain what a change to one file is for, to a reviewer.\n")
	if sess.Prompt != "" {
		b.WriteString("The developer asked the agent to: " + sess.Prompt + "\n")
	}
	b.WriteString("\nFile: " + strings.Join(u.Files, ", ") + "\n")

	compact, _ := CompactDiff(d, fileDescribeLines)
	if strings.TrimSpace(compact.Text) != "" {
		b.WriteString("\nWhat changed:\n" + compact.Text + "\n")
	}
	b.WriteString(`
In one sentence of at most 160 characters, say what this change is FOR — the
behaviour or intent, not the file name. JSON only:
{"meaning":".."}`)
	return b.String()
}

// fileDescribeLines is how much of one file's diff the model is shown. More than
// a group gets, because there is only one file to spend it on.
const fileDescribeLines = 40
