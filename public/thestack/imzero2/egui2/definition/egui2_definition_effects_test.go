package definition

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

func TestEveryProceduralNodeDeclaresAnEffect(t *testing.T) {
	require.NoError(t, CheckEffects(Definitions()))
}

func TestCheckEffectsRefusesAnUndeclaredNode(t *testing.T) {
	err := CheckEffects([]ir.NodeI{&ir.ProceduralNode{Name: "undeclared"}})
	assert.Error(t, err)
}

// The nodes whose apply code reaches beyond the UI. A capture replay skips
// them (ADR-0281 §SD5); a change here is a change to what a capture repeats.
func TestHostEffectNodes(t *testing.T) {
	var host []string
	for _, n := range Definitions() {
		if p, ok := n.(*ir.ProceduralNode); ok && p.Effect == ir.EffectHost {
			host = append(host, string(p.Name))
		}
	}
	assert.ElementsMatch(t, []string{
		"contextSendViewPortCommandClose", "copyTextToClipboard", "exportSvg", "exportSvgWindow",
		"prepareNextFrame", "requestScreenshot", "requestScreenshotRect", "setAnimationFreeze",
		"setVideoPipeline", "windowPlace",
	}, host)
}
