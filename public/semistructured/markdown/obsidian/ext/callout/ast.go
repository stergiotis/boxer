package callout

import (
	"fmt"

	"github.com/yuin/goldmark/ast"
)

// Node represents an Obsidian callout block (> [!type] Title).
type Node struct {
	ast.BaseBlock
	CalloutType []byte
	Title       []byte
	Foldable    bool
	DefaultOpen bool
}

var Kind = ast.NewNodeKind("ObsidianCallout")

func (inst *Node) Kind() ast.NodeKind { return Kind }

func (inst *Node) Dump(source []byte, level int) {
	m := map[string]string{
		"CalloutType": string(inst.CalloutType),
	}
	if len(inst.Title) > 0 {
		m["Title"] = string(inst.Title)
	}
	if inst.Foldable {
		m["Foldable"] = "true"
	}
	if inst.DefaultOpen {
		m["DefaultOpen"] = "true"
	}
	ast.DumpHelper(inst, source, level, m, func(level int) {
		for c := inst.FirstChild(); c != nil; c = c.NextSibling() {
			fmt.Println()
			c.Dump(source, level+1)
		}
	})
}
