//go:build !(js || wasip1)

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
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine/internal/gvtmpdir"
)

// go-graphviz has initialized by now, with its cache in the per-user directory.
func init() {
	gvtmpdir.Restore()
}

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
		return nil, eh.Errorf("instantiate graphviz: %w", err)
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
	edgeIDs, err := layeredgraph.EdgeIDs(m)
	if err != nil {
		return nil, err
	}
	dot, err := e.renderLaidOutDot(ctx, m, opts)
	if err != nil {
		return nil, err
	}
	lay, err := parseLayout(dot, m, edgeIDs, opts)
	if err != nil {
		return nil, err
	}
	// Report the size the boxes were sized to fit so a renderer paints node
	// labels at the same size — no layout/render font drift.
	lay.FontSize = effectiveFontSize(opts.FontSize)
	lay.EdgeFontSize = effectiveEdgeFontSize(opts.EdgeFontSize)
	return lay, nil
}

// nodeMarginX / nodeMarginY are the Graphviz node `margin` (inches) at the
// layout-wide font size. The horizontal component is bumped above Graphviz's
// 0.11 default so the sans-metric label keeps a clear, consistent gap from the
// box border once the painter draws it; the vertical component stays at the
// default.
const (
	nodeMarginX = 0.16
	nodeMarginY = 0.055
)

// nodeMarginFor scales the horizontal margin with a node's own font size.
//
// What the bump absorbs is the Helvetica-vs-Noto metric difference, and that
// error is proportional to the drawn text — so it grows with the font. A
// magnitude-scaled label (ADR-0167 §SD3) at 2.4× the layout font overruns a
// margin calibrated for the layout font, and the painter clips it at the box
// edge. Scaling relative to the LAYOUT-WIDE size rather than to some absolute
// keeps an unweighted node at exactly the old margin, so a graph with no
// weights is laid out byte-for-byte as before.
func nodeMarginFor(nodePt float64, layoutPt float64) string {
	scale := 1.0
	if layoutPt > 0 && nodePt > layoutPt {
		scale = nodePt / layoutPt
	}
	return fmt.Sprintf("%g,%g", nodeMarginX*scale, nodeMarginY)
}

// effectiveEdgeFontSize resolves the size edge labels are painted at.
func effectiveEdgeFontSize(f float64) float64 {
	if f > 0 {
		return f
	}
	return layeredgraph.DefaultEdgeFontPt
}

// edgeLabelHeadroom is how much larger than painted an edge label is
// measured. Graphviz has no margin for an edge label: two parallel edges'
// middle labels abut at the width it measures, so any shortfall overlaps.
// It measures in Helvetica, and the painter's sans (Noto Sans, measured
// 2026-10-24) runs 3–17 % wider on lowercase words — "first" 15 %, "title"
// 16 % — with t, f and r the widest letters against it.
const edgeLabelHeadroom = 1.2

// endLabelDistance is the Graphviz labeldistance of an edge with end labels:
// how far, in multiples of 10 pt, a head or tail label sits from its end.
const endLabelDistance = 1.6

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

// nodeFontSize resolves one node's layout font: the caller's weight mapping
// when it accepts this node, else the layout-wide size. A mapping that returns
// a non-positive size is treated as a decline rather than as an instruction to
// draw nothing.
func nodeFontSize(n layeredgraph.Node, opts layeredgraph.LayoutOpts) float64 {
	if opts.NodeFontSize != nil {
		if pt, ok := opts.NodeFontSize(n.Weight); ok && pt > 0 {
			return pt
		}
	}
	return effectiveFontSize(opts.FontSize)
}

