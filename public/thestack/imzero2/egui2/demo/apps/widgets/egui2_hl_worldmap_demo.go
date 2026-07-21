package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

// =============================================================================
// worldmap widget demo — schematic world choropleth (ADR-0114)
//
// Synthetic per-country values (an arbitrary 0–100 index keyed by ISO code,
// NOT real statistics) resolved through the widget's atlas and rendered as a
// choropleth. Controls flip between graded values and presence-only mode and
// scrub the on-screen map width; hover shows the country readout, matching what
// the play World tab does with a query result.
// =============================================================================

type worldmapDemoState struct {
	widget   *worldmap.Widget
	width    float64
	presence bool
	applied  bool // demo data pushed for the current mode
}

// worldmapDemoValues is the synthetic dataset: ISO codes + a couple of name
// spellings so the demo also exercises the resolver's name path.
var worldmapDemoValues = map[string]float64{
	"DEU": 84, "FRA": 71, "GBR": 66, "ESP": 48, "ITA": 59, "NOR": 92,
	"USA": 77, "CAN": 61, "MEX": 39, "Brazil": 55, "ARG": 33,
	"RUS": 44, "CHN": 68, "IND": 52, "JPN": 81, "AUS": 63,
	"ZAF": 29, "EGY": 24, "NGA": 18, "KEN": 21,
	"South Korea": 74, "IDN": 36, "SAU": 42, "TUR": 47,
}

func init() {
	registry.Register(registry.Demo{
		Name:     "worldmap",
		Category: "Charts & plots",
		Title:    icons.IconChartBar + " worldmap",
		Stage:    [2]float32{1024, 700},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindUX,
		Description: "Schematic world choropleth (ADR-0114): Natural Earth 110m outlines, " +
			"Natural Earth projection, Go-side scanline rasterization into a content-versioned " +
			"Image, colormap legend, O(1) hover hit-testing. Synthetic values keyed by ISO code " +
			"and country name; presence mode fills membership without a legend.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &worldmapDemoState{width: 900}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoWorldmap(ids, state.(*worldmapDemoState))
		},
		SourceFunc: demoWorldmap,
	})
}

func demoWorldmap(ids *c.WidgetIdStack, st *worldmapDemoState) {
	// Construct on first render so the widget binds the same id stack the
	// render pass uses.
	if st.widget == nil {
		st.widget = worldmap.New(ids, "worldmap-demo")
		// Size the map explicitly by on-screen width — the "Width:" slider
		// below drives this every frame, so scrubbing it visibly resizes the
		// map. SetDisplayWidth also sidesteps the gallery's vertical ScrollArea
		// (a fill-available map reads a ~0 available height there and collapses
		// to nothing): an explicit width derives the height from the projection
		// aspect and needs no available-size read. SetPixelWidth keeps the
		// raster resolution in step so the map stays crisp at the chosen size.
		st.widget.SetPixelWidth(st.width)
		st.widget.SetDisplayWidth(st.width)
	}

	for range c.Horizontal().KeepIter() {
		c.Label("Width:").Send()
		c.AddSpace(padInner())
		// On-screen width in points, capped near the tour-stage / gallery-pane
		// width so the map stays fully visible: the host ScrollArea scrolls
		// vertically only, so a wider map would clip on the right rather than
		// scroll.
		c.SliderF64(ids.PrepareStr("wm-width"), st.width, 320, 1024).
			SendRespVal(&st.width)
		c.AddSpace(gapSections())
		if c.Checkbox(ids.PrepareStr("wm-presence"), st.presence, "presence only").
			SendRespVal(&st.presence).HasChanged() {
			st.applied = false
		}
	}
	c.Separator().Horizontal().Send()

	st.widget.SetPixelWidth(st.width)
	st.widget.SetDisplayWidth(st.width)
	if !st.applied {
		st.applied = true
		atlas := st.widget.Atlas()
		if atlas != nil {
			if st.presence {
				present := make(map[worldmap.CountryIdx]bool, len(worldmapDemoValues))
				for key := range worldmapDemoValues {
					if idx, ok := atlas.Resolve(key); ok {
						present[idx] = true
					}
				}
				st.widget.SetPresence(present)
			} else {
				vals := make(map[worldmap.CountryIdx]float64, len(worldmapDemoValues))
				for key, v := range worldmapDemoValues {
					if idx, ok := atlas.Resolve(key); ok {
						vals[idx] = v
					}
				}
				st.widget.SetValues(vals)
			}
		}
	}
	st.widget.Render()
}
