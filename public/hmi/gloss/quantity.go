package gloss

import (
	"fmt"
	"math"
	"strconv"
	"strings"

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
	MediaTypeVelocity    = "gloss/velocity"
	MediaTypePlaneAngle  = "gloss/planeangle"
)

// The quantity family's parameters. ParamUnit names the stored unit and is
// required throughout; ParamShow and ParamAs are optional and choose how the
// value is presented rather than how it is stored.
const (
	ParamUnit = "unit"
	ParamShow = "show"
	ParamAs   = "as"
)

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
	// The nautical pair. Both are exact: the nautical mile has been exactly
	// 1852 m since 1929, and the fathom is six international feet.
	UnitNauticalMile = "NM"
	UnitFathom       = "fathom"
)

// lengthToMetres is the factor from a unit to metres.
var lengthToMetres = map[string]float64{
	UnitMetre:        1,
	UnitCentimetre:   0.01,
	UnitMillimetre:   0.001,
	UnitKilometre:    1000,
	UnitFoot:         0.3048,
	UnitNauticalMile: 1852,
	UnitFathom:       6 * 0.3048,
}

// ShowSI is ParamShow's default for a length: auto-scaled SI, the behaviour
// this gloss has always had. Naming it lets a declaration ask for it back
// explicitly after a rule set a different default.
const ShowSI = "si"

var lengthUnits = []string{
	UnitMetre, UnitCentimetre, UnitMillimetre, UnitKilometre,
	UnitFoot, UnitNauticalMile, UnitFathom,
}
var lengthShows = append([]string{ShowSI}, lengthUnits...)

func (lengthGloss) MediaType() string { return MediaTypeLength }
func (lengthGloss) Doc() string {
	return "a length, auto-scaled in SI by default, or in the unit `show` names (including NM and fathoms)"
}
func (lengthGloss) Params() []ParamSpec {
	return []ParamSpec{
		{Name: ParamUnit, Doc: "the stored unit", Values: lengthUnits},
		{Name: ParamShow, Doc: "the unit to display in, or si to auto-scale; defaults to si", Values: lengthShows},
	}
}
func (lengthGloss) Affinities() []string { return nil }

// Bind resolves the stored unit and how to show it. The default stays SI
// auto-scaling, so every declaration written before `show` existed renders
// exactly as it did.
//
// `show` is what makes the gloss usable outside SI. Auto-scaling is right for
// a length that ranges over orders of magnitude and wrong for one that does
// not: a sounder reading auto-scaled to centimetres in the shallows and metres
// outside them is harder to read than one that stays in the unit the
// instrument is calibrated in, and a distance run is quoted in nautical miles
// or not at all.
func (inst lengthGloss) Bind(params map[string]string) (InstanceI, error) {
	unit, ok := params[ParamUnit]
	if !ok {
		return nil, eb.Build().Str("mediaType", MediaTypeLength).
			Str("accepted", strings.Join(lengthUnits, "|")).
			Errorf(MediaTypeLength + " requires " + ParamUnit + "=<unit> naming the stored unit")
	}
	show := params[ParamShow]
	if show == "" {
		show = ShowSI
	}
	li := &lengthInstance{params: params, toMetres: lengthToMetres[unit], show: show}
	if show != ShowSI {
		li.factor = lengthToMetres[unit] / lengthToMetres[show]
	}
	return li, nil
}

