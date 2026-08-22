package widgets

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// The portolan demo: the slippy-map widget of ADR-0204 over the registry's
// basemap (OpenStreetMap unless BOXER_MAP_TILE_URL points elsewhere), with a
// readout of the view and the tile pipeline so a headless scene can assert on
// it, and one overlay drawn through the Projector hook.

const (
	portolanDemoW = 720
	portolanDemoH = 460
)

type portolanDemoState struct {
	m *portolan.Map
}

func newPortolanDemoState(ids *c.WidgetIdStack) *portolanDemoState {
	return &portolanDemoState{
		m: portolan.New(ids, portolan.Options{
			Source: basemap.PortolanSource(),
			Loader: basemap.PortolanLoader(),
			Center: portolan.LL(walkersDemoCenterLat, walkersDemoCenterLon),
			Zoom:   12,
		}),
	}
}

func demoPortolan(st *portolanDemoState) {
	m := st.m
	m.Render(portolanDemoW, portolanDemoH, func(p portolan.Projector) {
		// One overlay through the projector: a marker on the initial centre,
		// which stays put under pan and zoom if the projection is right.
		at := p.ToCanvas(portolan.LL(walkersDemoCenterLat, walkersDemoCenterLon))
		c.PaintCircleFilled(float32(at.X), float32(at.Y), 6, color.Hex(0xe0303080)).Send()
		c.PaintCircleStroke(float32(at.X), float32(at.Y), 6, color.Hex(0xe03030ff), 1.5).Send()
	})
	v := m.View()
	b := v.Bounds()
	ps := m.Stats()
	h := m.Health()
	c.Label(fmt.Sprintf("centre %.5f, %.5f   zoom %.2f   bounds %.4f,%.4f → %.4f,%.4f",
		v.Center().Lat, v.Center().Lng, v.Zoom(), b.GetSouth(), b.GetWest(), b.GetNorth(), b.GetEast())).Send()
	c.Label(fmt.Sprintf("tiles: %d requested · %d loaded · %d errors · %d unloaded · %d pending · loading %v",
		ps.TileLoadStart, ps.TileLoad, ps.TileError, ps.TileUnload, h.Pending+h.InFlight, m.Loading())).Send()
	c.Label(fmt.Sprintf("shipped %.2f MB through paintImage · re-ships %d · consecutive fetch failures %d",
		float64(m.BytesShipped())/(1<<20), m.Reships(), h.ConsecutiveFailures)).Send()
	c.Label("portolan (ADR-0204 M2): drag to pan, wheel to zoom about the pointer, double-click to zoom in " +
		"(shift: out). Tiles retained across levels while the new level loads; pan and zoom read the painter lane's " +
		"registers one frame behind.").Wrap().Send()
}
