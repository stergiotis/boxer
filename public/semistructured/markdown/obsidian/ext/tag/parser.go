package tag

import (
	"unicode"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

type tagParser struct{}

func NewParser() parser.InlineParser {
	return &tagParser{}
}

func (inst *tagParser) Trigger() []byte {
	return []byte{'#'}
}

func (inst *tagParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) (node ast.Node) {
	line, segment := block.PeekLine()
	if len(line) < 2 || line[0] != '#' {
		return
	}

	// A tag opens at a word boundary. goldmark fires an inline trigger at
	// EVERY `#` in the text, so `word#word` reaches here too — a URL fragment
	// outside a link destination, a CSS selector, `C#sharp`. Obsidian reads
	// those as part of the word they are attached to, and so do we.
	//
	// PrecendingCharacter reports '\n' at the start of the block, which is a
	// boundary; it is rune-aware, so a letter in any script counts as a word
	// character rather than only ASCII.
	//
	// Two punctuation marks are rejected as well. A preceding `#` keeps
	// `##word` in flowing prose plain instead of splitting it into a stray `#`
	// and a tag. A preceding `{` belongs to the attribute syntax `{#anchor}`
	// that [obsidian.FeatureHeadingAnchor] owns — when the anchor does not
	// terminate its line the braces stay literal text, and a tag parser
	// eating the inside of them would delete what the reader wrote.
	if prev := block.PrecendingCharacter(); prev == '#' || prev == '{' || isWordRune(prev) {
		return
	}

	// First char after # must be a letter, digit, or underscore (not space, punctuation, or #)
	if !isTagChar(line[1]) || line[1] == '#' {
		return
	}

	// Scan the tag body: letters, digits, underscores, hyphens, slashes (for nested tags)
	i := 1
	for i < len(line) && isTagBodyChar(line[i]) {
		i++
	}

	// Tag must not end with a slash
	if line[i-1] == '/' {
		i--
	}
	if i <= 1 {
		return
	}

	// Obsidian requires at least one non-numeric character: `#4` is the
	// English "number four", not a tag. Without this rule technical prose
	// turns into tags wherever it counts things — issue references, numbered
	// open questions, ordinals — which is the single largest source of false
	// tags in the documents this parser is pointed at.
	if isAllDigits(line[1:i]) {
		return
	}

	n := &Node{
		Tag: line[1:i],
	}
	_ = segment // segment used for offset tracking
	block.Advance(i)
	return n
}

// isWordRune reports whether r would make a preceding `#` part of a word
// rather than the start of a tag.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// isAllDigits reports whether every byte of the tag body is 0-9. A tag body is
// ASCII by construction ([isTagBodyChar]), so a byte scan is exact here.
func isAllDigits(body []byte) bool {
	for _, c := range body {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(body) > 0
}

func isTagChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_'
}

func isTagBodyChar(c byte) bool {
	return isTagChar(c) || c == '-' || c == '/'
}
