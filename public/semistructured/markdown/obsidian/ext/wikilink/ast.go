package wikilink

import (
	"fmt"

	"github.com/yuin/goldmark/ast"
)

// Node represents a wikilink ([[page]], [[page|alias]], [[page#heading|alias]]).
type Node struct {
	ast.BaseInline
	Page    []byte
	Heading []byte
	Alias   []byte
}

var Kind = ast.NewNodeKind("ObsidianWikilink")

func (inst *Node) Kind() ast.NodeKind { return Kind }

func (inst *Node) Dump(source []byte, level int) {
	m := map[string]string{
		"Page": string(inst.Page),
	}
	if len(inst.Heading) > 0 {
		m["Heading"] = string(inst.Heading)
	}
	if len(inst.Alias) > 0 {
		m["Alias"] = string(inst.Alias)
	}
	ast.DumpHelper(inst, source, level, m, func(level int) {
		for c := inst.FirstChild(); c != nil; c = c.NextSibling() {
			fmt.Println()
			c.Dump(source, level+1)
		}
	})
}

// DisplayText returns the text to display: alias if present, otherwise page
// (with heading suffix).
//
// A same-page link (`[[#Section]]`) has an empty Page, and the "page >
// heading" join then rendered as a stray leading "> " with nothing on its
// left. Obsidian shows such a link as the heading alone, which is what the
// empty-page branch below produces.
func (inst *Node) DisplayText() string {
	if len(inst.Alias) > 0 {
		return string(inst.Alias)
	}
	s := string(inst.Page)
	if len(inst.Heading) > 0 {
		if s == "" {
			return string(inst.Heading)
		}
		s += " > " + string(inst.Heading)
	}
	return s
}
