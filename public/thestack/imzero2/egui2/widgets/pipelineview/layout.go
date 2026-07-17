package pipelineview

import (
	"fmt"
	"math"
)

// Layout geometry is in the imzero2 painter's space: points, top-left
// origin, y increasing downward. Compute is deterministic by construction
// (ADR-0119 SD2): model order is the universal tiebreak — tree order for
// stages, declaration order for ports and endpoints, model order for edges —
// and no map iteration or randomness reaches the output.

// LayoutOpts tunes Compute. The zero value is valid.
type LayoutOpts struct {
	// FontSize is the stage-label font size in points; 0 means 14. Endpoint
	// labels use 0.85× of it.
	FontSize float64
	// MeasureText returns the rendered extent of text at fontSize. Nil uses a
	// monospace-flavoured estimate (0.60×size per rune, 1.45×size line
	// height) — adequate for box sizing; a renderer with real measurements
	// (MeasureTextBind) can inject them for tighter boxes.
	MeasureText func(text string, fontSize float64) (w, h float64)
}

// Spacing constants (points). Deliberately not knobs until a consumer needs
// them — the closed set keeps layouts uniform across the gallery.
const (
	defaultFontSize   = 14.0
	endpointFontScale = 0.85
	stagePadX         = 14.0
	stagePadY         = 9.0
	endPadX           = 10.0
	endPadY           = 6.0
	minStageW         = 64.0
	minEndW           = 52.0
	colGap            = 56.0 // horizontal gap between adjacent columns
	parGap            = 26.0 // vertical gap between parallel branches
	sideGap           = 22.0 // stage edge → north/south endpoint gap
	axialGap          = 40.0 // stage edge → east/west endpoint gap
	laneSep           = 24.0 // separation between stacked skip/feedback lanes
	trackSep          = 10.0 // separation between vertical segments in one gap
	epStackGap        = 10.0 // separation between endpoints homed to one pin
	storeCapPad       = 20.0 // extra height for Store cylinders: the ellipse caps eat ~2×ry of visual height, so the body still fits the label block
	sublabelScale     = 0.85 // sublabel font relative to the endpoint font
	margin            = 14.0
)

// Point is a 2-D coordinate in the output space.
type Point struct{ X, Y float64 }

// NodeKind distinguishes the two drawable node families.
type NodeKind uint8

const (
	// NodeStage is a spine stage box.
	NodeStage NodeKind = iota
	// NodeEndpoint is a terminal artifact glyph.
	NodeEndpoint
)

// NodeLayout is a placed node. Center is the node centre; W and H the full
// bounding-box size.
type NodeLayout struct {
	ID       string
	Label    string
	Sublabel string // endpoint detail line; drawn smaller under Label
	Kind     NodeKind
	EKind    EndpointKind // meaningful when Kind == NodeEndpoint
	Center   Point
	W, H     float64
}

// PortPin is a placed named port: a point on its stage's border.
type PortPin struct {
	Stage string
	Port  string
	Class PortClass
	Pos   Point
}

// EdgeKind tags how an edge was routed, for styling.
type EdgeKind uint8

const (
	// EdgeSpine is primary flow along the spine (implied adjacency, explicit
	// adjacent edges, and axial endpoint sources/sinks).
	EdgeSpine EdgeKind = iota
	// EdgeSide connects a named side port with its endpoint.
	EdgeSide
	// EdgeSkip is an explicit forward primary edge bypassing at least one
	// column; routed through a lane above the diagram.
	EdgeSkip
	// EdgeFeedback is an explicit primary edge whose target is not later
	// than its source; routed through a lane below the diagram, dashed.
	EdgeFeedback
)

// EdgeLayout is a routed edge: an orthogonal polyline (Points, ≥2) the
// renderer may round at interior corners. The final segment's direction is
// the arrow-head direction.
type EdgeLayout struct {
	From, To Ref
	Label    string
	Kind     EdgeKind
	Class    PortClass // Primary for spine/skip/feedback; the port's class for side edges
	Dashed   bool
	Points   []Point
	LabelPos *Point // anchor for Label (longest-segment midpoint); nil when unlabeled
	Volume   float64
}

// Layout is the positioned result. Every coordinate falls within
// [0,Width] × [0,Height].
type Layout struct {
	Nodes  []NodeLayout
	Pins   []PortPin
	Edges  []EdgeLayout
	Width  float64
	Height float64
	// FontSize is the stage-label size the boxes were measured for; a
	// renderer should paint labels at this size (pre-fit-scale) so text and
	// boxes agree. Endpoint labels use endpointFontScale× of it.
	FontSize float64
}

