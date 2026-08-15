package gloss

import (
	"math"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The temporal pair: a number that is a moment (gloss/epoch) or a span
// (gloss/duration), stored in some resolution the column knows and the
// display does not — hence `unit`, the stored resolution.
const (
	MediaTypeEpoch    = "gloss/epoch"
	MediaTypeDuration = "gloss/duration"
)

// Time-unit spellings, as written after `unit=`.
const (
	UnitNanosecond  = "ns"
	UnitMicrosecond = "us"
	UnitMillisecond = "ms"
	UnitSecond      = "s"
	UnitMinute      = "min"
	UnitHour        = "h"
)

// timeUnitNs is the factor from a stored unit to nanoseconds.
var timeUnitNs = map[string]float64{
	UnitNanosecond:  1,
	UnitMicrosecond: 1e3,
	UnitMillisecond: 1e6,
	UnitSecond:      1e9,
	UnitMinute:      60e9,
	UnitHour:        3600e9,
}

// --- epoch ---

// epochGloss shows a Unix epoch as an RFC 3339 UTC moment. `unit` is the
// stored resolution and defaults to seconds — the Unix epoch is seconds by
// definition, and a milliseconds column read as seconds lands in year
// 50000+ (or a seconds column read as milliseconds in January 1970), which
// is loud rather than plausible: such a moment shows in the warning tone.
type epochGloss struct{}

var _ GlossI = epochGloss{}

func (epochGloss) MediaType() string { return MediaTypeEpoch }
func (epochGloss) Doc() string {
	return "a Unix epoch (seconds unless unit= says otherwise) as an RFC 3339 UTC moment"
}
func (epochGloss) Params() []ParamSpec {
	return []ParamSpec{{Name: ParamUnit, Doc: "the stored resolution; s by default", Values: []string{UnitSecond, UnitMillisecond, UnitMicrosecond, UnitNanosecond}}}
}
func (epochGloss) Affinities() []string { return nil }
func (inst epochGloss) Bind(params map[string]string) (InstanceI, error) {
	unit := params[ParamUnit]
	if unit == "" {
		unit = UnitSecond
	}
	layout := time.RFC3339
	switch unit {
	case UnitMillisecond:
		layout = "2006-01-02T15:04:05.000Z07:00"
	case UnitMicrosecond:
		layout = "2006-01-02T15:04:05.000000Z07:00"
	case UnitNanosecond:
		// Fixed nine digits rather than RFC3339Nano, which trims trailing
		// zeros and would make a column's cells vary in width.
		layout = "2006-01-02T15:04:05.000000000Z07:00"
	}
	return &epochInstance{params: params, perUnitNs: timeUnitNs[unit], layout: layout}, nil
}

type epochInstance struct {
	params    map[string]string
	perUnitNs float64
	layout    string
}

var _ InstanceI = (*epochInstance)(nil)

func (inst *epochInstance) Gloss() GlossI             { return epochGloss{} }
func (inst *epochInstance) Params() map[string]string { return inst.params }
func (inst *epochInstance) Accepts(kind ValueKindE) (bool, string) {
	return acceptsKind(MediaTypeEpoch, numericOnly, kind)
}
func (inst *epochInstance) Inline(cell CellI) Inline {
	ns, ok, overflow := cellNanos(cell, inst.perUnitNs)
	if !ok {
		return Inline{Text: cell.Text()}
	}
	if overflow {
		return Inline{Text: cell.Text(), Tone: ToneWarning}
	}
	t := time.Unix(0, ns).UTC()
	// A year outside a human range is the s/ms mix-up made visible.
	if y := t.Year(); y < 1000 || y > 9999 {
		return Inline{Text: cell.Text() + " (" + strconv.Itoa(y) + ")", Tone: ToneWarning}
	}
	return Inline{Text: t.Format(inst.layout)}
}

// cellNanos reads a cell as nanoseconds in the stored unit: exactly through
// Int64 when the cell holds an integer (a nanosecond epoch has more digits
// than a float64 keeps), through Float64 otherwise (fractional seconds).
// overflow reports a value past what int64 nanoseconds hold — ~292 years
// either side of the epoch.
func cellNanos(cell CellI, perUnitNs float64) (ns int64, ok bool, overflow bool) {
	factor := int64(perUnitNs)
	if i, isInt := cell.Int64(); isInt {
		if i > math.MaxInt64/factor || i < math.MinInt64/factor {
			return 0, true, true
		}
		return i * factor, true, false
	}
	v, isFloat := cell.Float64()
	if !isFloat {
		return 0, false, false
	}
	f := v * perUnitNs
	if math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) >= float64(math.MaxInt64) {
		return 0, true, true
	}
	return int64(f), true, false
}

