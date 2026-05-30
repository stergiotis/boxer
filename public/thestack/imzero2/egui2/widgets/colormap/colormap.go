// Package colormap maps a stream of float32 samples to packed RGBA
// (0xRRGGBBAA) u32 colors, with NaN/Inf and out-of-range substitution
// (matplotlib set_bad / set_under / set_over style), intensity scaling
// (linear, log10, or dB-from-linear-power), and linearly-interpolated
// palette lookup.
//
// See doc/adr/0009-imzero2-scrolling-texture-widget.md SD1, SD5, SD6 for
// why the colormap lives in Go and not Rust. The package is widget-neutral:
// the heatmapscroll wrapper composes it with the scrollingTexture widget,
// but it is equally usable by any caller that needs to convert intensity
// values to colors (e.g. a static heatmap, legend strip, or export buffer).
//
// # Typical use
//
//	cfg := colormap.NewConfig(colormap.Viridis8, 0, 1)
//	cfg.BadColor = color.NRGBA{A: 0} // NaN → transparent
//	dst := make([]uint32, len(samples))
//	stats := cfg.Map(samples, dst)
//	if stats.BadSamples > 0 { ... }
//
// The RGBA layout matches the scrollingTexture IDL: big-endian u32 with
// 0xRR in the top byte, 0xAA in the bottom. Alpha is preserved from the
// palette; use palette entries with a=0xff for fully opaque heatmaps.
package colormap

import (
	"image/color"
	"math"
)

// ScaleE selects how an input float32 value is transformed before it is
// normalized into the palette's [0, 1] lookup range.
type ScaleE uint8

const (
	// ScaleLinearE: t = (v - DataMin) / (DataMax - DataMin).
	ScaleLinearE ScaleE = iota
	// ScaleLogE: t = (log10(v) - log10(DataMin)) / (log10(DataMax) - log10(DataMin)).
	// DataMin and DataMax must both be strictly positive; non-positive
	// samples count as underflow.
	ScaleLogE
	// ScaleDbE: converts samples (assumed to be linear power or magnitude)
	// to decibels via 10*log10(v), then normalizes linearly against
	// [DataMin, DataMax] — which are interpreted as dB values (e.g. -80, 0).
	// Non-positive samples count as underflow.
	ScaleDbE
)

// Config is the full f32 → RGBA mapping configuration. A zero-valued
// Config is not usable; construct via NewConfig and tune the three bad/
// under/over colors as needed.
//
// The Palette is interpreted as equispaced stops at t ∈ {0, 1/(n-1), …, 1}
// with linear RGBA interpolation between neighbouring stops. A 2-stop
// palette is the minimum; longer palettes produce smoother gradients
// without changing the hot-loop cost (still two fetches + four lerps).
type Config struct {
	// Palette holds RGBA u32 stops in 0xRRGGBBAA layout. Length ≥ 2.
	Palette []uint32
	// DataMin / DataMax delimit the sample range that maps to [0, 1] of
	// the palette. Samples outside this range are recoloured via
	// UnderflowColor / OverflowColor.
	DataMin float64
	DataMax float64
	// Scale selects Linear / Log / Db behaviour (see ScaleE docs).
	Scale ScaleE
	// BadColor substitutes samples that are NaN or ±Inf (matplotlib
	// set_bad equivalent). Default transparent (Map zero-allocs on this).
	BadColor color.NRGBA
	// UnderflowColor substitutes samples strictly below DataMin (or
	// non-positive in Log/Db mode). Default transparent.
	UnderflowColor color.NRGBA
	// OverflowColor substitutes samples strictly above DataMax.
	// Default transparent.
	OverflowColor color.NRGBA
}

// NewConfig constructs a Config with a linear scale, the given palette
// (must have ≥ 2 stops), the given data range (min < max), and all
// three substitution colours set to fully transparent.
func NewConfig(palette []uint32, min, max float64) *Config {
	if len(palette) < 2 {
		panic("colormap: palette must have at least 2 stops")
	}
	if !(min < max) {
		panic("colormap: NewConfig requires min < max")
	}
	return &Config{
		Palette: palette,
		DataMin: min,
		DataMax: max,
		Scale:   ScaleLinearE,
	}
}

