package jpeg2000

// The MQ arithmetic decoder of Annex C, in the software conventions of its
// flow charts: a 32-bit code register whose upper half is compared against
// the interval, byte-in with the 0xFF bit-stuffing rule, and the
// probability estimation state machine of Table C.2. A read past the end
// of the segment feeds 0xFF bytes, as D.4.1 tells the decoder to.

// qeEntry is one row of Table C.2.
type qeEntry struct {
	qe    uint32
	nmps  uint8
	nlps  uint8
	swtch uint8
}

var qeTable = [47]qeEntry{
	{0x5601, 1, 1, 1}, {0x3401, 2, 6, 0}, {0x1801, 3, 9, 0}, {0x0AC1, 4, 12, 0}, {0x0521, 5, 29, 0}, {0x0221, 38, 33, 0},
	{0x5601, 7, 6, 1}, {0x5401, 8, 14, 0}, {0x4801, 9, 14, 0}, {0x3801, 10, 14, 0}, {0x3001, 11, 17, 0}, {0x2401, 12, 18, 0},
	{0x1C01, 13, 20, 0}, {0x1601, 29, 21, 0}, {0x5601, 15, 14, 1}, {0x5401, 16, 14, 0}, {0x5101, 17, 15, 0}, {0x4801, 18, 16, 0},
	{0x3801, 19, 17, 0}, {0x3401, 20, 18, 0}, {0x3001, 21, 19, 0}, {0x2801, 22, 19, 0}, {0x2401, 23, 20, 0}, {0x2201, 24, 21, 0},
	{0x1C01, 25, 22, 0}, {0x1801, 26, 23, 0}, {0x1601, 27, 24, 0}, {0x1401, 28, 25, 0}, {0x1201, 29, 26, 0}, {0x1101, 30, 27, 0},
	{0x0AC1, 31, 28, 0}, {0x09C1, 32, 29, 0}, {0x08A1, 33, 30, 0}, {0x0521, 34, 31, 0}, {0x0441, 35, 32, 0}, {0x02A1, 36, 33, 0},
	{0x0221, 37, 34, 0}, {0x0141, 38, 35, 0}, {0x0111, 39, 36, 0}, {0x0085, 40, 37, 0}, {0x0049, 41, 38, 0}, {0x0025, 42, 39, 0},
	{0x0015, 43, 40, 0}, {0x0009, 44, 41, 0}, {0x0005, 45, 42, 0}, {0x0001, 45, 43, 0}, {0x5601, 46, 46, 0},
}

// mqContext is one context's state: its index into Table C.2 and its more
// probable symbol.
type mqContext struct {
	index uint8
	mps   uint8
}

type mqDecoder struct {
	data []byte
	bp   int
	c    uint32
	a    uint32
	ct   int
}

// byteAt returns the byte at bp, or 0xFF past the end (D.4.1).
func (inst *mqDecoder) byteAt(i int) (b uint32) {
	if i < len(inst.data) {
		b = uint32(inst.data[i])
		return
	}
	b = 0xff
	return
}

// init is INITDEC.
func (inst *mqDecoder) init(data []byte) {
	inst.data = data
	inst.bp = 0
	inst.c = inst.byteAt(0) << 16
	inst.byteIn()
	inst.c <<= 7
	inst.ct -= 7
	inst.a = 0x8000
}

// byteIn is BYTEIN: a 0xFF followed by a byte above 0x8F is a marker, past
// which the decoder feeds ones without advancing.
func (inst *mqDecoder) byteIn() {
	if inst.byteAt(inst.bp) == 0xff {
		if inst.byteAt(inst.bp+1) > 0x8f {
			inst.c += 0xff00
			inst.ct = 8
		} else {
			inst.bp++
			inst.c += inst.byteAt(inst.bp) << 9
			inst.ct = 7
		}
	} else {
		inst.bp++
		inst.c += inst.byteAt(inst.bp) << 8
		inst.ct = 8
	}
}

// decode is DECODE with the MPS/LPS conditional exchanges of C.3.2.
func (inst *mqDecoder) decode(cx *mqContext) (d uint8) {
	q := qeTable[cx.index]
	qe := q.qe
	inst.a -= qe
	if inst.c>>16 < qe {
		// LPS exchange (or MPS, when the intervals crossed).
		if inst.a < qe {
			inst.a = qe
			d = cx.mps
			cx.index = q.nmps
		} else {
			inst.a = qe
			d = 1 - cx.mps
			if q.swtch == 1 {
				cx.mps = 1 - cx.mps
			}
			cx.index = q.nlps
		}
	} else {
		inst.c -= qe << 16
		if inst.a&0x8000 != 0 {
			d = cx.mps
			return
		}
		// MPS exchange.
		if inst.a < qe {
			d = 1 - cx.mps
			if q.swtch == 1 {
				cx.mps = 1 - cx.mps
			}
			cx.index = q.nlps
		} else {
			d = cx.mps
			cx.index = q.nmps
		}
	}
	// RENORMD.
	for {
		if inst.ct == 0 {
			inst.byteIn()
		}
		inst.a <<= 1
		inst.c <<= 1
		inst.ct--
		if inst.a&0x8000 != 0 {
			break
		}
	}
	return
}
