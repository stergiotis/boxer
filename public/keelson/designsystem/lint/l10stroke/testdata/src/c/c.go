// Stand-in for the egui2 bindings surface used by analysistest fixtures.
// The L10 detection is syntactic on the selector name and the
// numeric-BasicLit positions; full import paths are irrelevant.
package c

type Color struct{}

type Frame struct{}        // width-first: Stroke(width, col)
type H3Region struct{}     // color-first: Stroke(col, width)
type MapPolyline struct{}  // color-first: Stroke(col, width)
type TintedScope struct{}  // width-first: Stroke(width, col)

func Hex(v uint32) (cl Color) { _ = v; return }

func (Frame) Stroke(width float32, col Color) (f Frame)       { _ = width; _ = col; return }
func (H3Region) Stroke(col Color, width float32) (h H3Region) { _ = col; _ = width; return }
func (MapPolyline) Stroke(col Color, width float32) (m MapPolyline) {
	_ = col
	_ = width
	return
}
func (TintedScope) Stroke(width float32, strokeCol Color) (t TintedScope) {
	_ = width
	_ = strokeCol
	return
}

func NewFrame() (f Frame)               { return }
func NewH3Region() (h H3Region)         { return }
func NewMapPolyline() (m MapPolyline)   { return }
func NewTintedScope() (t TintedScope)   { return }

// Free painter functions: strokeWidth at a per-name arg index,
// everything before it a coordinate, the rounding, or the color.
type PaintEllipse struct{}

func PaintEllipseStroke(cx float32, cy float32, rx float32, ry float32, col Color, strokeWidth float32) (p PaintEllipse) {
	_ = cx
	_ = cy
	_ = rx
	_ = ry
	_ = col
	_ = strokeWidth
	return
}

type PaintCircle struct{}

func PaintCircleStroke(cx float32, cy float32, radius float32, col Color, strokeWidth float32) (p PaintCircle) {
	_ = cx
	_ = cy
	_ = radius
	_ = col
	_ = strokeWidth
	return
}

type PaintRect struct{}

func PaintRectStroke(minX float32, minY float32, maxX float32, maxY float32, rounding float32, col Color, strokeWidth float32) (p PaintRect) {
	_ = minX
	_ = minY
	_ = maxX
	_ = maxY
	_ = rounding
	_ = col
	_ = strokeWidth
	return
}
