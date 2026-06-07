// Package goccyengine implements layeredgraph.Engine with Graphviz `dot`, run
// in-process as WebAssembly via goccy/go-graphviz (wazero, cgo-free). It is
// the only package that imports the Graphviz dependency: the ADR-0069 seam
// confines the dependency here so the engine can be swapped (a pure-Go
// Sugiyama, a clean-room) without touching the widget, FFI or painter.
//
// Graphviz reports coordinates with a lower-left origin in points; results are
// converted to the layeredgraph output space (top-left origin, y-down, points)
// before returning.
package goccyengine

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// Engine lays out graphs with Graphviz. It owns one embedded-Graphviz (wazero)
// instance; instantiating that runtime is the costly part, so reuse an Engine
// across layouts rather than constructing one per call. Not safe for
// concurrent use.
type Engine struct {
	gv *graphviz.Graphviz
}

// compile-time assertion that Engine satisfies the seam.
var _ layeredgraph.Engine = (*Engine)(nil)

// New instantiates the embedded Graphviz runtime and pins the layout engine to
// `dot` (the layered/Sugiyama engine). ctx bounds the lifetime of the wasm
// instance. Call Close when finished.
func New(ctx context.Context) (*Engine, error) {
	gv, err := graphviz.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("goccyengine: instantiate graphviz: %w", err)
	}
	gv.SetLayout(graphviz.DOT)
	return &Engine{gv: gv}, nil
}

// Close releases the embedded Graphviz runtime.
func (e *Engine) Close() error {
	return e.gv.Close()
}

var (
	sharedMu  sync.Mutex
	sharedEng *Engine
)

// Shared returns a lazily-created, process-wide Engine built with
// context.Background(). The successful instance is created once and reused; a
// failed instantiation is NOT cached, so a transient failure is retried on the
// next call. Intended for the single-threaded imzero2 render loop — Engine is
// not safe for concurrent use. The instance lives for the process and is never
// Closed; this avoids spinning up a WebAssembly runtime per widget instance.
func Shared() (*Engine, error) {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedEng != nil {
		return sharedEng, nil
	}
	eng, err := New(context.Background())
	if err != nil {
		return nil, err // don't cache the failure — retry on the next call
	}
	sharedEng = eng
	return sharedEng, nil
}

// Layout builds the model as a Graphviz graph, runs `dot`, and parses the
// laid-out canonical DOT back into engine-neutral geometry. We capture
// coordinates from the rendered DOT rather than off the live graph because
// Render frees the layout as it returns.
func (e *Engine) Layout(ctx context.Context, m layeredgraph.GraphModel, opts layeredgraph.LayoutOpts) (*layeredgraph.Layout, error) {
	dot, err := e.renderLaidOutDot(ctx, m, opts)
	if err != nil {
		return nil, err
	}
	lay, err := parseLayout(dot, m)
	if err != nil {
		return nil, err
	}
	// Report the size the boxes were sized to fit so a renderer paints node
	// labels at the same size — no layout/render font drift.
	lay.FontSize = effectiveFontSize(opts.FontSize)
	return lay, nil
}

// nodeMargin is the Graphviz node `margin` (inches, "x,y") applied to every
// node. The horizontal component is bumped above Graphviz's 0.11 default so the
// sans-metric label keeps a clear, consistent gap from the box border once the
// painter draws it; the vertical component stays at the default.
const nodeMargin = "0.16,0.055"

// defaultFontSize mirrors Graphviz's own default node font size (points). Used
// when LayoutOpts.FontSize is unset, so Layout.FontSize reports exactly the size
// Graphviz laid the boxes out with.
const defaultFontSize = 14.0

// effectiveFontSize resolves the node font size used for layout: the caller's
// value when positive, else the Graphviz default.
func effectiveFontSize(f float64) float64 {
	if f > 0 {
		return f
	}
	return defaultFontSize
}

