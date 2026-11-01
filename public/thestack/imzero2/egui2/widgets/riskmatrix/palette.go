package riskmatrix

import (
	"math"
	"sort"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/contrast"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/cvd"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/oklab"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

const (
	// rampSamples is the data-encoding LUTs' resolution: Sequential reads
	// entry round(t·255).
	rampSamples = 256
	// rampFloorDeltaE is the least OKLab ΔE the lowest class keeps from the
	// surface: four times the OKLab just-noticeable difference of about 2, so
	// a low-loss cell reads as a cell and not as a gap in the grid. The search
	// counts the surface as a stop on the scale and usually lands above it.
	rampFloorDeltaE = 8.0
	// rampMinRise is the least ΔE each class must gain on the one below in
	// distance from the surface: that JND, so "farther" is visible and a step
	// cannot be hue alone.
	rampMinRise = 2.0
)

// rampSequential is the palette the shade classes come from: whatever
// IDS_ACCESSIBILITY or a valid IDS_PALETTE_SEQUENTIAL selects, and Oslo with
// neither. SequentialDefault cannot tell an unset palette from an explicit
// Batlow, so the choice is read here. Oslo rather than the IDS default because
// a loss scale should not change hue: Batlow's top class on the light theme is
// a green (ADR-0122's Updates has the measurement).
func rampSequential() styletokens.SequentialE {
	if styletokens.AccessibilityFromEnv() != styletokens.AccessibilityDefault {
		return styletokens.SequentialDefault()
	}
	if raw, set := styletokens.PaletteSequentialEnv.Lookup(); set && styletokens.PaletteSequentialEnv.IsAllowed(raw) {
		return styletokens.SequentialDefault()
	}
	return styletokens.SequentialOslo
}

type rampKey struct {
	seq styletokens.SequentialE
	n   int
}

// rampCache holds the classes per palette and class count. Surface and mark
// need not be in the key: the theme is fixed for the life of the process
// (ADR-0258 §SD3).
var rampCache sync.Map

// rampFor is selectRamp for this process's theme, computed once.
func rampFor(seq styletokens.SequentialE, n int) []styletokens.RGBA8 {
	k := rampKey{seq: seq, n: n}
	if v, ok := rampCache.Load(k); ok {
		return v.([]styletokens.RGBA8)
	}
	out := selectRamp(seq, styletokens.NeutralBgFaint, styletokens.NeutralTextExtreme, n)
	rampCache.Store(k, out)
	return out
}

// visions are normal sight (the zero value) and the simulated dichromacies.
var visions = [...]cvd.Type{"", cvd.Deuteranopia, cvd.Protanopia, cvd.Tritanopia}

type labVec [3]float64

func (inst labVec) deltaE(o labVec) float64 {
	dL, dA, dB := inst[0]-o[0], inst[1]-o[1], inst[2]-o[2]
	return 100 * math.Sqrt(dL*dL+dA*dA+dB*dB)
}

func labUnder(v cvd.Type, c styletokens.RGBA8) labVec {
	r, g, b := c.R, c.G, c.B
	if v != "" {
		r, g, b = cvd.Simulate(v, r, g, b)
	}
	L, A, B := oklab.LinearSrgbToOklab(
		oklab.SrgbToLinear(float64(r)/255), oklab.SrgbToLinear(float64(g)/255), oklab.SrgbToLinear(float64(b)/255))
	return labVec{L, A, B}
}

// rampStop is one LUT entry, measured.
type rampStop struct {
	col styletokens.RGBA8
	lab [len(visions)]labVec
	// fromSurface is the normal-vision ΔE from the surface; surfaceSep the
	// least ΔE from it under any vision.
	fromSurface, surfaceSep float64
	// away is the lightness gained toward the mark's side of the surface.
	away   float64
	usable bool
}

// selectRamp picks n classes from a sequential LUT for one surface and mark.
//
// A class must lie on the mark's side of the surface in lightness, so the ramp
// runs from the surface toward the dot's ink and never past it; keep rampFloorDeltaE
// from the surface; and keep 3:1 (WCAG's non-text floor) from the dot drawn on it.
// Among the sequences of such entries, monotone along the LUT, with lightness
// and distance from the surface rising class by class, it takes the one whose
// least step is largest, a step being the smallest ΔE between neighbours — the
// surface counting as the neighbour below the lowest class — over normal sight
// and the three simulated dichromacies. Lightness is the axis dichromacy keeps,
// so this finds the same shape on any LUT, including a reversed one (grayC) and
// one that passes through the surface's own colour (oslo).
func selectRamp(seq styletokens.SequentialE, surface, mark styletokens.RGBA8, n int) []styletokens.RGBA8 {
	var bg [len(visions)]labVec
	for v, ty := range visions {
		bg[v] = labUnder(ty, surface)
	}
	polarity := 1.0
	if labUnder("", mark)[0] < bg[0][0] {
		polarity = -1
	}
	minDot := contrast.AAFloor(contrast.KindUI)
	stops := make([]rampStop, rampSamples)
	for i := range stops {
		s := &stops[i]
		s.col = styletokens.Sequential(seq, float32(i)/float32(rampSamples-1))
		s.surfaceSep = math.Inf(1)
		for v, ty := range visions {
			s.lab[v] = labUnder(ty, s.col)
			s.surfaceSep = math.Min(s.surfaceSep, s.lab[v].deltaE(bg[v]))
		}
		s.fromSurface = s.lab[0].deltaE(bg[0])
		s.away = (s.lab[0][0] - bg[0][0]) * polarity
		s.usable = s.away > 0 && s.fromSurface >= rampFloorDeltaE &&
			contrast.Ratio(mark.R, mark.G, mark.B, s.col.R, s.col.G, s.col.B) >= minDot
	}
	sep := make([]float32, rampSamples*rampSamples)
	for i := range stops {
		if !stops[i].usable {
			continue
		}
		for j := i + 1; j < rampSamples; j++ {
			if !stops[j].usable {
				continue
			}
			d := math.Inf(1)
			for v := range visions {
				d = math.Min(d, stops[i].lab[v].deltaE(stops[j].lab[v]))
			}
			sep[i*rampSamples+j], sep[j*rampSamples+i] = float32(d), float32(d)
		}
	}
	path := searchRamp(stops, sep, n)
	if path == nil {
		path = evenRamp(stops, n)
	}
	out := make([]styletokens.RGBA8, n)
	for k, i := range path {
		out[k] = stops[i].col
	}
	return out
}

// searchRamp is the max-min dynamic programme over the stops, in both LUT
// directions. path is the sequence whose least step is largest, nil when no
// sequence of n stops satisfies the constraints.
func searchRamp(stops []rampStop, sep []float32, n int) (path []int) {
	best := make([]float64, rampSamples)
	next := make([]float64, rampSamples)
	from := make([][]int16, n)
	for k := range from {
		from[k] = make([]int16, rampSamples)
	}
	score := -1.0
	for _, dir := range [...]int{1, -1} {
		for i := range stops {
			best[i] = -1
			if stops[i].usable {
				best[i] = stops[i].surfaceSep
			}
		}
		for k := 1; k < n; k++ {
			for i := range stops {
				next[i] = -1
				if !stops[i].usable {
					continue
				}
				for j := range stops {
					if (i-j)*dir <= 0 || best[j] < 0 {
						continue
					}
					if stops[i].fromSurface-stops[j].fromSurface < rampMinRise || stops[i].away <= stops[j].away {
						continue
					}
					if x := math.Min(best[j], float64(sep[i*rampSamples+j])); x > next[i] {
						next[i], from[k][i] = x, int16(j)
					}
				}
			}
			best, next = next, best
		}
		for i, v := range best {
			if v > score {
				score = v
				path = make([]int, n)
				cur := i
				for k := n - 1; k >= 0; k-- {
					path[k] = cur
					cur = int(from[k][cur])
				}
			}
		}
	}
	return path
}

// evenRamp spaces n classes over the usable stops by distance from the
// surface: the fallback for more classes than the constraints have room for.
// With no usable stop at all it spaces them over the whole LUT.
func evenRamp(stops []rampStop, n int) []int {
	idx := make([]int, 0, len(stops))
	for i := range stops {
		if stops[i].usable {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		for i := range stops {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool { return stops[idx[a]].fromSurface < stops[idx[b]].fromSurface })
	out := make([]int, n)
	for k := range out {
		pos := 0
		if n > 1 {
			pos = k * (len(idx) - 1) / (n - 1)
		}
		out[k] = idx[pos]
	}
	return out
}