// Range returns the data range [DataMin, DataMax] this config maps onto the
// palette. A legend widget uses it as the value axis.
func (inst *Config) Range() (min, max float64) { return inst.DataMin, inst.DataMax }

// IsLog reports whether the scale is logarithmic, so a legend can place
// log-spaced ticks. Db is linear in decibels, so it reports false.
func (inst *Config) IsLog() bool { return inst.Scale == ScaleLogE }

// Normalize returns the 0..1 palette position for value under the configured
// scale, clamped to [0, 1] — the single-value form of the per-scale math in Map.
// Non-positive values under Log/Db, and degenerate ranges, return 0.
func (inst *Config) Normalize(value float64) (t float64) {
	switch inst.Scale {
	case ScaleLogE:
		if value <= 0 || inst.DataMin <= 0 || inst.DataMax <= 0 {
			return 0
		}
		logMin := math.Log10(inst.DataMin)
		logSpan := math.Log10(inst.DataMax) - logMin
		if !(logSpan > 0) {
			return 0
		}
		t = (math.Log10(value) - logMin) / logSpan
	case ScaleDbE:
		span := inst.DataMax - inst.DataMin
		if !(span > 0) || value <= 0 {
			return 0
		}
		t = (10*math.Log10(value) - inst.DataMin) / span
	default: // ScaleLinearE
		span := inst.DataMax - inst.DataMin
		if !(span > 0) {
			return 0
		}
		t = (value - inst.DataMin) / span
	}
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return
}

// At returns the interpolated palette colour (0xRRGGBBAA) for value — the
// gradient sample a legend draws. Uses the same Normalize + palette lerp as Map,
// so the legend matches the rendered texture exactly.
func (inst *Config) At(value float64) uint32 {
	return paletteLerp(inst.Palette, inst.Normalize(value))
}

