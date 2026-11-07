// Stand-in for github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color
// used by analysistest fixtures. The call detection is syntactic and does
// not depend on the package's full import path — only the local alias
// (which is the package's name) needs to be `color`.
package color

type Color struct{ rgba uint32 }

func Hex(rgba uint32) (c Color)    { c.rgba = rgba; return }
func RGB(r, g, b uint8) (c Color)  { _, _, _ = r, g, b; return }
func Other(rgba uint32) (c Color)  { c.rgba = rgba; return }
func Paint(c Color, width float32) { _, _ = c, width }
func Fill(packed uint32)           { _ = packed }
