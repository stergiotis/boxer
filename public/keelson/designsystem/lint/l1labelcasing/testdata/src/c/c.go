// Stand-in for the egui2 bindings surface used by analysistest fixtures.
// The L1 detection is syntactic on the selector name + receiver ident — full
// import path is irrelevant.
package c

type LabelFluid struct{}

func (LabelFluid) Send() {}

func Label(text string) (l LabelFluid) { _ = text; return }
