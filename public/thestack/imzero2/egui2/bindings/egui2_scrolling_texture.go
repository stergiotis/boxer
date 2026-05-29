package bindings

// Hand-written helpers for the generated ScrollingTexture opcode pair (see
// doc/adr/0009-imzero2-scrolling-texture-widget.md SD3, SD8, SD11, SD12).
// The wire arguments `orientation` and `filter` are plain u8 at the IDL
// layer; these Go consts name the values for caller readability. The
// values match the Rust-side ORIENTATION_SCROLL_* / FILTER_* constants in
// src/rust/src/imzero2/scrolling_texture.rs.

// OrientationE names the four scroll orientations supported by the
// scrollingTexture widget. See ADR-0058 SD8.
type OrientationE uint8

const (
	// OrientationScrollLeftE — append right, scroll left. Classical audio
	// spectrogram convention: newest column on the right, oldest on the
	// left, gradient flows right-to-left over time.
	OrientationScrollLeftE OrientationE = 0
	// OrientationScrollRightE — append left, scroll right. Mirror of
	// ScrollLeft: newest on the left, oldest on the right.
	OrientationScrollRightE OrientationE = 1
	// OrientationScrollUpE — append bottom, scroll up. Newest column at
	// the bottom, oldest at the top. Vertical sibling of ScrollLeft.
	OrientationScrollUpE OrientationE = 2
	// OrientationScrollDownE — append top, scroll down. Classical RF
	// waterfall convention: newest column at the top, oldest at the bottom.
	OrientationScrollDownE OrientationE = 3
)

// FilterE selects the GPU texture sampling mode for the scrollingTexture
// widget. See ADR-0058 SD3 — naming mirrors egui's TextureOptions::NEAREST
// and ::LINEAR deliberately, rather than a misleading `bilinear: bool`,
// so callers reading "Linear" understand it as sampling, not as
// column-to-column data interpolation.
type FilterE uint8

const (
	// FilterNearestE — nearest-neighbour sampling. Default for scientific
	// visualisation: each sample is rendered as a sharp rectangle with no
	// cross-column blurring. Faithful to the data.
	FilterNearestE FilterE = 0
	// FilterLinearE — bilinear sampling. Smoother appearance, but blurs
	// across neighbouring columns. Only use when the visual smoothness
	// outweighs the risk of misreading blended values as real samples.
	FilterLinearE FilterE = 1
)

// SendRespVal flushes the scrollingTexture opcode and registers r9_u64 /
// r10 databindings for the widget id. `hoverRc` receives the packed
// hover readout — ((row as uint64) << 32) | col, or u64::MAX when the
// pointer is outside the widget rect (per ADR-0058 SD11). `clicked`
// receives true on frames where egui recognises a primary click on the
// widget rect (SD12).
//
// FFFI databindings reset each Sync; callers must call this every frame
// for the bindings to remain live. Returns the response flags, matching
// other widgets' SendRespVal convention.
func (inst ScrollingTextureFluid) SendRespVal(hoverRc *uint64, clicked *bool) ResponseFlagsE {
	inst.Send()
	s := CurrentApplicationState.StateManager
	id := inst.id
	s.AddR9U64Databinding(id, hoverRc)
	s.AddR10Databinding(id, clicked)
	return s.GetResponseByIdRaw(id)
}

// UnpackHoverRc splits the packed (row:col) hover readout returned via
// r9_u64 into (row, col, hovered). hovered == false when the widget
// reports the u64::MAX sentinel. See ADR-0058 SD11.
func UnpackHoverRc(packed uint64) (row uint32, col uint32, hovered bool) {
	const sentinel = ^uint64(0)
	if packed == sentinel {
		return 0, 0, false
	}
	return uint32(packed >> 32), uint32(packed), true
}
