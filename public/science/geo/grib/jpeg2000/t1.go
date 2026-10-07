package jpeg2000

// The coefficient bit modelling of Annex D: a code-block is decoded a
// bit-plane at a time from its first non-zero plane, each plane in a
// significance propagation pass, a magnitude refinement pass and a
// cleanup pass (the first plane in a cleanup pass alone), over stripes of
// four rows scanned column by column, with the contexts of Tables D.1 to
// D.4 driving the MQ decoder.

// Context labels: 0–8 significance (Table D.1), 9–13 sign (Table D.3),
// 14–16 refinement (Table D.4), 17 run-length, 18 uniform.
const (
	ctxSignFirst = 9
	ctxMagFirst  = 14
	ctxRunLength = 17
	ctxUniform   = 18
	numContexts  = 19
)

// Per-coefficient state bits.
const (
	stSig     = 1 << 0 // significant
	stNeg     = 1 << 1 // negative
	stVisited = 1 << 2 // coded in this plane's significance propagation pass
	stRefined = 1 << 3 // has had a refinement bit
	stFresh   = 1 << 4 // became significant in this plane
)

type t1Decoder struct {
	w, h     int
	state    []uint8 // (w+2) × (h+2) with a border of zeros
	mag      []int32 // w × h magnitudes
	ctx      [numContexts]mqContext
	mq       mqDecoder
	kind     bandKindE
	causal   bool
	segsym   bool
	resetCtx bool
}

func (inst *t1Decoder) resetContexts() {
	for i := range inst.ctx {
		inst.ctx[i] = mqContext{}
	}
	inst.ctx[0] = mqContext{index: 4}
	inst.ctx[ctxRunLength] = mqContext{index: 3}
	inst.ctx[ctxUniform] = mqContext{index: 46}
}

// zcContext maps the neighbourhood counts to a Table D.1 label for the
// sub-band's orientation.
func (inst *t1Decoder) zcContext(h, v, d int) (cx int) {
	switch inst.kind {
	case bandKindHL:
		h, v = v, h
		fallthrough
	case bandKindLL, bandKindLH:
		switch {
		case h == 2:
			cx = 8
		case h == 1:
			switch {
			case v >= 1:
				cx = 7
			case d >= 1:
				cx = 6
			default:
				cx = 5
			}
		case v == 2:
			cx = 4
		case v == 1:
			cx = 3
		case d >= 2:
			cx = 2
		case d == 1:
			cx = 1
		}
	case bandKindHH:
		hv := h + v
		switch {
		case d >= 3:
			cx = 8
		case d == 2:
			if hv >= 1 {
				cx = 7
			} else {
				cx = 6
			}
		case d == 1:
			switch {
			case hv >= 2:
				cx = 5
			case hv == 1:
				cx = 4
			default:
				cx = 3
			}
		case hv >= 2:
			cx = 2
		case hv == 1:
			cx = 1
		}
	}
	return
}

// neighbourhood counts the significant horizontal, vertical and diagonal
// neighbours of (x, y); in vertically causal mode the row below a stripe's
// last row is treated as insignificant.
func (inst *t1Decoder) neighbourhood(x, y int) (h, v, d int) {
	stride := inst.w + 2
	i := (y+1)*stride + x + 1
	s := inst.state
	h = int(s[i-1]&stSig) + int(s[i+1]&stSig)
	up := int(s[i-stride]&stSig) + int(s[i-stride-1]&stSig) + int(s[i-stride+1]&stSig)
	dn := int(s[i+stride]&stSig) + int(s[i+stride-1]&stSig) + int(s[i+stride+1]&stSig)
	if inst.causal && y%4 == 3 {
		dn = 0
	}
	v = int(s[i-stride]&stSig) + int(s[i+stride]&stSig)
	if inst.causal && y%4 == 3 {
		v = int(s[i-stride] & stSig)
	}
	d = up + dn - v
	return
}

