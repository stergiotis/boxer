// Package landoverlay draws the world's landmasses and country borders on a
// portolan map from the worldmap atlas — the vendored Natural Earth 110m
// admin-0 outlines — through the projector hook, so a map with no tile server
// still shows geography (ADR-0204 §SD9's pattern, as h3overlay follows it).
// The map itself does not depend on this package: only a caller that wants an
// offline basemap pays for the atlas.
//
// The outlines are coarse by design. At country-and-continent zooms they read
// as a basemap; past roughly zoom 8 a 110m coastline is visibly a polygon, so
// this is the offline stand-in for tiles rather than a replacement for them.
//
// Rings are drawn as the atlas holds them. No ring in the vendored asset
// crosses the antimeridian: the one ring whose longitudes step a full turn is
// Antarctica's, and that step is the polygon's seam over the pole — down the
// 180th meridian to latitude −90, across, and back up the −180th — not a path
// going anywhere. Treating it as a crossing and making the longitudes
// continuous moves 554 of its 556 vertices and puts the continent a world
// width east of everything else, which is how that was found out. Latitudes
// past the Mercator clamp, which those pole vertices are, flatten onto it as
// they do for any geometry the projection is given, so the seam lands along
// the bottom edge of the map where it belongs.
//
//	layer := &landoverlay.Layer{}
//	atlas, _ := worldmap.LoadAtlas()
//	m.Render(w, h, func(p portolan.Projector) {
//	    layer.Draw(p, atlas, landoverlay.DefaultStyle())
//	})
package landoverlay

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

// Style is the overlay's appearance. A zero Style is DefaultStyle, detected
// through a non-positive BorderWidth.
type Style struct {
	Land        color.Color // filled landmass
	Border      color.Color // country outline
	BorderWidth float32     // screen pixels
	// NoFill strokes the outlines and fills nothing. Beside being a look of
	// its own, it is the way to tell a fill artefact from a geometry one: the
	// stroke follows the ring the overlay computed, while the fill is that
	// ring triangulated, and only the second can go wrong on its own.
	NoFill bool
}

// DefaultStyle is the design system's land and border tokens.
func DefaultStyle() Style {
	return Style{
		Land:        color.Hex(styletokens.NeutralBgSurface.AsHex()),
		Border:      color.Hex(styletokens.NeutralBorderDefault.AsHex()),
		BorderWidth: styletokens.StrokeHair,
	}
}

// Layer holds the buffers one overlay reuses across frames: a ring is
// re-projected every frame because the view moves, and its degrees come back
// into these slices rather than fresh ones. The zero value is ready to use.
type Layer struct {
	lats, lngs []float64
	// drawn counts the countries the last Draw painted, for a readout.
	drawn int
}

// Drawn is how many countries the last Draw painted — the rest were outside
// the viewport and cost their bounds test and nothing more.
func (l *Layer) Drawn() int { return l.drawn }

// Draw paints every country that meets the current viewport: the outer rings
// filled and stroked, the holes stroked only, since a filled polygon on the
// painter lane carries a single outer ring and would fill an enclave shut.
// A nil atlas draws nothing.
func (l *Layer) Draw(p portolan.Projector, atlas *worldmap.Atlas, st Style) {
	l.drawn = 0
	if atlas == nil {
		return
	}
	if st.BorderWidth <= 0 {
		st = DefaultStyle()
	}
	b := p.View().Bounds()
	south, west, north, east := b.GetSouth(), b.GetWest(), b.GetNorth(), b.GetEast()
	for i := range atlas.Countries {
		cy := &atlas.Countries[i]
		if !meets(cy, south, west, north, east) {
			continue
		}
		l.drawn++
		for r := range cy.RingCount() {
			l.lats, l.lngs = l.lats[:0], l.lngs[:0]
			var hole bool
			l.lats, l.lngs, hole = cy.Ring(r, l.lats, l.lngs)
			if len(l.lats) < 3 {
				continue
			}
			if hole || st.NoFill {
				// An enclave: its border, never the fill that would swallow
				// it. NoFill takes every ring down the same path.
				p.Polyline(l.lats, l.lngs, st.Border, st.BorderWidth)
				continue
			}
			p.Polygon(l.lats, l.lngs, st.Land, st.Border, st.BorderWidth)
		}
	}
}

// meets reports whether a country's extent overlaps the viewport box. It is
// the whole cull: a country that fails costs its bounds and no projection.
// A country with no geometry never meets, and the naive longitude test
// over-includes an antimeridian-crossing country rather than dropping it.
func meets(cy *worldmap.Country, south, west, north, east float64) bool {
	minLat, minLng, maxLat, maxLng, ok := cy.GeoBounds()
	return ok && maxLat >= south && minLat <= north && maxLng >= west && minLng <= east
}
