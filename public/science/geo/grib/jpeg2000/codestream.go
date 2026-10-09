package jpeg2000

import (
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Marker codes of Annex A.
const (
	mkSOC = 0xff4f
	mkSIZ = 0xff51
	mkCOD = 0xff52
	mkCOC = 0xff53
	mkTLM = 0xff55
	mkPLM = 0xff57
	mkPLT = 0xff58
	mkQCD = 0xff5c
	mkQCC = 0xff5d
	mkRGN = 0xff5e
	mkPOC = 0xff5f
	mkPPM = 0xff60
	mkPPT = 0xff61
	mkCRG = 0xff63
	mkCOM = 0xff64
	mkSOT = 0xff90
	mkSOP = 0xff91
	mkEPH = 0xff92
	mkSOD = 0xff93
	mkEOC = 0xffd9
)

// codingStyle is COD/COC: the parameters of one component's coding.
type codingStyle struct {
	levels     uint8
	xcb, ycb   uint8 // log2 of the code-block size
	cbStyle    uint8
	reversible bool
	precincts  []uint8 // PPx | PPy<<4 per resolution, nil for the default
	// From SGcod, present only in COD.
	progression uint8
	layers      uint16
	mct         uint8
	sop, eph    bool
}

// quantization is QCD/QCC with no quantization: guard bits and the
// exponent per sub-band in codestream order (LL, then HL, LH, HH per
// level from the coarsest).
type quantization struct {
	style     uint8
	guard     uint8
	exponents []uint8
}

// header is what the main header and the tile's tile-part headers say.
type header struct {
	width, height    uint32 // Xsiz, Ysiz
	x0, y0           uint32 // XOsiz, YOsiz
	tileW, tileH     uint32
	tileX0, tileY0   uint32
	precision        uint8
	signed           bool
	cod              codingStyle
	qcd              quantization
	haveCOD, haveQCD bool
	// data is the concatenated tile-part bodies of the single tile.
	data []byte
}

func be16(b []byte) (v uint16) {
	v = uint16(b[0])<<8 | uint16(b[1])
	return
}

func be32(b []byte) (v uint32) {
	v = uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	return
}

// parseHeader walks the codestream's marker segments. A JP2 box wrapper is
// unwrapped to its contiguous codestream box first.
func parseHeader(cs []byte) (h header, err error) {
	cs, err = unwrapJP2(cs)
	if err != nil {
		return
	}
	if len(cs) < 4 || be16(cs) != mkSOC {
		err = corrupt("no SOC marker")
		return
	}
	pos := 2
	haveSIZ := false
	tileParts := 0
	for {
		if pos+2 > len(cs) {
			err = corrupt("codestream ends before EOC")
			return
		}
		marker := be16(cs[pos:])
		if marker == mkEOC {
			break
		}
		if marker == mkSOT {
			var next int
			next, err = h.parseTilePart(cs, pos, tileParts)
			if err != nil {
				return
			}
			tileParts++
			pos = next
			continue
		}
		if pos+4 > len(cs) {
			err = corrupt("marker segment truncated")
			return
		}
		length := int(be16(cs[pos+2:]))
		if length < 2 || pos+2+length > len(cs) {
			err = eb.Build().Int("marker", int(marker)).Int("length", length).Errorf("marker segment length: %w", ErrCorrupt)
			return
		}
		seg := cs[pos+4 : pos+2+length]
		switch marker {
		case mkSIZ:
			err = h.parseSIZ(seg)
			haveSIZ = true
		case mkCOD:
			err = h.cod.parseCOD(seg)
			h.haveCOD = true
		case mkCOC:
			err = h.cod.parseCOC(seg)
		case mkQCD:
			err = h.qcd.parseQCD(seg)
			h.haveQCD = true
		case mkQCC:
			err = h.qcd.parseQCC(seg)
		case mkRGN:
			err = unsupported("region of interest")
		case mkPOC:
			err = unsupported("progression order change")
		case mkPPM, mkPPT:
			err = unsupported("packed packet headers")
		case mkTLM, mkPLM, mkPLT, mkCRG, mkCOM:
			// Informative; nothing the decoder needs.
		default:
			if marker&0xff00 != 0xff00 {
				err = corrupt("byte where a marker was expected")
			} else {
				err = unsupported("marker " + strconv.FormatInt(int64(marker), 16))
			}
		}
		if err != nil {
			return
		}
		if !haveSIZ {
			err = corrupt("marker before SIZ")
			return
		}
		pos += 2 + length
	}
	if !haveSIZ || !h.haveCOD || !h.haveQCD {
		err = corrupt("main header lacks SIZ, COD or QCD")
		return
	}
	if tileParts == 0 {
		err = corrupt("no tile-part")
		return
	}
	return
}

// unwrapJP2 returns the codestream inside a JP2 file's contiguous
// codestream box, or the input when it is a raw codestream.
func unwrapJP2(b []byte) (cs []byte, err error) {
	cs = b
	if len(b) < 12 || string(b[4:8]) != "jP  " {
		return
	}
	pos := 0
	for pos+8 <= len(b) {
		size := int(be32(b[pos:]))
		typ := string(b[pos+4 : pos+8])
		hdr := 8
		if size == 1 {
			if pos+16 > len(b) {
				break
			}
			size = int(be32(b[pos+12:]))
			if be32(b[pos+8:]) != 0 {
				break
			}
			hdr = 16
		}
		if size == 0 {
			size = len(b) - pos
		}
		if size < hdr || pos+size > len(b) {
			break
		}
		if typ == "jp2c" {
			cs = b[pos+hdr : pos+size]
			return
		}
		pos += size
	}
	err = corrupt("jp2 file without a contiguous codestream box")
	return
}

func (inst *header) parseSIZ(seg []byte) (err error) {
	if len(seg) < 36 {
		err = corrupt("SIZ too short")
		return
	}
	inst.width = be32(seg[2:])
	inst.height = be32(seg[6:])
	inst.x0 = be32(seg[10:])
	inst.y0 = be32(seg[14:])
	inst.tileW = be32(seg[18:])
	inst.tileH = be32(seg[22:])
	inst.tileX0 = be32(seg[26:])
	inst.tileY0 = be32(seg[30:])
	comps := be16(seg[34:])
	if comps != 1 {
		err = unsupported(strconv.Itoa(int(comps)) + " components")
		return
	}
	if len(seg) < 39 {
		err = corrupt("SIZ component fields missing")
		return
	}
	ssiz := seg[36]
	inst.precision = ssiz&0x7f + 1
	inst.signed = ssiz&0x80 != 0
	if seg[37] != 1 || seg[38] != 1 {
		err = unsupported("component sub-sampling")
		return
	}
	if inst.width == 0 || inst.height == 0 || inst.x0 >= inst.width || inst.y0 >= inst.height || inst.tileW == 0 || inst.tileH == 0 {
		err = corrupt("SIZ geometry")
		return
	}
	if inst.precision > 31 {
		err = unsupported("precision above 31 bits")
		return
	}
	// One tile: the tile grid must cover the image area with a single cell.
	if inst.tileX0 > inst.x0 || inst.tileY0 > inst.y0 {
		err = corrupt("tile origin after image origin")
		return
	}
	nx := (uint64(inst.width) - uint64(inst.tileX0) + uint64(inst.tileW) - 1) / uint64(inst.tileW)
	ny := (uint64(inst.height) - uint64(inst.tileY0) + uint64(inst.tileH) - 1) / uint64(inst.tileH)
	if nx*ny != 1 {
		err = unsupported(strconv.FormatUint(nx*ny, 10) + " tiles")
		return
	}
	return
}

func (inst *codingStyle) parseCOD(seg []byte) (err error) {
	if len(seg) < 10 {
		err = corrupt("COD too short")
		return
	}
	scod := seg[0]
	inst.progression = seg[1]
	inst.layers = be16(seg[2:])
	inst.mct = seg[4]
	inst.sop = scod&2 != 0
	inst.eph = scod&4 != 0
	err = inst.parseSP(seg[5:], scod&1 != 0)
	return
}

func (inst *codingStyle) parseCOC(seg []byte) (err error) {
	// One component: the index is one octet.
	if len(seg) < 7 {
		err = corrupt("COC too short")
		return
	}
	if seg[0] != 0 {
		err = corrupt("COC for a component that does not exist")
		return
	}
	err = inst.parseSP(seg[2:], seg[1]&1 != 0)
	return
}

func (inst *codingStyle) parseSP(sp []byte, precinctsDefined bool) (err error) {
	if len(sp) < 5 {
		err = corrupt("coding style parameters too short")
		return
	}
	inst.levels = sp[0]
	inst.xcb = sp[1]&0x0f + 2
	inst.ycb = sp[2]&0x0f + 2
	inst.cbStyle = sp[3]
	inst.reversible = sp[4] == 1
	if inst.levels > 32 {
		err = corrupt("decomposition levels above 32")
		return
	}
	if inst.xcb+inst.ycb > 12 {
		err = corrupt("code-block larger than 4096 samples")
		return
	}
	inst.precincts = nil
	if precinctsDefined {
		if len(sp) < 5+int(inst.levels)+1 {
			err = corrupt("precinct sizes missing")
			return
		}
		inst.precincts = sp[5 : 5+int(inst.levels)+1]
	}
	return
}

func (inst *quantization) parseQCD(seg []byte) (err error) {
	err = inst.parseSPq(seg)
	return
}

func (inst *quantization) parseQCC(seg []byte) (err error) {
	if len(seg) < 2 {
		err = corrupt("QCC too short")
		return
	}
	if seg[0] != 0 {
		err = corrupt("QCC for a component that does not exist")
		return
	}
	err = inst.parseSPq(seg[1:])
	return
}

func (inst *quantization) parseSPq(sq []byte) (err error) {
	if len(sq) < 2 {
		err = corrupt("quantization parameters too short")
		return
	}
	inst.style = sq[0] & 0x1f
	inst.guard = sq[0] >> 5
	if inst.style != 0 {
		// Scalar quantization belongs to the irreversible path; the
		// profile check names the wavelet, which is the feature a reader
		// recognises.
		return
	}
	inst.exponents = make([]uint8, len(sq)-1)
	for i, b := range sq[1:] {
		inst.exponents[i] = b >> 3
	}
	return
}

// parseTilePart reads one tile-part: its SOT, the tile-part header
// markers, and the data up to Psot; the data is appended to the tile.
func (inst *header) parseTilePart(cs []byte, pos int, index int) (next int, err error) {
	if pos+12 > len(cs) || be16(cs[pos+2:]) != 10 {
		err = corrupt("SOT segment")
		return
	}
	isot := be16(cs[pos+4:])
	psot := int(be32(cs[pos+6:]))
	tpsot := cs[pos+10]
	if isot != 0 {
		err = unsupported("tile " + strconv.Itoa(int(isot)))
		return
	}
	if int(tpsot) != index {
		err = corrupt("tile-parts out of order")
		return
	}
	end := len(cs)
	if psot != 0 {
		if psot < 12 || pos+psot > len(cs) {
			err = eb.Build().Int("psot", psot).Errorf("tile-part length: %w", ErrCorrupt)
			return
		}
		end = pos + psot
	}
	p := pos + 12
	for {
		if p+2 > end {
			err = corrupt("tile-part header ends before SOD")
			return
		}
		marker := be16(cs[p:])
		if marker == mkSOD {
			p += 2
			break
		}
		if p+4 > end {
			err = corrupt("tile-part marker truncated")
			return
		}
		length := int(be16(cs[p+2:]))
		if length < 2 || p+2+length > end {
			err = corrupt("tile-part marker length")
			return
		}
		seg := cs[p+4 : p+2+length]
		switch marker {
		case mkCOD:
			if index != 0 {
				err = corrupt("COD in a later tile-part")
				return
			}
			err = inst.cod.parseCOD(seg)
		case mkCOC:
			err = inst.cod.parseCOC(seg)
		case mkQCD:
			err = inst.qcd.parseQCD(seg)
		case mkQCC:
			err = inst.qcd.parseQCC(seg)
		case mkRGN:
			err = unsupported("region of interest")
		case mkPOC:
			err = unsupported("progression order change")
		case mkPPT:
			err = unsupported("packed packet headers")
		case mkPLT, mkCOM:
		default:
			err = unsupported("tile-part marker " + strconv.FormatInt(int64(marker), 16))
		}
		if err != nil {
			return
		}
		p += 2 + length
	}
	if psot == 0 {
		// The last tile-part may run to the EOC marker, which is then the
		// codestream's last two bytes.
		if end >= p+2 && be16(cs[end-2:]) == mkEOC {
			end -= 2
		}
		inst.data = append(inst.data, cs[p:end]...)
		next = end
		return
	}
	inst.data = append(inst.data, cs[p:end]...)
	next = end
	return
}

// checkProfile refuses what the decoder does not do (ADR-0292 §R6).
func (inst *header) checkProfile() (err error) {
	c := &inst.cod
	if !c.reversible {
		err = unsupported("9/7 irreversible wavelet")
		return
	}
	if inst.qcd.style != 0 {
		err = unsupported("quantization style " + strconv.Itoa(int(inst.qcd.style)))
		return
	}
	if c.layers != 1 {
		err = unsupported(strconv.Itoa(int(c.layers)) + " quality layers")
		return
	}
	if c.mct != 0 {
		err = unsupported("multiple component transform")
		return
	}
	if c.cbStyle&0x01 != 0 {
		err = unsupported("arithmetic coder bypass")
		return
	}
	if c.cbStyle&0x04 != 0 {
		err = unsupported("termination on each coding pass")
		return
	}
	want := 3*int(c.levels) + 1
	if len(inst.qcd.exponents) < want {
		err = corrupt("fewer quantization exponents than sub-bands")
		return
	}
	return
}
