// Stand-in for the egui2 bindings surface used by analysistest fixtures.
// The L4 detection is syntactic on the selector name — full import path
// is irrelevant.
package c

type Frame struct{}
type ProgressBar struct{}
type TintedScope struct{}

func (Frame) CornerRadius(px float32) (f Frame)             { _ = px; return }
func (ProgressBar) CornerRadius(px uint8) (p ProgressBar)   { _ = px; return }
func (TintedScope) CornerRadius(px float32) (t TintedScope) { _ = px; return }

func NewFrame() (f Frame)               { return }
func NewProgressBar() (p ProgressBar)   { return }
func NewTintedScope() (t TintedScope)   { return }
