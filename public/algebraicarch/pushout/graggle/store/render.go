//go:build llm_generated_opus47

package store

import (
	"bytes"
	"sort"

	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/algo"
	t "github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/types"
)

// Render produces the text output of the graggle's live subgraph.
// If the graph is linearly ordered, returns clean file content.
// If there are conflicts, returns content with conflict markers.
//
// Render mutates the graggle: it calls ResolvePseudoEdges() so the live
// subgraph is consistent before traversal. Callers that need a read-only
// view should snapshot via Clone() first.
func (g *Graggle) Render() []byte {
	g.ResolvePseudoEdges()

	// Try the simple case first.
	order := algo.LinearOrder(g)
	if order != nil {
		return g.renderLinear(order)
	}

	// Conflict case: DFS-based rendering with conflict markers.
	return g.renderWithConflicts()
}

// renderLinear renders a linearly ordered graggle to plain text.
func (g *Graggle) renderLinear(order []t.NodeID) []byte {
	var buf bytes.Buffer
	for _, id := range order {
		if id == t.RootNodeID {
			continue
		}
		buf.Write(g.contents[id])
	}
	return buf.Bytes()
}

// renderWithConflicts does a DFS traversal of the live subgraph,
// emitting conflict markers where the graph forks.
//
// The traversal is iterative: a recursive DFS would blow the goroutine
// stack on long files. We use a work stack with two op kinds — visit (do
// the SCC's own emission and queue its children) and emit (write a literal
// string). To preserve the ">>>>>>> ... ======= ... <<<<<<<" bracketing
// around recursive children, we push the trailing marker first, then the
// children in reverse with separators interleaved, then the leading marker
// — LIFO pop order then yields the desired output sequence.
func (g *Graggle) renderWithConflicts() []byte {
	var buf bytes.Buffer
	visited := make(map[t.NodeID]bool)

	// Build the condensed DAG from Tarjan SCCs.
	sccs := algo.Tarjan(g)
	sccID := make(map[t.NodeID]int)
	for i, scc := range sccs {
		for _, v := range scc {
			sccID[v] = i
		}
	}

	type opKind uint8
	const (
		opVisit opKind = iota
		opEmit
	)
	type op struct {
		kind opKind
		scc  int
		s    string
	}

	work := []op{{kind: opVisit, scc: sccID[t.RootNodeID]}}
	for len(work) > 0 {
		top := work[len(work)-1]
		work = work[:len(work)-1]

		if top.kind == opEmit {
			buf.WriteString(top.s)
			continue
		}

		scc := top.scc
		if visited[sccs[scc][0]] {
			continue
		}
		for _, v := range sccs[scc] {
			visited[v] = true
		}

		if len(sccs[scc]) > 1 {
			buf.WriteString(">>>>>>> cycle conflict\n")
			for _, v := range sccs[scc] {
				if v == t.RootNodeID {
					continue
				}
				buf.Write(g.contents[v])
			}
			buf.WriteString("<<<<<<< cycle conflict\n")
		} else {
			v := sccs[scc][0]
			if v != t.RootNodeID {
				buf.Write(g.contents[v])
			}
		}

		// Find successor SCCs.
		childSCCs := make(map[int]struct{})
		for _, v := range sccs[scc] {
			for w := range g.LiveChildren(v) {
				if !g.IsLive(w) {
					continue
				}
				ws := sccID[w]
				if ws != scc && !visited[sccs[ws][0]] {
					childSCCs[ws] = struct{}{}
				}
			}
		}
		if len(childSCCs) == 0 {
			continue
		}
		children := make([]int, 0, len(childSCCs))
		for c := range childSCCs {
			children = append(children, c)
		}
		sort.Ints(children)

		if len(children) == 1 {
			work = append(work, op{kind: opVisit, scc: children[0]})
			continue
		}

		// Multi-child order conflict. Push trailing marker, then children in
		// reverse with "=======" separators, then leading marker so LIFO pop
		// produces: leading marker, child0, separator, child1, ..., trailing.
		work = append(work, op{kind: opEmit, s: "<<<<<<< order conflict\n"})
		for i := len(children) - 1; i >= 0; i-- {
			if i > 0 {
				work = append(work, op{kind: opVisit, scc: children[i]})
				work = append(work, op{kind: opEmit, s: "=======\n"})
			} else {
				work = append(work, op{kind: opVisit, scc: children[i]})
			}
		}
		work = append(work, op{kind: opEmit, s: ">>>>>>> order conflict\n"})
	}

	return buf.Bytes()
}

// RenderLines returns the rendered output split into lines (each line keeps
// its trailing newline; the final line keeps whatever it ended with).
func (g *Graggle) RenderLines() []string {
	content := g.Render()
	if len(content) == 0 {
		return nil
	}
	parts := bytes.SplitAfter(content, []byte{'\n'})
	// SplitAfter leaves a trailing empty element if content ends with '\n'.
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	lines := make([]string, len(parts))
	for i, p := range parts {
		lines[i] = string(p)
	}
	return lines
}