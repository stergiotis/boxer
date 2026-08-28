package waveform

import (
	"sort"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Layers are the host-owned annotations the player draws over and under the
// waveform (ADR-0208 SD8): regions and markers on the waveform canvas, curves
// in strips of their own beneath the channels. Interval and point lanes
// belong to [Lanes], the timeline stacked under the player.
//
// Every slice is sorted by its start frame — the player binary-searches the
// visible span, so ten thousand regions cost what the visible ones cost. The
// host keeps ownership: the player never appends to or reorders a slice, and
// an edit comes back as an [Events.RegionEdit] the host applies itself.
type Layers struct {
	Regions []Region
	Markers []Marker
	Curves  []Curve
}

// Region is a span of the track: a detector's segment, a selection, a loop.
type Region struct {
	FromFrame int64
	ToFrame   int64
	Label     string
	// Color is the region's tint; the zero value uses the player's default.
	Color color.Color
	// Editable regions move by dragging their body and resize by dragging
	// their edges; the player reports, the host applies.
	Editable bool
}

// Marker is an instant with a label.
type Marker struct {
	Frame int64
	Label string
	Color color.Color
}

// Curve is a scalar over time — a detector's probability, a loudness
// envelope — drawn as a polyline in a strip under the channels.
type Curve struct {
	Label string
	// Frames ascend; Values has the same length.
	Frames []int64
	Values []float32
	// Min and Max bound the strip's vertical scale; both zero means [0, 1].
	Min, Max float32
	Color    color.Color
	// Height of the strip in pixels; zero is the default.
	Height float32
}

// RegionEdit is an in-progress or finished drag on an editable region: the
// index into Layers.Regions and the bounds the pointer implies. Done is set
// on the frame the pointer is released.
type RegionEdit struct {
	Index     int
	FromFrame int64
	ToFrame   int64
	Done      bool
}

// Events is what the pointer did to the layers during the last frame. Read
// it after Render; indices refer to the Layers in place for that frame.
type Events struct {
	// RegionEdit is non-nil while an editable region is being dragged.
	RegionEdit *RegionEdit
	// RegionClicked is the region under a primary click, or -1.
	RegionClicked int
	// MarkerClicked is the marker under a primary click, or -1.
	MarkerClicked int
}

// ReadoutE selects how the ruler and the readouts print a frame.
type ReadoutE uint8

const (
	// ReadoutAuto shows wall-clock time when the track has an epoch and an
	// offset from the start otherwise.
	ReadoutAuto ReadoutE = iota
	// ReadoutRelative always shows an offset from the start.
	ReadoutRelative
	// ReadoutAbsolute shows wall-clock time; without an epoch it falls back
	// to offsets.
	ReadoutAbsolute
)

// AllReadouts lists the readout modes.
var AllReadouts = []ReadoutE{ReadoutAuto, ReadoutRelative, ReadoutAbsolute}

const (
	defaultCurveHeight  float32 = 40
	curveGap            float32 = 4
	regionEdgeGrabPx    float32 = 6
	markerGrabPx        float32 = 4
	regionFillAlpha     uint8   = 0x48
	regionHoverAlpha    uint8   = 0x70
	maxCurvePointsPerPx         = 4
)

// visibleRegions returns the index range [lo, hi) of regions that intersect
// [fromFrame, toFrame). Regions are sorted by FromFrame; maxLen is the
// longest region's duration, which bounds how far back a region starting
// before the span can still reach into it.
func visibleRegions(regions []Region, maxLen int64, fromFrame, toFrame int64) (lo, hi int) {
	hi = sort.Search(len(regions), func(i int) bool { return regions[i].FromFrame >= toFrame })
	lo = sort.Search(hi, func(i int) bool { return regions[i].FromFrame >= fromFrame-maxLen })
	return lo, hi
}

// maxRegionLen is the longest region duration, the bound visibleRegions needs.
func maxRegionLen(regions []Region) (n int64) {
	for i := range regions {
		if d := regions[i].ToFrame - regions[i].FromFrame; d > n {
			n = d
		}
	}
	return n
}

// visibleMarkers returns the index range [lo, hi) of markers inside
// [fromFrame, toFrame).
func visibleMarkers(markers []Marker, fromFrame, toFrame int64) (lo, hi int) {
	lo = sort.Search(len(markers), func(i int) bool { return markers[i].Frame >= fromFrame })
	hi = sort.Search(len(markers), func(i int) bool { return markers[i].Frame >= toFrame })
	return lo, hi
}

// visiblePoints returns the index range [lo, hi) of a curve's points to draw
// for [fromFrame, toFrame): the points inside plus one on each side, so the
// polyline enters and leaves the view rather than starting inside it.
func visiblePoints(frames []int64, fromFrame, toFrame int64) (lo, hi int) {
	lo = sort.Search(len(frames), func(i int) bool { return frames[i] >= fromFrame })
	hi = sort.Search(len(frames), func(i int) bool { return frames[i] >= toFrame })
	if lo > 0 {
		lo--
	}
	if hi < len(frames) {
		hi++
	}
	return lo, hi
}

// curvesHeight is the vertical room the curve strips take from the wave area.
func curvesHeight(curves []Curve) (h float32) {
	for i := range curves {
		h += curveStripHeight(curves[i]) + curveGap
	}
	return h
}

func curveStripHeight(cv Curve) (h float32) {
	if cv.Height > 0 {
		return cv.Height
	}
	return defaultCurveHeight
}

// withAlpha replaces a literal colour's alpha.
func withAlpha(col color.Color, alpha uint8) (out color.Color) {
	return color.Hex(col.Literal()&^0xff | uint32(alpha))
}
