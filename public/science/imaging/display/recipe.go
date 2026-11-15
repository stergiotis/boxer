package display

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// TransferE is the transfer function of a recipe. The zero value is linear.
type TransferE uint8

const (
	TransferLinear TransferE = iota
	TransferLog
	TransferSqrt
	TransferAsinh
	TransferGamma
)

// AllTransfers lists the transfer functions.
var AllTransfers = []TransferE{TransferLinear, TransferLog, TransferSqrt, TransferAsinh, TransferGamma}

var transferNames = [...]string{"linear", "log", "sqrt", "asinh", "gamma"}

func (inst TransferE) String() string {
	if int(inst) < len(transferNames) {
		return transferNames[inst]
	}
	return "unknown"
}

// MarshalText writes the function's name, so a stored recipe reads as text.
func (inst TransferE) MarshalText() ([]byte, error) {
	if int(inst) >= len(transferNames) {
		return nil, eb.Build().Uint8("transfer", uint8(inst)).Errorf("transfer function has no name")
	}
	return []byte(transferNames[inst]), nil
}

// UnmarshalText reads a function's name.
func (inst *TransferE) UnmarshalText(text []byte) error {
	for i, name := range transferNames {
		if string(text) == name {
			*inst = TransferE(i)
			return nil
		}
	}
	return eb.Build().Str("name", string(text)).Errorf("no transfer function of that name")
}

// DefaultParam is the parameter a transfer function uses when a recipe gives
// none; 0 for a function that takes none.
func (inst TransferE) DefaultParam() float64 {
	switch inst {
	case TransferLog:
		return 1000
	case TransferAsinh:
		return 0.1
	case TransferGamma:
		return 1
	}
	return 0
}

// Apply evaluates the function at x in [0, 1] with parameter param, or the
// function's default when param is not positive. The formulas are in the
// package documentation.
func (inst TransferE) Apply(x, param float64) float64 {
	if !(param > 0) {
		param = inst.DefaultParam()
	}
	switch inst {
	case TransferLog:
		return math.Log(param*x+1) / math.Log(param+1)
	case TransferSqrt:
		return math.Sqrt(x)
	case TransferAsinh:
		return math.Asinh(x/param) / math.Asinh(1/param)
	case TransferGamma:
		return math.Pow(x, param)
	}
	return x
}

// LimitsSourceE says how a recipe's limits were obtained. The zero value is
// typed in by a person.
type LimitsSourceE uint8

const (
	LimitsSourceManual LimitsSourceE = iota
	// LimitsSourceNominal: the whole range the sample kind can hold.
	LimitsSourceNominal
	// LimitsSourceData: the smallest and largest valid value of the data.
	LimitsSourceData
	// LimitsSourcePercentile: the values at two percentiles of the data,
	// recorded in [Limits.PercentLo] and [Limits.PercentHi].
	LimitsSourcePercentile
)

// AllLimitsSources lists the ways limits are obtained.
var AllLimitsSources = []LimitsSourceE{LimitsSourceManual, LimitsSourceNominal, LimitsSourceData, LimitsSourcePercentile}

var limitsSourceNames = [...]string{"manual", "nominal", "data", "percentile"}

func (inst LimitsSourceE) String() string {
	if int(inst) < len(limitsSourceNames) {
		return limitsSourceNames[inst]
	}
	return "unknown"
}

// MarshalText writes the source's name.
func (inst LimitsSourceE) MarshalText() ([]byte, error) {
	if int(inst) >= len(limitsSourceNames) {
		return nil, eb.Build().Uint8("source", uint8(inst)).Errorf("limits source has no name")
	}
	return []byte(limitsSourceNames[inst]), nil
}

// UnmarshalText reads a source's name.
func (inst *LimitsSourceE) UnmarshalText(text []byte) error {
	for i, name := range limitsSourceNames {
		if string(text) == name {
			*inst = LimitsSourceE(i)
			return nil
		}
	}
	return eb.Build().Str("name", string(text)).Errorf("no limits source of that name")
}

// Limits is the range of values a recipe shows, in the image's values — the
// stored samples through their value transform — with a record of where the
// two numbers came from (ADR-0373 §SD4).
type Limits struct {
	Lo, Hi    float64
	Source    LimitsSourceE
	PercentLo float64
	PercentHi float64
}

// Recipe is how an image is shown. It is a plain value: it can be compared,
// stored and sent, and no operation on an image reads one. The zero value is
// not usable, because its limits are empty; start from [DefaultRecipe].
type Recipe struct {
	Limits   Limits
	Transfer TransferE
	// Param is the transfer function's parameter; 0 takes its default.
	Param float64
	// Invert shows the highest value in the lowest colour.
	Invert bool
	// FlagClipped draws values outside the limits in the palette's colours
	// for them instead of the ends of the ramp. It applies to a single
	// plane; a colour image has no colour to spare for a flag.
	FlagClipped bool
	// Palette names the colour map of a single plane; empty is the grey
	// ramp. The name is resolved to a [Palette] by whoever compiles the
	// recipe: this package holds the grey ramp only.
	Palette string
}

