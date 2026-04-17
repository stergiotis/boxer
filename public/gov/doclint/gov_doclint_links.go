package doclint

import (
	"bytes"
	"regexp"
	"strings"
)

// inlineLinkRe matches Markdown inline links of the form [text](url) or
// [text](url "title"). Reference-style links and image embeds are not
// distinguished separately; image links happen to match too because the
// '!' prefix is not part of the captured groups.
//
// Limitations: text containing literal ']' or unescaped '\n' will not
// match; URLs containing whitespace or ')' will not match. Acceptable
// for the project's documentation conventions.
var inlineLinkRe = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

// inlineLink describes a Markdown link extracted from a doc body.
//
// Line is 1-based and counts lines within the slice the extractor was
// called on (typically the body, front-matter excluded). Callers that
// need file-level lines should add the consumed front-matter line count.
type inlineLink struct {
	Text string
	URL  string
	Line int32
}

// extractInlineLinks finds inline Markdown links in the supplied bytes,
// skipping anything inside fenced code blocks (``` or ~~~). Inline code
// spans (`code`) are NOT yet excluded — false positives there are rare
// and the cost of a proper Markdown parser would dwarf the benefit.
func extractInlineLinks(body []byte) (links []inlineLink) {
	inFence := false
	for i, raw := range bytes.Split(body, []byte("\n")) {
		trimmed := bytes.TrimLeft(raw, " \t")
		if bytes.HasPrefix(trimmed, []byte("```")) || bytes.HasPrefix(trimmed, []byte("~~~")) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		matches := inlineLinkRe.FindAllSubmatchIndex(raw, -1)
		for _, m := range matches {
			text := string(raw[m[2]:m[3]])
			url := string(raw[m[4]:m[5]])
			links = append(links, inlineLink{
				Text: text,
				URL:  url,
				Line: int32(i + 1),
			})
		}
	}
	return
}

// frontMatterLineOffset returns the number of lines the front-matter
// (including the blank line that separates it from the body) consumed
// in data, given the body slice that ExtractFrontMatter produced.
//
// When the file has no front-matter (body == data byte-for-byte), the
// offset is 0.
func frontMatterLineOffset(data []byte, body []byte) (offset int32) {
	offset = int32(bytes.Count(data, []byte("\n")) - bytes.Count(body, []byte("\n")))
	if offset < 0 {
		offset = 0
	}
	return
}

// isExternalUrl returns true if u looks like an external URI and should
// be skipped by in-repo link rules.
func isExternalUrl(u string) (ext bool) {
	schemes := []string{"http://", "https://", "mailto:", "ftp://", "ftps://", "ssh://", "tel:"}
	for _, s := range schemes {
		if strings.HasPrefix(u, s) {
			ext = true
			return
		}
	}
	return
}

// stripBackticks removes a single layer of surrounding backticks from a
// Markdown link text — common for code-span styling like
// [`pkg/name`](...). Whitespace is trimmed first.
func stripBackticks(s string) (out string) {
	out = strings.TrimSpace(s)
	out = strings.TrimPrefix(out, "`")
	out = strings.TrimSuffix(out, "`")
	return
}

// stripUrlFragment returns the URL with any '#anchor' or '?query' suffix
// removed, plus a flag indicating whether the input was a pure-anchor
// link (starts with '#').
func stripUrlFragment(u string) (clean string, anchorOnly bool) {
	if strings.HasPrefix(u, "#") {
		anchorOnly = true
		return
	}
	clean = u
	if i := strings.Index(clean, "#"); i >= 0 {
		clean = clean[:i]
	}
	if i := strings.Index(clean, "?"); i >= 0 {
		clean = clean[:i]
	}
	return
}
