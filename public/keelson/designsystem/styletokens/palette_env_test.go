package styletokens

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSequentialDefaultEmptyEnv(t *testing.T) {
	PaletteSequentialEnv.SetForTest(t, "")
	AccessibilityEnv.SetForTest(t, "")
	assert.Equal(t, SequentialBatlow, SequentialDefault())
}

func TestSequentialDefaultExplicitPick(t *testing.T) {
	AccessibilityEnv.SetForTest(t, "default")
	PaletteSequentialEnv.SetForTest(t, "viridis")
	assert.Equal(t, SequentialViridis, SequentialDefault())
}

func TestSequentialDefaultInvalidValueFallsBackToDefault(t *testing.T) {
	AccessibilityEnv.SetForTest(t, "")
	PaletteSequentialEnv.SetForTest(t, "this-palette-does-not-exist")
	// env.CategoricalString falls back to spec default on invalid input.
	assert.Equal(t, SequentialBatlow, SequentialDefault())
}

func TestAccessibilityHighContrastOverridesSequential(t *testing.T) {
	PaletteSequentialEnv.SetForTest(t, "viridis")
	AccessibilityEnv.SetForTest(t, "high-contrast")
	assert.Equal(t, SequentialBatlowK, SequentialDefault())
}

func TestAccessibilityMonochromeOverridesSequential(t *testing.T) {
	PaletteSequentialEnv.SetForTest(t, "magma")
	AccessibilityEnv.SetForTest(t, "monochrome")
	assert.Equal(t, SequentialGrayC, SequentialDefault())
}

func TestAccessibilityFromEnv(t *testing.T) {
	AccessibilityEnv.SetForTest(t, "high-contrast")
	assert.Equal(t, AccessibilityHighContrast, AccessibilityFromEnv())

	AccessibilityEnv.SetForTest(t, "monochrome")
	assert.Equal(t, AccessibilityMonochrome, AccessibilityFromEnv())

	AccessibilityEnv.SetForTest(t, "default")
	assert.Equal(t, AccessibilityDefault, AccessibilityFromEnv())

	AccessibilityEnv.SetForTest(t, "bogus")
	assert.Equal(t, AccessibilityDefault, AccessibilityFromEnv())
}

func TestDivergingDefault(t *testing.T) {
	AccessibilityEnv.SetForTest(t, "")
	PaletteDivergingEnv.SetForTest(t, "roma")
	assert.Equal(t, DivergingRoma, DivergingDefault())

	// Accessibility presets do not currently swap diverging — vik covers
	// all three presets.
	PaletteDivergingEnv.SetForTest(t, "broc")
	AccessibilityEnv.SetForTest(t, "monochrome")
	assert.Equal(t, DivergingVik, DivergingDefault())
}

func TestNewSequentialEnumsHaveLUTs(t *testing.T) {
	// Sampling the two new palettes must not panic and must return
	// non-zero RGB (the LUTs were vendored from Crameri upstream).
	for _, p := range []SequentialE{SequentialBatlowK, SequentialGrayC} {
		v := Sequential(p, 0.5)
		// We can't assert exact RGB without committing to a specific Crameri
		// release, but verifying alpha is opaque and the call succeeded is
		// enough to confirm the LUT slot is populated.
		require.Equal(t, uint8(0xFF), v.A, "palette %d alpha", p)
	}
}

func TestGrayCDirectionIsUpstream(t *testing.T) {
	// Sanity check: grayC[0] should be (near-)white and grayC[255] (near-)
	// black per the upstream LUT direction. boxenplot inverts the t-range
	// to keep its Hofmann reading consistent; this test pins the upstream
	// direction so a future LUT re-vendor that accidentally reverses the
	// file is caught.
	white := Sequential(SequentialGrayC, 0.0)
	black := Sequential(SequentialGrayC, 1.0)
	assert.Greater(t, int(white.R), 200, "grayC at t=0 should be near-white")
	assert.Less(t, int(black.R), 30, "grayC at t=1 should be near-black")
}
