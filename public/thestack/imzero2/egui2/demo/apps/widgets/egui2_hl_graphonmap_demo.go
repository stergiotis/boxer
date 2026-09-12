package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
	cam "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/camera"
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
// its projected layer point, so the map places it. A node *without* any —
// something the data joins to but does not locate — is declared unpinned, and
// the force step lays it out among the pinned ones, which is what ADR-0224
// §SD10 gives for free: a declared pin is fixed and everything else still
// feels it. Between frames such a node is anchored in geography rather than
// on screen: its settled position is unprojected after the paint and
// projected back before the next one, so it rides the map instead of sliding
// against it when the view moves.

const (
	graphOnMapW = 720
	graphOnMapH = 460
)

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
	// freeAt anchors an unlocated node in geography between frames: where the
	// force step left it, unprojected. Empty until it has been placed once.
	freeAt map[uint64]portolan.LatLng

	claimed   bool
	claimNode uint64
	hasNode   bool
	lastEvent string
	selected  int
	auras     bool
	tiles     bool
	showLand  bool
	showFree  bool
}

func newGraphOnMapState(ids *c.WidgetIdStack) *graphOnMapState {
	st := &graphOnMapState{
		m: portolan.New(ids, portolan.Options{
			Source: basemap.PortolanSource(),
			Loader: basemap.PortolanLoader(),
			Center: portolan.LL(46.85, 8.0),
			Zoom:   7.2,
		}),
		gv: graphview.New(ids, "graph-on-map", graphview.Options{
			// A force layout, because the unlocated nodes need laying out;
			// every located node is pinned, so the step only moves those.
			Layout:        graphview.LayoutForceDirected,
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
		freeAt:   make(map[uint64]portolan.LatLng, len(graphOnMapFree)),
		auras:    true,
		tiles:    true,
		showLand: true,
		showFree: true,
	}
	if a, err := worldmap.LoadAtlas(); err == nil {
		st.atlas = a
	}
	for _, l := range graphOnMapLinks {
		st.edges = append(st.edges, graphview.EdgeSpec{From: l[0], To: l[1], Width: 2})
	}
	return st
}

// declare rebuilds the frame's node set: located nodes pinned to their
// projected layer points, unlocated ones left to the force step.
func (st *graphOnMapState) declare(p portolan.Projector) {
	st.nodes = st.nodes[:0]
	for _, s := range graphOnMapSites {
		at := p.ToCanvas(portolan.LL(s.lat, s.lng))
		n := graphview.NodeSpec{
			Id: s.id, Label: s.label, Radius: 7,
			Pinned: true, PinX: float32(at.X), PinY: float32(at.Y),
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
			Id: f.id, Label: f.label, Radius: 6,
			Color: color.Hex(styletokens.WarningDefault.AsHex()),
		}
		if st.auras {
			n.Auras = []string{"unlocated"}
		}
		st.nodes = append(st.nodes, n)
		// Put it back where geography left it before the step runs again.
		if ll, ok := st.freeAt[f.id]; ok {
			at := p.ToCanvas(ll)
			st.gv.SetNodePosition(f.id, float32(at.X), float32(at.Y))
		}
	}
}

// adopt remembers where the force step left each unlocated node, as
// geography rather than as pixels, so the next frame's projection carries it
// with the map.
func (st *graphOnMapState) adopt(p portolan.Projector) {
	for _, f := range graphOnMapFree {
		if x, y, ok := st.gv.NodePosition(f.id); ok {
			st.freeAt[f.id] = p.ToLatLng(portolan.Point{X: float64(x), Y: float64(y)})
		}
	}
}

func demoGraphOnMap(ids *c.WidgetIdStack, st *graphOnMapState) {
	m, gv := st.m, st.gv
	gv.Opts.Auras.Enabled = st.auras
	m.SetNoTiles(!st.tiles)

	// 1. The guest reads the pointer first and says what it took, so the map
	//    can stand down before it handles the same frame's input (§SD2).
	canvas, area := m.Handles()
	claim := gv.HostedInput(graphview.HostCanvas{
		Canvas: canvas, Area: area,
		W: graphOnMapW, H: graphOnMapH,
		// Layer points are canvas pixels, so the transform is the identity —
		// the recipe that stays exact at every zoom (ADR-0228 §Context).
		Camera: cam.Camera{Zoom: 1},
	})
	st.claimed, st.claimNode, st.hasNode = claim.Pointer, claim.Node, claim.HasNode
	m.SetPointerVeto(claim.Pointer)

	// 2. The map draws; the offline outlines and the graph paint inside its
	//    canvas, through the same projector.
	m.Render(graphOnMapW, graphOnMapH, func(p portolan.Projector) {
		if st.showLand {
			st.land.Draw(p, st.atlas, landoverlay.DefaultStyle())
		}
		st.declare(p)
		gv.HostedPaint(st.nodes, st.edges)
		st.adopt(p)
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
		"Located nodes are pinned to their coordinates and reprojected every frame; the amber ones have no coordinates at " +
		"all and are placed by the force layout among them, then anchored in geography so they ride the map.").Wrap().Send()

	for range c.CollapsingHeader(ids.PrepareStr("gom-controls"), c.WidgetText().Text("controls and legend").Keep()).DefaultOpen(true).KeepIter() {
		c.Checkbox(ids.PrepareStr("gom-auras"), st.auras, "group auras").SendRespVal(&st.auras)
		c.Checkbox(ids.PrepareStr("gom-free"), st.showFree, "nodes without coordinates").SendRespVal(&st.showFree)
		c.Checkbox(ids.PrepareStr("gom-tiles"), st.tiles, "basemap tiles (needs a tile server)").SendRespVal(&st.tiles)
		c.Checkbox(ids.PrepareStr("gom-land"), st.showLand, "offline country outlines (landoverlay)").SendRespVal(&st.showLand)
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
