package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// Review is one review as the tools serve it: what it is, what a human wrote on
// it, and what the model said about it.
//
// Human and inferred material travel together here and are separated by the
// tools, not by the workspace — so there is exactly one place in the code where
// the distinction is enforced, and it is the place an agent can see (ADR 0031).
type Review struct {
	ID    string
	Title string
	Ref   string
	Repo  string

	Notes    []domain.Note
	Analyses []domain.Analysis
	Signoff  domain.Signoff

	// Files resolves a note's unit id to the file it concerns, for notes
	// written before the file was recorded on the note itself.
	Files map[string]string
}

// Workspace is where the tools get their material.
//
// Two methods rather than one, because they cost different amounts: Open is the
// review already in front of the reviewer, and All has to walk the store. The
// split is what lets the cheap tools stay cheap (ADR 0031).
type Workspace interface {
	Open() (Review, error)
	All() ([]Review, error)
}

// Tools is the msr surface an agent can pull on.
func Tools(w Workspace) []Tool {
	return []Tool{
		feedbackTool(w),
	}
}

// feedbackTool hands back what the reviewer is still asking for.
func feedbackTool(w Workspace) Tool {
	return Tool{
		Name: "review_feedback",
		Description: "What the human reviewer is still asking for on the review " +
			"currently open in mondspace-reviewer: their questions, objections and " +
			"noted debt, in their own words. Excludes anything a model inferred. " +
			"Optionally narrowed to one file.",
		Schema: object(map[string]any{
			"path": str("file to narrow to, as it appears in the diff; omit for everything"),
		}),
		Call: func(_ context.Context, args map[string]any) (string, error) {
			review, err := w.Open()
			if err != nil {
				return "", err
			}
			return humanFeedback(review, text(args, "path")), nil
		},
	}
}

// humanFeedback renders the outstanding human notes, newest last so it reads as
// a record rather than a ranking.
func humanFeedback(r Review, path string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Review: %s\n", describe(r))

	shown := 0
	for _, n := range r.Notes {
		if !n.Actionable() {
			continue
		}
		file := fileOf(r, n)
		if path != "" && file != path {
			continue
		}
		shown++
		fmt.Fprintf(&b, "\n%d. [%s] %s\n", shown, n.Kind, where(file, n.Anchor))
		fmt.Fprintf(&b, "   %s\n", n.Text)
	}

	if shown == 0 {
		b.WriteString("\nThe reviewer has nothing outstanding")
		if path != "" {
			fmt.Fprintf(&b, " on %s", path)
		}
		b.WriteString(".\n")
	}
	return b.String()
}

// where says what a note is about: a file, and the line when there is one.
func where(file, anchor string) string {
	switch {
	case file == "" && anchor == "":
		return "the change as a whole"
	case anchor == "":
		return file
	case file == "":
		return "on: " + strings.TrimSpace(anchor)
	default:
		return file + " — on: " + strings.TrimSpace(anchor)
	}
}

// fileOf is the file a note concerns, from the note itself or from the review's
// units for notes written before that was recorded.
func fileOf(r Review, n domain.Note) string {
	if n.File != "" {
		return n.File
	}
	return r.Files[n.UnitID]
}

// describe names a review the way a human would recognise it.
func describe(r Review) string {
	parts := []string{}
	if r.Title != "" {
		parts = append(parts, r.Title)
	}
	if r.Ref != "" && r.Ref != r.Title {
		parts = append(parts, "("+r.Ref+")")
	}
	if len(parts) == 0 {
		return r.ID
	}
	if r.Repo != "" {
		parts = append(parts, "in "+r.Repo)
	}
	return strings.Join(parts, " ")
}

// text pulls a string argument, tolerating its absence: every argument on this
// surface is optional or checked by the tool itself, and a missing one is a
// question to answer rather than a fault to report.
func text(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// object and str build the small JSON schemas these tools declare.
func object(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
