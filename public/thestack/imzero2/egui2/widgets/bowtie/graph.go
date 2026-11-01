package bowtie

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// ElementKindE is what a node of the diagram stands for.
type ElementKindE uint8

const (
	// ElementNone is no element: nothing hovered, nothing clicked.
	ElementNone ElementKindE = iota
	ElementTopEvent
	// ElementPath is a threat or a consequence, by Side.
	ElementPath
	ElementBarrier
)

// Element addresses one node of the diagram in the Model it was drawn from.
// Path indexes Model.Paths(Side) and Barrier that path's Barriers; either is
// -1 where it does not apply.
type Element struct {
	Kind    ElementKindE
	Side    SideE
	Path    int
	Barrier int
}

// noElement is the zero Element with the -1 sentinels set.
var noElement = Element{Path: -1, Barrier: -1}

// wrapWidth is the line length, in characters, a path or top-event label is
// wrapped at. A barrier's name is not wrapped: a row node holds one line.
const wrapWidth = 24

// statusRowID is the row of a barrier node that carries its status.
const statusRowID = "status"

// diagramGraph is the model as a layered graph: one node per element, pinned
// to the layer the bow-tie puts it on, and the element each node stands for.
type diagramGraph struct {
	model     layeredgraph.GraphModel
	elementOf map[string]Element
	key       string
}

// buildGraph lays the bow-tie out as layers, left to right:
//
//	threats | prevention barriers | top event | mitigation barriers | consequences
//
// with as many barrier layers per side as its longest path has barriers. A
// shorter path's barriers keep to the layers next to the top event, so the
// barrier nearest the top event is in the same column on every path and a
// path's gap opens at its threat or consequence — the bow-tie convention, and
// the mirror image on the two sides.
func buildGraph(m *Model) (g diagramGraph) {
	maxBarriers := func(paths []Path) (n int) {
		for _, p := range paths {
			n = max(n, len(p.Barriers))
		}
		return
	}
	nPrev, nMit := maxBarriers(m.Threats), maxBarriers(m.Consequences)
	topRank := nPrev + 2

	g.elementOf = make(map[string]Element, 1+len(m.Threats)+len(m.Consequences))
	var nodes []layeredgraph.Node
	var edges []layeredgraph.Edge
	topID := "top"
	top := m.TopEvent
	if top == "" {
		top = "top event"
	}
	// The top event carries the one weight in the graph, which LayoutOpts
	// turns into a larger font and so a larger box: the knot of the bow-tie
	// is what the diagram is about.
	nodes = append(nodes, layeredgraph.Node{ID: topID, Label: wrap(top, wrapWidth), Rank: topRank, Weight: 1})
	g.elementOf[topID] = Element{Kind: ElementTopEvent, Path: -1, Barrier: -1}

	for side, paths := range [2][]Path{m.Threats, m.Consequences} {
		s := SideE(side)
		prefix := "t"
		if s == SideMitigation {
			prefix = "c"
		}
		for pi, p := range paths {
			pathID := prefix + strconv.Itoa(pi)
			n := len(p.Barriers)
			pathRank := 1
			if s == SideMitigation {
				pathRank = topRank + nMit + 1
			}
			nodes = append(nodes, layeredgraph.Node{ID: pathID, Label: wrap(p.Name, wrapWidth), Rank: pathRank})
			g.elementOf[pathID] = Element{Kind: ElementPath, Side: s, Path: pi, Barrier: -1}

			// The chain in drawing order, left to right.
			chain := make([]string, 0, n+2)
			if s == SidePrevention {
				chain = append(chain, pathID)
			} else {
				chain = append(chain, topID)
			}
			for bi, b := range p.Barriers {
				id := pathID + "b" + strconv.Itoa(bi)
				rank := 1 + (nPrev - n) + (bi + 1) // threat-side: flush against the top event
				if s == SideMitigation {
					rank = topRank + bi + 1 // consequence-side: from the top event outward
				}
				nodes = append(nodes, layeredgraph.Node{
					ID: id, Label: b.Name, Rank: rank,
					Rows: []layeredgraph.Row{{ID: statusRowID, Text: b.Status.Icon() + " " + b.Status.Label()}},
				})
				g.elementOf[id] = Element{Kind: ElementBarrier, Side: s, Path: pi, Barrier: bi}
				chain = append(chain, id)
			}
			if s == SidePrevention {
				chain = append(chain, topID)
			} else {
				chain = append(chain, pathID)
			}
			for i := 0; i+1 < len(chain); i++ {
				edges = append(edges, layeredgraph.Edge{From: chain[i], To: chain[i+1], Head: layeredgraph.EdgeHeadNone})
			}
		}
	}
	g.model = layeredgraph.GraphModel{Nodes: nodes, Edges: edges}
	g.key = graphKey(g.model)
	return
}

// graphKey fingerprints everything the layout depends on: ids, labels, rows
// and ranks, and the edges. A status change alters a barrier's status row, so
// it re-lays-out — the row's text, and therefore the box, changes with it.
func graphKey(m layeredgraph.GraphModel) string {
	h := fnv.New64a()
	for _, n := range m.Nodes {
		_, _ = fmt.Fprintf(h, "n|%s|%s|%d", n.ID, n.Label, n.Rank)
		for _, r := range n.Rows {
			_, _ = fmt.Fprintf(h, "|%s=%s", r.ID, r.Text)
		}
		_, _ = h.Write([]byte{';'})
	}
	for _, e := range m.Edges {
		_, _ = fmt.Fprintf(h, "e|%s|%s;", e.From, e.To)
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

// wrap breaks text into lines of at most width characters at spaces, joined
// by "\n", which both the engine and the view read as a line break. The lines
// are balanced: the text takes as many lines as a greedy fill at width needs,
// and then the narrowest width that still fits it in that many, so a label
// does not end on an orphaned word. A word longer than width keeps its own
// line rather than being split.
func wrap(text string, width int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	lines := greedyLines(words, width)
	total := 0
	for _, w := range words {
		total += len([]rune(w))
	}
	total += len(words) - 1
	for w := (total + len(lines) - 1) / len(lines); w < width; w++ {
		if l := greedyLines(words, w); len(l) <= len(lines) {
			lines = l
			break
		}
	}
	return strings.Join(lines, "\n")
}

// greedyLines fills lines of at most width characters, word by word.
func greedyLines(words []string, width int) (lines []string) {
	var b strings.Builder
	line := 0
	for i, w := range words {
		n := len([]rune(w))
		if i > 0 && line+1+n > width {
			lines = append(lines, b.String())
			b.Reset()
			line = 0
		} else if i > 0 {
			b.WriteByte(' ')
			line++
		}
		b.WriteString(w)
		line += n
	}
	return append(lines, b.String())
}
