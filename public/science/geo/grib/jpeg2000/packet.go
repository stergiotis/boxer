package jpeg2000

import (
	"math/bits"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// readPacketsE walks the tile's packets in the order the resolution-major
// progressions give with one layer and one component: resolution 0
// upwards, precincts in raster order (B.12). Each packet's header assigns
// the included code-blocks their pass count, missing bit-planes and
// segment bytes.
func (inst *header) readPacketsE(res []*resolution) (err error) {
	data := inst.data
	pos := 0
	for r, rs := range res {
		for k := 0; k < rs.npw*rs.nph; k++ {
			pos, err = inst.readPacketE(data, pos, r, k, rs)
			if err != nil {
				return
			}
		}
	}
	return
}

// readPacketE reads the packet of precinct k at resolution r starting at
// pos and returns where the next packet begins.
func (inst *header) readPacketE(data []byte, pos int, r int, k int, rs *resolution) (next int, err error) {
	if inst.cod.sop {
		// Optional SOP marker segment before the packet.
		if pos+6 <= len(data) && be16(data[pos:]) == mkSOP {
			pos += 6
		}
	}
	br := headerBits{b: data, pos: pos}
	type pending struct {
		cb     *codeBlock
		length int
	}
	var body []pending
	if br.bit() == 1 {
		for _, b := range rs.bands {
			pb := &b.precincts[k]
			for j := 0; j < pb.cbh; j++ {
				for i := 0; i < pb.cbw; i++ {
					cb := &pb.blocks[j*pb.cbw+i]
					// First (and only) layer: inclusion through the tag tree.
					_, included := pb.inclusion.decode(&br, i, j, 1)
					if !included {
						continue
					}
					cb.included = true
					cb.zeroPlanes = pb.planes.decodeFull(&br, i, j)
					cb.passes = readPassCount(&br)
					lblock := 3
					for br.bit() == 1 {
						lblock++
						if lblock > 64 || br.bad {
							break
						}
					}
					nbits := lblock + bits.Len(uint(cb.passes)) - 1
					if nbits > 31 {
						err = corruptE("code-block length field wider than 31 bits")
						return
					}
					length := int(br.bits(nbits))
					body = append(body, pending{cb: cb, length: length})
				}
			}
		}
	}
	if br.bad {
		err = eb.Build().Int("resolution", r).Int("precinct", k).Errorf("packet header past the end of the data: %w", ErrCorrupt)
		return
	}
	pos = br.finish()
	if inst.cod.eph {
		if pos+2 <= len(data) && be16(data[pos:]) == mkEPH {
			pos += 2
		}
	}
	for _, p := range body {
		if p.length < 0 || pos+p.length > len(data) {
			err = eb.Build().Int("resolution", r).Int("precinct", k).Int("length", p.length).Int("remaining", len(data)-pos).Errorf("code-block segment past the end of the data: %w", ErrCorrupt)
			return
		}
		p.cb.data = data[pos : pos+p.length]
		pos += p.length
	}
	next = pos
	return
}

// readPassCount decodes Table B.4.
func readPassCount(br *headerBits) (n int) {
	if br.bit() == 0 {
		n = 1
		return
	}
	if br.bit() == 0 {
		n = 2
		return
	}
	v := br.bits(2)
	if v < 3 {
		n = 3 + int(v)
		return
	}
	v = br.bits(5)
	if v < 31 {
		n = 6 + int(v)
		return
	}
	n = 37 + int(br.bits(7))
	return
}
