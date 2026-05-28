//go:build llm_generated_opus47

package bindings

// Hand-written helpers for the generated Image / ImageRelease opcode pair.
// Wire arguments `fit`, `filter`, and `tintRgba` are plain scalars at the
// IDL layer; these Go consts/types name the values for caller readability.
// The values match the Rust-side FIT_* constants in src/rust/src/imzero2/
// image.rs and the FILTER_* constants in scrolling_texture.rs (deliberately
// shared — both widgets use the same egui::TextureOptions sampling modes).
//
// Hover readout uses the same packed (row:col) convention as the
// scrollingTexture widget; UnpackHoverRc lives in egui2_scrolling_texture.go
// and is reused here verbatim.

// FitE selects how the image is sized inside the allocated ui slot.
type FitE uint8

const (
	// FitNativeE — render at the texture's native pixel size. Useful for
	// pixel-exact icons and embedded assets where any scaling would alias.
	// `fixedW` / `fixedH` are ignored.
	FitNativeE FitE = 0
	// FitFixedE — render at exactly (fixedW × fixedH) screen pixels,
	// possibly distorting the aspect ratio. Useful when the layout dictates
	// the slot size and the caller is OK with non-uniform scaling.
	FitFixedE FitE = 1
	// FitFillRectE — render at the ui's available size, possibly
	// distorting the aspect ratio. The image fills the remaining slot.
	// `fixedW` / `fixedH` are ignored.
	FitFillRectE FitE = 2
	// FitAspectMaxE — render aspect-preserved, scaled to fit inside the
	// (fixedW × fixedH) bounding box. The actually-rendered size is the
	// largest such rect that preserves the native aspect ratio.
	FitAspectMaxE FitE = 3
)

// TintNoneRgba is the sentinel passed for `tintRgba` to render the image
// without a multiplicative tint (i.e. plain white, the egui pass-through).
// Any other value tints the image as `Color32::from_rgba_unmultiplied`.
const TintNoneRgba uint32 = 0xFFFFFFFF

// SendResp flushes the image opcode and returns the standard r7
// ResponseFlags for the widget id (HasHovered, HasPrimaryClicked, etc.).
// The hover *position* is pushed separately into r9_u64 every frame; use
// SendRespHoverPx to register a databinding for it.
func (inst ImageFluid) SendResp() ResponseFlagsE {
	inst.Send()
	return CurrentApplicationState.StateManager.GetResponseByIdRaw(inst.id)
}

// SendRespHoverPx flushes the image opcode, registers an r9_u64
// databinding so the next StateManager.Sync() writes the packed
// (row<<32)|col hover readout into *hoverRc, and returns the standard r7
// response flags.
//
// `hoverRc` is in **image-pixel space** regardless of fit mode — i.e. the
// row/col indexes the source texture, not the screen rect. Pass the
// packed value through UnpackHoverRc to split into (row, col, hovered).
//
// FFFI databindings reset each Sync; callers must call this every frame
// for the binding to remain live.
func (inst ImageFluid) SendRespHoverPx(hoverRc *uint64) ResponseFlagsE {
	inst.Send()
	s := CurrentApplicationState.StateManager
	id := inst.id
	s.AddR9U64Databinding(id, hoverRc)
	return s.GetResponseByIdRaw(id)
}

// SendResp flushes the imageRelease opcode. Use to drop the Rust-side cache
// entry for a widget id before the LRU would reap it (e.g. when the
// caller knows the asset will never be shown again).
func (inst ImageReleaseFluid) SendResp() {
	inst.Send()
}

// ImageVersionTracker is the Go-side companion to the image widget's
// content_version contract. The widget's wire protocol reserves the case
// `pixels=[]uint32{}` (empty, NOT nil — see FFFI2 nil-sentinel asymmetry)
// to mean "draw the cached texture, don't re-upload". A tracker remembers
// the last contentVersion it sent for each key, so the caller can ship
// the empty slice when nothing changed.
//
// Keying contract: the tracker key must be 1:1 with the **widget id**,
// not with the logical asset. The Rust-side GPU cache is keyed by widget
// id, so two widgets that show the same asset have two cache entries
// that need independent first-frame uploads. If you reuse one tracker
// key across N widget ids, the second-through-Nth widget will receive
// the empty slice on its first frame and render nothing. The simplest
// safe pattern is to pass the same stable string to both the tracker
// and `ids.PrepareStr(...)`:
//
//	const key = "my-image"
//	pixels := tracker.PixelsToSend(key, currentVersion, fullPixels)
//	c.Image(ids.PrepareStr(key), w, h, currentVersion, ...).SendResp()
//
// For static assets shown a small fixed number of times, skipping the
// tracker entirely is usually clearer — the per-widget-id one-shot
// upload cost is negligible.
//
// Forget(key) when you call ImageRelease so the next show re-uploads.
type ImageVersionTracker[K comparable] struct {
	last map[K]uint64
}

// NewImageVersionTracker constructs an empty tracker. The type parameter K
// is whatever stable identifier the caller already uses to address the
// asset (string, struct{}, int — any comparable type).
func NewImageVersionTracker[K comparable]() (out *ImageVersionTracker[K]) {
	out = &ImageVersionTracker[K]{last: make(map[K]uint64)}
	return
}

// PixelsToSend returns the pixel slice the caller should pass to Image().
// If the supplied contentVersion matches the last version recorded for
// `key`, returns an empty (non-nil) slice to signal "use cached".
// Otherwise returns the supplied `pixels` and records the new version.
func (inst *ImageVersionTracker[K]) PixelsToSend(key K, contentVersion uint64, pixels []uint32) (out []uint32) {
	if last, ok := inst.last[key]; ok && last == contentVersion {
		out = []uint32{}
		return
	}
	inst.last[key] = contentVersion
	out = pixels
	return
}

// Forget drops the version record for `key`. Call after ImageRelease() so
// the next Image() call for the same id re-uploads fresh pixels.
func (inst *ImageVersionTracker[K]) Forget(key K) {
	delete(inst.last, key)
}
