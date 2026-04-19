//go:build llm_generated_opus46

package comment

import "github.com/yuin/goldmark/ast"

// Node represents an Obsidian comment (%%text%%) that is stripped from output.
type Node struct {
	ast.BaseInline
}

var Kind = ast.NewNodeKind("ObsidianComment")

func (inst *Node) Kind() ast.NodeKind { return Kind }

func (inst *Node) Dump(source []byte, level int) {
	ast.DumpHelper(inst, source, level, nil, nil)
}
