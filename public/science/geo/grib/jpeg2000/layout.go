package jpeg2000

import "strconv"

// The geometry of Annex B for one tile and one component: resolution
// levels, their precinct partitions (B.6), their sub-bands with the
// coordinates of (B-15), and the code-block partition of B.7 confined to
// each precinct, all anchored at the origin.

type bandKindE uint8

const (
	bandKindLL bandKindE = iota
	bandKindHL
	bandKindLH
	bandKindHH
)

type codeBlock struct {
	x0, y0, x1, y1 int32 // in sub-band coordinates
	// From the packet header.
	included   bool
	zeroPlanes int32
	passes     int
	data       []byte
}

// precinctBand is one precinct's share of one sub-band: its code-blocks in
// raster order and the two tag trees over them.
type precinctBand struct {
	cbw, cbh          int
	blocks            []codeBlock
	inclusion, planes *tagTree
}

type subBand struct {
	kind           bandKindE
	x0, y0, x1, y1 int32 // tbx0, tby0, tbx1, tby1
	mb             int32 // Mb of (E-2)
	precincts      []precinctBand
	coeffs         []int32 // (x1−x0) × (y1−y0)
}

type resolution struct {
	x0, y0, x1, y1 int32 // trx0, try0, trx1, try1
	ppx, ppy       uint  // precinct size exponents
	npw, nph       int   // precincts across and down
	bands          []*subBand
}

// ceilDivPow2 is ⌈a / 2^s⌉ for a ≥ 0.
func ceilDivPow2(a int64, s uint) (v int32) {
	v = int32((a + (int64(1) << s) - 1) >> s)
	return
}

func floorDivPow2(a int64, s uint) (v int32) {
	v = int32(a >> s)
	return
}