// Validate reports why a recipe cannot be compiled, in terms a person can
// act on.
func (inst Recipe) Validate() error {
	l := inst.Limits
	if math.IsNaN(l.Lo) || math.IsNaN(l.Hi) || math.IsInf(l.Lo, 0) || math.IsInf(l.Hi, 0) {
		return eb.Build().Float64("lo", l.Lo).Float64("hi", l.Hi).Errorf("display limits must be finite numbers")
	}
	if !(l.Hi > l.Lo) {
		return eb.Build().Float64("lo", l.Lo).Float64("hi", l.Hi).Errorf("upper display limit must be above the lower one")
	}
	if int(inst.Transfer) >= len(transferNames) {
		return eb.Build().Uint8("transfer", uint8(inst.Transfer)).Errorf("recipe names no known transfer function")
	}
	if math.IsNaN(inst.Param) || math.IsInf(inst.Param, 0) || inst.Param < 0 {
		return eb.Build().Float64("param", inst.Param).Errorf("transfer parameter must be a positive number, or 0 for the default")
	}
	return nil
}

// position returns where value sits between the limits, clamped to [0, 1],
// and whether it lay below or above them.
func (inst Recipe) position(value float64) (x float64, under, over bool) {
	x = (value - inst.Limits.Lo) / (inst.Limits.Hi - inst.Limits.Lo)
	if x < 0 {
		return 0, true, false
	}
	if x > 1 {
		return 1, false, true
	}
	return x, false, false
}

// level returns the transfer function's output for a position, inverted when
// the recipe says so, in [0, 1].
func (inst Recipe) level(x float64) float64 {
	y := inst.Transfer.Apply(x, inst.Param)
	y = min(max(y, 0), 1)
	if inst.Invert {
		y = 1 - y
	}
	return y
}

// Level is the level a value is drawn at, in [0, 1]: its position between
// the limits, clamped, through the transfer function and the inversion. A
// single plane looks the level up in its palette; a colour bar draws it.
func (inst Recipe) Level(value float64) float64 {
	x, _, _ := inst.position(value)
	return inst.level(x)
}

// NominalLimits is the whole range an integer sample kind can hold, through
// the value transform. A float kind has none and reports ok false.
func NominalLimits(kind imaging.SampleKindE, transform imaging.ValueTransform) (limits Limits, ok bool) {
	hi, ok := kind.NominalMax()
	if !ok {
		return limits, false
	}
	a, b := transform.Apply(0), transform.Apply(hi)
	return Limits{Lo: min(a, b), Hi: max(a, b), Source: LimitsSourceNominal}, true
}

// DataLimits is the range of a plane's valid values. A plane whose valid
// samples are all equal, or which has none, has no range to show and reports
// ok false.
func DataLimits(plane imaging.Plane) (limits Limits, ok bool) {
	lo, hi, ok := plane.StoredRange()
	if !ok || lo == hi {
		return limits, false
	}
	a, b := plane.Transform().Apply(lo), plane.Transform().Apply(hi)
	return Limits{Lo: min(a, b), Hi: max(a, b), Source: LimitsSourceData}, true
}

// DefaultRecipe is the view an image opens in (ADR-0373 §SD4): a colour image
// over the nominal range of its sample kind, a single plane over the range of
// its data, both linear. Where that range does not exist — a constant plane,
// a float colour image — it falls back to the other, and to [0, 1] when
// neither does; [Limits.Source] says which was used.
func DefaultRecipe(img imaging.Image) Recipe {
	first := img.Plane(0)
	nominal, hasNominal := NominalLimits(img.Kind(), first.Transform())
	data, hasData := DataLimits(first)
	var limits Limits
	switch {
	case img.Model().IsColor() && hasNominal:
		limits = nominal
	case !img.Model().IsColor() && hasData:
		limits = data
	case hasNominal:
		limits = nominal
	case hasData:
		limits = data
	default:
		limits = Limits{Lo: 0, Hi: 1, Source: LimitsSourceManual}
	}
	return Recipe{Limits: limits}
}

// DefaultPlaneRecipe is the view one plane opens in when it is shown by
// itself, whatever image it belongs to: the range of its data, linear, with
// the same fallbacks as [DefaultRecipe].
func DefaultPlaneRecipe(plane imaging.Plane) Recipe {
	if limits, ok := DataLimits(plane); ok {
		return Recipe{Limits: limits}
	}
	if limits, ok := NominalLimits(plane.Kind(), plane.Transform()); ok {
		return Recipe{Limits: limits}
	}
	return Recipe{Limits: Limits{Lo: 0, Hi: 1, Source: LimitsSourceManual}}
}
