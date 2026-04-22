//go:build llm_generated_opus47

package h3

// ResolutionE is an H3 cell resolution in the inclusive range [0, 15].
// Resolution 0 is the coarsest (122 cells cover the globe); resolution 15
// is the finest (~1 m^2 per cell). The integer value matches the H3
// specification and the on-the-wire ABI to the wasm bridge.
type ResolutionE uint8

const (
	ResolutionR0  ResolutionE = 0
	ResolutionR1  ResolutionE = 1
	ResolutionR2  ResolutionE = 2
	ResolutionR3  ResolutionE = 3
	ResolutionR4  ResolutionE = 4
	ResolutionR5  ResolutionE = 5
	ResolutionR6  ResolutionE = 6
	ResolutionR7  ResolutionE = 7
	ResolutionR8  ResolutionE = 8
	ResolutionR9  ResolutionE = 9
	ResolutionR10 ResolutionE = 10
	ResolutionR11 ResolutionE = 11
	ResolutionR12 ResolutionE = 12
	ResolutionR13 ResolutionE = 13
	ResolutionR14 ResolutionE = 14
	ResolutionR15 ResolutionE = 15
)

var AllResolutions = []ResolutionE{
	ResolutionR0, ResolutionR1, ResolutionR2, ResolutionR3,
	ResolutionR4, ResolutionR5, ResolutionR6, ResolutionR7,
	ResolutionR8, ResolutionR9, ResolutionR10, ResolutionR11,
	ResolutionR12, ResolutionR13, ResolutionR14, ResolutionR15,
}

// StatusE is the per-element status byte for bulk operations. Kept in
// lock-step with `STATUS_*` constants in rust/h3bridge/src/lib.rs.
type StatusE uint8

const (
	StatusOk                StatusE = 0
	StatusInvalidLatLng     StatusE = 1
	StatusInvalidCell       StatusE = 2
	StatusInvalidResolution StatusE = 3
	StatusInvalidString     StatusE = 4
	StatusInternal          StatusE = 5
)

var AllStatuses = []StatusE{
	StatusOk,
	StatusInvalidLatLng,
	StatusInvalidCell,
	StatusInvalidResolution,
	StatusInvalidString,
	StatusInternal,
}

// String returns a readable label for s.
func (inst StatusE) String() (s string) {
	switch inst {
	case StatusOk:
		s = "ok"
	case StatusInvalidLatLng:
		s = "invalid_latlng"
	case StatusInvalidCell:
		s = "invalid_cell"
	case StatusInvalidResolution:
		s = "invalid_resolution"
	case StatusInvalidString:
		s = "invalid_string"
	case StatusInternal:
		s = "internal"
	default:
		s = "unknown"
	}
	return
}

// LatLng is a coordinate pair view in degrees. Bulk APIs do not use this
// type on the hot path — they operate on parallel []float64 slices — but
// it is the natural shape for iterator views and examples.
type LatLng struct {
	LatDeg float64
	LngDeg float64
}

// ContainmentModeE selects the criterion H3 uses to decide whether a cell
// falls inside a polygon during polyfill. Values mirror h3o's
// ContainmentMode enum with the same ordinal values.
type ContainmentModeE uint8

const (
	// ContainmentContainsCentroid selects cells whose centroid lies inside
	// the polygon. Fastest; guarantees unique cell-to-polygon assignment
	// but may leave polygon edges uncovered or overshoot near boundaries.
	ContainmentContainsCentroid ContainmentModeE = 0
	// ContainmentContainsBoundary selects cells whose entire boundary is
	// inside the polygon. Unique assignment; no overshoot; may leave more
	// of the polygon uncovered than ContainsCentroid.
	ContainmentContainsBoundary ContainmentModeE = 1
	// ContainmentIntersectsBoundary selects cells whose boundary intersects
	// the polygon boundary (even partially). Full coverage; cells may be
	// shared across adjacent polygons; cells fully containing the polygon
	// are NOT returned (no intersection).
	ContainmentIntersectsBoundary ContainmentModeE = 2
	// ContainmentCovers is like IntersectsBoundary but additionally
	// includes a single covering cell when the polygon is entirely inside
	// one cell with no boundary intersection.
	ContainmentCovers ContainmentModeE = 3
)

var AllContainmentModes = []ContainmentModeE{
	ContainmentContainsCentroid,
	ContainmentContainsBoundary,
	ContainmentIntersectsBoundary,
	ContainmentCovers,
}
