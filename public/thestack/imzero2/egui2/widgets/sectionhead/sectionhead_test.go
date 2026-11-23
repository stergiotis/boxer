package sectionhead

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

func TestHeadingRendersWithAndWithoutTip(t *testing.T) {
	t.Cleanup(egui2test.Install())
	require.NotPanics(t, func() {
		New("Model").Send()
		New("Label").Tip("what the label means").Send()
		New("").Send()
	})
}

func TestTipLeavesTheTextAlone(t *testing.T) {
	h := New("Model").Tip("tip")
	require.Equal(t, "Model", h.text)
	require.Equal(t, "tip", h.tip)
}

// A section head ranks between the text it opens and the pane's title, at
// every density.
func TestSizeRanksBetweenBodyAndTitle(t *testing.T) {
	for _, d := range []styletokens.DensityE{styletokens.DensityTight, styletokens.DensityStandard, styletokens.DensityRoomy} {
		sz := sizePt(d)
		require.Greater(t, sz, styletokens.ScaledPt(styletokens.BodyPt, d))
		require.Less(t, sz, styletokens.ScaledPt(styletokens.HeadingPt, d))
	}
}
