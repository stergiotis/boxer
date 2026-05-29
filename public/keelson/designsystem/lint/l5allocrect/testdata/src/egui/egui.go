// Stand-in for the egui2 Ctx surface used by analysistest fixtures.
package egui

import "iter"

type NilIter struct{}

type Iter = iter.Seq[NilIter]

func nilSeq() (s Iter) {
	s = func(yield func(NilIter) bool) {}
	return
}

type Ctx struct{}

type Scope struct{}

func (Scope) KeepIter() (s Iter) { s = nilSeq(); return }
func (Scope) SendIter() (s Iter) { s = nilSeq(); return }

func (Ctx) Vertical() (s Scope)           { return }
func (Ctx) Horizontal() (s Scope)         { return }
func (Ctx) VerticalCentered() (s Scope)   { return }
func (Ctx) HorizontalCentered() (s Scope) { return }
func (Ctx) HorizontalWrapped() (s Scope)  { return }
func (Ctx) Grid(id string) (s Scope)      { _ = id; return }

func (Ctx) Frame() (s Scope) { return }

func (Ctx) AllocateUiAtRect(x, y, w, h float32) (s Scope) { _, _, _, _ = x, y, w, h; return }

// Hold a global so test fixtures can reference c.<...> without parameter plumbing.
var C = Ctx{}
