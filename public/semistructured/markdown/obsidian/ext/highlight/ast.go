//go:build llm_generated_opus46

package highlight

import (
	"fmt"

	"github.com/yuin/goldmark/ast"
)

// Node represents highlighted text (==text==) rendered as <mark>.
type Node struct {
	ast.BaseInline
}

var Kind = ast.NewNodeKind("ObsidianHighlight")

func (inst *Node) Kind() ast.NodeKind { return Kind }

func (inst *Node) Dump(source []byte, level int) {
	ast.DumpHelper(inst, source, level, nil, func(level int) {
		for c := inst.FirstChild(); c != nil; c = c.NextSibling() {
			fmt.Println()
			c.Dump(source, level+1)
		}
	})
}
