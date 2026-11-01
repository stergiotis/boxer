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
// canvas — each shade farther from it than the one below. Hue may wander, as
// a multi-hue LUT's does; the order is carried by distance, which is what a
// dichromat and a greyscale print keep.
func TestHighestLossIsMostSalient(t *testing.T) {
	p := newPalette(5)
	bg := styletokens.NeutralBgFaint
	last := -1.0
	for i, r := range p.rawRamp {
		de := deltaE(r, bg)
		t.Logf("%s: step %d #%s: ΔE vs canvas %.1f, chroma %.1f", styletokens.ActiveTheme(), i, hex(r), de, chroma(r))
		assert.Greater(t, de, last, "step %d is no farther from the canvas than step %d", i, i-1)
		last = de
	}
}

// sequentials is every palette the presets and IDS_PALETTE_SEQUENTIAL can
// select, by the enum and its env key.
var sequentials = []struct {
	seq styletokens.SequentialE
	key string
}{
	{styletokens.SequentialBatlow, "batlow"},
	{styletokens.SequentialLapaz, "lapaz"},
	{styletokens.SequentialOslo, "oslo"},
	{styletokens.SequentialLajolla, "lajolla"},
	{styletokens.SequentialViridis, "viridis"},
	{styletokens.SequentialMagma, "magma"},
	{styletokens.SequentialPlasma, "plasma"},
	{styletokens.SequentialInferno, "inferno"},
	{styletokens.SequentialBatlowK, "batlow_k"},
	{styletokens.SequentialGrayC, "gray_c"},
}

// minClassStepDeltaE is the least step this test holds any palette to, in any
// vision: past the JND, below which the classes would read as one, and what the
// tightest palette reaches. It pins a floor; selectRamp maximises above it.
const minClassStepDeltaE = 7.0

func deltaEUnder(v cvd.Type, a, b styletokens.RGBA8) float64 {
	return labUnder(v, a).deltaE(labUnder(v, b))
}

// TestEveryPaletteYieldsAUsableRamp holds each palette to what the matrix needs
// of its classes, in this process's theme, and logs the measurements.
func TestEveryPaletteYieldsAUsableRamp(t *testing.T) {
	bg, mk := styletokens.NeutralBgFaint, styletokens.NeutralTextExtreme
	for _, tc := range sequentials {
		t.Run(tc.key, func(t *testing.T) {
			ramp := selectRamp(tc.seq, bg, mk, 5)
			require.Len(t, ramp, 5)
			var steps, floors [len(visions)]float64
			for v, ty := range visions {
				steps[v], floors[v] = math.Inf(1), deltaEUnder(ty, ramp[0], bg)
				for i := 1; i < len(ramp); i++ {
					steps[v] = math.Min(steps[v], deltaEUnder(ty, ramp[i], ramp[i-1]))
				}
				assert.GreaterOrEqual(t, steps[v], minClassStepDeltaE, "adjacent classes under %q", ty)
				assert.GreaterOrEqual(t, floors[v], float64(rampFloorDeltaE)-1, "the lowest class vs the canvas under %q", ty)
			}
			assert.GreaterOrEqual(t, floors[0], float64(rampFloorDeltaE), "the floor holds under normal sight")
			last, dotMin, dotDE, inkMin := -1.0, math.Inf(1), math.Inf(1), math.Inf(1)
			ink := styletokens.NeutralTextPrimary
			for i, r := range ramp {
				de := deltaE(r, bg)
				assert.Greater(t, de, last, "class %d is no farther from the canvas than the one below", i)
				last = de
				dotMin = math.Min(dotMin, contrast.Ratio(mk.R, mk.G, mk.B, r.R, r.G, r.B))
				dotDE = math.Min(dotDE, deltaE(mk, r))
				inkMin = math.Min(inkMin, contrast.Ratio(ink.R, ink.G, ink.B, r.R, r.G, r.B))
			}
			assert.GreaterOrEqual(t, dotMin, contrast.AAFloor(contrast.KindUI), "the dot against the class it sits on")
			assert.GreaterOrEqual(t, dotDE, float64(minMarkRampDeltaE))
			floorMin := math.Inf(1)
			for _, f := range floors {
				floorMin = math.Min(floorMin, f)
			}
			top := ramp[len(ramp)-1]
			chromaOrdered := true
			for _, r := range ramp {
				chromaOrdered = chromaOrdered && chroma(top)+0.05 >= chroma(r)
			}
			t.Logf("table %s %-8s normal %4.1f deut %4.1f prot %4.1f trit %4.1f | floor %4.1f (any vision %4.1f) top %4.1f | dot %3.1f:1 ΔE %4.1f | ink %3.1f:1 | top most saturated %v | #%s–#%s",
				styletokens.ActiveTheme(), tc.key, steps[0], steps[1], steps[2], steps[3], floors[0], floorMin, last, dotMin, dotDE, inkMin, chromaOrdered, hex(ramp[0]), hex(top))
		})
	}
}

