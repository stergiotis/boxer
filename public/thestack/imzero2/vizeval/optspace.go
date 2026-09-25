package vizeval

import (
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// OptionKindE is the kind of value an option takes.
type OptionKindE uint8

const (
	// OptionKindEnum takes one of Choices, as a string.
	OptionKindEnum OptionKindE = iota
	// OptionKindInt takes an integer in [Min, Max], as an int64.
	OptionKindInt
	// OptionKindFloat takes a number in [Min, Max], as a float64.
	OptionKindFloat
	// OptionKindBool takes true or false.
	OptionKindBool
)

var AllOptionKinds = []OptionKindE{OptionKindEnum, OptionKindInt, OptionKindFloat, OptionKindBool}

func (inst OptionKindE) String() string {
	switch inst {
	case OptionKindEnum:
		return "enum"
	case OptionKindInt:
		return "int"
	case OptionKindFloat:
		return "float"
	case OptionKindBool:
		return "bool"
	}
	return "unknown"
}

// Option declares one setting of a sink. Min and Max bound a numeric option and
// are the range a search may explore; Choices lists an enum's values in the
// order a control offers them. Default must itself be valid.
type Option struct {
	Name        string
	Description string
	Kind        OptionKindE
	Default     any
	Choices     []string
	Min, Max    float64
}

// Space is a sink's settings, in the order its controls are drawn.
type Space []Option

// Values maps an option name to its value, typed by kind: string, int64,
// float64 or bool. A resolved Values holds every option of its space.
type Values map[string]any

// Resolve validates raw against the space and returns it with every absent
// option at its default. An unknown name, a value of the wrong type or one out
// of range is refused, never clamped or dropped: a candidate that is not what
// its seed said would be scored under the wrong identity.
//
// Numbers may arrive as any Go numeric type (JSON decodes them as float64); an
// int option refuses a number with a fractional part.
func (inst Space) Resolve(raw map[string]any) (vals Values, err error) {
	vals = make(Values, len(inst))
	for name := range raw {
		if !slices.ContainsFunc(inst, func(o Option) bool { return o.Name == name }) {
			return nil, eb.Build().Str("option", name).Errorf("unknown option")
		}
	}
	for _, o := range inst {
		v, present := raw[o.Name]
		if !present {
			v = o.Default
		}
		var c any
		c, err = o.coerce(v)
		if err != nil {
			return nil, eb.Build().Str("option", o.Name).Errorf("invalid value: %w", err)
		}
		vals[o.Name] = c
	}
	return vals, nil
}

// Defaults is the space resolved with nothing set.
func (inst Space) Defaults() (vals Values) {
	vals, err := inst.Resolve(nil)
	if err != nil {
		// A default that does not validate is a declaration bug, caught by
		// TestSinkDefaultsResolve for every catalogued sink.
		panic(err)
	}
	return vals
}

// coerce returns v as the canonical Go type of the option's kind, or refuses it.
func (inst Option) coerce(v any) (c any, err error) {
	switch inst.Kind {
	case OptionKindEnum:
		s, ok := v.(string)
		if !ok {
			return nil, eh.Errorf("an enum option takes a string")
		}
		if !slices.Contains(inst.Choices, s) {
			return nil, eb.Build().Str("value", s).Strs("choices", inst.Choices).Errorf("not one of the choices")
		}
		return s, nil
	case OptionKindBool:
		b, ok := v.(bool)
		if !ok {
			return nil, eh.Errorf("a bool option takes true or false")
		}
		return b, nil
	case OptionKindInt, OptionKindFloat:
		f, ok := asFloat(v)
		if !ok {
			return nil, eh.Errorf("a numeric option takes a number")
		}
		if math.IsNaN(f) || f < inst.Min || f > inst.Max {
			return nil, eb.Build().Float64("value", f).Float64("min", inst.Min).Float64("max", inst.Max).
				Errorf("out of range")
		}
		if inst.Kind == OptionKindFloat {
			return f, nil
		}
		if f != math.Trunc(f) {
			return nil, eb.Build().Float64("value", f).Errorf("an int option takes a whole number")
		}
		return int64(f), nil
	}
	return nil, eb.Build().Stringer("kind", inst.Kind).Errorf("unknown option kind")
}

func asFloat(v any) (f float64, ok bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}