// renderLaidOutDot constructs the Graphviz graph from the model and renders it
// to the canonical DOT format. graphviz.XDOT is the "dot" format: after
// layout it carries computed pos/width/height on nodes, pos (the spline) on
// edges, and bb on the graph — not the xdot draw-op stream.
func (e *Engine) renderLaidOutDot(ctx context.Context, m layeredgraph.GraphModel, opts layeredgraph.LayoutOpts) ([]byte, error) {
	graph, err := e.gv.Graph()
	if err != nil {
		return nil, fmt.Errorf("goccyengine: new graph: %w", err)
	}
	defer graph.Close()

	graph.SetRankDir(rankDir(opts.RankDir))
	if opts.RankSep > 0 {
		graph.SetRankSeparator(opts.RankSep)
	}
	if opts.NodeSep > 0 {
		graph.SetNodeSeparator(opts.NodeSep)
	}

	gnodes := make(map[string]*cgraph.Node, len(m.Nodes))
	for _, n := range m.Nodes {
		if _, dup := gnodes[n.ID]; dup {
			// Same id = same node (e.g. fsmview states that share a label):
			// merge rather than fail, matching Graphviz's idempotent
			// CreateNodeByName. The first occurrence's label/shape wins.
			continue
		}
		gn, err := graph.CreateNodeByName(n.ID)
		if err != nil {
			return nil, fmt.Errorf("goccyengine: create node %q: %w", n.ID, err)
		}
		label := n.Label
		if label == "" {
			label = n.ID
		}
		gn.SetLabel(label)
		gn.SetShape(shape(n.Shape))
		// Size boxes with sans-serif metrics. The imzero2 painter draws labels
		// in the UI sans font (Noto), but Graphviz defaults to Times — a narrow
		// serif — so it under-measures the label and long ones overflow the box
		// in the painter (worse the longer the label, which also reads as
		// non-uniform). Helvetica is the closest built-in metric family to the
		// rendered font; the inner-margin bump (default is 0.11,0.055) then
		// leaves a consistent gap so text never touches the frame.
		gn.SetFontName("Helvetica")
		_ = gn.SafeSet("margin", nodeMargin, "")
		gn.SetFontSize(effectiveFontSize(opts.FontSize))
		gnodes[n.ID] = gn
	}

	for i, ed := range m.Edges {
		tail, ok := gnodes[ed.From]
		if !ok {
			return nil, fmt.Errorf("goccyengine: edge %d: unknown from-node %q", i, ed.From)
		}
		head, ok := gnodes[ed.To]
		if !ok {
			return nil, fmt.Errorf("goccyengine: edge %d: unknown to-node %q", i, ed.To)
		}
		// Unique per-edge name keeps parallel edges distinct in the graph.
		ge, err := graph.CreateEdgeByName(strconv.Itoa(i), tail, head)
		if err != nil {
			return nil, fmt.Errorf("goccyengine: create edge %d (%s->%s): %w", i, ed.From, ed.To, err)
		}
		if ed.Label != "" {
			ge.SetLabel(ed.Label)
		}
	}

	var buf bytes.Buffer
	if err := e.gv.Render(ctx, graph, graphviz.XDOT, &buf); err != nil {
		return nil, fmt.Errorf("goccyengine: render dot: %w", err)
	}
	return buf.Bytes(), nil
}

