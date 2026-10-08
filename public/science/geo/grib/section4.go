package grib

import (
	"math"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TimeUnit is code table 4.4. Duration reports the unit's length where it
// has a fixed one; months, years and their multiples do not, and are not
// converted (ADR-0292 §R7).
type TimeUnit uint8

// Duration returns the unit's length and whether it has one.
func (inst TimeUnit) Duration() (d time.Duration, ok bool) {
	ok = true
	switch inst {
	case 0:
		d = time.Minute
	case 1:
		d = time.Hour
	case 2:
		d = 24 * time.Hour
	case 10:
		d = 3 * time.Hour
	case 11:
		d = 6 * time.Hour
	case 12:
		d = 12 * time.Hour
	case 13:
		d = time.Second
	default:
		ok = false
	}
	return
}

// Surface is a fixed surface of code table 4.5 with its scaled value. Value
// is the resolved quantity when the value is coded; Missing reports the
// all-ones value or scale.
type Surface struct {
	Type        uint8
	ScaleFactor int8
	ScaledValue int32
	Missing     bool
	Value       float64
}

func parseSurface(r *rd) (s Surface) {
	s.Type = r.u8()
	scale, scaleMissing := r.u8m()
	value, valueMissing := r.s32m()
	s.ScaleFactor = int8(scale & 0x7f)
	if scale&0x80 != 0 {
		s.ScaleFactor = -s.ScaleFactor
	}
	s.ScaledValue = value
	s.Missing = scaleMissing || valueMissing || s.Type == math.MaxUint8
	if !s.Missing {
		s.Value = float64(value) * math.Pow(10, -float64(s.ScaleFactor))
	}
	return
}

// TimeRange is one entry of a statistical template's list of time ranges
// (4.8, 4.11, 4.12, 4.15). Length and Increment are signed as coded; a
// negative length is legal (ECCC codes them) and is returned as such.
type TimeRange struct {
	Statistic     uint8
	IncrementType uint8
	LengthUnit    TimeUnit
	Length        int32
	IncrementUnit TimeUnit
	Increment     int32
}

// Statistics is the statistical-processing tail shared by templates 4.8,
// 4.11, 4.12 and 4.15.
type Statistics struct {
	// IntervalEnd is the end of the overall time interval as coded.
	IntervalEnd time.Time
	// NumMissing is the number of data values missing from the statistic.
	NumMissing uint32
	Ranges     []TimeRange
}

// Ensemble is the member identification of templates 4.1 and 4.11.
type Ensemble struct {
	Type   uint8
	Number uint8
	Size   uint8
}

// Derived is the derived-forecast identification of templates 4.2 and 4.12.
type Derived struct {
	Type uint8
	Size uint8
}

// Product is Section 4. The fields of the 4.0 prefix — parameter, process,
// cut-off, forecast time, surfaces — are decoded for every template that
// begins with it; the typed tails are set for the templates the reader lays
// out, and Raw holds the whole template for the rest (ADR-0292 §R1).
type Product struct {
	Template uint16
	// Category and Number complete the parameter triplet with the message's
	// discipline. Local ranges (192–254) are as coded, never named.
	Category, Number      uint8
	GeneratingProcessType uint8
	BackgroundProcess     uint8
	GeneratingProcessID   uint8
	HoursAfterCutoff      uint16
	MinutesAfterCutoff    uint8
	// ForecastTimeUnit and ForecastTime are octets 18–22 as coded, the
	// latter sign-magnitude.
	ForecastTimeUnit TimeUnit
	ForecastTime     int32
	// ForecastOffset is ForecastTime in ForecastTimeUnit, when the unit has
	// a duration; ForecastOffsetOK says so.
	ForecastOffset   time.Duration
	ForecastOffsetOK bool
	Surface1         Surface
	Surface2         Surface
	// VerticalCoordinates are the NV values Section 4 carries after the
	// template (hybrid level coefficients; DWD's grid UUID and level count).
	VerticalCoordinates []float32
	// Prefixed reports whether the template begins with the 4.0 octets and
	// the fields above are meaningful. Templates 4.20, 4.30–4.35 and 4.254
	// do not.
	Prefixed bool
	Ensemble *Ensemble
	Derived  *Derived
	// Percentile is template 4.6's and 4.10's percentile value.
	Percentile uint8
	Statistics *Statistics
	Raw        []byte
}

// ValidTime is the reference time plus the forecast offset, when the
// latter is a duration. For a statistical template the interval end is the
// more useful instant and sits in [Statistics.IntervalEnd].
func (inst *Product) ValidTime(ref time.Time) (t time.Time, ok bool) {
	if !inst.ForecastOffsetOK {
		return
	}
	t = ref.Add(inst.ForecastOffset)
	ok = true
	return
}

// prefixInsert says whether a product template opens with the octets of
// 4.0 (parameter through second surface) and how many octets its family
// inserts after the parameter number: chemistry (4.40–4.43, 4.67–4.68)
// adds the constituent type; aerosol (4.44–4.47, 4.49) the aerosol type and
// its size interval; optical aerosol (4.48) a wavelength interval as well;
// post-processing (4.70–4.73) the input process and its centre. The reader
// decodes the prefix for these and the tail for a few; a template not
// listed keeps only Raw.
func prefixInsert(n uint16) (insert int, ok bool) {
	switch {
	case n <= 15, n == 51, n == 60, n == 61:
		ok = true
	case n >= 40 && n <= 43, n == 67, n == 68:
		insert, ok = 2, true
	case n >= 44 && n <= 47, n == 49:
		insert, ok = 13, true
	case n == 48:
		insert, ok = 24, true
	case n >= 70 && n <= 73:
		insert, ok = 5, true
	}
	return
}

func parseProductE(body []byte) (p Product, err error) {
	r := rd{b: body}
	nv := r.u16()
	p.Template = r.u16()
	err = r.errE("product definition section")
	if err != nil {
		return
	}
	tpl := r.rest()
	if int(nv)*4 > len(tpl) {
		err = eb.Build().Uint16("nv", nv).Int("available", len(tpl)).Errorf("vertical coordinate count exceeds the section: %w", ErrMalformed)
		return
	}
	// The template is the section minus the trailing NV coordinates.
	p.Raw = tpl[:len(tpl)-int(nv)*4]
	if nv > 0 {
		vr := rd{b: tpl[len(tpl)-int(nv)*4:]}
		p.VerticalCoordinates = make([]float32, nv)
		for i := range p.VerticalCoordinates {
			p.VerticalCoordinates[i] = vr.f32()
		}
	}
	insert, prefixed := prefixInsert(p.Template)
	p.Prefixed = prefixed
	if !p.Prefixed {
		return
	}
	t := rd{b: p.Raw}
	p.Category = t.u8()
	p.Number = t.u8()
	t.skip(insert)
	p.GeneratingProcessType = t.u8()
	p.BackgroundProcess = t.u8()
	p.GeneratingProcessID = t.u8()
	p.HoursAfterCutoff = t.u16()
	p.MinutesAfterCutoff = t.u8()
	p.ForecastTimeUnit = TimeUnit(t.u8())
	ft, ftMissing := t.s32m()
	p.ForecastTime = ft
	p.Surface1 = parseSurface(&t)
	p.Surface2 = parseSurface(&t)
	err = t.errE("product template 4." + strconv.Itoa(int(p.Template)))
	if err != nil {
		return
	}
	if d, ok := p.ForecastTimeUnit.Duration(); ok && !ftMissing {
		p.ForecastOffset = time.Duration(ft) * d
		p.ForecastOffsetOK = true
	}
	switch p.Template {
	case 1, 11:
		p.Ensemble = &Ensemble{Type: t.u8(), Number: t.u8(), Size: t.u8()}
	case 2, 12:
		p.Derived = &Derived{Type: t.u8(), Size: t.u8()}
	case 6, 10:
		p.Percentile = t.u8()
	case 15:
		// Statistical processing over a spatial area: three octets before
		// the interval, not decoded by name.
		t.skip(3)
	}
	switch p.Template {
	case 8, 9, 10, 11, 12, 13, 14, 15:
		if p.Template == 9 {
			// Probability forecasts carry the probability tail (12 octets)
			// before the interval; template 5's probability fields are not
			// laid out.
			t.skip(13)
		}
		if p.Template == 13 || p.Template == 14 {
			err = unsupportedE("product template 4." + strconv.Itoa(int(p.Template)))
			return
		}
		var st Statistics
		year := int(t.u16())
		month, day, hour, minute, second := int(t.u8()), int(t.u8()), int(t.u8()), int(t.u8()), int(t.u8())
		nRanges := t.u8()
		st.NumMissing = t.u32()
		if int(nRanges)*12 > t.remaining() {
			err = eb.Build().Uint8("ranges", nRanges).Int("available", t.remaining()).Errorf("time range count exceeds the template: %w", ErrMalformed)
			return
		}
		st.Ranges = make([]TimeRange, nRanges)
		for i := range st.Ranges {
			tr := &st.Ranges[i]
			tr.Statistic = t.u8()
			tr.IncrementType = t.u8()
			tr.LengthUnit = TimeUnit(t.u8())
			tr.Length = t.s32()
			tr.IncrementUnit = TimeUnit(t.u8())
			tr.Increment = t.s32()
		}
		err = t.errE("product template 4." + strconv.Itoa(int(p.Template)))
		if err != nil {
			return
		}
		// An interval end of all zeros is how some producers code "not
		// applicable"; it is left as the zero time rather than refused.
		if year != 0 || month != 0 || day != 0 {
			st.IntervalEnd, err = makeTimeE(year, month, day, hour, minute, second)
			if err != nil {
				return
			}
		}
		p.Statistics = &st
	}
	err = t.errE("product template 4." + strconv.Itoa(int(p.Template)))
	return
}
