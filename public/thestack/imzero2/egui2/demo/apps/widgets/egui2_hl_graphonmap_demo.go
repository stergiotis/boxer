package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/landoverlay"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

// The graph-on-a-map demo of ADR-0228: one canvas, owned by the map, painted
// and picked by graphview. The map keeps its drag, wheel, box zoom and
// keyboard; the graph takes the pointer only where it lands on a node, and
// says so through the claim the map is told about before it handles input.
//
// It also shows the mixed case. A node with coordinates is declared Pinned at
// its projected position; a node *without* any — something the data joins to
// but does not locate — is declared unpinned, and the force step lays it out
// among the pinned ones, which is what ADR-0224 §SD10 gives for free: a
// declared pin is fixed and everything else still feels it.
//
// Because a layout runs, this demo takes the map's camera at a fixed
// reference zoom rather than an identity camera over layer points. World
// units are then projected pixels at that zoom — the same geometry whatever
// the map is showing — so the layout has one equilibrium instead of one that
// moves with the view, and an unlocated node needs no anchoring at all: its
// retained world position is already geographic.
//
// The world is measured from a local origin near the data rather than from
// the projection's corner, which is what CameraAt is for: Zürich is 19,713
// projected pixels from the antimeridian at this zoom and about 60 from the
// origin below. Small numbers keep float32 sub-pixel at every zoom, and they
// keep a newly declared node — seated beside a placed neighbour, or in a box
// at the world origin when it has none — somewhere near the picture.
//
// The price of the fixed world is that world-unit sizes scale with the view,
// so the node radius is divided by the camera's zoom to stay a constant size
// on screen (ADR-0228 §SD3a).
const (
	graphOnMapW = 720
	graphOnMapH = 460
	// graphOnMapRefZoom fixes the world the layout runs in. Near the initial
	// view, so the ideal edge length is comparable to the spread of the
	// pinned nodes at that zoom.
	graphOnMapRefZoom = 7.2
	// graphOnMapNodePx is the node radius as it should appear on screen.
	graphOnMapNodePx = 7
)

// graphOnMapCentre is where the world is measured from: near the data, so
// projected coordinates stay small (see the note above).
var graphOnMapCentre = portolan.LL(46.85, 8.0)

// graphOnMapSite is a node the data locates.
type graphOnMapSite struct {
	id       uint64
	label    string
	lat, lng float64
	group    string
}

var graphOnMapSites = []graphOnMapSite{
	{1, "Zürich", 47.3769, 8.5417, "core"},
	{2, "Bern", 46.9480, 7.4474, "core"},
	{3, "Genève", 46.2044, 6.1432, "west"},
	{4, "Basel", 47.5596, 7.5886, "core"},
	{5, "Lugano", 46.0037, 8.9511, "south"},
	{6, "St. Gallen", 47.4245, 9.3767, "east"},
	{7, "Lausanne", 46.5197, 6.6323, "west"},
	{8, "Luzern", 47.0502, 8.3093, "core"},
}

// graphOnMapFree are nodes the data joins to but never locates — a relay, an
// operator, an archive. They have no coordinates and no pin.
var graphOnMapFree = []struct {
	id    uint64
	label string
}{
	{101, "relay-north"},
	{102, "relay-south"},
	{103, "archive"},
}

var graphOnMapLinks = [][2]uint64{
	{1, 4}, {1, 8}, {1, 6}, {2, 8}, {2, 7}, {3, 7}, {4, 2}, {8, 5}, {2, 5}, {1, 2},
	// the unlocated nodes hang off located ones
	{101, 1}, {101, 4}, {101, 6}, {102, 5}, {102, 3}, {102, 7}, {103, 101}, {103, 102},
}

type graphOnMapState struct {
	m     *portolan.Map
	gv    *graphview.View
	land  *landoverlay.Layer
	atlas *worldmap.Atlas

	nodes []graphview.NodeSpec
	edges []graphview.EdgeSpec
	// origin is graphOnMapCentre projected at the reference zoom: the point
	// world coordinates are measured from.
	origin portolan.Point

	claimed    bool
	claimNode  uint64
	hasNode    bool
	lastEvent  string
	selected   int
	auras      bool
	tiles      bool
	showLand   bool
	showFree   bool
	landNoFill bool
}

