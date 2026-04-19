//go:build llm_generated_opus46

package tag

import "github.com/yuin/goldmark/ast"

// Node represents an Obsidian tag (#tag or #nested/tag).
type Node struct {
	ast.BaseInline
	Tag []byte
}

var Kind = ast.NewNodeKind("ObsidianTag")

func (inst *Node) Kind() ast.NodeKind { return Kind }

func (inst *Node) Dump(source []byte, level int) {
	ast.DumpHelper(inst, source, level, map[string]string{
		"Tag": string(inst.Tag),
	}, nil)
}