func estimateText(text string, fontSize float64) (w, h float64) {
	n := 0
	for range text {
		n++
	}
	return 0.60 * fontSize * float64(n), 1.45 * fontSize
}

type homeSide uint8

const (
	homeSouth homeSide = iota
	homeNorth
	homeEast
	homeWest
)

type stageInfo struct {
	st               Stage
	col              int
	w                float64
	centerX, centerY float64
	southPorts       []Port // diagnostics (decl order) then artifacts (decl order)
	northPorts       []Port // configs (decl order)
	maxNorthEndH     float64
	maxSouthEndH     float64
	above, below     float64 // vertical extent around the spine line
}

type epInfo struct {
	ep               Endpoint
	w, h             float64
	homed            bool
	side             homeSide
	stage            string // home stage
	port             string // home pin name ("" for east/west axial homes)
	centerX, centerY float64
}

// Compute validates p and lays it out. See the package doc for the space and
// the determinism contract.
func Compute(p Pipeline, opts LayoutOpts) (*Layout, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	fontSize := opts.FontSize
	if fontSize <= 0 {
		fontSize = defaultFontSize
	}
	measure := opts.MeasureText
	if measure == nil {
		measure = estimateText
	}

	// ---- stages: tree order, sizes, columns -------------------------------
	var order []string
	byID := make(map[string]*stageInfo, 16)
	stageH := 0.0
	walkStages(p.Root, func(st Stage) {
		si := &stageInfo{st: st}
		lw, lh := measure(labelOr(st.Label, st.ID), fontSize)
		si.w = math.Max(minStageW, lw+2*stagePadX)
		if lh+2*stagePadY > stageH {
			stageH = lh + 2*stagePadY
		}
		for _, po := range st.Ports {
			switch po.Class {
			case PortDiagnostic:
				si.southPorts = append(si.southPorts, po)
			case PortConfig:
				si.northPorts = append(si.northPorts, po)
			}
		}
		for _, po := range st.Ports { // artifacts east of diagnostics
			if po.Class == PortArtifact {
				si.southPorts = append(si.southPorts, po)
			}
		}
		byID[st.ID] = si
		order = append(order, st.ID)
	})

	var colSpan func(el Element) int
	colSpan = func(el Element) int {
		switch v := el.(type) {
		case Stage:
			return 1
		case Group:
			if v.Par {
				m := 0
				for _, ch := range v.Children {
					if s := colSpan(ch); s > m {
						m = s
					}
				}
				return m
			}
			s := 0
			for _, ch := range v.Children {
				s += colSpan(ch)
			}
			return s
		}
		return 0
	}
	var assignCols func(el Element, c0 int)
	assignCols = func(el Element, c0 int) {
		switch v := el.(type) {
		case Stage:
			byID[v.ID].col = c0
		case Group:
			if v.Par {
				for _, ch := range v.Children {
					assignCols(ch, c0)
				}
				return
			}
			cc := c0
			for _, ch := range v.Children {
				assignCols(ch, cc)
				cc += colSpan(ch)
			}
		}
	}
	nCols := colSpan(p.Root)
	assignCols(p.Root, 0)

	// ---- endpoints: sizes and homes ---------------------------------------
	var epOrder []string
	eps := make(map[string]*epInfo, len(p.Endpoints))
	for _, ep := range p.Endpoints {
		lw, lh := measure(labelOr(ep.Label, ep.ID), fontSize*endpointFontScale)
		if ep.Sublabel != "" {
			slw, slh := measure(ep.Sublabel, fontSize*endpointFontScale*sublabelScale)
			lw = math.Max(lw, slw)
			lh += slh
		}
		h := lh + 2*endPadY
		if ep.Kind == EndpointStore {
			h += storeCapPad
		}
		eps[ep.ID] = &epInfo{ep: ep, w: math.Max(minEndW, lw+2*endPadX), h: h}
		epOrder = append(epOrder, ep.ID)
	}
	portClassOf := func(st Stage, name string) PortClass {
		for _, po := range st.Ports {
			if po.Name == name {
				return po.Class
			}
		}
		return PortPrimary
	}
	// The first edge referencing an endpoint homes it (model order — SD2's
	// universal tiebreak); later edges route to wherever it landed.
	for _, e := range p.Edges {
		switch {
		case e.To.IsEndpoint() && !e.From.IsEndpoint():
			ei := eps[e.To.Endpoint]
			if ei.homed {
				continue
			}
			ei.homed = true
			ei.stage = e.From.Stage
			if e.From.Port == "" {
				ei.side = homeEast
			} else {
				ei.side = homeSouth
				ei.port = e.From.Port
			}
		case e.From.IsEndpoint() && !e.To.IsEndpoint():
			ei := eps[e.From.Endpoint]
			if ei.homed {
				continue
			}
			ei.homed = true
			ei.stage = e.To.Stage
			if e.To.Port == "" {
				ei.side = homeWest
			} else {
				ei.side = homeNorth
				ei.port = e.To.Port
			}
		}
	}
	for _, id := range epOrder {
		ei := eps[id]
		if !ei.homed {
			return nil, fmt.Errorf("pipelineview: endpoint %q is not referenced by any edge", id)
		}
		si := byID[ei.stage]
		switch ei.side {
		case homeSouth:
			if ei.h > si.maxSouthEndH {
				si.maxSouthEndH = ei.h
			}
		case homeNorth:
			if ei.h > si.maxNorthEndH {
				si.maxNorthEndH = ei.h
			}
		}
	}

	// ---- vertical extents and spine placement -----------------------------
	for _, id := range order {
		si := byID[id]
		si.above = stageH / 2
		if si.maxNorthEndH > 0 {
			si.above += sideGap + si.maxNorthEndH
		}
		si.below = stageH / 2
		if si.maxSouthEndH > 0 {
			si.below += sideGap + si.maxSouthEndH
		}
	}
	var extents func(el Element) (above, below float64)
	extents = func(el Element) (above, below float64) {
		switch v := el.(type) {
		case Stage:
			si := byID[v.ID]
			return si.above, si.below
		case Group:
			if !v.Par {
				for _, ch := range v.Children {
					ca, cb := extents(ch)
					above = math.Max(above, ca)
					below = math.Max(below, cb)
				}
				return
			}
			total := 0.0
			for i, ch := range v.Children {
				ca, cb := extents(ch)
				total += ca + cb
				if i > 0 {
					total += parGap
				}
			}
			return total / 2, total / 2
		}
		return
	}
	var placeY func(el Element, line float64)
	placeY = func(el Element, line float64) {
		switch v := el.(type) {
		case Stage:
			byID[v.ID].centerY = line
		case Group:
			if !v.Par {
				for _, ch := range v.Children {
					placeY(ch, line)
				}
				return
			}
			above, _ := extents(v)
			cursor := line - above
			for i, ch := range v.Children {
				if i > 0 {
					cursor += parGap
				}
				ca, cb := extents(ch)
				placeY(ch, cursor+ca)
				cursor += ca + cb
			}
		}
	}
	placeY(p.Root, 0)

	// ---- shelves ----------------------------------------------------------
	// North/south endpoints of one stage form a non-overlapping row centred
	// on the stage (a shelf). A shelf is part of its column's width, so gap
	// tracks (which run at spine heights) never cross a shelf box.
	southRow := make(map[string][]string, 4)
	northRow := make(map[string][]string, 4)
	for _, id := range epOrder {
		ei := eps[id]
		switch ei.side {
		case homeSouth:
			southRow[ei.stage] = append(southRow[ei.stage], id)
		case homeNorth:
			northRow[ei.stage] = append(northRow[ei.stage], id)
		}
	}
	rowWidth := func(ids []string) (total float64) {
		for i, id := range ids {
			total += eps[id].w
			if i > 0 {
				total += epStackGap
			}
		}
		return
	}
	shelfW := make(map[string]float64, 4)
	for _, sid := range order {
		shelfW[sid] = math.Max(rowWidth(southRow[sid]), rowWidth(northRow[sid]))
	}

	// ---- columns → x ------------------------------------------------------
	colW := make([]float64, nCols)
	for _, id := range order {
		si := byID[id]
		if w := math.Max(si.w, shelfW[id]); w > colW[si.col] {
			colW[si.col] = w
		}
	}
	colX := make([]float64, nCols)
	xcur := 0.0
	for ci := range colW {
		colX[ci] = xcur
		xcur += colW[ci] + colGap
	}
	for _, id := range order {
		si := byID[id]
		si.centerX = colX[si.col] + colW[si.col]/2
	}
	// gapX(g) is the centre of the gap right of column g; g = -1 is the
	// virtual gap left of the first column, g = nCols-1 right of the last.
	gapX := func(g int) float64 {
		if g < 0 {
			return colX[0] - colGap/2
		}
		return colX[g] + colW[g] + colGap/2
	}

	// ---- pins -------------------------------------------------------------
	var pins []PortPin
	pinPos := make(map[string]Point, 8)
	pinKey := func(stage, port string) string { return stage + "\x00" + port }
	for _, id := range order {
		si := byID[id]
		left := si.centerX - si.w/2
		top := si.centerY - stageH/2
		bottom := si.centerY + stageH/2
		for i, po := range si.southPorts {
			px := left + si.w*float64(i+1)/float64(len(si.southPorts)+1)
			pt := Point{px, bottom}
			pins = append(pins, PortPin{Stage: id, Port: po.Name, Class: po.Class, Pos: pt})
			pinPos[pinKey(id, po.Name)] = pt
		}
		for i, po := range si.northPorts {
			px := left + si.w*float64(i+1)/float64(len(si.northPorts)+1)
			pt := Point{px, top}
			pins = append(pins, PortPin{Stage: id, Port: po.Name, Class: po.Class, Pos: pt})
			pinPos[pinKey(id, po.Name)] = pt
		}
	}

	// ---- endpoint placement -----------------------------------------------
	// Shelves centre on their stage; edges elbow from their pin when pin and
	// endpoint drift apart (sideRoute).
	placeRow := func(si *stageInfo, ids []string, north bool) {
		x := si.centerX - rowWidth(ids)/2
		for _, id := range ids {
			ei := eps[id]
			ei.centerX = x + ei.w/2
			if north {
				ei.centerY = si.centerY - stageH/2 - sideGap - ei.h/2
			} else {
				ei.centerY = si.centerY + stageH/2 + sideGap + ei.h/2
			}
			x += ei.w + epStackGap
		}
	}
	stacked := make(map[string]int, 4) // east/west axial stacking per stage
	for _, sid := range order {
		si := byID[sid]
		placeRow(si, southRow[sid], false)
		placeRow(si, northRow[sid], true)
	}
	for _, id := range epOrder {
		ei := eps[id]
		si := byID[ei.stage]
		switch ei.side {
		case homeEast:
			k := pinKey(ei.stage, "\x01east")
			ei.centerX = colX[si.col] + colW[si.col] + axialGap + ei.w/2
			ei.centerY = si.centerY + float64(stacked[k])*(ei.h+epStackGap)
			stacked[k]++
		case homeWest:
			k := pinKey(ei.stage, "\x01west")
			ei.centerX = colX[si.col] - axialGap - ei.w/2
			ei.centerY = si.centerY + float64(stacked[k])*(ei.h+epStackGap)
			stacked[k]++
		}
	}

	// ---- content bounds (boxes + endpoints), for the lanes ----------------
	contentMinY, contentMaxY := math.Inf(1), math.Inf(-1)
	for _, id := range order {
		si := byID[id]
		contentMinY = math.Min(contentMinY, si.centerY-stageH/2)
		contentMaxY = math.Max(contentMaxY, si.centerY+stageH/2)
	}
	for _, id := range epOrder {
		ei := eps[id]
		contentMinY = math.Min(contentMinY, ei.centerY-ei.h/2)
		contentMaxY = math.Max(contentMaxY, ei.centerY+ei.h/2)
	}

	// ---- edges ------------------------------------------------------------
	trackCount := make(map[int]int, 8)
	nextTrack := func(g int) float64 {
		idx := trackCount[g]
		trackCount[g]++
		// 0, +1, -1, +2, -2, … around the gap centre.
		var k float64
		if idx > 0 {
			if idx%2 == 1 {
				k = float64((idx + 1) / 2)
			} else {
				k = -float64(idx / 2)
			}
		}
		return k * trackSep
	}
	routeAxial := func(x0, y0, x1, y1 float64, gapIdx int) []Point {
		if math.Abs(y0-y1) < 0.5 {
			return []Point{{x0, y0}, {x1, y1}}
		}
		gx := gapX(gapIdx) + nextTrack(gapIdx)
		return []Point{{x0, y0}, {gx, y0}, {gx, y1}, {x1, y1}}
	}
	labelAt := func(pts []Point) *Point {
		best := 0.0
		var bp Point
		for i := 0; i+1 < len(pts); i++ {
			d := math.Hypot(pts[i+1].X-pts[i].X, pts[i+1].Y-pts[i].Y)
			if d > best {
				best = d
				bp = Point{(pts[i].X + pts[i+1].X) / 2, (pts[i].Y + pts[i+1].Y) / 2}
			}
		}
		if best <= 0 {
			return nil
		}
		return &bp
	}
	finish := func(e EdgeLayout) EdgeLayout {
		if e.Label != "" {
			e.LabelPos = labelAt(e.Points)
		}
		return e
	}

	var edges []EdgeLayout
	// Explicit forward-adjacent primary edges replace their implied spine
	// twin, so a consumer can label a pipe without drawing it twice.
	explicitAdjacent := make(map[[2]string]bool, 4)
	for _, e := range p.Edges {
		if !e.From.IsEndpoint() && !e.To.IsEndpoint() && e.From.Port == "" && e.To.Port == "" {
			a, b := byID[e.From.Stage], byID[e.To.Stage]
			if b.col == a.col+1 {
				explicitAdjacent[[2]string{e.From.Stage, e.To.Stage}] = true
			}
		}
	}
	for _, pr := range impliedSpineEdges(p.Root) {
		if explicitAdjacent[pr] {
			continue
		}
		a, b := byID[pr[0]], byID[pr[1]]
		edges = append(edges, EdgeLayout{
			From: Ref{Stage: pr[0]}, To: Ref{Stage: pr[1]},
			Kind: EdgeSpine, Class: PortPrimary,
			Points: routeAxial(a.centerX+a.w/2, a.centerY, b.centerX-b.w/2, b.centerY, b.col-1),
		})
	}

	skipLanes, feedbackLanes := 0, 0
	for _, e := range p.Edges {
		switch {
		case !e.From.IsEndpoint() && !e.To.IsEndpoint():
			// primary stage → stage (named ports were rejected by Validate)
			a, b := byID[e.From.Stage], byID[e.To.Stage]
			x0, y0 := a.centerX+a.w/2, a.centerY
			x1, y1 := b.centerX-b.w/2, b.centerY
			if b.col > a.col {
				if b.col == a.col+1 {
					edges = append(edges, finish(EdgeLayout{From: e.From, To: e.To, Label: e.Label,
						Kind: EdgeSpine, Class: PortPrimary, Volume: e.Volume,
						Points: routeAxial(x0, y0, x1, y1, b.col-1)}))
					continue
				}
				laneY := contentMinY - laneSep*float64(skipLanes+1)
				skipLanes++
				g1 := gapX(a.col) + nextTrack(a.col)
				g2 := gapX(b.col-1) + nextTrack(b.col-1)
				edges = append(edges, finish(EdgeLayout{From: e.From, To: e.To, Label: e.Label,
					Kind: EdgeSkip, Class: PortPrimary, Volume: e.Volume,
					Points: []Point{{x0, y0}, {g1, y0}, {g1, laneY}, {g2, laneY}, {g2, y1}, {x1, y1}}}))
				continue
			}
			laneY := contentMaxY + laneSep*float64(feedbackLanes+1)
			feedbackLanes++
			g1 := gapX(a.col) + nextTrack(a.col)
			g2 := gapX(b.col-1) + nextTrack(b.col-1)
			edges = append(edges, finish(EdgeLayout{From: e.From, To: e.To, Label: e.Label,
				Kind: EdgeFeedback, Class: PortPrimary, Dashed: true, Volume: e.Volume,
				Points: []Point{{x0, y0}, {g1, y0}, {g1, laneY}, {g2, laneY}, {g2, y1}, {x1, y1}}}))

		case e.To.IsEndpoint():
			ei := eps[e.To.Endpoint]
			if e.From.Port == "" { // axial sink: stage east → endpoint west
				a := byID[e.From.Stage]
				edges = append(edges, finish(EdgeLayout{From: e.From, To: e.To, Label: e.Label,
					Kind: EdgeSpine, Class: PortPrimary, Volume: e.Volume,
					Points: []Point{{a.centerX + a.w/2, a.centerY}, {ei.centerX - ei.w/2, ei.centerY}}}))
				continue
			}
			pt := pinPos[pinKey(e.From.Stage, e.From.Port)]
			cl := portClassOf(byID[e.From.Stage].st, e.From.Port)
			edges = append(edges, finish(EdgeLayout{From: e.From, To: e.To, Label: e.Label,
				Kind: EdgeSide, Class: cl, Volume: e.Volume,
				Points: sideRoute(pt, ei, false)}))

		default: // e.From.IsEndpoint()
			ei := eps[e.From.Endpoint]
			if e.To.Port == "" { // axial source: endpoint east → stage west
				b := byID[e.To.Stage]
				edges = append(edges, finish(EdgeLayout{From: e.From, To: e.To, Label: e.Label,
					Kind: EdgeSpine, Class: PortPrimary, Volume: e.Volume,
					Points: []Point{{ei.centerX + ei.w/2, ei.centerY}, {b.centerX - b.w/2, b.centerY}}}))
				continue
			}
			pt := pinPos[pinKey(e.To.Stage, e.To.Port)]
			edges = append(edges, finish(EdgeLayout{From: e.From, To: e.To, Label: e.Label,
				Kind: EdgeSide, Class: PortConfig, Volume: e.Volume,
				Points: reversed(sideRoute(pt, ei, true))}))
		}
	}

	// ---- assemble + normalise to a non-negative origin --------------------
	lay := &Layout{Pins: pins, Edges: edges, FontSize: fontSize}
	for _, id := range order {
		si := byID[id]
		lay.Nodes = append(lay.Nodes, NodeLayout{
			ID: id, Label: labelOr(si.st.Label, id), Kind: NodeStage,
			Center: Point{si.centerX, si.centerY}, W: si.w, H: stageH,
		})
	}
	for _, id := range epOrder {
		ei := eps[id]
		lay.Nodes = append(lay.Nodes, NodeLayout{
			ID: id, Label: labelOr(ei.ep.Label, id), Sublabel: ei.ep.Sublabel,
			Kind: NodeEndpoint, EKind: ei.ep.Kind,
			Center: Point{ei.centerX, ei.centerY}, W: ei.w, H: ei.h,
		})
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	visit := func(x, y float64) {
		minX = math.Min(minX, x)
		minY = math.Min(minY, y)
		maxX = math.Max(maxX, x)
		maxY = math.Max(maxY, y)
	}
	for _, n := range lay.Nodes {
		visit(n.Center.X-n.W/2, n.Center.Y-n.H/2)
		visit(n.Center.X+n.W/2, n.Center.Y+n.H/2)
	}
	for _, e := range lay.Edges {
		for _, pt := range e.Points {
			visit(pt.X, pt.Y)
		}
	}
	dx, dy := margin-minX, margin-minY
	for i := range lay.Nodes {
		lay.Nodes[i].Center.X += dx
		lay.Nodes[i].Center.Y += dy
	}
	for i := range lay.Pins {
		lay.Pins[i].Pos.X += dx
		lay.Pins[i].Pos.Y += dy
	}
	for i := range lay.Edges {
		for j := range lay.Edges[i].Points {
			lay.Edges[i].Points[j].X += dx
			lay.Edges[i].Points[j].Y += dy
		}
		if lp := lay.Edges[i].LabelPos; lp != nil {
			lp.X += dx
			lp.Y += dy
		}
	}
	lay.Width = maxX - minX + 2*margin
	lay.Height = maxY - minY + 2*margin
	return lay, nil
}

// sideRoute routes from a stage-border pin to an endpoint on the stage's
// south shelf (north == false, the pin sits above the endpoint) or north
// shelf (north == true, the pin sits below it). When the pin's x falls
// within the endpoint's horizontal span the edge drops straight into the
// near edge; otherwise it elbows at the endpoint's centre line and enters
// the side facing the pin.
func sideRoute(pin Point, ei *epInfo, north bool) []Point {
	left, right := ei.centerX-ei.w/2, ei.centerX+ei.w/2
	nearY := ei.centerY + ei.h/2
	if !north {
		nearY = ei.centerY - ei.h/2
	}
	if pin.X > left+2 && pin.X < right-2 {
		return []Point{pin, {pin.X, nearY}}
	}
	if pin.X >= right-2 {
		return []Point{pin, {pin.X, ei.centerY}, {right, ei.centerY}}
	}
	return []Point{pin, {pin.X, ei.centerY}, {left, ei.centerY}}
}

func reversed(pts []Point) []Point {
	for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
		pts[i], pts[j] = pts[j], pts[i]
	}
	return pts
}
