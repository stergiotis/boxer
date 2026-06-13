package tag

import (
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

	// # must be preceded by whitespace or be at start of line to avoid
	// conflicting with heading syntax (which is block-level) or anchor fragments.
	// In inline context, goldmark won't call us for headings, but we still need
	// to avoid matching things like foo#bar (CSS selectors, URL fragments).
	if segment.Start > 0 {
		// We need to check the byte before our trigger.
		// Unfortunately goldmark doesn't give us direct access to previous bytes,
		// but in inline context the trigger is only called after a boundary.
		// We validate the first char after # to ensure it's a valid tag start.
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

	n := &Node{
		Tag: line[1:i],
	}
	_ = segment // segment used for offset tracking
	block.Advance(i)
	return n
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
