//go:build llm_generated_opus47

// Package dot provides Graphviz DOT format export for graggle graphs.
package dot

import (
	"bytes"
	"fmt"
	"strings"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// Dot returns a Graphviz DOT representation of the graph.
// Live nodes are shown as boxes, deleted nodes as dashed grey boxes.
// Edge styles: live=solid black, deleted=dashed grey, pseudo=dotted blue.
func Dot(g t.VisualizableI) string {
	var buf bytes.Buffer
	buf.WriteString("digraph graggle {\n")
	buf.WriteString("  rankdir=TB;\n")
	buf.WriteString("  node [fontname=\"monospace\", fontsize=10];\n")
	buf.WriteString("  edge [fontname=\"monospace\", fontsize=8];\n\n")

	// Emit live nodes. Labels are emitted as "%s" with our own escaping:
	// %q would escape the label's DOT \n line-break sequence into a
	// literal backslash-n in the rendered output.
	for id := range g.AllLiveNodes() {
		if id == t.RootNodeID {
			fmt.Fprintf(&buf, "  %s [label=\"root\", shape=diamond, style=filled, fillcolor=lightgrey];\n",
				dotID(id))
		} else {
			fmt.Fprintf(&buf, "  %s [label=\"%s\", shape=box, style=filled, fillcolor=lightyellow];\n",
				dotID(id), dotNodeLabel(id, g.NodeContent(id)))
		}
	}

	// Emit deleted nodes.
	for id := range g.AllDeletedNodes() {
		fmt.Fprintf(&buf, "  %s [label=\"%s\", shape=box, style=\"dashed,filled\", fillcolor=lightgrey, fontcolor=grey40];\n",
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

// dotNodeLabel produces a readable label: short hash + content preview,
// separated by a DOT \n line break. The content is escaped for use
// inside a double-quoted DOT string (backslashes, quotes) and embedded
// literal newlines become DOT line breaks.
func dotNodeLabel(id t.NodeID, content []byte) string {
	s := strings.TrimRight(string(content), "\n")
	if len(s) > 40 {
		s = s[:37] + "..."
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return fmt.Sprintf(`%s/%d\n%s`, id.Patch.String()[:8], id.Index, s)
}

// dotEdgeStyle returns DOT style and color for an edge kind.
func dotEdgeStyle(kind t.EdgeKindE) (style, color string) {
	switch kind {
	case t.EdgeKindLive:
		return "solid", "black"
	case t.EdgeKindDeleted:
		return "dashed", "grey"
	case t.EdgeKindPseudo:
		return "dotted", "blue"
	default:
		return "solid", "red"
	}
}
