// Package h3overlay draws H3 cells on a portolan map: cell fills from the
// cells' boundaries, a region's outline from their dissolve, both computed
// through the h3 wasm bridge (public/science/geo/h3), which the map widget
// itself does not depend on — only a caller that wants H3 pays for the
// runtime. The port of the walkers binding's h3Cells/h3Region overlays
// (ADR-0204 §SD9).
package h3overlay

import (
	"context"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/science/geo/h3"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// Layer holds the scratch buffers one overlay reuses across frames: a cell
// layer is re-projected every frame (the map's view moves), and its
// boundaries come back from the bridge into these slices rather than fresh
// ones. The zero value is ready to use.
type Layer struct {
	lats, lngs []float64
	offsets    []int32
	status     []h3.StatusE
}

// Cells fills each cell with its colour — fills[i], or fills[0] for every
// cell when one colour is given — and strokes its boundary when strokeWidth
// is positive. A cell outside the padded viewport costs its projection and
// nothing more; an invalid cell id is skipped. Hexagons are convex, so the
// fill is the painter's feathered fan (Projector.ConvexPolygon).
func (l *Layer) Cells(ctx context.Context, p portolan.Projector, h *h3.Handle, cells []uint64, fills []color.Color, stroke color.Color, strokeWidth float32) (err error) {
	if len(cells) == 0 || len(fills) == 0 {
		return
	}
	l.lats, l.lngs, l.offsets, l.status, err = h.CellsToBoundariesE(ctx, cells, l.lats, l.lngs, l.offsets, l.status)
	if err != nil {
		return eh.Errorf("h3overlay: cell boundaries: %w", err)
	}
	for i, row := range h3.AllCSRRowsLatLng(l.lats, l.lngs, l.offsets) {
		if l.status[i] != h3.StatusOk {
			continue
		}
		fill := fills[min(i, len(fills)-1)]
		p.ConvexPolygon(row[0], row[1], fill, stroke, strokeWidth)
	}
	return
}

// ViewportCells is the cells at a resolution whose boundary intersects the
// view's bounds — what a viewport-driven layer (a heatmap) recomputes when
// the view hash changes.
func ViewportCells(ctx context.Context, h *h3.Handle, b portolan.LatLngBounds, res h3.ResolutionE) ([]uint64, error) {
	if !b.IsValid() {
		return nil, nil
	}
	s, w, n, e := b.GetSouth(), b.GetWest(), b.GetNorth(), b.GetEast()
	// A closed exterior ring, no holes — the scalar convenience wrapper.
	lats := []float64{s, s, n, n, s}
	lngs := []float64{w, e, e, w, w}
	cells, err := h.PolygonToCellsSimpleE(ctx, res, h3.ContainmentIntersectsBoundary, lats, lngs)
	if err != nil {
		return nil, eh.Errorf("h3overlay: viewport cells: %w", err)
	}
	return cells, nil
}

// ResolutionForZoom picks an H3 resolution for a web-mercator zoom so cells
// stay a sensible size on screen: world zooms hit res 1–3, continental 4–5,
// country 6–7, metro 8–9, street 10–12; clamped to [1, 12] to keep the cell
// counts of a viewport manageable.
func ResolutionForZoom(zoom float64) h3.ResolutionE {
	r := min(max(int(math.Round(zoom/2.0-1.0)), 1), 12)
	return h3.ResolutionE(r)
}
