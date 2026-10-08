package grib

import (
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Bitmap is Section 6. Present is false for indicator 255 (no bitmap); a
// present bitmap has one bit per grid point, 1 where a value is coded. Any
// other indicator — a predefined bitmap (1–253) or one defined earlier in
// the message (254) — is a refusal (ADR-0292 §R1).
type Bitmap struct {
	Indicator uint8
	Present   bool
	bits      []byte
	numPoints uint32
}

func parseBitmapE(body []byte, numPoints uint32) (b Bitmap, err error) {
	r := rd{b: body}
	b.Indicator = r.u8()
	b.numPoints = numPoints
	err = r.errE("bitmap section")
	if err != nil {
		return
	}
	switch b.Indicator {
	case 255:
		if r.remaining() != 0 {
			err = eb.Build().Int("extra", r.remaining()).Errorf("bytes in an absent bitmap: %w", ErrMalformed)
		}
	case 0:
		b.Present = true
		b.bits = r.rest()
		need := (int(numPoints) + 7) / 8
		if len(b.bits) != need {
			err = eb.Build().Int("bytes", len(b.bits)).Int("needed", need).Uint32("points", numPoints).Errorf("bitmap length does not match the point count: %w", ErrInconsistent)
		}
	default:
		err = unsupportedE("bitmap indicator " + strconv.Itoa(int(b.Indicator)))
	}
	return
}

// Set reports whether point i carries a value. Without a bitmap every
// point does.
func (inst *Bitmap) Set(i int) (ok bool) {
	if !inst.Present {
		return true
	}
	ok = inst.bits[i>>3]&(0x80>>(i&7)) != 0
	return
}

// Count returns the number of points that carry a value.
func (inst *Bitmap) Count() (n int) {
	if !inst.Present {
		n = int(inst.numPoints)
		return
	}
	for i := 0; i < int(inst.numPoints); i++ {
		if inst.Set(i) {
			n++
		}
	}
	return
}
