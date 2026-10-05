package definition

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

// CheckEffects refuses a definition in which a procedural node has not
// declared what its apply code reaches (ADR-0281 §SD5). Builder factories
// draw and fetchers answer the server, by their kinds, so only procedural
// nodes must say.
func CheckEffects(nodes []ir.NodeI) (err error) {
	var unset []string
	for _, n := range nodes {
		if p, ok := n.(*ir.ProceduralNode); ok && p.Effect == ir.EffectUnset {
			unset = append(unset, string(p.Name))
		}
	}
	if len(unset) > 0 {
		err = eb.Build().Strs("nodes", unset).Errorf("procedural nodes without a declared effect: add WithEffect(ir.EffectLocal) or WithEffect(ir.EffectHost)")
	}
	return
}
