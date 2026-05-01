//go:build llm_generated_opus47

// Package dot provides Graphviz DOT format export for graggle graphs.
package dot

import (
	"bytes"
	"fmt"
	"strings"

	t "github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/types"
)

// Dot returns a Graphviz DOT representation of the graph.
// Live nodes are shown as boxes, deleted nodes as dashed grey boxes.
// Edge styles: live=solid black, deleted=dashed grey, pseudo=dotted blue.
func Dot(g t.Visualizable) string {
	var buf bytes.Buffer
	buf.WriteString("digraph graggle {\n")
	buf.WriteString("  rankdir=TB;\n")
	buf.WriteString("  node [fontname=\"monospace\", fontsize=10];\n")
	buf.WriteString("  edge [fontname=\"monospace\", fontsize=8];\n\n")

	// Emit live nodes.
	for id := range g.AllLiveNodes() {
		if id == t.RootNodeID {
			fmt.Fprintf(&buf, "  %s [label=%q, shape=diamond, style=filled, fillcolor=lightgrey];\n",
				dotID(id), "root")
		} else {
			fmt.Fprintf(&buf, "  %s [label=%q, shape=box, style=filled, fillcolor=lightyellow];\n",
				dotID(id), dotNodeLabel(id, g.NodeContent(id)))
		}
	}

	// Emit deleted nodes.
	for id := range g.AllDeletedNodes() {
		fmt.Fprintf(&buf, "  %s [label=%q, shape=box, style=\"dashed,filled\", fillcolor=lightgrey, fontcolor=grey40];\n",
			dotID(id), dotNodeLabel(id, g.NodeContent(id)))
	}

	buf.WriteString("\n")

	// Emit edges.
	for src := range g.ForwardEdgeSources() {
		for e := range g.ForwardEdges(src) {
			style, color := dotEdgeStyle(e.Kind)
			fmt.Fprintf(&buf, "  %s -> %s [style=%s, color=%s];\n",
				dotID(src), dotID(e.Dest), style, color)
		}
	}

	buf.WriteString("}\n")
	return buf.String()
}

// dotID produces a valid DOT node identifier from a NodeID.
func dotID(id t.NodeID) string {
	if id == t.RootNodeID {
		return "root"
	}
	return fmt.Sprintf("n_%s_%d", id.Patch.String(), id.Index)
}

// dotNodeLabel produces a readable label: short hash + content preview.
func dotNodeLabel(id t.NodeID, content []byte) string {
	s := strings.TrimRight(string(content), "\n")
	if len(s) > 40 {
		s = s[:37] + "..."
	}
	return fmt.Sprintf("%s/%d\\n%s", id.Patch.String()[:8], id.Index, s)
}

// dotEdgeStyle returns DOT style and color for an edge kind.
func dotEdgeStyle(kind t.EdgeKind) (style, color string) {
	switch kind {
	case t.EdgeLive:
		return "solid", "black"
	case t.EdgeDeleted:
		return "dashed", "grey"
	case t.EdgePseudo:
		return "dotted", "blue"
	default:
		return "solid", "red"
	}
}