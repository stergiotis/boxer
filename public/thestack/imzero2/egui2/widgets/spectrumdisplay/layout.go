package spectrumdisplay

// rect is a sub-region in widget-local coordinates (origin top-left). The
// AllocateUiAtRect opcode treats these as offsets from the current Ui cursor — the
// treemap idiom — so the widget never needs the viewport-absolute origin.
type rect struct {
	minX, minY, maxX, maxY float32
}

func (r rect) w() float32  { return r.maxX - r.minX }
func (r rect) h() float32  { return r.maxY - r.minY }
func (r rect) valid() bool { return r.maxX-r.minX >= 1 && r.maxY-r.minY >= 1 }

// layoutOpts parameterises partition. Widths/heights are in logical pixels.
type layoutOpts struct {
	leftGutterW float32
	freqGutterH float32
	colorbarW   float32 // 0 hides the colorbar (strip + labels)
	showLine    bool
	splitRatio  float32 // line-panel fraction of the data height, in (0,1)
	lineGapY    float32 // gap between the line panel and the texture
}

// layoutRects is the partition result. Any region may be !valid() when hidden or when
// the box is too small to hold it.
type layoutRects struct {
	leftGutter rect // power/time axis labels (left of the data area)
	linePanel  rect // optional spectrum-line trace (above the texture)
	texture    rect // the waterfall
	colorbar   rect // gradient strip + dB labels (right of the data area)
	freqGutter rect // frequency axis labels (below the data area)
}

// partition splits a W×H widget box into the left gutter, an optional spectrum-line
// panel, the waterfall texture, the colorbar, and the bottom frequency gutter. It is
// deterministic and GUI-free so the geometry is unit-tested, and every region derives
// from the same (W,H) in one pass, so gutters never lag the texture on resize.
func partition(W, H float32, o layoutOpts) layoutRects {
	dataX0 := o.leftGutterW
	dataX1 := W
	if o.colorbarW > 0 {
		dataX1 = W - o.colorbarW
	}
	dataY0 := float32(0)
	dataY1 := H - o.freqGutterH

	tex := rect{dataX0, dataY0, dataX1, dataY1}
	var line rect
	if o.showLine && o.splitRatio > 0 && o.splitRatio < 1 {
		lineH := (dataY1 - dataY0) * o.splitRatio
		line = rect{dataX0, dataY0, dataX1, dataY0 + lineH}
		tex = rect{dataX0, dataY0 + lineH + o.lineGapY, dataX1, dataY1}
	}

	lr := layoutRects{
		texture:    tex,
		linePanel:  line,
		leftGutter: rect{0, dataY0, o.leftGutterW, dataY1},
		freqGutter: rect{dataX0, dataY1, dataX1, H},
	}
	if o.colorbarW > 0 {
		lr.colorbar = rect{W - o.colorbarW, tex.minY, W, tex.maxY}
	}
	return lr
}