// IndexAt quantizes value to the nearest of n equispaced palette slots. Used by
// callers (e.g. treemap) that key pre-derived per-slot data off the colormap.
func (inst *Config) IndexAt(value float64, n int) (idx int) {
	if n <= 1 {
		return 0
	}
	idx = int(inst.Normalize(value) * float64(n-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return
}

// ColumnStats carries per-Map counts of samples that fell into each
// substitution bucket. Useful for test assertions ("expect BadSamples=0
// on this fixture") and production dashboards ("underflow rate over the
// last 10 s" via an external counter).
type ColumnStats struct {
	BadSamples uint32 // NaN or ±Inf
	Underflow  uint32 // v < DataMin (or v <= 0 under Log/Db)
	Overflow   uint32 // v > DataMax
}

// Add accumulates another ColumnStats' counts into this one.
// Useful for summing stats across many column pushes.
func (inst *ColumnStats) Add(other ColumnStats) {
	inst.BadSamples += other.BadSamples
	inst.Underflow += other.Underflow
	inst.Overflow += other.Overflow
}

// Map writes len(src) RGBA u32 colours into dst, applying the configured
// scale + palette + substitution pipeline. Returns per-call stats.
// dst must be at least as long as src; no allocation beyond that.
//
// Not goroutine-safe: callers sharing a Config across goroutines must
// synchronize externally. The Palette slice is read in the hot loop; do
// not mutate it concurrently with Map.
func (inst *Config) Map(src []float32, dst []uint32) (stats ColumnStats) {
	if len(dst) < len(src) {
		panic("colormap: Map dst shorter than src")
	}
	paletteLen := len(inst.Palette)
	if paletteLen < 2 {
		panic("colormap: palette has fewer than 2 stops")
	}
	badRGBA := nrgbaToRGBAu32(inst.BadColor)
	underRGBA := nrgbaToRGBAu32(inst.UnderflowColor)
	overRGBA := nrgbaToRGBAu32(inst.OverflowColor)

	switch inst.Scale {
	case ScaleLinearE:
		span := inst.DataMax - inst.DataMin
		if !(span > 0) {
			panic("colormap: Scale=Linear requires DataMax > DataMin")
		}
		invSpan := 1.0 / span
		min := inst.DataMin
		for i, v := range src {
			f := float64(v)
			switch {
			case math.IsNaN(f) || math.IsInf(f, 0):
				stats.BadSamples++
				dst[i] = badRGBA
			case f < min:
				stats.Underflow++
				dst[i] = underRGBA
			case f > inst.DataMax:
				stats.Overflow++
				dst[i] = overRGBA
			default:
				t := (f - min) * invSpan
				dst[i] = paletteLerp(inst.Palette, t)
			}
		}
	case ScaleLogE:
		if !(inst.DataMin > 0) || !(inst.DataMax > 0) {
			panic("colormap: Scale=Log requires DataMin > 0 and DataMax > 0")
		}
		logMin := math.Log10(inst.DataMin)
		logSpan := math.Log10(inst.DataMax) - logMin
		if !(logSpan > 0) {
			panic("colormap: Scale=Log requires DataMax > DataMin")
		}
		invLogSpan := 1.0 / logSpan
		for i, v := range src {
			f := float64(v)
			switch {
			case math.IsNaN(f) || math.IsInf(f, 0):
				stats.BadSamples++
				dst[i] = badRGBA
			case f <= 0 || f < inst.DataMin:
				stats.Underflow++
				dst[i] = underRGBA
			case f > inst.DataMax:
				stats.Overflow++
				dst[i] = overRGBA
			default:
				t := (math.Log10(f) - logMin) * invLogSpan
				dst[i] = paletteLerp(inst.Palette, t)
			}
		}
	case ScaleDbE:
		span := inst.DataMax - inst.DataMin
		if !(span > 0) {
			panic("colormap: Scale=Db requires DataMax > DataMin")
		}
		invSpan := 1.0 / span
		for i, v := range src {
			f := float64(v)
			switch {
			case math.IsNaN(f) || math.IsInf(f, 0):
				stats.BadSamples++
				dst[i] = badRGBA
			case f <= 0:
				stats.Underflow++
				dst[i] = underRGBA
			default:
				dBVal := 10.0 * math.Log10(f)
				switch {
				case dBVal < inst.DataMin:
					stats.Underflow++
					dst[i] = underRGBA
				case dBVal > inst.DataMax:
					stats.Overflow++
					dst[i] = overRGBA
				default:
					t := (dBVal - inst.DataMin) * invSpan
					dst[i] = paletteLerp(inst.Palette, t)
				}
			}
		}
	default:
		panic("colormap: unknown Scale value")
	}
	return
}

// paletteLerp samples palette at t ∈ [0, 1] with per-channel linear
// interpolation between the two nearest stops. t is assumed pre-clamped
// by the caller; t outside [0, 1] clamps to the nearest endpoint.
func paletteLerp(palette []uint32, t float64) uint32 {
	n := len(palette) - 1
	switch {
	case t <= 0:
		return palette[0]
	case t >= 1:
		return palette[n]
	}
	scaled := t * float64(n)
	idx := int(scaled)
	if idx >= n {
		return palette[n]
	}
	frac := scaled - float64(idx)
	return lerpRGBA(palette[idx], palette[idx+1], frac)
}

// lerpRGBA blends two 0xRRGGBBAA values linearly in each channel.
// Input t ∈ [0, 1]; output has nearest-integer rounding per channel.
func lerpRGBA(a, b uint32, t float64) uint32 {
	ar := float64((a >> 24) & 0xff)
	ag := float64((a >> 16) & 0xff)
	ab := float64((a >> 8) & 0xff)
	aa := float64(a & 0xff)
	br := float64((b >> 24) & 0xff)
	bg := float64((b >> 16) & 0xff)
	bb := float64((b >> 8) & 0xff)
	ba := float64(b & 0xff)
	u := 1.0 - t
	r := uint32(ar*u + br*t + 0.5)
	g := uint32(ag*u + bg*t + 0.5)
	bl := uint32(ab*u + bb*t + 0.5)
	al := uint32(aa*u + ba*t + 0.5)
	return (r << 24) | (g << 16) | (bl << 8) | al
}

// nrgbaToRGBAu32 packs a color.NRGBA into the 0xRRGGBBAA layout used
// throughout this package and the scrollingTexture IDL.
func nrgbaToRGBAu32(c color.NRGBA) uint32 {
	return (uint32(c.R) << 24) | (uint32(c.G) << 16) | (uint32(c.B) << 8) | uint32(c.A)
}