// signContribution is Table D.2 for a pair of neighbours.
func signContribution(a, b uint8) (c int) {
	ca, cb := 0, 0
	if a&stSig != 0 {
		ca = 1
		if a&stNeg != 0 {
			ca = -1
		}
	}
	if b&stSig != 0 {
		cb = 1
		if b&stNeg != 0 {
			cb = -1
		}
	}
	c = ca + cb
	if c > 1 {
		c = 1
	} else if c < -1 {
		c = -1
	}
	return
}

// decodeSign reads a sign with the contexts of Table D.3 and returns true
// for negative.
func (inst *t1Decoder) decodeSign(x, y int) (neg bool) {
	stride := inst.w + 2
	i := (y+1)*stride + x + 1
	s := inst.state
	hc := signContribution(s[i-1], s[i+1])
	below := s[i+stride]
	if inst.causal && y%4 == 3 {
		below = 0
	}
	vc := signContribution(s[i-stride], below)
	var cx int
	var xor uint8
	if hc == 0 {
		cx = 9 + abs(vc)
		if vc < 0 {
			xor = 1
		}
	} else if hc > 0 {
		cx = 12 + vc
	} else {
		cx = 12 - vc
		xor = 1
	}
	d := inst.mq.decode(&inst.ctx[cx])
	neg = d^xor == 1
	return
}

func abs(v int) (a int) {
	a = v
	if v < 0 {
		a = -v
	}
	return
}

func (inst *t1Decoder) becomeSignificant(x, y int, plane int32) {
	stride := inst.w + 2
	i := (y+1)*stride + x + 1
	neg := inst.decodeSign(x, y)
	inst.state[i] |= stSig | stFresh
	if neg {
		inst.state[i] |= stNeg
	}
	inst.mag[y*inst.w+x] = 1 << uint(plane)
}

// decodeBlock runs the passes of one code-block and writes its
// coefficients into the sub-band array. planes is the number of coded
// bit-planes (Mb − P); passes the number the packet carried.
func (inst *t1Decoder) decodeBlock(cb *codeBlock, b *subBand, planes int32, passes int, style uint8) {
	inst.w = int(cb.x1 - cb.x0)
	inst.h = int(cb.y1 - cb.y0)
	inst.kind = b.kind
	inst.causal = style&0x08 != 0
	inst.segsym = style&0x20 != 0
	inst.resetCtx = style&0x02 != 0
	n := (inst.w + 2) * (inst.h + 2)
	if cap(inst.state) < n {
		inst.state = make([]uint8, n)
	}
	inst.state = inst.state[:n]
	clear(inst.state)
	if cap(inst.mag) < inst.w*inst.h {
		inst.mag = make([]int32, inst.w*inst.h)
	}
	inst.mag = inst.mag[:inst.w*inst.h]
	clear(inst.mag)
	inst.resetContexts()
	inst.mq.init(cb.data)
	plane := planes - 1
	passType := 2 // the first pass is a cleanup pass
	lastPlane := plane
	for p := 0; p < passes && plane >= 0; p++ {
		switch passType {
		case 0:
			inst.sigPropPass(plane)
		case 1:
			inst.magRefPass(plane)
		case 2:
			inst.cleanupPass(plane)
			if inst.segsym {
				for range 4 {
					inst.mq.decode(&inst.ctx[ctxUniform])
				}
			}
			inst.clearPlaneFlags()
		}
		if inst.resetCtx {
			inst.resetContexts()
		}
		lastPlane = plane
		if passType == 2 {
			plane--
			passType = 0
		} else {
			passType++
		}
	}
	// Reconstruction (E.1.2.2): a coefficient whose lower planes were not
	// coded is placed at the middle of its interval (r = ½).
	var half int32
	if lastPlane > 0 && (passType != 0 || plane >= 0) {
		half = 1 << uint(lastPlane-1)
	}
	if passType == 0 && plane < 0 {
		half = 0
	}
	stride := inst.w + 2
	bw := int(b.x1 - b.x0)
	for y := 0; y < inst.h; y++ {
		for x := 0; x < inst.w; x++ {
			s := inst.state[(y+1)*stride+x+1]
			if s&stSig == 0 {
				continue
			}
			v := inst.mag[y*inst.w+x]
			if half != 0 {
				v |= half
			}
			if s&stNeg != 0 {
				v = -v
			}
			b.coeffs[(int(cb.y0-b.y0)+y)*bw+int(cb.x0-b.x0)+x] = v
		}
	}
}

