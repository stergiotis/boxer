package jpeg2000

// The 2D_SR procedure of Annex F for the reversible 5/3 filter: interleave
// the four sub-bands of a level into the coordinates of the next lower LL
// band, then reconstruct every row and every column with the lifting steps
// of F.3.8.2 over a periodic symmetric extension.

// sr2D reconstructs (lev−1)LL over [u0,u1)×[v0,v1) from ll, hl, lh, hh,
// whose own coordinate ranges follow from (B-15). out has (u1−u0)×(v1−v0)
// entries in row-major order; scratch is reused across calls.
func sr2D(ll, hl, lh, hh *subBand, u0, u1, v0, v1 int32, out []int32, scratch *[]int32) {
	w := int(u1 - u0)
	h := int(v1 - v0)
	if w <= 0 || h <= 0 {
		return
	}
	// 2D_INTERLEAVE: a(2ub, 2vb) = LL, a(2ub+1, 2vb) = HL, a(2ub, 2vb+1) = LH,
	// a(2ub+1, 2vb+1) = HH, in absolute coordinates.
	place := func(b *subBand, ox, oy int32) {
		if b == nil || b.coeffs == nil {
			return
		}
		bw := int(b.x1 - b.x0)
		for vb := b.y0; vb < b.y1; vb++ {
			y := 2*vb + oy - v0
			if y < 0 || int(y) >= h {
				continue
			}
			row := b.coeffs[int(vb-b.y0)*bw : int(vb-b.y0+1)*bw]
			for i, c := range row {
				x := 2*(b.x0+int32(i)) + ox - u0
				if x < 0 || int(x) >= w {
					continue
				}
				out[int(y)*w+int(x)] = c
			}
		}
	}
	place(ll, 0, 0)
	place(hl, 1, 0)
	place(lh, 0, 1)
	place(hh, 1, 1)
	// HOR_SR then VER_SR.
	need := max(w, h) + 8
	if cap(*scratch) < 2*need {
		*scratch = make([]int32, 2*need)
	}
	y := (*scratch)[:need]
	x := (*scratch)[need : 2*need]
	for r := 0; r < h; r++ {
		row := out[r*w : (r+1)*w]
		sr1D(row, u0, u1, y, x)
	}
	col := y[:h]
	for c := 0; c < w; c++ {
		for r := 0; r < h; r++ {
			col[r] = out[r*w+c]
		}
		sr1D(col, v0, v1, x, (*scratch)[:0])
		for r := 0; r < h; r++ {
			out[r*w+c] = col[r]
		}
	}
}

// sr1D is 1D_SR for the 5/3 reversible filter on the signal sig covering
// coordinates [i0, i1). ext and _ are scratch space of at least
// len(sig)+8 entries; the result replaces sig.
func sr1D(sig []int32, i0, i1 int32, ext []int32, _ []int32) {
	n := int(i1 - i0)
	if n == 1 {
		// F.3.6: a single sample is itself when its coordinate is even and
		// half when odd.
		if i0&1 == 1 {
			sig[0] /= 2
		}
		return
	}
	// 1D_EXTR with two samples each side (at or above the minima of Table
	// F.2), by periodic symmetric extension about the end samples.
	const pad = 2
	ext = ext[:n+2*pad]
	copy(ext[pad:], sig)
	period := 2 * (n - 1)
	reflect := func(k int) (idx int) {
		// k is relative to i0 and may be outside [0, n).
		idx = k % period
		if idx < 0 {
			idx += period
		}
		if idx >= n {
			idx = period - idx
		}
		return
	}
	for k := 1; k <= pad; k++ {
		ext[pad-k] = sig[reflect(-k)]
		ext[pad+n-1+k] = sig[reflect(n-1+k)]
	}
	// Lifting on absolute parity: STEP1 on even coordinates, STEP2 on odd.
	// ext index e ↔ coordinate i0 − pad + e.
	at := func(coord int32) (e int) {
		e = int(coord-i0) + pad
		return
	}
	// STEP1: X(2n) = Y(2n) − ⌊(Y(2n−1) + Y(2n+1) + 2) / 4⌋ for the even
	// coordinates from just below i0 to just above i1.
	start := i0 - 1
	if start&1 != 0 {
		start--
	}
	for c := start; c <= i1; c += 2 {
		e := at(c)
		if e-1 < 0 || e+1 >= len(ext) {
			continue
		}
		ext[e] -= (ext[e-1] + ext[e+1] + 2) >> 2
	}
	// STEP2: X(2n+1) = Y(2n+1) + ⌊(X(2n) + X(2n+2)) / 2⌋ for the odd
	// coordinates in [i0, i1).
	for c := i0; c < i1; c++ {
		if c&1 == 0 {
			continue
		}
		e := at(c)
		ext[e] += (ext[e-1] + ext[e+1]) >> 1
	}
	copy(sig, ext[pad:pad+n])
}