// layout builds the resolutions, precincts and sub-bands of the
// tile-component (tcx0, tcy0)–(tcx1, tcy1) for NL decomposition levels.
func (inst *header) layout() (res []*resolution, err error) {
	c := &inst.cod
	nl := uint(c.levels)
	tcx0, tcy0 := int64(inst.x0), int64(inst.y0)
	tcx1, tcy1 := int64(inst.width), int64(inst.height)
	res = make([]*resolution, nl+1)
	for r := uint(0); r <= nl; r++ {
		shift := nl - r
		rs := &resolution{
			x0: ceilDivPow2(tcx0, shift), y0: ceilDivPow2(tcy0, shift),
			x1: ceilDivPow2(tcx1, shift), y1: ceilDivPow2(tcy1, shift),
			ppx: 15, ppy: 15,
		}
		if c.precincts != nil {
			rs.ppx = uint(c.precincts[r] & 0x0f)
			rs.ppy = uint(c.precincts[r] >> 4)
		}
		if r > 0 && (rs.ppx == 0 || rs.ppy == 0) {
			err = corrupt("precinct size zero above resolution 0")
			return
		}
		// (B-16): precincts across and down.
		if rs.x1 > rs.x0 && rs.y1 > rs.y0 {
			rs.npw = int(ceilDivPow2(int64(rs.x1), rs.ppx) - floorDivPow2(int64(rs.x0), rs.ppx))
			rs.nph = int(ceilDivPow2(int64(rs.y1), rs.ppy) - floorDivPow2(int64(rs.y0), rs.ppy))
		}
		if rs.npw*rs.nph > 1<<20 {
			err = corrupt("more than a million precincts at one resolution")
			return
		}
		var kinds []bandKindE
		var nb uint
		if r == 0 {
			kinds = []bandKindE{bandKindLL}
			nb = nl
		} else {
			kinds = []bandKindE{bandKindHL, bandKindLH, bandKindHH}
			nb = nl - r + 1
		}
		// The precinct's footprint in a sub-band and the code-block size
		// within it (B-17, B-18): a level down from the resolution for r > 0.
		bppx, bppy := rs.ppx, rs.ppy
		if r > 0 {
			bppx--
			bppy--
		}
		cbwLog := min(uint(c.xcb), bppx)
		cbhLog := min(uint(c.ycb), bppy)
		px0 := floorDivPow2(int64(rs.x0), rs.ppx) // first precinct column index
		py0 := floorDivPow2(int64(rs.y0), rs.ppy)
		for _, k := range kinds {
			b := &subBand{kind: k}
			var xob, yob int64
			if k == bandKindHL || k == bandKindHH {
				xob = 1
			}
			if k == bandKindLH || k == bandKindHH {
				yob = 1
			}
			if nb == 0 {
				b.x0, b.y0, b.x1, b.y1 = int32(tcx0), int32(tcy0), int32(tcx1), int32(tcy1)
			} else {
				half := int64(1) << (nb - 1)
				b.x0 = ceilDivPow2(tcx0-half*xob, nb)
				b.y0 = ceilDivPow2(tcy0-half*yob, nb)
				b.x1 = ceilDivPow2(tcx1-half*xob, nb)
				b.y1 = ceilDivPow2(tcy1-half*yob, nb)
			}
			// Exponent index in QCD order: LL first, then HL, LH, HH per
			// level from the coarsest.
			var qi int
			if k != bandKindLL {
				qi = 3*int(nl-nb) + int(k)
			}
			if qi >= len(inst.qcd.exponents) {
				err = corrupt("no quantization exponent for a sub-band")
				return
			}
			b.mb = int32(inst.qcd.guard) + int32(inst.qcd.exponents[qi]) - 1
			if b.mb < 0 || b.mb > 37 {
				err = corrupt("bit-plane count outside 0…37")
				return
			}
			w, h := int64(b.x1-b.x0), int64(b.y1-b.y0)
			if w > 0 && h > 0 {
				b.coeffs = make([]int32, w*h)
			}
			b.precincts = make([]precinctBand, rs.npw*rs.nph)
			for pj := 0; pj < rs.nph; pj++ {
				for pi := 0; pi < rs.npw; pi++ {
					pb := &b.precincts[pj*rs.npw+pi]
					// The precinct's region in band coordinates, clipped
					// to the band.
					rx0 := max(b.x0, (px0+int32(pi))<<bppx)
					ry0 := max(b.y0, (py0+int32(pj))<<bppy)
					rx1 := min(b.x1, (px0+int32(pi)+1)<<bppx)
					ry1 := min(b.y1, (py0+int32(pj)+1)<<bppy)
					if rx1 <= rx0 || ry1 <= ry0 {
						continue
					}
					pb.cbw = int(ceilDivPow2(int64(rx1), cbwLog) - floorDivPow2(int64(rx0), cbwLog))
					pb.cbh = int(ceilDivPow2(int64(ry1), cbhLog) - floorDivPow2(int64(ry0), cbhLog))
					pb.blocks = make([]codeBlock, pb.cbw*pb.cbh)
					bx0 := floorDivPow2(int64(rx0), cbwLog) << cbwLog
					by0 := floorDivPow2(int64(ry0), cbhLog) << cbhLog
					for j := 0; j < pb.cbh; j++ {
						for i := 0; i < pb.cbw; i++ {
							cb := &pb.blocks[j*pb.cbw+i]
							cb.x0 = max(rx0, bx0+int32(i)<<cbwLog)
							cb.y0 = max(ry0, by0+int32(j)<<cbhLog)
							cb.x1 = min(rx1, bx0+int32(i+1)<<cbwLog)
							cb.y1 = min(ry1, by0+int32(j+1)<<cbhLog)
						}
					}
					pb.inclusion = newTagTree(pb.cbw, pb.cbh)
					pb.planes = newTagTree(pb.cbw, pb.cbh)
				}
			}
			rs.bands = append(rs.bands, b)
		}
		res[r] = rs
	}
	// With one layer and one component, LRCP, RLCP and RPCL all visit the
	// precincts of each resolution in raster order; the position-major
	// orders interleave resolutions and need more than one precinct to
	// differ, which is where they are refused.
	if c.progression > 2 {
		for _, rs := range res {
			if rs.npw*rs.nph > 1 {
				err = unsupported("progression order " + strconv.Itoa(int(c.progression)) + " with several precincts")
				return
			}
		}
	}
	return
}
