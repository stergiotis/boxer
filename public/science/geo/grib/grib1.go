package grib

import (
	"math"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Grib1Header is edition 1's product definition section: what identifies
// the message, its level, its time range and, for ECMWF, local definition
// 1 (ADR-0292). Angles, scales and times are as coded; the time is also
// resolved to offsets where the unit has a duration.
type Grib1Header struct {
	TableVersion   uint8
	Process        uint8
	GridID         uint8
	HasGDS, HasBMS bool
	Parameter      uint8
	LevelType      uint8
	// Level is octets 11–12 as one value; Level1 and Level2 the same octets
	// as two, which is how the layer types of code table 3 use them (IsLayer).
	Level          uint16
	Level1, Level2 uint8
	IsLayer        bool
	TimeUnit       uint8
	P1, P2         uint8
	TimeRange      uint8
	// ForecastOffset is P1 (or P1 and P2 as one 16-bit value for indicator
	// 10) in the coded unit; for the interval indicators 2–5 it is the
	// interval's start and IntervalEnd its end, and Valid is where the
	// product is considered valid.
	ForecastOffset   time.Duration
	ForecastOffsetOK bool
	HasInterval      bool
	IntervalEnd      time.Duration
	Included         uint16
	Missing          uint8
	DecimalScale     int16
	// Local is ECMWF's local definition 1 when present.
	Local *Grib1Local
	// VerticalCoordinates are the NV values of the grid description section.
	VerticalCoordinates []float64
}

// Grib1Local is ECMWF local definition 1: MARS labelling with the ensemble
// member.
type Grib1Local struct {
	Definition uint8
	Class      uint8
	Type       uint8
	Stream     uint16
	ExpVer     string
	Number     uint8
	Total      uint8
}

// Valid returns the instant the product is valid at: the reference time
// plus the forecast offset, or plus the interval end for an accumulation or
// difference (indicators 4 and 5).
func (inst *Grib1Header) Valid(ref time.Time) (t time.Time, ok bool) {
	if !inst.ForecastOffsetOK {
		return
	}
	ok = true
	if inst.HasInterval && (inst.TimeRange == 4 || inst.TimeRange == 5) {
		t = ref.Add(inst.IntervalEnd)
		return
	}
	t = ref.Add(inst.ForecastOffset)
	return
}

// grib1TimeUnit maps code table 4 onto the edition 2 units, which share
// their codes except that a second is 254.
func grib1TimeUnit(u uint8) (unit TimeUnit) {
	if u == 254 {
		unit = 13
		return
	}
	unit = TimeUnit(u)
	return
}

// grib1LayerTypes are the level types of code table 3 whose octets 11–12
// hold two one-octet values.
func grib1LayerType(t uint8) (ok bool) {
	switch t {
	case 101, 104, 106, 108, 110, 112, 114, 116, 120, 121, 128, 141:
		ok = true
	}
	return
}

// ibmFloat decodes the IBM System/360 single: a sign bit, a 7-bit exponent
// in excess 64 to base 16, and a 24-bit fraction — the encoding of every
// edition 1 reference value and rotation angle. Zero is all zeros.
func ibmFloat(raw uint32) (v float64) {
	if raw == 0 {
		return
	}
	mant := float64(raw & 0xffffff)
	exp := int(raw>>24&0x7f) - 64
	v = math.Ldexp(mant, 4*exp-24)
	if raw&0x80000000 != 0 {
		v = -v
	}
	return
}

// s24 reads a 3-octet sign-magnitude integer, the width of edition 1's
// coordinates.
func (inst *rd) s24() (v int32) {
	raw := inst.uN(3)
	v = int32(raw & 0x7fffff)
	if raw&0x800000 != 0 {
		v = -v
	}
	return
}

// parseGrib1E reads an edition 1 message's four sections into one [Field]:
// the product definition into [Message.Grib1], the grid description into
// the field's [Grid] mapped onto the equivalent edition 2 template, the
// bitmap, and the data section's simple grid-point packing into a
// [Packing] whose values decode through the same path as template 5.0.
func (inst *Message) parseGrib1E() (err error) {
	raw := inst.raw
	end := len(raw) - 4
	pos := 8
	// Product definition section.
	pdsLen, err := inst.grib1SectionE(pos, end, 28, "product definition")
	if err != nil {
		return
	}
	pds := raw[pos : pos+pdsLen]
	h := &Grib1Header{}
	r := rd{b: pds[3:]}
	h.TableVersion = r.u8()
	inst.Ident.Centre = uint16(r.u8())
	h.Process = r.u8()
	h.GridID = r.u8()
	flags := r.u8()
	h.HasGDS = flags&0x80 != 0
	h.HasBMS = flags&0x40 != 0
	h.Parameter = r.u8()
	h.LevelType = r.u8()
	h.Level1 = r.u8()
	h.Level2 = r.u8()
	h.Level = uint16(h.Level1)<<8 | uint16(h.Level2)
	h.IsLayer = grib1LayerType(h.LevelType)
	yearOfCentury := int(r.u8())
	month, day, hour, minute := int(r.u8()), int(r.u8()), int(r.u8()), int(r.u8())
	h.TimeUnit = r.u8()
	h.P1 = r.u8()
	h.P2 = r.u8()
	h.TimeRange = r.u8()
	h.Included = r.u16()
	h.Missing = r.u8()
	century := int(r.u8())
	inst.Ident.SubCentre = uint16(r.u8())
	h.DecimalScale = r.s16()
	err = r.errE("grib1 product definition section")
	if err != nil {
		return
	}
	// Year 2000 is coded as year 100 of century 20.
	year := (century-1)*100 + yearOfCentury
	inst.Ident.RefTime, err = makeTimeE(year, month, day, hour, minute, 0)
	if err != nil {
		return
	}
	inst.Ident.TablesVersion = h.TableVersion
	if d, ok := grib1TimeUnit(h.TimeUnit).Duration(); ok {
		h.ForecastOffsetOK = true
		switch h.TimeRange {
		case 1:
			h.ForecastOffset = 0
		case 10:
			h.ForecastOffset = time.Duration(uint16(h.P1)<<8|uint16(h.P2)) * d
		case 2, 3, 4, 5:
			h.ForecastOffset = time.Duration(h.P1) * d
			h.IntervalEnd = time.Duration(h.P2) * d
			h.HasInterval = true
		default:
			h.ForecastOffset = time.Duration(h.P1) * d
		}
	}
	// ECMWF local definition 1 (MARS labelling with the ensemble member).
	if inst.Ident.Centre == 98 && pdsLen >= 52 && pds[40] == 1 {
		l := rd{b: pds[40:52]}
		loc := &Grib1Local{}
		loc.Definition = l.u8()
		loc.Class = l.u8()
		loc.Type = l.u8()
		loc.Stream = l.u16()
		loc.ExpVer = string(l.take(4))
		loc.Number = l.u8()
		loc.Total = l.u8()
		h.Local = loc
	}
	inst.Grib1 = h
	pos += pdsLen

	f := &Field{Message: inst, Index: 0, Local: nil}
	// Grid description section.
	if h.HasGDS {
		var gdsLen int
		gdsLen, err = inst.grib1SectionE(pos, end, 32, "grid description")
		if err != nil {
			return
		}
		f.Section3Offset = inst.Offset + int64(pos)
		f.Grid, h.VerticalCoordinates, err = parseGrib1GridE(raw[pos : pos+gdsLen])
		if err != nil {
			return
		}
		pos += gdsLen
	} else {
		err = unsupportedE("grib1 grid from catalogue " + strconv.Itoa(int(h.GridID)))
		return
	}
	// Bitmap section.
	f.Bitmap = Bitmap{Indicator: 255, numPoints: f.Grid.NumPoints}
	if h.HasBMS {
		var bmsLen int
		bmsLen, err = inst.grib1SectionE(pos, end, 6, "bitmap")
		if err != nil {
			return
		}
		f.Section6Offset = inst.Offset + int64(pos)
		bms := raw[pos : pos+bmsLen]
		tableRef := uint16(bms[4])<<8 | uint16(bms[5])
		if tableRef != 0 {
			err = unsupportedE("grib1 predefined bitmap " + strconv.Itoa(int(tableRef)))
			return
		}
		// The bitmap's bit count is the point count, which a reduced
		// sub-area's row lengths do not give.
		unused := int(bms[3])
		bitCount := (bmsLen-6)*8 - unused
		numPoints := f.Grid.NumPoints
		if ll := f.Grid.LatLon; ll != nil && ll.PL != nil && bitCount >= 0 && uint64(bitCount) != uint64(numPoints) {
			numPoints = uint32(bitCount)
		}
		need := (int(numPoints) + 7) / 8
		if unused > 15 || bmsLen-6 < need || bitCount < int(numPoints) {
			err = eb.Build().Int("bytes", bmsLen-6).Int("needed", need).Uint32("points", numPoints).Errorf("grib1 bitmap shorter than the point count: %w", ErrInconsistent)
			return
		}
		f.Bitmap = Bitmap{Indicator: 0, Present: true, bits: bms[6 : 6+need], numPoints: numPoints}
		pos += bmsLen
	}
	// Binary data section: the last section, running to the end marker.
	if end-pos < 11 {
		err = eb.Build().Int64("offset", inst.Offset).Errorf("grib1 data section truncated: %w", ErrMalformed)
		return
	}
	f.Section7Offset = inst.Offset + int64(pos)
	bds := raw[pos:end]
	codedLen := int(bds[0])<<16 | int(bds[1])<<8 | int(bds[2])
	if !inst.grib1Large && codedLen != len(bds) {
		err = eb.Build().Int("codedLength", codedLen).Int("actual", len(bds)).Errorf("grib1 data section length does not reach the end marker: %w", ErrInconsistent)
		return
	}
	b := rd{b: bds[3:]}
	dflags := b.u8()
	p := Packing{Grib1: true, Grib1Flags: dflags, Template: 0, DecimalScale: h.DecimalScale}
	rawE := b.u16()
	rawR := b.u32()
	p.BinaryScale = int16(rawE & 0x7fff)
	if rawE&0x8000 != 0 {
		p.BinaryScale = -p.BinaryScale
	}
	p.Reference = ibmFloat(rawR)
	p.Bits = b.u8()
	err = b.errE("grib1 data section")
	if err != nil {
		return
	}
	// A binary scale and a reference value both all ones is a field with no
	// values — DWD's missing-field convention, which the reference
	// implementation decodes as every point missing whatever the bitmap
	// says. The formula would give −7.2 × 10⁷⁵ everywhere.
	p.Grib1MissingField = rawE == math.MaxUint16 && rawR == math.MaxUint32
	// Edition 1 codes no point count: a reduced grid's sub-area is coded
	// with the global row lengths, so the count comes from the data —
	// the bitmap's bits, or the packed values the section holds.
	unused := int(dflags & 0x0f)
	data := bds[11:]
	if f.Bitmap.Present {
		f.Grid.NumPoints = f.Bitmap.numPoints
	} else if p.Bits > 0 && f.Grid.LatLon != nil && f.Grid.LatLon.PL != nil {
		coded := (uint64(len(data))*8 - uint64(unused)) / uint64(p.Bits)
		if coded > maxPoints {
			err = eb.Build().Uint64("coded", coded).Errorf("grib1 coded value count not credible: %w", ErrMalformed)
			return
		}
		if coded != uint64(f.Grid.NumPoints) {
			f.Grid.NumPoints = uint32(coded)
		}
		f.Bitmap.numPoints = f.Grid.NumPoints
	}
	p.NumValues = uint32(f.Bitmap.Count())
	if dflags&0x20 != 0 {
		p.OriginalType = 1
	}
	if p.Bits > 64 {
		err = eb.Build().Uint8("bits", p.Bits).Errorf("bits per value above 64: %w", ErrMalformed)
		return
	}
	if math.IsNaN(p.Reference) || math.IsInf(p.Reference, 0) {
		err = eb.Build().Errorf("reference value is not a number: %w", ErrMalformed)
		return
	}
	p.Raw = bds[3:11]
	f.Packing = p
	f.data = data
	inst.Fields = []*Field{f}
	return
}

// grib1SectionE reads a section's 3-octet length at pos and checks it
// against the message and the section's minimum.
func (inst *Message) grib1SectionE(pos, end, minLen int, what string) (n int, err error) {
	if end-pos < 3 {
		err = eb.Build().Int64("offset", inst.Offset).Str("section", what).Errorf("grib1 section truncated: %w", ErrMalformed)
		return
	}
	raw := inst.raw
	n = int(raw[pos])<<16 | int(raw[pos+1])<<8 | int(raw[pos+2])
	if n < minLen || n > end-pos {
		err = eb.Build().Int64("offset", inst.Offset).Str("section", what).Int("length", n).Int("available", end-pos).Errorf("grib1 section length not credible: %w", ErrMalformed)
	}
	return
}

// parseGrib1GridE lays out edition 1's grid description section onto the
// edition 2 [Grid] that describes the same geometry. Coordinates are
// millidegrees; a Ni of all ones marks a reduced grid whose row counts
// follow the vertical coordinates at octet PV, two octets each.
func parseGrib1GridE(gds []byte) (g Grid, vertical []float64, err error) {
	r := rd{b: gds[3:]}
	nv := int(r.u8())
	pv := int(r.u8())
	typ := r.u8()
	g.Grib1Type = typ
	g.Shape = EarthShape{Code: 0, Radius: 6367470}
	const milli = 1e-3
	switch typ {
	case 0, 4, 10, 14:
		switch typ {
		case 0:
			g.Template = 0
		case 4, 14:
			g.Template = 40
		case 10:
			g.Template = 1
		}
		ll := &LatLonGrid{AngleUnit: milli}
		ni, niMissing := r.u16m()
		ll.Nj = uint32(r.u16())
		ll.Lat1 = float64(r.s24()) * milli
		ll.Lon1 = float64(r.s24()) * milli
		ll.ResolutionFlags = r.u8()
		ll.Lat2 = float64(r.s24()) * milli
		ll.Lon2 = float64(r.s24()) * milli
		di, diMissing := r.u16m()
		second := r.u16()
		g.Scan = parseScanFlags(r.u8())
		r.skip(4)
		if typ == 10 || typ == 14 {
			rot := &Rotation{}
			rot.SouthPoleLat = float64(r.s24()) * milli
			rot.SouthPoleLon = float64(r.s24()) * milli
			rot.Angle = ibmFloat(r.u32())
			ll.Rotated = rot
		}
		err = r.errE("grib1 grid description type " + strconv.Itoa(int(typ)))
		if err != nil {
			return
		}
		ll.DiGiven = ll.ResolutionFlags&0x80 != 0
		ll.DjGiven = ll.DiGiven
		ll.UVRelativeToGrid = ll.ResolutionFlags&0x08 != 0
		if !diMissing {
			ll.Di = float64(di) * milli
		}
		if typ == 4 || typ == 14 {
			ll.N = uint32(second)
		} else {
			ll.Dj = float64(second) * milli
		}
		if ll.ResolutionFlags&0x40 != 0 {
			g.Shape = EarthShape{Code: 2, Major: 6378160, Minor: 6356775}
		}
		if ll.Nj == 0 {
			err = eb.Build().Errorf("grib1 row count zero: %w", ErrMalformed)
			return
		}
		// Vertical coordinates and the row list sit at octet PV.
		listPos := pv - 1
		if nv > 0 || niMissing {
			if pv == 0 || listPos+nv*4 > len(gds) {
				err = eb.Build().Int("pv", pv).Int("nv", nv).Int("length", len(gds)).Errorf("grib1 vertical coordinate list outside the section: %w", ErrMalformed)
				return
			}
			vr := rd{b: gds[listPos:]}
			vertical = make([]float64, nv)
			for i := range vertical {
				vertical[i] = ibmFloat(vr.u32())
			}
			if niMissing {
				g.OctetsPerPoint = 2
				g.Interpretation = 1
				if vr.remaining() < int(ll.Nj)*2 {
					err = eb.Build().Int("available", vr.remaining()).Uint32("nj", ll.Nj).Errorf("grib1 row list shorter than the row count: %w", ErrInconsistent)
					return
				}
				ll.PL = make([]uint32, ll.Nj)
				for j := range ll.PL {
					ll.PL[j] = uint32(vr.u16())
					ll.PLSum += uint64(ll.PL[j])
				}
				if ll.PLSum > maxPoints {
					err = eb.Build().Uint64("points", ll.PLSum).Errorf("grid point count not credible: %w", ErrMalformed)
					return
				}
				g.NumPoints = uint32(ll.PLSum)
			}
		}
		if !niMissing {
			ll.Ni = uint32(ni)
			total := uint64(ll.Ni) * uint64(ll.Nj)
			if total == 0 || total > maxPoints {
				err = eb.Build().Uint32("ni", ll.Ni).Uint32("nj", ll.Nj).Errorf("grid point count not credible: %w", ErrMalformed)
				return
			}
			g.NumPoints = uint32(total)
		}
		g.Raw = gds[6:]
		g.LatLon = ll
	case 1, 3, 5:
		switch typ {
		case 1:
			g.Template = 10
		case 3:
			g.Template = 30
		case 5:
			g.Template = 20
		}
		nx := uint64(r.u16())
		ny := uint64(r.u16())
		err = r.errE("grib1 grid description type " + strconv.Itoa(int(typ)))
		if err != nil {
			return
		}
		if nx*ny == 0 || nx*ny > maxPoints {
			err = eb.Build().Uint64("nx", nx).Uint64("ny", ny).Errorf("grid point count not credible: %w", ErrMalformed)
			return
		}
		g.NumPoints = uint32(nx * ny)
		g.Raw = gds[6:]
	case 50:
		g.Template = 50
		jt := uint64(r.u16())
		kt := uint64(r.u16())
		mt := uint64(r.u16())
		err = r.errE("grib1 grid description type 50")
		if err != nil {
			return
		}
		// Triangular truncation: (J+1)(J+2) real coefficients.
		if jt == kt && kt == mt && (jt+1)*(jt+2) <= maxPoints {
			g.NumPoints = uint32((jt + 1) * (jt + 2))
		}
		g.Raw = gds[6:]
	default:
		err = unsupportedE("grib1 grid type " + strconv.Itoa(int(typ)))
	}
	return
}
