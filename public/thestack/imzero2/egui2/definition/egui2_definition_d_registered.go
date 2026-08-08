package definition

import (
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

// definitionsRegistered holds nodes whose apply pushes into a process-global
// register that some later call drains, rather than drawing where they are
// emitted.
//
// It is empty, and that is the intended state. Its only occupants were
// `nodeDir` / `nodeLeaf` / `nodeDirClose`, drained by `tree` — the
// egui_ltreeview binding retired in ADR-0176. The shape it represents is the
// one that ADR ruled against: emission decoupled from placement, so two trees
// in one frame had to alternate, and Go re-marshalled every node every frame
// because the expansion state lived on the Rust side.
//
// The category stays because the generator takes one slice per category and an
// empty one costs nothing — and because a future node that genuinely needs a
// drain protocol should land here, next to this note about what it implies.
func definitionsRegistered() (registered []*ir.BuilderFactoryNode) {
	return nil
}
