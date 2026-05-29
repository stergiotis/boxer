// Stand-in for the egui2 bindings surface used by analysistest fixtures.
// The L3 detection is syntactic on the selector name — full import path
// is irrelevant.
package c

type Frame struct{}

func AddSpace(px float32) { _ = px }

func (Frame) InnerMargin(px float32) (f Frame) { _ = px; return }
func (Frame) OuterMargin(px float32) (f Frame) { _ = px; return }

func NewFrame() (f Frame) { return }
