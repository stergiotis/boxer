package gloss

import (
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The quantity family: a numeric value carrying a physical unit, formatted
// with the unit's symbol. Names follow public/science/units' quantities so
// later members (mass, duration, …) have names waiting. `unit` names the
// STORED unit — a temperature column holding kelvin says `unit=K` — and is
// required: a temperature without a unit is a number.
const (
	MediaTypeTemperature = "gloss/temperature"
	MediaTypeLength      = "gloss/length"
)

// ParamUnit is the quantity family's one parameter.
const ParamUnit = "unit"

var numericOnly = []ValueKindE{ValueKindNumeric}

// --- temperature ---

type temperatureGloss struct{}

var _ GlossI = temperatureGloss{}

// Temperature unit spellings, as written after `unit=`. Case-sensitive: the
// catalog compares values as-is, so `unit=k` is refused rather than read as
// kelvin.
const (
	UnitKelvin     = "K"
	UnitCelsius    = "C"
	UnitFahrenheit = "F"
)

func (temperatureGloss) MediaType() string { return MediaTypeTemperature }
func (temperatureGloss) Doc() string {
	return "a temperature in its stored unit, one decimal, with the unit symbol"
}
func (temperatureGloss) Params() []ParamSpec {
	return []ParamSpec{{Name: ParamUnit, Doc: "the stored unit", Values: []string{UnitKelvin, UnitCelsius, UnitFahrenheit}}}
}
func (temperatureGloss) Affinities() []string { return nil }
func (inst temperatureGloss) Bind(params map[string]string) (InstanceI, error) {
	unit, ok := params[ParamUnit]
	if !ok {
		return nil, eb.Build().Str("mediaType", MediaTypeTemperature).Errorf(MediaTypeTemperature + " requires " + ParamUnit + "=" + UnitKelvin + "|" + UnitCelsius + "|" + UnitFahrenheit + " (the stored unit)")
	}
	symbol := map[string]string{UnitKelvin: "K", UnitCelsius: "°C", UnitFahrenheit: "°F"}[unit]
	return &temperatureInstance{params: params, symbol: symbol}, nil
}

type temperatureInstance struct {
	params map[string]string
	symbol string
}

var _ InstanceI = (*temperatureInstance)(nil)

func (inst *temperatureInstance) Gloss() GlossI             { return temperatureGloss{} }
func (inst *temperatureInstance) Params() map[string]string { return inst.params }
func (inst *temperatureInstance) Accepts(kind ValueKindE) (bool, string) {
	return acceptsKind(MediaTypeTemperature, numericOnly, kind)
}
func (inst *temperatureInstance) Inline(cell CellI) Inline {
	v, ok := cell.Float64()
	if !ok {
		return Inline{Text: cell.Text()}
	}
	return Inline{Text: strconv.FormatFloat(v, 'f', 1, 64) + " " + inst.symbol}
}

// --- length ---

type lengthGloss struct{}

var _ GlossI = lengthGloss{}

// Length unit spellings, as written after `unit=`.
const (
	UnitMetre      = "m"
	UnitCentimetre = "cm"
	UnitMillimetre = "mm"
	UnitKilometre  = "km"
	UnitFoot       = "ft"
)

// lengthToMetres is the factor from a stored unit to metres.
var lengthToMetres = map[string]float64{
	UnitMetre:      1,
	UnitCentimetre: 0.01,
	UnitMillimetre: 0.001,
	UnitKilometre:  1000,
	UnitFoot:       0.3048,
}

func (lengthGloss) MediaType() string { return MediaTypeLength }
func (lengthGloss) Doc() string {
	return "a length in its stored unit, shown auto-scaled in SI (mm, cm, m, km)"
}
func (lengthGloss) Params() []ParamSpec {
	return []ParamSpec{{Name: ParamUnit, Doc: "the stored unit", Values: []string{UnitMetre, UnitCentimetre, UnitMillimetre, UnitKilometre, UnitFoot}}}
}
func (lengthGloss) Affinities() []string { return nil }
func (inst lengthGloss) Bind(params map[string]string) (InstanceI, error) {
	unit, ok := params[ParamUnit]
	if !ok {
		return nil, eb.Build().Str("mediaType", MediaTypeLength).Errorf(MediaTypeLength + " requires " + ParamUnit + "=" + UnitMetre + "|" + UnitCentimetre + "|" + UnitMillimetre + "|" + UnitKilometre + "|" + UnitFoot + " (the stored unit)")
	}
	return &lengthInstance{params: params, toMetres: lengthToMetres[unit]}, nil
}

type lengthInstance struct {
	params   map[string]string
	toMetres float64
}

var _ InstanceI = (*lengthInstance)(nil)

func (inst *lengthInstance) Gloss() GlossI             { return lengthGloss{} }
func (inst *lengthInstance) Params() map[string]string { return inst.params }
func (inst *lengthInstance) Accepts(kind ValueKindE) (bool, string) {
	return acceptsKind(MediaTypeLength, numericOnly, kind)
}
func (inst *lengthInstance) Inline(cell CellI) Inline {
	v, ok := cell.Float64()
	if !ok {
		return Inline{Text: cell.Text()}
	}
	return Inline{Text: FormatMetres(v * inst.toMetres)}
}

// FormatMetres auto-scales a length in metres to the SI unit that reads
// best: km from a kilometre up, m from a metre up, cm from a centimetre up,
// mm below. Sign is preserved; the scale is chosen on the magnitude.
func FormatMetres(m float64) string {
	abs := m
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= 1000:
		return strconv.FormatFloat(m/1000, 'f', 3, 64) + " km"
	case abs >= 1:
		return strconv.FormatFloat(m, 'f', 2, 64) + " m"
	case abs >= 0.01:
		return strconv.FormatFloat(m*100, 'f', 1, 64) + " cm"
	default:
		return strconv.FormatFloat(m*1000, 'f', 1, 64) + " mm"
	}
}