// --- duration ---

// durationGloss shows a span in the largest units that read well. `unit` is
// the stored unit and is required: a duration column is milliseconds as
// often as seconds, and neither is a default the other would forgive.
type durationGloss struct{}

var _ GlossI = durationGloss{}

func (durationGloss) MediaType() string { return MediaTypeDuration }
func (durationGloss) Doc() string {
	return "a span in its stored unit, shown as 12.3 ms, 1m 05s, 3d 4h 05m"
}
func (durationGloss) Params() []ParamSpec {
	return []ParamSpec{{Name: ParamUnit, Doc: "the stored unit", Values: []string{UnitNanosecond, UnitMicrosecond, UnitMillisecond, UnitSecond, UnitMinute, UnitHour}}}
}
func (durationGloss) Affinities() []string { return nil }
func (inst durationGloss) Bind(params map[string]string) (InstanceI, error) {
	unit, ok := params[ParamUnit]
	if !ok {
		return nil, eb.Build().Str("mediaType", MediaTypeDuration).Errorf("%s requires %s=%s|%s|%s|%s|%s|%s (the stored unit)", MediaTypeDuration, ParamUnit, UnitNanosecond, UnitMicrosecond, UnitMillisecond, UnitSecond, UnitMinute, UnitHour)
	}
	return &durationInstance{params: params, perUnitNs: timeUnitNs[unit]}, nil
}

type durationInstance struct {
	params    map[string]string
	perUnitNs float64
}

var _ InstanceI = (*durationInstance)(nil)

func (inst *durationInstance) Gloss() GlossI             { return durationGloss{} }
func (inst *durationInstance) Params() map[string]string { return inst.params }
func (inst *durationInstance) Accepts(kind ValueKindE) (bool, string) {
	return acceptsKind(MediaTypeDuration, numericOnly, kind)
}
func (inst *durationInstance) Inline(cell CellI) Inline {
	ns, ok, overflow := cellNanos(cell, inst.perUnitNs)
	if !ok {
		return Inline{Text: cell.Text()}
	}
	if overflow {
		// Past ~292 years time.Duration cannot hold it; say so rather than wrap.
		return Inline{Text: cell.Text(), Tone: ToneWarning}
	}
	return Inline{Text: FormatDuration(time.Duration(ns))}
}

// FormatDuration writes a span in the two largest units that apply, sub-second
// spans with three significant-ish digits and their SI unit:
//
//	123 ns · 12.3 µs · 12.3 ms · 12.34 s · 1m 05s · 1h 02m · 3d 4h 05m
//
// The sign is kept in front. Zero is "0 s".
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "0 s"
	}
	sign := ""
	if d < 0 {
		sign = "-"
		d = -d
	}
	var s string
	switch {
	case d < time.Microsecond:
		s = strconv.FormatInt(int64(d), 10) + " ns"
	case d < time.Millisecond:
		s = strconv.FormatFloat(float64(d)/float64(time.Microsecond), 'f', 1, 64) + " µs"
	case d < time.Second:
		s = strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 1, 64) + " ms"
	case d < time.Minute:
		s = strconv.FormatFloat(d.Seconds(), 'f', 2, 64) + " s"
	case d < time.Hour:
		m := int64(d / time.Minute)
		sec := int64((d % time.Minute) / time.Second)
		s = strconv.FormatInt(m, 10) + "m " + pad2(sec) + "s"
	case d < 24*time.Hour:
		h := int64(d / time.Hour)
		m := int64((d % time.Hour) / time.Minute)
		s = strconv.FormatInt(h, 10) + "h " + pad2(m) + "m"
	default:
		days := int64(d / (24 * time.Hour))
		rem := d % (24 * time.Hour)
		h := int64(rem / time.Hour)
		m := int64((rem % time.Hour) / time.Minute)
		s = strconv.FormatInt(days, 10) + "d " + strconv.FormatInt(h, 10) + "h " + pad2(m) + "m"
	}
	return sign + s
}

func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}
