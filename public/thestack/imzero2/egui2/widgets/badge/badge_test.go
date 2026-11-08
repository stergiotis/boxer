package badge

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

var allVariants = [...]VariantE{VariantSolid, VariantSoft, VariantOutline, VariantGhost}

func TestDefaultSizeSetsTheLabelBelowAButton(t *testing.T) {
	require.Equal(t, SizeSm, New(nil, "x").size)
	_, _, _, _, _, useSmall := New(nil, "x").resolveLayout()
	require.True(t, useSmall)
}

// A badge that cannot be clicked takes neither of the looks it shares with a
// button (ADR-0273 SD5).
func TestDisplayOnlyBadgeDoesNotLookClickable(t *testing.T) {
	soft := New(nil, "x").Tone(ToneError).Variant(VariantSoft)
	fill, stroke, strokeW, fg := soft.resolveTone(false, false)
	for _, va := range []VariantE{VariantSolid, VariantOutline} {
		f, s, w, g := soft.Variant(va).resolveTone(false, false)
		require.Equal(t, fill, f)
		require.Equal(t, stroke, s)
		require.Equal(t, strokeW, w)
		require.Equal(t, fg, g)
	}
	f, _, _, g := soft.Selected(true).resolveTone(false, false)
	require.Equal(t, fill, f)
	require.Equal(t, fg, g)
}

func TestDisplayOnlyBadgeIgnoresHover(t *testing.T) {
	for _, va := range allVariants {
		b := New(nil, "x").Tone(TonePrimary).Variant(va)
		f, s, w, g := b.resolveTone(false, false)
		fh, sh, wh, gh := b.resolveTone(false, true)
		require.Equal(t, f, fh)
		require.Equal(t, s, sh)
		require.Equal(t, w, wh)
		require.Equal(t, g, gh)
	}
}

func TestClickableBadgeKeepsItsVariantAndChangesOnHover(t *testing.T) {
	check := func(b Fluid) {
		t.Helper()
		f, s, w, _ := b.resolveTone(true, false)
		fh, sh, wh, _ := b.resolveTone(true, true)
		require.NotEqual(t, [2]color.Color{f, s}, [2]color.Color{fh, sh})
		// egui counts the stroke in a Frame's size: a width that changed on
		// hover would move the layout.
		require.Equal(t, w, wh)
		require.Positive(t, w)
	}
	for _, va := range allVariants {
		check(New(nil, "x").Tone(ToneWarning).Variant(va))
		check(New(nil, "x").Tone(ToneWarning).Variant(va).Selected(true))
	}

	solid, _, _, _ := New(nil, "x").Tone(ToneError).Variant(VariantSolid).resolveTone(true, false)
	soft, _, _, _ := New(nil, "x").Tone(ToneError).Variant(VariantSoft).resolveTone(true, false)
	require.NotEqual(t, soft, solid)
}