// renderLaidOutDot constructs the Graphviz graph from the model and renders it
// to the canonical DOT format. graphviz.XDOT is the "dot" format: after
// layout it carries computed pos/width/height on nodes, pos (the spline) on
// edges, and bb on the graph — not the xdot draw-op stream.
func (e *Engine) renderLaidOutDot(ctx context.Context, m layeredgraph.GraphModel, opts layeredgraph.LayoutOpts) ([]byte, error) {
	graph, err := e.gv.Graph()
	if err != nil {
		return nil, eh.Errorf("new graph: %w", err)
	}
	defer func() { _ = graph.Close() }()

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
			return nil, eb.Build().Str("id", n.ID).Errorf("create node: %w", err)
		}
		label := n.Label
		if label == "" {
			label = n.ID
		}
		if n.IsRowNode() {
			// A row node is a record: Graphviz sizes every field from its
			// text and reports each field's rectangle back as "rects".
			rec, rerr := recordLabel(n, label, opts.RankDir)
			if rerr != nil {
				return nil, rerr
			}
			gn.SetLabel(rec)
			gn.SetShape(recordShape)
		} else {
			gn.SetLabel(label)
			gn.SetShape(shape(n.Shape))
		}
		// Size boxes with sans-serif metrics. The imzero2 painter draws labels
		// in the UI sans font (Noto), but Graphviz defaults to Times — a narrow
		// serif — so it under-measures the label and long ones overflow the box
		// in the painter (worse the longer the label, which also reads as
		// non-uniform). Helvetica is the closest built-in metric family to the
		// rendered font; the inner-margin bump (default is 0.11,0.055) then
		// leaves a consistent gap so text never touches the frame.
		gn.SetFontName("Helvetica")
		// A weighted node is laid out at a scaled font, and Graphviz fits the
		// box to the scaled label — so magnitude arrives through the existing
		// size-to-fit path rather than beside it, and a label can never
		// overflow its own box (ADR-0167 §SD3). An unweighted node, or a nil
		// mapping, keeps the layout-wide size.
		nodePt := nodeFontSize(n, opts)
		_ = gn.SafeSet("margin", nodeMarginFor(nodePt, effectiveFontSize(opts.FontSize)), "")
		gn.SetFontSize(nodePt)
		gnodes[n.ID] = gn
	}

	for i, ed := range m.Edges {
		tail, ok := gnodes[ed.From]
		if !ok {
			return nil, eb.Build().Int("edge", i).Str("from", ed.From).Errorf("edge names an unknown from-node")
		}
		head, ok := gnodes[ed.To]
		if !ok {
			return nil, eb.Build().Int("edge", i).Str("to", ed.To).Errorf("edge names an unknown to-node")
		}
		// The edge's model index is its Graphviz name: unique, so parallel
		// edges stay distinct, and written back as the edge's key, which is
		// how parseLayout finds the model edge again. Caller IDs never reach
		// DOT, so they may hold any string.
		ge, err := graph.CreateEdgeByName(strconv.Itoa(i), tail, head)
		if err != nil {
			return nil, eb.Build().Int("edge", i).Str("from", ed.From).Str("to", ed.To).Errorf("create edge: %w", err)
		}
		if ed.Label != "" {
			ge.SetLabel(ed.Label)
		}
		// Notation is emitted only when an edge departs from the default, so
		// a graph that uses none of it produces the same DOT as before.
		switch ed.Head {
		case layeredgraph.EdgeHeadNone:
			ge.SetDir(cgraph.NoneDir)
		case layeredgraph.EdgeHeadOpen:
			ge.SetArrowHead(cgraph.EmptyArrow)
		}
		if ed.Dashed {
			ge.SetStyle(cgraph.DashedEdgeStyle)
		}
		if ed.HeadLabel != "" {
			ge.SetHeadLabel(ed.HeadLabel)
		}
		if ed.TailLabel != "" {
			ge.SetTailLabel(ed.TailLabel)
		}
		if ed.Label != "" || ed.HeadLabel != "" || ed.TailLabel != "" {
			// Measured in the painter's metric family, as node labels are,
			// at the painted size plus headroom (LG-12: parallel edges'
			// labels overlapped when Graphviz measured them in Times).
			ge.SetFontName("Helvetica")
			ge.SetFontSize(effectiveEdgeFontSize(opts.EdgeFontSize) * edgeLabelHeadroom)
		}
		if ed.HeadLabel != "" || ed.TailLabel != "" {
			// Graphviz puts an end label endLabelDistance×10 pt from the
			// end; its default of 1 lands the label on a 10 pt arrowhead.
			ge.SetLabelDistance(endLabelDistance)
		}
	}

	var buf bytes.Buffer
	if err := e.gv.Render(ctx, graph, graphviz.XDOT, &buf); err != nil {
		return nil, eh.Errorf("render dot: %w", err)
	}
	return buf.Bytes(), nil
}

