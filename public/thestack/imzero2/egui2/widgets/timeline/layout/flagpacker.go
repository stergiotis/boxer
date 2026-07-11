package layout

import (
	"sort"
)

// PackFlagRows staggers annotation flags into horizontal rows so flags
// whose screen-x extents would collide never share a row — the annotation
// counterpart of PackLanes. Input is one center-x per flag (screen pixels,
// index-aligned with the caller's annotation slice) plus the uniform flag
// width; output is the row index per flag (same index alignment) and the
// number of rows used.
//
// Strategy: flags are visited in ascending center-x order (stable, so
// equal-x flags keep input order) and placed in the lowest-indexed row
// whose rightmost flag sits at least flagW away — first-fit rather than
// PackLanes' earliest-freed, chosen for visual calm: the top row stays
// densest and a flag only drops a row when it must. Centers exactly flagW
// apart count as fitting (the rects touch edge-to-edge without overlap).
//
// maxRows caps the stagger (values < 1 are treated as 1). A flag that fits
// no row within the cap degrades to the row whose rightmost flag center is
// smallest — the row where the forced overlap is least — with ties toward
// the lower row index. Degraded flags MAY overlap visually within that
// row; the cap trades unbounded band growth for the pre-stagger overlap
// behaviour at extreme density.
//
// flagW <= 0 disables collision detection entirely (every flag lands in
// row 0). Coordinates are expected finite; NaN/Inf inputs yield
// unspecified row assignments.
func PackFlagRows(centersPx []float64, flagW float64, maxRows int32) (rows []int32, rowCount int32) {
	if len(centersPx) == 0 {
		return
	}
	if maxRows < 1 {
		maxRows = 1
	}
	order := make([]int, len(centersPx))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return centersPx[order[a]] < centersPx[order[b]]
	})

	rows = make([]int32, len(centersPx))
	// Per open row: center-x of its rightmost flag. Visited in ascending-x
	// order, so "fits after the rightmost" is the full non-collision check.
	rightmost := make([]float64, 0, min(int(maxRows), len(centersPx)))
	for _, idx := range order {
		x := centersPx[idx]
		placed := false
		for r, last := range rightmost {
			if x-last >= flagW {
				rows[idx] = int32(r)
				rightmost[r] = x
				placed = true
				break
			}
		}
		if placed {
			continue
		}
		if int32(len(rightmost)) < maxRows {
			rows[idx] = int32(len(rightmost))
			rightmost = append(rightmost, x)
			continue
		}
		best := 0
		for r := 1; r < len(rightmost); r++ {
			if rightmost[r] < rightmost[best] {
				best = r
			}
		}
		rows[idx] = int32(best)
		rightmost[best] = x
	}
	rowCount = int32(len(rightmost))
	return
}
