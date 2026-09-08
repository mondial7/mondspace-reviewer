package usecase

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/mondial7/mondspace-reviewer/contract"
)

// The station's output (ADR 0044).
//
// One store, several renderers, because the two consumers want different
// shapes: an implementation agent wants a flat list it can work down without
// deciding anything, and a planner wants themes it can put on a board. Neither
// of them wants the other's.

// ExportAgent renders items for an implementation agent.
//
// Flat and ordered by file, to minimise the context switching an agent pays for
// every time it moves between files. Each entry carries enough to act on
// without the review being open: where, what to do, and the code in question.
func ExportAgent(items []contract.Item) string {
	items = byFileThenLine(items)

	var out strings.Builder
	out.WriteString("# Review tasks\n\n")
	if len(items) == 0 {
		out.WriteString("Nothing outstanding.\n")
		return out.String()
	}
	fmt.Fprintf(&out, "%d item(s). Work them in the order given.\n", len(items))

	file := ""
	for _, item := range items {
		if item.Location.Path != file {
			file = item.Location.Path
			fmt.Fprintf(&out, "\n## %s\n", file)
		}
		fmt.Fprintf(&out, "\n### %s (%s)\n\n", item.Where(), item.Severity.Normalise())
		fmt.Fprintf(&out, "- **do:** %s\n", item.Directive)
		fmt.Fprintf(&out, "- **id:** `%s`\n", item.ID)
		if item.Snippet != "" {
			out.WriteString("\n```\n" + strings.TrimRight(item.Snippet, "\n") + "\n```\n")
		}
	}
	return out.String()
}

// ExportPlan renders items for a planning agent.
//
// Grouped rather than listed: a planner putting forty findings on a board one
// at a time produces a board nobody reads. The theme is the rule for an
// analyser's findings — twelve instances of one rule is one piece of work — and
// the directory for everything else, which is the closest thing to a module msr
// can know without a language server.
func ExportPlan(items []contract.Item) string {
	clusters := map[string][]contract.Item{}
	for _, item := range items {
		clusters[theme(item)] = append(clusters[theme(item)], item)
	}

	names := make([]string, 0, len(clusters))
	for name := range clusters {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := clusters[names[i]], clusters[names[j]]
		if ra, rb := worst(a), worst(b); ra != rb {
			return ra < rb
		}
		if len(a) != len(b) {
			return len(a) > len(b)
		}
		return names[i] < names[j]
	})

	var out strings.Builder
	out.WriteString("# Review backlog\n\n")
	if len(items) == 0 {
		out.WriteString("Nothing outstanding.\n")
		return out.String()
	}
	fmt.Fprintf(&out, "%d item(s) in %d theme(s), worst first.\n\n", len(items), len(names))

	for _, name := range names {
		members := byFileThenLine(clusters[name])
		severity := contract.Severities[worst(members)]
		fmt.Fprintf(&out, "## %s — %d item(s), %s\n\n", name, len(members), severity)
		fmt.Fprintf(&out, "%s\n\n", summarise(members))
		for _, item := range members {
			fmt.Fprintf(&out, "- `%s` %s — %s\n", item.ID, item.Where(), item.Directive)
		}
		out.WriteString("\n")
	}
	return out.String()
}

// theme is what a cluster is called.
func theme(item contract.Item) string {
	if item.Source == contract.SourceAnalyser && item.RuleID != "" {
		return item.Producer + ":" + item.RuleID
	}
	dir := path.Dir(item.Location.Path)
	if dir == "." || dir == "" {
		dir = "(root)"
	}
	return string(item.Source) + " in " + dir
}

// summarise is the one line a planner reads instead of the members.
func summarise(items []contract.Item) string {
	files := map[string]bool{}
	for _, item := range items {
		files[item.Location.Path] = true
	}
	first := items[0]
	what := first.Message
	if what == "" {
		what = first.Title
	}
	return fmt.Sprintf("%s Across %d file(s).", strings.TrimSpace(Brief(what, 160)), len(files))
}

// worst is the rank of the most severe item in a cluster.
func worst(items []contract.Item) int {
	rank := len(contract.Severities) - 1
	for _, item := range items {
		if r := item.Severity.Normalise().Rank(); r < rank {
			rank = r
		}
	}
	return rank
}

// ExportItemsMarkdown is the human-readable review document: the same items,
// grouped by file, with what said it and what was decided.
func ExportItemsMarkdown(items []contract.Item) string {
	items = byFileThenLine(items)

	var out strings.Builder
	out.WriteString("# Review\n\n")
	if len(items) == 0 {
		out.WriteString("Nothing outstanding.\n")
		return out.String()
	}

	file := ""
	for _, item := range items {
		if item.Location.Path != file {
			file = item.Location.Path
			fmt.Fprintf(&out, "\n## %s\n\n", file)
		}
		state := string(item.CurrentState())
		if item.Verdict != "" {
			state = string(item.Verdict)
		}
		fmt.Fprintf(&out, "- **%s** %s — %s _(%s, %s)_\n",
			item.Severity.Normalise(), item.Where(), item.Title, source(item), state)
	}
	return out.String()
}

// ExportGitHubIssues renders one issue per item, deduplicated by fingerprint so
// that a finding seen on two branches does not become two issues.
func ExportGitHubIssues(items []contract.Item) ([]byte, error) {
	type issue struct {
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Labels []string `json:"labels,omitempty"`
	}

	seen := map[string]bool{}
	out := []issue{}
	for _, item := range byFileThenLine(items) {
		if seen[item.Fingerprint] {
			continue
		}
		seen[item.Fingerprint] = true

		body := item.Directive
		if item.Message != "" {
			body += "\n\n> " + strings.ReplaceAll(item.Message, "\n", "\n> ")
		}
		if item.Snippet != "" {
			body += "\n\n```\n" + strings.TrimRight(item.Snippet, "\n") + "\n```"
		}
		body += fmt.Sprintf("\n\nFound at `%s` by %s.\nmsr item `%s`.", item.Where(), source(item), item.ID)

		labels := []string{"msr", string(item.Severity.Normalise())}
		if item.RuleID != "" {
			labels = append(labels, item.Producer+":"+item.RuleID)
		}
		out = append(out, issue{Title: item.Title, Body: body, Labels: labels})
	}
	return json.MarshalIndent(out, "", "  ")
}

// ExportItemsJSONL is the raw contract, for anything else.
func ExportItemsJSONL(items []contract.Item) ([]byte, error) {
	var out strings.Builder
	for _, item := range items {
		line, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		out.Write(line)
		out.WriteString("\n")
	}
	return []byte(out.String()), nil
}

// source is who said it, for a line a person reads.
func source(item contract.Item) string {
	who := string(item.Source)
	if item.Producer != "" && item.Producer != string(item.Source) {
		who = item.Producer
	}
	if item.RuleID != "" {
		who += " " + item.RuleID
	}
	return who
}

// byFileThenLine is the order every renderer here uses. Sorting a copy, because
// the caller's slice is usually the store's.
func byFileThenLine(items []contract.Item) []contract.Item {
	out := make([]contract.Item, len(items))
	copy(out, items)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Location.Path != out[j].Location.Path {
			return out[i].Location.Path < out[j].Location.Path
		}
		if out[i].Location.StartLine != out[j].Location.StartLine {
			return out[i].Location.StartLine < out[j].Location.StartLine
		}
		return out[i].ID < out[j].ID
	})
	return out
}
