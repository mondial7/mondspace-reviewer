package web

import (
	"fmt"
	"html/template"
	"regexp"
	"strings"
)

// renderMarkdown turns a model's answer into the small subset of markdown it
// actually uses: headings, bold, italic, inline code, fenced code, and lists.
//
// It is not a markdown implementation and does not try to be. It exists because
// a reviewer's answer arrives full of `**bold**` and `- bullets` that were
// rendering as literal asterisks on the page.
//
// The text is untrusted — it comes from a model — so everything is HTML-escaped
// *first* and markup is only ever added afterwards. That ordering is the whole
// safety argument: no path can turn model output into live markup.
func renderMarkdown(text string) template.HTML {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return ""
	}

	// The text is walked in segments rather than marked with placeholders: an
	// earlier version substituted a NUL-delimited token, and HTML escaping
	// rewrites NUL to U+FFFD, so the token never survived to be substituted back.
	var out strings.Builder
	last := 0
	for _, m := range fencedCode.FindAllStringSubmatchIndex(text, -1) {
		writeProse(&out, text[last:m[0]])
		body := strings.TrimRight(text[m[4]:m[5]], "\n")
		out.WriteString(`<pre class="md__code"><code>` +
			template.HTMLEscapeString(body) + "</code></pre>")
		last = m[1]
	}
	writeProse(&out, text[last:])

	return template.HTML(out.String())
}

// writeProse escapes a run of ordinary text and renders its paragraphs. Escaping
// happens here and markup is only ever added afterwards, which is the whole
// safety argument: no path can turn model output into live markup.
func writeProse(out *strings.Builder, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	for _, para := range strings.Split(template.HTMLEscapeString(text), "\n\n") {
		if para = strings.TrimSpace(para); para != "" {
			out.WriteString(renderBlock(para))
		}
	}
}

var (
	fencedCode    = regexp.MustCompile("(?s)```([a-zA-Z0-9_+-]*)\n(.*?)```")
	placeholderRe = regexp.MustCompile(`^\x00CODE(\d+)\x00$`)
	headingRe     = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	bulletRe      = regexp.MustCompile(`^\s*[-*+]\s+(.*)$`)
	numberedRe    = regexp.MustCompile(`^\s*\d+[.)]\s+(.*)$`)
	boldRe        = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	italicRe      = regexp.MustCompile(`(^|[^*\w])\*([^*\n]+)\*`)
	inlineCodeRe  = regexp.MustCompile("`([^`\n]+)`")
)

// renderBlock turns one paragraph into a heading, a list, or a paragraph.
func renderBlock(para string) string {
	lines := strings.Split(para, "\n")

	// A list is a block whose first line starts one. Mixed content falls through
	// to a paragraph, which is the safe reading.
	if bulletRe.MatchString(lines[0]) || numberedRe.MatchString(lines[0]) {
		ordered := numberedRe.MatchString(lines[0])
		tag := "ul"
		if ordered {
			tag = "ol"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "<%s class=\"md__list\">", tag)
		for _, line := range lines {
			re := bulletRe
			if ordered {
				re = numberedRe
			}
			if m := re.FindStringSubmatch(line); m != nil {
				b.WriteString("<li>" + inline(m[1]) + "</li>")
			} else if strings.TrimSpace(line) != "" {
				b.WriteString("<li>" + inline(strings.TrimSpace(line)) + "</li>")
			}
		}
		fmt.Fprintf(&b, "</%s>", tag)
		return b.String()
	}

	if m := headingRe.FindStringSubmatch(lines[0]); m != nil && len(lines) == 1 {
		// Headings are rendered as h3 and below: an answer sits inside a page
		// that already has an h1 and an h2, and a model's "#" is not a page title.
		level := len(m[1]) + 2
		if level > 6 {
			level = 6
		}
		return fmt.Sprintf("<h%d>%s</h%d>", level, inline(m[2]), level)
	}

	return "<p>" + inline(strings.Join(lines, "<br>")) + "</p>"
}

// inline applies the character-level marks. Code first, so `**` inside backticks
// is left alone.
func inline(s string) string {
	s = inlineCodeRe.ReplaceAllString(s, "<code>$1</code>")
	s = boldRe.ReplaceAllString(s, "<strong>$1</strong>")
	s = italicRe.ReplaceAllString(s, "$1<em>$2</em>")
	return s
}
