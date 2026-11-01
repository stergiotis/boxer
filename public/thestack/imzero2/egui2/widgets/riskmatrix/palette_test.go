package riskmatrix

import (
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/contrast"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/cvd"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/gen"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/oklab"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

// minMarkRampDeltaE is how far, in OKLab ΔE, a risk dot must stay from every
// shade it can sit on: well past the cross-palette advisory level (ΔE 15) the
// dot is held to against the status colours.
const minMarkRampDeltaE = 25

func deltaE(a, b styletokens.RGBA8) float64 { return cvd.DeltaEOklab(a.R, a.G, a.B, b.R, b.G, b.B) }

func chroma(a styletokens.RGBA8) float64 {
	_, x, y := oklab.LinearSrgbToOklab(
		oklab.SrgbToLinear(float64(a.R)/255), oklab.SrgbToLinear(float64(a.G)/255), oklab.SrgbToLinear(float64(a.B)/255))
	return 100 * math.Hypot(x, y)
}

// TestMarkColourIsNoStatusColour: the dot colour, measured by the colour
// generators' own cross-palette check (ADR-0029 §SD4; ADR-0156, Updates
// 2026-11-01) against the semantic Defaults of every theme, has no finding at
// the gate or the advisory level, stands apart from every ramp step, and
// clears 3:1 against the canvas it is ringed in.
func TestMarkColourIsNoStatusColour(t *testing.T) {
	p := newPalette(5)
	mk := p.rawMark
	gating, advisory, err := gen.CrossPalette("", [][3]uint8{{mk.R, mk.G, mk.B}})
	require.NoError(t, err)
	assert.Empty(t, gating)
	assert.Empty(t, advisory, "the dot colour is within the advisory distance of a status colour")
	for i, r := range p.rawRamp {
		de := deltaE(mk, r)
		t.Logf("%s: dot #%s vs ramp step %d #%s: ΔE %.1f", styletokens.ActiveTheme(), hex(mk), i, hex(r), de)
		assert.GreaterOrEqual(t, de, float64(minMarkRampDeltaE), "dot vs ramp step %d", i)
	}
	bg := styletokens.NeutralBgFaint
	assert.GreaterOrEqual(t, contrast.Ratio(mk.R, mk.G, mk.B, bg.R, bg.G, bg.B), 3.0)
}

// TestHighestLossIsMostSalient: magnitude maps to contrast against the
// canvas — each shade farther from it than the one below — and the
// highest-loss shade is not paler than any other: its chroma is the ramp's
// highest.
func TestHighestLossIsMostSalient(t *testing.T) {
	p := newPalette(5)
	bg := styletokens.NeutralBgFaint
	last := -1.0
	top := p.rawRamp[len(p.rawRamp)-1]
	for i, r := range p.rawRamp {
		de := deltaE(r, bg)
		t.Logf("%s: step %d #%s: ΔE vs canvas %.1f, chroma %.1f", styletokens.ActiveTheme(), i, hex(r), de, chroma(r))
		assert.Greater(t, de, last, "step %d is no farther from the canvas than step %d", i, i-1)
		assert.GreaterOrEqual(t, chroma(top), chroma(r), "the top step is paler than step %d", i)
		last = de
	}
}

// TestPaletteInTheLightTheme runs the two tests above in a child process
// under IMZERO2_THEME=fresh: the theme is fixed per process (ADR-0258 §SD3).
func TestPaletteInTheLightTheme(t *testing.T) {
	if styletokens.ActiveTheme() == styletokens.ThemeFresh {
		t.Skip("already the light theme")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^(TestMarkColourIsNoStatusColour|TestHighestLossIsMostSalient)$", "-test.v")
	cmd.Env = append(os.Environ(), "IMZERO2_THEME=fresh")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Contains(t, string(out), "fresh: step", "the child did not run under the light theme")
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "ΔE") {
			t.Log(strings.TrimSpace(l))
		}
	}
}

func hex(c styletokens.RGBA8) string {
	const digits = "0123456789abcdef"
	b := []byte{digits[c.R>>4], digits[c.R&15], digits[c.G>>4], digits[c.G&15], digits[c.B>>4], digits[c.B&15]}
	return string(b)
}
