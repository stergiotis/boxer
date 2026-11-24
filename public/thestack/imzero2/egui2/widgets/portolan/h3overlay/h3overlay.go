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
	"slices"

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
	seam       []float64
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
	l.lats, l.lngs, l.offsets, l.status, err = h.CellsToBoundaries(ctx, cells, l.lats, l.lngs, l.offsets, l.status)
	if err != nil {
		return eh.Errorf("h3overlay: cell boundaries: %w", err)
	}
	for i, row := range h3.AllCSRRowsLatLng(l.lats, l.lngs, l.offsets) {
		if l.status[i] != h3.StatusOk {
			continue
		}
		fill := fills[min(i, len(fills)-1)]
		if !CrossesAntimeridian(row[1]) {
			p.ConvexPolygon(row[0], row[1], fill, stroke, strokeWidth)
			continue
		}
		// The cell lies across the 180th meridian, its corners at both
		// ends of the longitudes: drawn as they stand it would span the
		// world. It is drawn at each end instead, its longitudes made
		// continuous there; the copy the view does not show costs its
		// projection.
		l.seam = unwrap(l.seam[:0], row[1], 360)
		p.ConvexPolygon(row[0], l.seam, fill, stroke, strokeWidth)
		l.seam = unwrap(l.seam[:0], row[1], -360)
		p.ConvexPolygon(row[0], l.seam, fill, stroke, strokeWidth)
	}
	return
}

// CrossesAntimeridian reports whether a cell's outline, given by the
// longitudes of its corners in degrees, lies across the 180th meridian:
// its corners are then more than half a turn apart. No cell is that wide.
func CrossesAntimeridian(lngs []float64) bool {
	if len(lngs) == 0 {
		return false
	}
	return slices.Max(lngs)-slices.Min(lngs) > 180
}

// unwrap appends lngs to dst with the ones on the far side of the 180th
// meridian moved by turn (360 or -360), so that the outline is continuous at
// the east end (turn 360) or the west end (turn -360) of the longitudes.
func unwrap(dst []float64, lngs []float64, turn float64) []float64 {
	for _, lng := range lngs {
		if (turn > 0 && lng < 0) || (turn < 0 && lng > 0) {
			lng += turn
		}
		dst = append(dst, lng)
	}
	return dst
}

// ViewportCells is the cells at a resolution whose boundary intersects the
// view's bounds — what a viewport-driven layer (a heatmap) recomputes when
// the view hash changes.
func ViewportCells(ctx context.Context, h *h3.Handle, b portolan.LatLngBounds, res h3.ResolutionE) ([]uint64, error) {
	if !b.IsValid() {
		return nil, nil
	}
	s, n := max(b.GetSouth(), -90), min(b.GetNorth(), 90)
	w, e := b.GetWest(), b.GetEast()
	if e-w > 360 {
		// More than one world: every longitude once.
		w, e = -180, 180
	}
	// H3 reads an edge between two corners as the shorter way round, so a
	// box wider than half a turn comes back as its complement, and one
	// reaching past the 180th meridian has no corners to give at all. The
	// view is asked for in slices narrow enough to mean what they say, each
	// with its longitudes brought into -180..180.
	parts := max(1, int(math.Ceil((e-w)/maxSliceDeg)))
	if parts == 1 {
		return sliceCells(ctx, h, res, s, n, w, e)
	}
	var cells []uint64
	seen := make(map[uint64]struct{})
	for i := range parts {
		lo := w + (e-w)*float64(i)/float64(parts)
		hi := w + (e-w)*float64(i+1)/float64(parts)
		part, err := sliceCells(ctx, h, res, s, n, lo, hi)
		if err != nil {
			return nil, err
		}
		for _, cell := range part {
			if _, has := seen[cell]; !has {
				seen[cell] = struct{}{}
				cells = append(cells, cell)
			}
		}
	}
	return cells, nil
}

// maxSliceDeg is the widest box, in degrees of longitude, asked of H3 in one
// piece.
const maxSliceDeg = 90

// sliceCells is ViewportCells for a box no wider than maxSliceDeg, whose
// longitudes may lie outside -180..180.
func sliceCells(ctx context.Context, h *h3.Handle, res h3.ResolutionE, s, n, w, e float64) ([]uint64, error) {
	w, e = wrapLng(w), wrapLng(e)
	// A closed exterior ring, no holes — the scalar convenience wrapper.
	lats := []float64{s, s, n, n, s}
	lngs := []float64{w, e, e, w, w}
	cells, err := h.PolygonToCellsSimple(ctx, res, h3.ContainmentIntersectsBoundary, lats, lngs)
	if err != nil {
		return nil, eh.Errorf("h3overlay: viewport cells: %w", err)
	}
	return cells, nil
}

// wrapLng brings a longitude in degrees into -180..180, keeping 180 itself.
func wrapLng(lng float64) float64 {
	if lng >= -180 && lng <= 180 {
		return lng
	}
	lng = math.Mod(lng+180, 360)
	if lng < 0 {
		lng += 360
	}
	return lng - 180
}

// ResolutionForZoom picks an H3 resolution for a web-mercator zoom so cells
// stay a sensible size on screen: world zooms hit res 1–3, continental 4–5,
// country 6–7, metro 8–9, street 10–12; clamped to [1, 12] to keep the cell
// counts of a viewport manageable.
func ResolutionForZoom(zoom float64) h3.ResolutionE {
	r := min(max(int(math.Round(zoom/2.0-1.0)), 1), 12)
	return h3.ResolutionE(r)
}
