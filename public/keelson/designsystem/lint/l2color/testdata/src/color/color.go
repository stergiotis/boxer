// Stand-in for github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color
// used by analysistest fixtures. The lint detection is syntactic and does
// not depend on the package's full import path — only the local alias
// (which is the package's name) needs to be `color`.
package color

type Color struct{}

func RGB(r, g, b uint8) (c Color)            { _, _, _ = r, g, b; return }
func RGBA(r, g, b, a uint8) (c Color)        { _, _, _, _ = r, g, b, a; return }
func Other(r, g, b uint8) (c Color)          { _, _, _ = r, g, b; return }
