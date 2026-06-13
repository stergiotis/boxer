package boxenplot

import (
	"math"

	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
)

// computeBoxWidth returns the width for the box at LV depth d.
// depth 2 (innermost rendered) → base; deeper → base · shrink^(d-2).
// depth < 2 is undefined (depth 1 is the degenerate median, not a box).
func computeBoxWidth(base, shrink float64, depth uint8) float64 {
	if depth < 2 {
		return base
	}
	return base * math.Pow(shrink, float64(depth-2))
}

// paletteT maps an LV depth ∈ [2, maxDepth] to a palette t ∈ [tStart, tEnd].
// The mapping is linear; maxDepth==2 collapses to tStart.
func paletteT(depth, maxDepth uint8, tStart, tEnd float32) float32 {
	if maxDepth <= 2 {
		return tStart
	}
	span := float32(maxDepth - 2)
	u := float32(depth-2) / span
	return tStart + u*(tEnd-tStart)
}

// resolveOutlierMode resolves Auto into Points or Count based on the
// per-tail count vs threshold. Non-Auto inputs pass through.
func resolveOutlierMode(mode OutlierModeE, perTailCount, autoThreshold int64) OutlierModeE {
	if mode != OutlierModeAuto {
		return mode
	}
	if perTailCount >= autoThreshold {
		return OutlierModeCount
	}
	return OutlierModePoints
}

// medianFromLevels finds the depth-1 LV entry (median sentinel) in
// levels. Standard letterval.Levels output has exactly one at index 0,
// but callers may hand-craft or slice the input — defending against
// either avoids drawing the median tick at the wrong height when the
// sentinel has been trimmed off the front.
//
// Falls back to the midpoint of the outermost interval when no depth-1
// entry is present.
func medianFromLevels(levels []letterval.LVLevel) (median float64) {
	for _, lv := range levels {
		if lv.Depth == 1 {
			median = lv.LowerValue
			return
		}
	}
	outer := levels[0]
	median = (outer.LowerValue + outer.UpperValue) / 2
	return
}

// findContainingLevel returns the smallest-depth LV entry (≥ depth 2)
// whose [LowerValue, UpperValue] interval contains y, or nil when y
// falls outside every drawn box. Because LV depths are nested with
// monotonically widening spreads — depth 2 = IQR (Q1..Q3), depth 3 =
// [Q_1/8, Q_7/8], …, deepest = widest — the smallest containing depth
// is the innermost visual ring the cursor sits inside, which is the
// box a reader naturally associates with the hover point.
//
// Depth-1 entries (the median sentinel) span a single value rather
// than a region and are skipped.
func findContainingLevel(levels []letterval.LVLevel, y float64) *letterval.LVLevel {
	if math.IsNaN(y) {
		return nil
	}
	for i := range levels {
		lv := &levels[i]
		if lv.Depth < 2 {
			continue
		}
		if y >= lv.LowerValue && y <= lv.UpperValue {
			return lv
		}
	}
	return nil
}

// deepestLevel returns the deepest LV (≥ 2) in levels, or nil when
// none are present (a depth-1-only slice has no boxes — the
// median-marker case the renderer covers separately). Standard
// letterval.Levels output is monotone in Depth so this is just the
// trailing entry, but the loop tolerates hand-crafted inputs.
func deepestLevel(levels []letterval.LVLevel) *letterval.LVLevel {
	var out *letterval.LVLevel
	for i := range levels {
		lv := &levels[i]
		if lv.Depth < 2 {
			continue
		}
		out = lv
	}
	return out
}

// recoverN estimates the sample size that produced levels by inverting
// the LVLevel.TailCount = floor(n · 2⁻ᵈ) definition. The shallowest
// non-median LV gives the tightest bound: depth 2 yields n ∈
// [4·TailCount, 4·TailCount+3]; depth 3 yields n ∈ [8·TailCount,
// 8·TailCount+7], etc. The recovered value is exact when the original
// n is divisible by 2ᵈ and otherwise rounds down by < 2ᵈ — accurate
// enough for a human-readable status-line readout.
//
// Falls back to the depth-1 entry (TailCount ≈ n/2) for slices that
// only carry the median sentinel; returns 0 for an empty slice.
func recoverN(levels []letterval.LVLevel) int64 {
	for _, lv := range levels {
		if lv.Depth >= 2 {
			return lv.TailCount << lv.Depth
		}
	}
	if len(levels) > 0 {
		return 2 * levels[0].TailCount
	}
	return 0
}
