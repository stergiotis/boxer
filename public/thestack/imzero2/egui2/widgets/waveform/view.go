package waveform

import "math"

// View is the visible window over a track: the leftmost visible frame and
// how many frames one pixel column covers. Both are float64 so an anchored
// zoom does not drift with repeated steps; callers that need a frame index
// round through [View.FrameAtX].
type View struct {
	FromFrame   float64
	FramesPerPx float64
}

const (
	// minFramesPerPx is the deepest zoom: 64 pixels per sample.
	minFramesPerPx = 1.0 / 64
)

// XToFrame maps a canvas x to a fractional frame.
func (inst View) XToFrame(x float32) (frame float64) {
	return inst.FromFrame + float64(x)*inst.FramesPerPx
}

// FrameAtX maps a canvas x to a whole frame, rounding down.
func (inst View) FrameAtX(x float32) (frame int64) {
	return int64(math.Floor(inst.XToFrame(x)))
}

// FrameToX maps a (fractional) frame to a canvas x.
func (inst View) FrameToX(frame float64) (x float32) {
	return float32((frame - inst.FromFrame) / inst.FramesPerPx)
}

// ToFrame returns the first frame past the visible span for a canvas of
// widthPx columns.
func (inst View) ToFrame(widthPx float32) (frame float64) {
	return inst.FromFrame + float64(widthPx)*inst.FramesPerPx
}

// Contains reports whether frame lies inside the visible span.
func (inst View) Contains(frame float64, widthPx float32) (yes bool) {
	return frame >= inst.FromFrame && frame < inst.ToFrame(widthPx)
}

// fitAll is the view that shows the whole track across widthPx columns.
func fitAll(frames int64, widthPx float32) (v View) {
	if widthPx < 1 {
		widthPx = 1
	}
	fpp := float64(frames) / float64(widthPx)
	if fpp < minFramesPerPx {
		fpp = minFramesPerPx
	}
	return View{FromFrame: 0, FramesPerPx: fpp}
}

// zoomAt scales frames-per-pixel by 1/factor (factor > 1 zooms in) keeping
// the frame under anchorX where it is, then clamps.
func (inst View) zoomAt(anchorX float32, factor float64, frames int64, widthPx float32) (v View) {
	if factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
		return inst.clamp(frames, widthPx)
	}
	anchorFrame := inst.XToFrame(anchorX)
	v = inst
	v.FramesPerPx = inst.FramesPerPx / factor
	v.FromFrame = anchorFrame - float64(anchorX)*v.FramesPerPx
	return v.clamp(frames, widthPx)
}

// panPx moves the view by dx pixels (positive: content moves right, i.e.
// the view moves to earlier frames) and clamps.
func (inst View) panPx(dx float32, frames int64, widthPx float32) (v View) {
	v = inst
	v.FromFrame -= float64(dx) * inst.FramesPerPx
	return v.clamp(frames, widthPx)
}

// clamp enforces the view's invariants against a track of frames frames
// drawn over widthPx columns: the zoom lies within [minFramesPerPx, fit-all],
// and the span lies within the track unless the whole track fits, in which
// case it starts at 0.
func (inst View) clamp(frames int64, widthPx float32) (v View) {
	v = inst
	if widthPx < 1 {
		widthPx = 1
	}
	fit := fitAll(frames, widthPx).FramesPerPx
	if math.IsNaN(v.FramesPerPx) || v.FramesPerPx <= 0 {
		v.FramesPerPx = fit
	}
	if v.FramesPerPx > fit {
		v.FramesPerPx = fit
	}
	if v.FramesPerPx < minFramesPerPx {
		v.FramesPerPx = minFramesPerPx
	}
	span := float64(widthPx) * v.FramesPerPx
	maxFrom := float64(frames) - span
	if maxFrom <= 0 || math.IsNaN(v.FromFrame) {
		v.FromFrame = 0
		return v
	}
	if v.FromFrame < 0 {
		v.FromFrame = 0
	}
	if v.FromFrame > maxFrom {
		v.FromFrame = maxFrom
	}
	return v
}
