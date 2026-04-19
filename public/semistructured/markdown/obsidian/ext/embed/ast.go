//go:build llm_generated_opus46

package embed

import (
	"fmt"

	"github.com/yuin/goldmark/ast"
)

// Node represents an Obsidian embed (![[target]]).
type Node struct {
	ast.BaseInline
	Target  []byte
	Heading []byte
}

var Kind = ast.NewNodeKind("ObsidianEmbed")

func (inst *Node) Kind() ast.NodeKind { return Kind }

func (inst *Node) Dump(source []byte, level int) {
	m := map[string]string{
		"Target": string(inst.Target),
	}
	if len(inst.Heading) > 0 {
		m["Heading"] = string(inst.Heading)
	}
	ast.DumpHelper(inst, source, level, m, func(level int) {
		for c := inst.FirstChild(); c != nil; c = c.NextSibling() {
			fmt.Println()
			c.Dump(source, level+1)
		}
	})
}