type lengthInstance struct {
	params   map[string]string
	toMetres float64
	// show is ShowSI, or the unit to render in; factor converts the stored
	// value to that unit and is unused under ShowSI.
	show   string
	factor float64
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
	if inst.show == ShowSI {
		return Inline{Text: FormatMetres(v * inst.toMetres)}
	}
	return Inline{Text: strconv.FormatFloat(v*inst.factor, 'f', 2, 64) + " " + inst.show}
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

// --- velocity ---

type velocityGloss struct{}

var _ GlossI = velocityGloss{}

// Velocity unit spellings, as written after `unit=` and `show=`.
//
// A declaration is a media type, so a parameter value must be a MIME token:
// `m/s` and `km/h` cannot be written literally, because `/` is a tspecial and
// mime.ParseMediaType refuses it unquoted. The spellings here are therefore
// token-safe and the symbol shown to the reader is a separate thing —
// `unit=mps` renders "m/s".
const (
	UnitKnot           = "kn"
	UnitMetrePerSecond = "mps"
	UnitKmPerHour      = "kmh"
	UnitMilePerHour    = "mph"
)

// velocitySymbol is what a unit is shown as, which is not how it is spelled in
// a declaration; see the spellings above.
var velocitySymbol = map[string]string{
	UnitKnot:           "kn",
	UnitMetrePerSecond: "m/s",
	UnitKmPerHour:      "km/h",
	UnitMilePerHour:    "mph",
}

// velocityToMPS is the factor from a unit to metres per second. The knot is
// exactly one nautical mile per hour, and the nautical mile has been exactly
// 1852 m since 1929 — so every factor here is exact, not a rounded constant.
var velocityToMPS = map[string]float64{
	UnitMetrePerSecond: 1,
	UnitKnot:           1852.0 / 3600.0,
	UnitKmPerHour:      1000.0 / 3600.0,
	UnitMilePerHour:    1609.344 / 3600.0,
}

var velocityUnits = []string{UnitKnot, UnitMetrePerSecond, UnitKmPerHour, UnitMilePerHour}

func (velocityGloss) MediaType() string { return MediaTypeVelocity }
func (velocityGloss) Doc() string {
	return "a speed, shown in its stored unit or in the unit `show` names, one decimal, with the unit symbol"
}
func (velocityGloss) Params() []ParamSpec {
	return []ParamSpec{
		{Name: ParamUnit, Doc: "the stored unit", Values: velocityUnits},
		{Name: ParamShow, Doc: "the unit to display in; defaults to the stored unit", Values: velocityUnits},
	}
}
func (velocityGloss) Affinities() []string { return nil }

// Bind resolves the stored unit and the display unit. Unlike length, a speed
// is not auto-scaled: a boat's speeds occupy a narrow band and a scale that
// slid between m/s and km/h down a column would make two readings of the same
// quantity look like different measurements.
func (inst velocityGloss) Bind(params map[string]string) (InstanceI, error) {
	unit, ok := params[ParamUnit]
	if !ok {
		return nil, eb.Build().Str("mediaType", MediaTypeVelocity).
			Str("accepted", strings.Join(velocityUnits, "|")).
			Errorf(MediaTypeVelocity + " requires " + ParamUnit + "=<unit> naming the stored unit")
	}
	show := params[ParamShow]
	if show == "" {
		show = unit
	}
	return &velocityInstance{
		params: params,
		factor: velocityToMPS[unit] / velocityToMPS[show],
		symbol: velocitySymbol[show],
	}, nil
}

type velocityInstance struct {
	params map[string]string
	// factor converts the stored value to the displayed unit in one multiply.
	factor float64
	symbol string
}

var _ InstanceI = (*velocityInstance)(nil)

func (inst *velocityInstance) Gloss() GlossI             { return velocityGloss{} }
func (inst *velocityInstance) Params() map[string]string { return inst.params }
func (inst *velocityInstance) Accepts(kind ValueKindE) (bool, string) {
	return acceptsKind(MediaTypeVelocity, numericOnly, kind)
}
func (inst *velocityInstance) Inline(cell CellI) Inline {
	v, ok := cell.Float64()
	if !ok {
		return Inline{Text: cell.Text()}
	}
	return Inline{Text: strconv.FormatFloat(v*inst.factor, 'f', 1, 64) + " " + inst.symbol}
}

// --- plane angle ---

type planeAngleGloss struct{}

var _ GlossI = planeAngleGloss{}

// Plane-angle unit spellings, as written after `unit=`.
const (
	UnitDegree = "deg"
	UnitRadian = "rad"
)

// The plane angle's presentations, as written after `as=`.
const (
	// AngleAsPlain is the angle as stored, one decimal, with a degree sign.
	AngleAsPlain = "plain"
	// AngleAsBearing is a compass bearing: wrapped into [0, 360) and written
	// with three digits, so 7° reads 007° and sorts and scans like every other
	// bearing on the page.
	AngleAsBearing = "bearing"
	// AngleAsSigned is wrapped into (-180, 180] — the form a relative angle
	// takes, where the sign is the side: apparent wind 40° off to port is -40,
	// not 320.
	AngleAsSigned = "signed"
)

var angleUnits = []string{UnitDegree, UnitRadian}
var anglePresentations = []string{AngleAsPlain, AngleAsBearing, AngleAsSigned}

func (planeAngleGloss) MediaType() string { return MediaTypePlaneAngle }
func (planeAngleGloss) Doc() string {
	return "an angle in degrees; as=bearing wraps to 000-359, as=signed to -180..180"
}
func (planeAngleGloss) Params() []ParamSpec {
	return []ParamSpec{
		{Name: ParamUnit, Doc: "the stored unit", Values: angleUnits},
		{Name: ParamAs, Doc: "the presentation; defaults to plain", Values: anglePresentations},
	}
}
func (planeAngleGloss) Affinities() []string { return nil }
func (inst planeAngleGloss) Bind(params map[string]string) (InstanceI, error) {
	unit, ok := params[ParamUnit]
	if !ok {
		return nil, eb.Build().Str("mediaType", MediaTypePlaneAngle).
			Str("accepted", strings.Join(angleUnits, "|")).
			Errorf(MediaTypePlaneAngle + " requires " + ParamUnit + "=<unit> naming the stored unit")
	}
	as := params[ParamAs]
	if as == "" {
		as = AngleAsPlain
	}
	toDeg := 1.0
	if unit == UnitRadian {
		toDeg = 180.0 / math.Pi
	}
	return &planeAngleInstance{params: params, toDeg: toDeg, as: as}, nil
}

type planeAngleInstance struct {
	params map[string]string
	toDeg  float64
	as     string
}

var _ InstanceI = (*planeAngleInstance)(nil)

func (inst *planeAngleInstance) Gloss() GlossI             { return planeAngleGloss{} }
func (inst *planeAngleInstance) Params() map[string]string { return inst.params }
func (inst *planeAngleInstance) Accepts(kind ValueKindE) (bool, string) {
	return acceptsKind(MediaTypePlaneAngle, numericOnly, kind)
}
func (inst *planeAngleInstance) Inline(cell CellI) Inline {
	v, ok := cell.Float64()
	if !ok {
		return Inline{Text: cell.Text()}
	}
	return Inline{Text: FormatAngleDegrees(v*inst.toDeg, inst.as)}
}

// FormatAngleDegrees renders an angle in degrees in one of the presentations
// above. An unknown presentation renders as plain rather than failing: the
// catalog has already refused an undeclared value, so reaching here with one
// means a caller built the instance directly.
func FormatAngleDegrees(deg float64, as string) string {
	switch as {
	case AngleAsBearing:
		w := math.Mod(deg, 360)
		if w < 0 {
			w += 360
		}
		// Three digits before the point, so a column of bearings lines up.
		return fmt.Sprintf("%05.1f°", w)
	case AngleAsSigned:
		w := math.Mod(deg, 360)
		if w <= -180 {
			w += 360
		}
		if w > 180 {
			w -= 360
		}
		return strconv.FormatFloat(w, 'f', 1, 64) + "°"
	default:
		return strconv.FormatFloat(deg, 'f', 1, 64) + "°"
	}
}