func (inst *t1Decoder) clearPlaneFlags() {
	for i := range inst.state {
		inst.state[i] &^= stVisited | stFresh
	}
}

func (inst *t1Decoder) sigPropPass(plane int32) {
	stride := inst.w + 2
	for y0 := 0; y0 < inst.h; y0 += 4 {
		for x := 0; x < inst.w; x++ {
			for y := y0; y < y0+4 && y < inst.h; y++ {
				i := (y+1)*stride + x + 1
				if inst.state[i]&stSig != 0 {
					continue
				}
				h, v, d := inst.neighbourhood(x, y)
				cx := inst.zcContext(h, v, d)
				if cx == 0 {
					continue
				}
				if inst.mq.decode(&inst.ctx[cx]) == 1 {
					inst.becomeSignificant(x, y, plane)
				}
				inst.state[i] |= stVisited
			}
		}
	}
}

func (inst *t1Decoder) magRefPass(plane int32) {
	stride := inst.w + 2
	for y0 := 0; y0 < inst.h; y0 += 4 {
		for x := 0; x < inst.w; x++ {
			for y := y0; y < y0+4 && y < inst.h; y++ {
				i := (y+1)*stride + x + 1
				s := inst.state[i]
				if s&stSig == 0 || s&stVisited != 0 || s&stFresh != 0 {
					continue
				}
				var cx int
				if s&stRefined != 0 {
					cx = 16
				} else {
					h, v, d := inst.neighbourhood(x, y)
					if h+v+d > 0 {
						cx = 15
					} else {
						cx = 14
					}
				}
				if inst.mq.decode(&inst.ctx[cx]) == 1 {
					inst.mag[y*inst.w+x] |= 1 << uint(plane)
				}
				inst.state[i] |= stRefined
			}
		}
	}
}

func (inst *t1Decoder) cleanupPass(plane int32) {
	stride := inst.w + 2
	for y0 := 0; y0 < inst.h; y0 += 4 {
		for x := 0; x < inst.w; x++ {
			y := y0
			// Run-length mode: four rows left, none significant or visited,
			// all with the zero context.
			if y0+4 <= inst.h {
				runMode := true
				for k := 0; k < 4; k++ {
					i := (y0+k+1)*stride + x + 1
					if inst.state[i]&(stSig|stVisited) != 0 {
						runMode = false
						break
					}
					h, v, d := inst.neighbourhood(x, y0+k)
					if inst.zcContext(h, v, d) != 0 {
						runMode = false
						break
					}
				}
				if runMode {
					if inst.mq.decode(&inst.ctx[ctxRunLength]) == 0 {
						continue
					}
					first := int(inst.mq.decode(&inst.ctx[ctxUniform]))<<1 | int(inst.mq.decode(&inst.ctx[ctxUniform]))
					y = y0 + first
					inst.becomeSignificant(x, y, plane)
					y++
				}
			}
			for ; y < y0+4 && y < inst.h; y++ {
				i := (y+1)*stride + x + 1
				if inst.state[i]&(stSig|stVisited) != 0 {
					continue
				}
				h, v, d := inst.neighbourhood(x, y)
				cx := inst.zcContext(h, v, d)
				if inst.mq.decode(&inst.ctx[cx]) == 1 {
					inst.becomeSignificant(x, y, plane)
				}
			}
		}
	}
}