// parseLayout reparses the laid-out canonical DOT and assembles the result.
// goccy's parser is used (rather than a hand-rolled scanner) because Graphviz
// line-wraps long edge splines; the parser unwraps them. Node labels/shapes
// are taken from the input model (a node whose label equals its name may be
// emitted with the "\N" default rather than an explicit label); only geometry
// is read from Graphviz. The same holds for edges, whose ID, label and weight
// come from the model edge their Graphviz name indexes. edgeIDs are the
// resolved IDs of m's edges, in model order.
func parseLayout(dot []byte, m layeredgraph.GraphModel, edgeIDs []string, opts layeredgraph.LayoutOpts) (*layeredgraph.Layout, error) {
	g, err := graphviz.ParseBytes(dot)
	if err != nil {
		return nil, eh.Errorf("reparse laid-out dot: %w", err)
	}
	defer func() { _ = g.Close() }()

	// The first node of a repeated ID is the one renderLaidOutDot emitted, so
	// it is the one whose label, shape and rows describe the laid-out node.
	meta := make(map[string]layeredgraph.Node, len(m.Nodes))
	for _, n := range m.Nodes {
		if _, dup := meta[n.ID]; !dup {
			meta[n.ID] = n
		}
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
			// The scaled font is reported only when it differs from the
			// layout-wide one, so an unweighted graph produces the zero value
			// throughout and a renderer's fallback path is the common one.
			// Recomputed rather than read back from the DOT: the mapping is
			// pure, so this is the same number Graphviz was given.
			var fontPt float64
			if pt := nodeFontSize(md, opts); pt != effectiveFontSize(opts.FontSize) {
				fontPt = pt
			}
			nl := layeredgraph.NodeLayout{
				FontSize: fontPt,
				ID:       name,
				Label:    label,
				Shape:    md.Shape,
				Center:   flip(cx, cy),
				W:        inchesToPoints(n.GetStr("width")),
				H:        inchesToPoints(n.GetStr("height")),
			}
			if md.IsRowNode() {
				// One rectangle per field, in label order: the title, then
				// each row.
				rects, rok := parseRects(n.GetStr("rects"), flip)
				if !rok || len(rects) != 1+len(md.Rows) {
					return nil, eb.Build().Str("node", name).Int("rects", len(rects)).Int("rows", len(md.Rows)).
						Errorf("a row node's laid-out fields do not match its rows")
				}
				nl.Subtitle = md.Subtitle
				nl.Title = rects[0]
				nl.Rows = make([]layeredgraph.RowLayout, len(md.Rows))
				for i, r := range md.Rows {
					nl.Rows[i] = layeredgraph.RowLayout{ID: r.ID, Text: r.Text, Rect: rects[1+i]}
				}
			}
			out.Nodes = append(out.Nodes, nl)
		}
		n, err = g.NextNode(n)
	}
	if err != nil {
		return nil, eh.Errorf("iterate nodes: %w", err)
	}

	// Edges: walk each node's out-edges, so every edge — a self-loop included
	// — is visited from its tail exactly once. The Graphviz name is the model
	// index renderLaidOutDot gave it; the model edge supplies everything but
	// geometry. Endpoints are taken from the model too: they are the caller's
	// IDs, which the node names already are.
	laid := make([]layeredgraph.EdgeLayout, len(m.Edges))
	placed := make([]bool, len(m.Edges))
	n, err = g.FirstNode()
	for err == nil && n != nil {
		ed, eerr := g.FirstOut(n)
		for eerr == nil && ed != nil {
			name, nerr := ed.Name()
			if nerr != nil {
				return nil, eh.Errorf("read edge name: %w", nerr)
			}
			idx, aerr := strconv.Atoi(name)
			if aerr != nil || idx < 0 || idx >= len(m.Edges) {
				return nil, eb.Build().Str("name", name).Errorf("laid-out edge does not name a model edge")
			}
			if placed[idx] {
				return nil, eb.Build().Int("edge", idx).Errorf("laid-out edge reported twice")
			}
			placed[idx] = true
			me := m.Edges[idx]
			el := layeredgraph.EdgeLayout{ID: edgeIDs[idx], Index: idx,
				From: me.From, To: me.To, Label: me.Label, Weight: me.Weight,
				Head: me.Head, Dashed: me.Dashed,
				HeadLabel: me.HeadLabel, TailLabel: me.TailLabel}
			el.Points, el.ArrowHead = parseSpline(ed.GetStr("pos"), flip)
			if me.Head == layeredgraph.EdgeHeadNone {
				// Graphviz emits no head point for dir=none; this keeps the
				// documented "no ArrowHead" true whatever the engine reports.
				el.ArrowHead = nil
			}
			if lx, ly, ok := parsePoint(ed.GetStr("lp")); ok {
				p := flip(lx, ly)
				el.LabelPos = &p
			}
			// End-label anchors are read only for labels the model gave, so a
			// position can never stand for text that does not exist.
			if me.HeadLabel != "" {
				if lx, ly, ok := parsePoint(ed.GetStr("head_lp")); ok {
					p := flip(lx, ly)
					el.HeadLabelPos = &p
				}
			}
			if me.TailLabel != "" {
				if lx, ly, ok := parsePoint(ed.GetStr("tail_lp")); ok {
					p := flip(lx, ly)
					el.TailLabelPos = &p
				}
			}
			laid[idx] = el
			ed, eerr = g.NextOut(ed)
		}
		if eerr != nil {
			return nil, eh.Errorf("iterate edges: %w", eerr)
		}
		n, err = g.NextNode(n)
	}
	if err != nil {
		return nil, eh.Errorf("iterate nodes for edges: %w", err)
	}
	for i, ok := range placed {
		if !ok {
			return nil, eb.Build().Int("edge", i).Str("id", edgeIDs[i]).Errorf("model edge missing from the laid-out graph")
		}
	}
	out.Edges = laid

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
	case layeredgraph.NodeShapeDiamond:
		return cgraph.DiamondShape
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