// parseLayout reparses the laid-out canonical DOT and assembles the result.
// goccy's parser is used (rather than a hand-rolled scanner) because Graphviz
// line-wraps long edge splines; the parser unwraps them. Node labels/shapes
// are taken from the input model (a node whose label equals its name may be
// emitted with the "\N" default rather than an explicit label); only geometry
// is read from Graphviz.
func parseLayout(dot []byte, m layeredgraph.GraphModel) (*layeredgraph.Layout, error) {
	g, err := graphviz.ParseBytes(dot)
	if err != nil {
		return nil, fmt.Errorf("goccyengine: reparse laid-out dot: %w", err)
	}
	defer g.Close()

	meta := make(map[string]layeredgraph.Node, len(m.Nodes))
	for _, n := range m.Nodes {
		meta[n.ID] = n
	}

	llx, lly, urx, ury := parseBB(g.GetStr("bb"))
	out := &layeredgraph.Layout{Width: urx - llx, Height: ury - lly}
	// Graphviz: lower-left origin, y-up. Convert to top-left origin, y-down.
	flip := func(x, y float64) layeredgraph.Point {
		return layeredgraph.Point{X: x - llx, Y: ury - y}
	}

	// Nodes: geometry from Graphviz, label/shape from the model.
	n, err := g.FirstNode()
	for err == nil && n != nil {
		name, _ := n.Name()
		if cx, cy, ok := parsePoint(n.GetStr("pos")); ok {
			md := meta[name]
			label := md.Label
			if label == "" {
				label = name
			}
			out.Nodes = append(out.Nodes, layeredgraph.NodeLayout{
				ID:     name,
				Label:  label,
				Shape:  md.Shape,
				Center: flip(cx, cy),
				W:      inchesToPoints(n.GetStr("width")),
				H:      inchesToPoints(n.GetStr("height")),
			})
		}
		n, err = g.NextNode(n)
	}
	if err != nil {
		return nil, fmt.Errorf("goccyengine: iterate nodes: %w", err)
	}

	// Edges: iterate each node's incident edges and process an edge once, from
	// its tail side. The seen set also collapses a self-loop that Graphviz
	// reports as both an out- and in-edge of the same node, and matches the v1
	// single-edge-per-pair contract.
	seen := make(map[[2]string]bool)
	n, err = g.FirstNode()
	for err == nil && n != nil {
		nName, _ := n.Name()
		ed, eerr := g.FirstEdge(n)
		for eerr == nil && ed != nil {
			tail, terr := ed.Tail()
			head, herr := ed.Head()
			if terr == nil && herr == nil && tail != nil && head != nil {
				tName, _ := tail.Name()
				hName, _ := head.Name()
				if tName == nName {
					key := [2]string{tName, hName}
					if !seen[key] {
						seen[key] = true
						el := layeredgraph.EdgeLayout{From: tName, To: hName, Label: ed.GetStr("label")}
						el.Points, el.ArrowHead = parseSpline(ed.GetStr("pos"), flip)
						if lx, ly, ok := parsePoint(ed.GetStr("lp")); ok {
							p := flip(lx, ly)
							el.LabelPos = &p
						}
						out.Edges = append(out.Edges, el)
					}
				}
			}
			ed, eerr = g.NextEdge(ed, n)
		}
		if eerr != nil {
			return nil, fmt.Errorf("goccyengine: iterate edges: %w", eerr)
		}
		n, err = g.NextNode(n)
	}
	if err != nil {
		return nil, fmt.Errorf("goccyengine: iterate nodes for edges: %w", err)
	}

	return out, nil
}

func rankDir(r layeredgraph.RankDir) cgraph.RankDir {
	switch r {
	case layeredgraph.RankDirLeftRight:
		return cgraph.LRRank
	case layeredgraph.RankDirBottomTop:
		return cgraph.BTRank
	case layeredgraph.RankDirRightLeft:
		return cgraph.RLRank
	default:
		return cgraph.TBRank
	}
}

func shape(s layeredgraph.NodeShape) cgraph.Shape {
	switch s {
	case layeredgraph.NodeShapeCircle:
		return cgraph.CircleShape
	case layeredgraph.NodeShapeEllipse:
		return cgraph.EllipseShape
	default:
		return cgraph.BoxShape
	}
}

// inchesToPoints converts a Graphviz size attribute (inches) to points.
func inchesToPoints(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v * 72.0
}

// parsePoint parses an "x,y" Graphviz coordinate.
func parsePoint(s string) (x, y float64, ok bool) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	x, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	y, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return x, y, true
}

// parseBB parses a Graphviz bounding box "llx,lly,urx,ury" (points).
func parseBB(s string) (llx, lly, urx, ury float64) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 4 {
		return 0, 0, 0, 0
	}
	llx, _ = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lly, _ = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	urx, _ = strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	ury, _ = strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
	return llx, lly, urx, ury
}

// parseSpline parses a Graphviz edge `pos` spline. Format: optional "s,x,y"
// and/or "e,x,y" prefix tokens (the points where the tail/head arrows attach),
// then whitespace-separated "x,y" B-spline control points. Returns the control
// points and the head arrow tip ("e," point) if present, all in output space.
func parseSpline(s string, flip func(x, y float64) layeredgraph.Point) ([]layeredgraph.Point, *layeredgraph.Point) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var pts []layeredgraph.Point
	var arrow *layeredgraph.Point
	for tok := range strings.FieldsSeq(s) {
		c := strings.Split(tok, ",")
		switch len(c) {
		case 3: // "e,x,y" (head) or "s,x,y" (tail) arrow attach point
			x, err1 := strconv.ParseFloat(c[1], 64)
			y, err2 := strconv.ParseFloat(c[2], 64)
			if err1 != nil || err2 != nil {
				continue
			}
			if c[0] == "e" {
				p := flip(x, y)
				arrow = &p
			}
			// "s," (start arrow) is not needed for v1 rendering.
		case 2: // "x,y" spline control point
			x, err1 := strconv.ParseFloat(c[0], 64)
			y, err2 := strconv.ParseFloat(c[1], 64)
			if err1 != nil || err2 != nil {
				continue
			}
			pts = append(pts, flip(x, y))
		}
	}
	return pts, arrow
}
