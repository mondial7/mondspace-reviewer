package web

import (
	"strings"
	"testing"
)

func TestMarkdownRendersTheBasics(t *testing.T) {
	got := string(renderMarkdown(`# Heading

Some **bold** and *italic* and ` + "`code`" + ` text.

- first
- second

1. one
2. two

Another paragraph.`))

	for _, want := range []string{
		"<h3>Heading</h3>", "<strong>bold</strong>", "<em>italic</em>",
		"<code>code</code>", "<li>first</li>", "<li>second</li>",
		"<ol class=", "<li>one</li>", "<p>Another paragraph.</p>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestMarkdownEscapesEverythingFirst(t *testing.T) {
	// The text comes from a model. If it emits a script tag it must render as
	// characters on the page, never as markup.
	got := string(renderMarkdown("<script>alert('x')</script> and <b>raw</b>"))

	if strings.Contains(got, "<script>") || strings.Contains(got, "<b>raw</b>") {
		t.Errorf("model output was not escaped:\n%s", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("the tag should be visible as text:\n%s", got)
	}
}

func TestMarkdownKeepsCodeBlocksVerbatim(t *testing.T) {
	got := string(renderMarkdown("Try this:\n\n```go\nif x != nil { **not bold** }\n```\n"))

	if !strings.Contains(got, "<pre") || !strings.Contains(got, "if x != nil") {
		t.Errorf("fenced code should survive:\n%s", got)
	}
	// Markdown inside a code block is content, not formatting.
	if strings.Contains(got, "<strong>not bold</strong>") {
		t.Errorf("markdown inside a code block must not be applied:\n%s", got)
	}
}

func TestMarkdownLeavesPlainProseAlone(t *testing.T) {
	got := string(renderMarkdown("The change in s-u001 adds a Validator interface."))

	if !strings.Contains(got, "<p>The change in s-u001 adds a Validator interface.</p>") {
		t.Errorf("plain prose should be one paragraph:\n%s", got)
	}
}

func TestMarkdownHandlesEmptyInput(t *testing.T) {
	if got := string(renderMarkdown("   \n\n ")); strings.TrimSpace(got) != "" {
		t.Errorf("empty input should render nothing, got %q", got)
	}
}
