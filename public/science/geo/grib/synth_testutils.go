package grib

import (
	"encoding/binary"
	"math"
	"time"
)

// SynthField describes a small simple-packed GRIB2 message built for a
// test: a regular lat/lon grid of Ni × Nj with the given scan flags, values
// coded as X with reference R, binary scale E and decimal scale D, and an
// optional bitmap. It exists so tests here and in the packages that read
// GRIB fields have fixtures whose right answer is known by construction
// rather than by an oracle. It is test support in a regular file because
// Go does not export test files across packages.
type SynthField struct {
	Ni, Nj   int
	Scan     uint8
	Lat1     float64
	Lon1     float64
	Lat2     float64
	Lon2     float64
	Ref      float32
	BinScale int16
	DecScale int16
	Bits     uint8
	X        []uint64 // packed integers, one per coded point
	Bitmap   []bool   // nil for none; else one per grid point
	Forecast int32    // sign-magnitude coded
	TimeUnit uint8
	// Discipline, Category and Number are the parameter triplet; Surface1
	// the first fixed surface's type and scaled value (scale factor 0).
	Discipline, Category, Number uint8
	SurfaceType                  uint8
	SurfaceValue                 int32
	// RefTime is the reference time; the zero value codes 2026-09-27 12:30:15.
	RefTime time.Time
	// Unstructured codes the field on template 3.101 with NumPoints cells
	// (the grid is named by UUID and not described), as ICON's native
	// output is; Ni, Nj, the corners and Scan are then unused.
	Unstructured bool
	NumPoints    int
	GridUUID     [16]byte
	// Ensemble codes template 4.1 with the member number and count, as
	// MeteoSwiss does for every field including the control.
	Ensemble        bool
	Member, Members uint8
}

func signMagnitude32(v int32) (raw uint32) {
	if v < 0 {
		raw = uint32(-v) | 0x80000000
	} else {
		raw = uint32(v)
	}
	return
}

func signMagnitude16(v int16) (raw uint16) {
	if v < 0 {
		raw = uint16(-v) | 0x8000
	} else {
		raw = uint16(v)
	}
	return
}

func microDeg(d float64) (raw uint32) {
	raw = signMagnitude32(int32(math.Round(d * 1e6)))
	return
}

// Encode builds the message.
func (inst SynthField) Encode() (msg []byte) {
	var out []byte
	be16 := func(v uint16) { out = binary.BigEndian.AppendUint16(out, v) }
	be32 := func(v uint32) { out = binary.BigEndian.AppendUint32(out, v) }
	section := func(number uint8, body func()) {
		start := len(out)
		be32(0)
		out = append(out, number)
		body()
		binary.BigEndian.PutUint32(out[start:], uint32(len(out)-start))
	}
	// Section 0
	out = append(out, 'G', 'R', 'I', 'B', 0, 0, 0, 2)
	out = binary.BigEndian.AppendUint64(out, 0)
	// Section 0's discipline.
	out[6] = inst.Discipline
	// Section 1
	rt := inst.RefTime
	if rt.IsZero() {
		rt = time.Date(2026, 9, 27, 12, 30, 15, 0, time.UTC)
	}
	section(1, func() {
		be16(98)
		be16(0)
		out = append(out, 30, 0, 1)
		be16(uint16(rt.Year()))
		out = append(out, uint8(rt.Month()), uint8(rt.Day()), uint8(rt.Hour()), uint8(rt.Minute()), uint8(rt.Second()), 0, 1)
	})
	npts := inst.Ni * inst.Nj
	if inst.Unstructured {
		npts = inst.NumPoints
	}
	// Section 3, template 3.0 — or 3.101, which names the grid and says
	// nothing else about it.
	section(3, func() {
		out = append(out, 0)
		be32(uint32(npts))
		out = append(out, 0, 0)
		if inst.Unstructured {
			be16(101)
			out = append(out, 1, 0)
			out = append(out, inst.GridUUID[:]...)
			return
		}
		be16(0)
		out = append(out, 6, 0xff)
		be32(0xffffffff)
		out = append(out, 0xff)
		be32(0xffffffff)
		out = append(out, 0xff)
		be32(0xffffffff)
		be32(uint32(inst.Ni))
		be32(uint32(inst.Nj))
		be32(0)
		be32(0xffffffff)
		be32(microDeg(inst.Lat1))
		be32(microDeg(inst.Lon1))
		out = append(out, 0x30)
		be32(microDeg(inst.Lat2))
		be32(microDeg(inst.Lon2))
		di := math.Abs(inst.Lon2-inst.Lon1) / float64(max(inst.Ni-1, 1))
		dj := math.Abs(inst.Lat2-inst.Lat1) / float64(max(inst.Nj-1, 1))
		be32(uint32(math.Round(di * 1e6)))
		be32(uint32(math.Round(dj * 1e6)))
		out = append(out, inst.Scan)
	})
	// Section 4, template 4.0, or 4.1 with the member after it
	section(4, func() {
		be16(0)
		if inst.Ensemble {
			be16(1)
		} else {
			be16(0)
		}
		out = append(out, inst.Category, inst.Number, 2, 0, 0)
		be16(0)
		out = append(out, 0, inst.TimeUnit)
		be32(signMagnitude32(inst.Forecast))
		sfc := inst.SurfaceType
		if sfc == 0 {
			sfc = 103
		}
		sv := inst.SurfaceValue
		if inst.SurfaceType == 0 {
			sv = 2
		}
		out = append(out, sfc, 0)
		be32(signMagnitude32(sv))
		out = append(out, 0xff, 0xff)
		be32(0xffffffff)
		if inst.Ensemble {
			out = append(out, 3, inst.Member, inst.Members)
		}
	})
	// Section 5, template 5.0: the coded count is the bitmap's set points,
	// or every point, whatever the bit width.
	coded := npts
	if inst.Bitmap != nil {
		coded = 0
		for _, set := range inst.Bitmap {
			if set {
				coded++
			}
		}
	}
	section(5, func() {
		be32(uint32(coded))
		be16(0)
		be32(math.Float32bits(inst.Ref))
		be16(signMagnitude16(inst.BinScale))
		be16(signMagnitude16(inst.DecScale))
		out = append(out, inst.Bits, 0)
	})
	// Section 6
	section(6, func() {
		if inst.Bitmap == nil {
			out = append(out, 255)
			return
		}
		out = append(out, 0)
		bits := make([]byte, (npts+7)/8)
		for i, set := range inst.Bitmap {
			if set {
				bits[i>>3] |= 0x80 >> (i & 7)
			}
		}
		out = append(out, bits...)
	})
	// Section 7
	section(7, func() {
		var acc uint64
		var nacc uint8
		for _, v := range inst.X {
			acc = acc<<inst.Bits | v
			nacc += inst.Bits
			for nacc >= 8 {
				out = append(out, byte(acc>>(nacc-8)))
				nacc -= 8
			}
		}
		if nacc > 0 {
			out = append(out, byte(acc<<(8-nacc)))
		}
	})
	out = append(out, '7', '7', '7', '7')
	binary.BigEndian.PutUint64(out[8:], uint64(len(out)))
	msg = out
	return
}
