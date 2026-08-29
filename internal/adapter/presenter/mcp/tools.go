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
		statusTool(w),
		feedbackTool(w),
		fileTool(w),
		findingsTool(w),
	}
}

// statusTool answers "where does this review stand" in a few lines.
//
// First because it is cheapest: an agent that reads this can decide whether any
// of the other calls are worth making, and most of the time the answer is no.
func statusTool(w Workspace) Tool {
	return Tool{
		Name: "review_status",
		Description: "Where the review currently open in mondspace-reviewer " +
			"stands: which change it covers, whether a human has signed it off " +
			"and what they said, and how much is outstanding. Cheap — ask this " +
			"first.",
		Schema: object(nil),
		Call: func(_ context.Context, args map[string]any) (string, error) {
			review, err := w.Open()
			if err != nil {
				return "", err
			}
			return status(review), nil
		},
	}
}

// status is the summary itself: counts and the human's own closing word, with
// pointers to the calls that would spend real context.
func status(r Review) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Review: %s\n", describe(r))

	outstanding := 0
	for _, n := range r.Notes {
		if n.Actionable() {
			outstanding++
		}
	}
	fmt.Fprintf(&b, "Reviewer notes outstanding: %d", outstanding)
	if outstanding > 0 {
		b.WriteString(" — read them with review_feedback")
	}
	b.WriteString("\n")

	if r.Signoff.Done() {
		fmt.Fprintf(&b, "Signed off %s", r.Signoff.At.Format("2006-01-02 15:04"))
		if r.Signoff.Comment != "" {
			fmt.Fprintf(&b, ": %q", r.Signoff.Comment)
		}
		b.WriteString("\n")
	} else {
		b.WriteString("Not signed off — a human has not finished with this yet.\n")
	}

	// Counted, never quoted. The point of the separate call is that an agent
	// opts into model output knowingly; leaking it into the cheap summary would
	// undo that.
	standing := 0
	for _, a := range r.Analyses {
		standing += len(a.Standing())
	}
	if standing > 0 {
		fmt.Fprintf(&b, "Model findings not yet ruled on: %d — read them with "+
			"model_findings, and verify each one yourself.\n", standing)
	}
	return b.String()
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

// fileTool is the whole human record of one file.
//
// More generous than review_feedback on purpose: naming a file is a deliberate
// narrowing, and someone who has narrowed that far wants the approvals too.
// "A human read this and was happy with it" is worth an agent knowing before it
// rewrites the thing.
func fileTool(w Workspace) Tool {
	return Tool{
		Name: "review_file",
		Description: "Everything the human reviewer wrote about one file in the " +
			"review currently open in mondspace-reviewer, including what they " +
			"approved. Excludes anything a model inferred.",
		Schema: object(map[string]any{
			"path": str("file to read the review of, as it appears in the diff"),
		}, "path"),
		Call: func(_ context.Context, args map[string]any) (string, error) {
			path := text(args, "path")
			if path == "" {
				return "", fmt.Errorf("which file? pass path, as it appears in the diff")
			}
			review, err := w.Open()
			if err != nil {
				return "", err
			}
			return fileRecord(review, path), nil
		},
	}
}

// fileRecord renders every standing human note on one file.
func fileRecord(r Review, path string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s in %s\n", path, describe(r))

	shown := 0
	for _, n := range r.Notes {
		// Superseded notes are dropped even here: a note the reviewer replaced
		// is a draft of the one below it, and showing both invites an agent to
		// answer the wording that was withdrawn.
		if n.SupersededBy != "" || fileOf(r, n) != path {
			continue
		}
		shown++
		fmt.Fprintf(&b, "\n%d. [%s] %s\n   %s\n",
			shown, n.Kind, line(n.Anchor), n.Text)
	}

	if shown == 0 {
		fmt.Fprintf(&b, "\nThe reviewer has written nothing about %s.\n", path)
	}
	return b.String()
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

// inferredWarning is attached to every reply that carries model output, and it
// is deliberately the first thing in it.
//
// The judge msr runs is a small local model on the reviewer's own machine. Its
// findings are worth reading and are not worth acting on unverified: an agent
// that treats them as instructions closes a loop with no human in it — the
// model proposes, the agent implements, and msr audits the result with the same
// model (ADR 0003, ADR 0031).
const inferredWarning = "These are INFERRED, not written by a human. They come from a " +
	"small local model reading the diff, and it is wrong often enough to matter: " +
	"it invents problems that are not there and misses ones that are. Verify each " +
	"one against the actual code before you act on it, and do not quote it back as " +
	"though a reviewer had said it."

// findingsTool is the inferred half of the surface, behind a name that says so.
//
// A separate call rather than a section of review_feedback: mixing them would
// mean an agent asking "what does the reviewer want" receives a machine's
// guesses in the same list, in the same voice, and no way to tell them apart.
func findingsTool(w Workspace) Tool {
	return Tool{
		Name: "model_findings",
		Description: "Findings from mondspace-reviewer's automated audits of the " +
			"open review (security, breaking changes). INFERRED by a small local " +
			"model, NOT written by a human: treat every one as a lead to verify " +
			"against the code, not as a reviewer's instruction. Use review_feedback " +
			"for what the human actually said.",
		Schema: object(map[string]any{
			"path": str("file to narrow to, as it appears in the diff; omit for everything"),
		}),
		Call: func(_ context.Context, args map[string]any) (string, error) {
			review, err := w.Open()
			if err != nil {
				return "", err
			}
			return modelFindings(review, text(args, "path")), nil
		},
	}
}

// modelFindings renders the audits, standing findings first and settled ones
// after, each carrying where it came from.
func modelFindings(r Review, path string) string {
	var b strings.Builder
	b.WriteString(inferredWarning + "\n\n")
	fmt.Fprintf(&b, "Review: %s\n", describe(r))

	shown := 0
	for _, a := range r.Analyses {
		if !a.Done() {
			continue
		}
		for _, pass := range []bool{true, false} {
			for _, f := range a.Findings {
				if f.Stands() != pass || (path != "" && f.File != path) {
					continue
				}
				shown++
				fmt.Fprintf(&b, "\n%d. [%s · %s] %s\n   %s\n",
					shown, a.Kind, f.Severity.Normalise(), firstNonEmpty(f.File, "the change as a whole"), f.Note)
				if !f.Stands() {
					// Kept rather than filtered: an agent that cannot see the
					// dismissal raises the same thing again next time.
					fmt.Fprintf(&b, "   The human reviewer dismissed this — settled, not work.\n")
				}
				fmt.Fprintf(&b, "   Inferred by %s on %s. Check it.\n",
					firstNonEmpty(a.Model, "a local model"), a.At.Format("2006-01-02 15:04"))
			}
		}
	}

	if shown == 0 {
		b.WriteString("\nNo audit has produced a finding")
		if path != "" {
			fmt.Fprintf(&b, " on %s", path)
		}
		b.WriteString(". That is the usual result, and it is not evidence that the change is safe.\n")
	}
	return b.String()
}

// firstNonEmpty is the first of its arguments with something in it.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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

// line says which line of an already-named file a note is about.
func line(anchor string) string {
	if anchor == "" {
		return "the file as a whole"
	}
	return "on: " + strings.TrimSpace(anchor)
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
	if props == nil {
		// Not nil: a client reading `"properties": null` is entitled to be
		// unhappy about it, and "no arguments" is an empty object.
		props = map[string]any{}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