// A class count past what the constraints have room for still yields a ramp
// that orders by distance from the canvas, rather than none.
func TestRampSurvivesManyClasses(t *testing.T) {
	bg := styletokens.NeutralBgFaint
	for _, n := range []int{1, 2, 12, 40} {
		ramp := selectRamp(styletokens.SequentialBatlow, bg, styletokens.NeutralTextExtreme, n)
		require.Len(t, ramp, n)
		for i := 1; i < n; i++ {
			assert.GreaterOrEqual(t, deltaE(ramp[i], bg), deltaE(ramp[i-1], bg), "n=%d class %d", n, i)
		}
	}
}

// TestRampPaletteFollowsIDS runs in a child per environment, since the env vars
// are cached on first read: the palette is the IDS's resolution, and the result
// is what newPalette draws.
func TestRampPaletteFollowsIDS(t *testing.T) {
	if want := os.Getenv("RISKMATRIX_TEST_WANT_SEQUENTIAL"); want != "" {
		for _, tc := range sequentials {
			if tc.key == want {
				assert.Equal(t, tc.seq, rampSequential())
				p := newPalette(5)
				assert.Equal(t, selectRamp(tc.seq, styletokens.NeutralBgFaint, styletokens.NeutralTextExtreme, 5), p.rawRamp)
				return
			}
		}
		t.Fatalf("unknown palette %q", want)
	}
	for _, tc := range []struct{ access, palette, want string }{
		{"", "", "oslo"},
		{"default", "", "oslo"},
		{"", "not-a-palette", "oslo"},
		{"", "batlow", "batlow"},
		{"default", "batlow", "batlow"},
		{"", "viridis", "viridis"},
		{"monochrome", "", "gray_c"},
		{"high-contrast", "", "batlow_k"},
		{"monochrome", "viridis", "gray_c"},
	} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRampPaletteFollowsIDS$", "-test.v")
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "IDS_") {
				cmd.Env = append(cmd.Env, kv)
			}
		}
		cmd.Env = append(cmd.Env, "RISKMATRIX_TEST_WANT_SEQUENTIAL="+tc.want)
		if tc.access != "" {
			cmd.Env = append(cmd.Env, "IDS_ACCESSIBILITY="+tc.access)
		}
		if tc.palette != "" {
			cmd.Env = append(cmd.Env, "IDS_PALETTE_SEQUENTIAL="+tc.palette)
		}
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%+v\n%s", tc, out)
	}
}

// TestPaletteInTheLightTheme runs the tests above in a child process under
// IMZERO2_THEME=fresh: the theme is fixed per process (ADR-0258 §SD3).
func TestPaletteInTheLightTheme(t *testing.T) {
	if styletokens.ActiveTheme() == styletokens.ThemeFresh {
		t.Skip("already the light theme")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^(TestMarkColourIsNoStatusColour|TestHighestLossIsMostSalient|TestEveryPaletteYieldsAUsableRamp|TestRampSurvivesManyClasses)$", "-test.v")
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
