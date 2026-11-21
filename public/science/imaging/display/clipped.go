package display

import (
	"math"

	"github.com/stergiotis/boxer/public/science/imaging/stats"
)

// ClippedShare is the share of a plane's valid samples that a recipe's
// limits clip (ADR-0401 §SD8), each between 0 and 1.
type ClippedShare struct {
	// Below is the share of valid values strictly below Lo; Above the share
	// strictly above Hi. A value equal to a limit is shown at an end of the
	// ramp, not clipped.
	Below, Above float64
	// Exact is true when the histogram has one bin per code, so the shares
	// are counts. Otherwise they are interpolated within the bin a limit
	// falls in, as if its samples were spread evenly over it, and each is
	// off by at most that bin's share.
	Exact bool
}

// ClippedShares is the share of the valid samples a histogram counted that
// limits clip (ADR-0401 §SD8): below Lo and above Hi, in values — the stored
// samples through the histogram's value transform. For a binned histogram
// a limit outside the range of the counted samples clips nothing or
// everything exactly; within it, the bin it falls in is split in
// proportion, its first and last bins taken to reach the smallest and
// largest sample. It reports ok false for a histogram with no valid sample
// and limits that are not finite or not ascending (Lo = Hi is accepted).
func ClippedShares(h stats.Histogram, limits Limits) (share ClippedShare, ok bool) {
	lo, hi := limits.Lo, limits.Hi
	if h.Valid() == 0 || math.IsNaN(lo) || math.IsNaN(hi) || math.IsInf(lo, 0) || math.IsInf(hi, 0) || lo > hi {
		return share, false
	}
	share.Exact = h.IsExact()
	total := float64(h.Valid())
	if share.Exact {
		var below, above uint64
		for i := range h.Bins() {
			n := h.Count(i)
			if n == 0 {
				continue
			}
			v := h.Value(h.BinStored(i))
			if v < lo {
				below += n
			} else if v > hi {
				above += n
			}
		}
		share.Below, share.Above = float64(below)/total, float64(above)/total
		return share, true
	}
	vmin, vmax, _ := h.Range()
	storedLo, storedHi, _ := h.StoredRange()
	var below, above float64
	switch {
	case lo <= vmin:
	case lo > vmax:
		below = total
	default:
		below = binnedBelow(h, storedLo, storedHi, lo, false)
	}
	switch {
	case hi >= vmax:
	case hi < vmin:
		above = total
	default:
		above = binnedBelow(h, storedLo, storedHi, hi, true)
	}
	share.Below, share.Above = min(below/total, 1), min(above/total, 1)
	return share, true
}

// binnedBelow is the count of a binned histogram's samples whose value lies
// below limit, or above it when above is set: each bin's stored interval,
// the first reaching down to the smallest sample and the last up to the
// largest and every one cut to the samples' range, is taken through the
// value transform and its count split at the limit in proportion.
func binnedBelow(h stats.Histogram, storedLo, storedHi, limit float64, above bool) (n float64) {
	last := h.Bins() - 1
	width := h.BinWidth()
	for i := range h.Bins() {
		c := h.Count(i)
		if c == 0 {
			continue
		}
		a := h.BinStored(i)
		b := a + width
		if i == 0 {
			a = min(a, storedLo)
		}
		if i == last {
			b = max(b, storedHi)
		}
		a, b = max(a, storedLo), min(b, storedHi)
		va, vb := h.Value(a), h.Value(b)
		va, vb = min(va, vb), max(va, vb)
		var frac float64
		switch {
		case vb <= va:
			// A bin of no width: all of it lies on one side.
			if (!above && va < limit) || (above && va > limit) {
				frac = 1
			}
		case above:
			frac = min(max((vb-limit)/(vb-va), 0), 1)
		default:
			frac = min(max((limit-va)/(vb-va), 0), 1)
		}
		n += frac * float64(c)
	}
	return n
}