func newGraphOnMapState(ids *c.WidgetIdStack) *graphOnMapState {
	st := &graphOnMapState{
		m: portolan.New(ids, portolan.Options{
			Source: basemap.PortolanSource(),
			Loader: basemap.PortolanLoader(),
			Center: graphOnMapCentre,
			Zoom:   graphOnMapRefZoom,
		}),
		gv: graphview.New(ids, "graph-on-map", graphview.Options{
			// A force layout, because the unlocated nodes need laying out;
			// every located node is pinned, so the step only moves those.
			Layout: graphview.LayoutForceDirected,
			Force: graphview.ForceParams{
				// The ideal edge length is derived from the canvas area while
				// the world is reference-zoom pixels; this brings the two to
				// the same order.
				KScale:        0.5,
				PauseOnSettle: true,
			},
			NoZoomAndPan:  true,
			NodeClicking:  true,
			NodeSelection: true,
			LabelsAlways:  true,
			EdgeClicking:  true,
			Auras: graphview.AuraParams{
				// External: a row stamped under the map's own drag region
				// could never be clicked (ADR-0224 §SD15).
				Legend: graphview.AuraLegendExternal,
			},
		}),
		land:     &landoverlay.Layer{},
		auras:    true,
		tiles:    true,
		showLand: true,
		showFree: true,
	}
	st.origin = st.m.View().ProjectAt(graphOnMapCentre, graphOnMapRefZoom)
	if a, err := worldmap.LoadAtlas(); err == nil {
		st.atlas = a
	}
	for _, l := range graphOnMapLinks {
		st.edges = append(st.edges, graphview.EdgeSpec{From: l[0], To: l[1], Width: 2})
	}
	return st
}

// declare rebuilds the frame's node set: located nodes pinned at the
// reference zoom's projection, unlocated ones left to the force step, which
// holds them in that same world between frames.
func (st *graphOnMapState) declare(v *portolan.View, zoom float32) {
	// World units are reference-zoom pixels, so a world radius would grow
	// with the view; divide to keep the marker a constant size on screen.
	r := float32(graphOnMapNodePx) / max(zoom, 1e-6)
	st.nodes = st.nodes[:0]
	for _, s := range graphOnMapSites {
		at := v.ProjectAt(portolan.LL(s.lat, s.lng), graphOnMapRefZoom)
		n := graphview.NodeSpec{
			Id: s.id, Label: s.label, Radius: r,
			Pinned: true,
			PinX:   float32(at.X - st.origin.X), PinY: float32(at.Y - st.origin.Y),
			Color: color.Hex(styletokens.AccentDefault.AsHex()),
		}
		if st.auras {
			n.Auras = []string{s.group}
		}
		st.nodes = append(st.nodes, n)
	}
	if !st.showFree {
		return
	}
	for _, f := range graphOnMapFree {
		n := graphview.NodeSpec{
			Id: f.id, Label: f.label, Radius: r * 0.85,
			Color: color.Hex(styletokens.WarningDefault.AsHex()),
		}
		if st.auras {
			n.Auras = []string{"unlocated"}
		}
		st.nodes = append(st.nodes, n)
	}
}

func demoGraphOnMap(ids *c.WidgetIdStack, st *graphOnMapState) {
	m, gv := st.m, st.gv
	gv.Opts.Auras.Enabled = st.auras
	m.SetNoTiles(!st.tiles)

	// 1. The guest reads the pointer first and says what it took, so the map
	//    can stand down before it handles the same frame's input (§SD2). The
	//    camera here is the view the pointer was over.
	canvas, area := m.Handles()
	claim := gv.HostedInput(graphview.HostCanvas{
		Canvas: canvas, Area: area,
		W: graphOnMapW, H: graphOnMapH,
		Camera: m.View().CameraAt(graphOnMapRefZoom, st.origin),
	})
	st.claimed, st.claimNode, st.hasNode = claim.Pointer, claim.Node, claim.HasNode
	m.SetPointerVeto(claim.Pointer)

	// 2. The map draws; the offline outlines and the graph paint inside its
	//    canvas, through the same projector.
	m.Render(graphOnMapW, graphOnMapH, func(p portolan.Projector) {
		if st.showLand {
			ls := landoverlay.DefaultStyle()
			ls.NoFill = st.landNoFill
			st.land.Draw(p, st.atlas, ls)
		}
		// The map's handlers ran at the top of this Render, so the paint
		// takes the view as it is now — not the one the pick used, which
		// would slide the graph against the tiles under a pan.
		now := p.CameraAt(graphOnMapRefZoom, st.origin)
		gv.SetHostCamera(now)
		st.declare(p.View(), now.Zoom)
		gv.HostedPaint(st.nodes, st.edges)
	})

	// 3. The guest's events are read after the paint, as after Render.
	for _, ev := range gv.Events() {
		switch ev.Kind {
		case graphview.EventKindNodeClick:
			st.lastEvent = fmt.Sprintf("click on %s", graphOnMapLabel(ev.Node))
		case graphview.EventKindNodeDragEnd:
			st.lastEvent = fmt.Sprintf("dragged %s (the map did not pan)", graphOnMapLabel(ev.Node))
		case graphview.EventKindEdgeClick:
			st.lastEvent = fmt.Sprintf("click on edge %s → %s", graphOnMapLabel(ev.From), graphOnMapLabel(ev.To))
		}
	}
	st.selected = 0
	for range gv.SelectedNodes() {
		st.selected++
	}

	v := m.View()
	mt := gv.Metrics()
	c.Label(fmt.Sprintf("map centre %.4f, %.4f   zoom %.2f   ·   pointer: %s   ·   selected %d",
		v.Center().Lat, v.Center().Lng, v.Zoom(), graphOnMapOwner(st), st.selected)).Send()
	c.Label(fmt.Sprintf("%d nodes, %d pinned by geography, %d laid out by the force step   ·   %d countries drawn   ·   settled %v",
		mt.NodeCount, mt.PinnedCount, mt.NodeCount-mt.PinnedCount, st.land.Drawn(), mt.Settled)).Send()
	if st.lastEvent != "" {
		c.Label("last graph event: " + st.lastEvent).Send()
	}
	c.Label("ADR-0228: one canvas, owned by the map. Drag the background to pan and wheel to zoom — those are the map's. " +
		"Hover, click or drag a node and the graph takes the pointer instead, so the map does not pan under the gesture. " +
		"Located nodes are pinned to their projected coordinates; the amber ones have no coordinates at all and are placed " +
		"by the force layout among them. The layout runs in a world fixed at one reference zoom, so its result does not " +
		"change as you zoom the map.").Wrap().Send()

	for range c.CollapsingHeader(ids.PrepareStr("gom-controls"), c.WidgetText().Text("controls and legend").Keep()).DefaultOpen(true).KeepIter() {
		c.Checkbox(ids.PrepareStr("gom-auras"), st.auras, "group auras").SendRespVal(&st.auras)
		c.Checkbox(ids.PrepareStr("gom-free"), st.showFree, "nodes without coordinates").SendRespVal(&st.showFree)
		c.Checkbox(ids.PrepareStr("gom-tiles"), st.tiles, "basemap tiles (needs a tile server)").SendRespVal(&st.tiles)
		c.Checkbox(ids.PrepareStr("gom-land"), st.showLand, "offline country outlines (landoverlay)").SendRespVal(&st.showLand)
		c.Checkbox(ids.PrepareStr("gom-land-nofill"), st.landNoFill, "   outlines only, no land fill").SendRespVal(&st.landNoFill)
		// The legend is external: the rows come from the view and are drawn
		// out here, in a canvas the map does not own.
		for _, it := range gv.AuraLegendItems() {
			c.Label(fmt.Sprintf("   ■ %s", it.Label)).Send()
		}
	}
}

func graphOnMapLabel(id uint64) string {
	for _, s := range graphOnMapSites {
		if s.id == id {
			return s.label
		}
	}
	for _, f := range graphOnMapFree {
		if f.id == id {
			return f.label
		}
	}
	return fmt.Sprintf("#%d", id)
}

func graphOnMapOwner(st *graphOnMapState) string {
	switch {
	case st.claimed && st.hasNode:
		return "the graph (over " + graphOnMapLabel(st.claimNode) + ")"
	case st.claimed:
		return "the graph"
	}
	return "the map"
}
