package ops

import (
	"encoding/json/jsontext"
	"maps"
	"math"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type valueKindE uint8

const (
	valueKindNumber valueKindE = iota + 1
	valueKindText
)

// Value is one parameter's value: a number, which an integer parameter also
// takes, or a text, which a choice and an expression also take. The zero
// value is no value. In JSON a number is a number and a text a string.
type Value struct {
	kind valueKindE
	num  float64
	text string
}

// Number is a value for a number or an integer parameter.
func Number(v float64) Value { return Value{kind: valueKindNumber, num: v} }

// Integer is a value for an integer parameter.
func Integer(v int64) Value { return Number(float64(v)) }

// Text is a value for a choice, a text or an expression parameter.
func Text(s string) Value { return Value{kind: valueKindText, text: s} }

// IsZero reports whether the value holds nothing.
func (inst Value) IsZero() bool { return inst.kind == 0 }

// Number returns a number value, and ok false for any other.
func (inst Value) Number() (v float64, ok bool) { return inst.num, inst.kind == valueKindNumber }

// Integer returns a number value that is a whole number a float64 holds
// exactly, and ok false for any other.
func (inst Value) Integer() (v int64, ok bool) {
	if inst.kind != valueKindNumber || !isExactInteger(inst.num) {
		return 0, false
	}
	return int64(inst.num), true
}

// Text returns a text value, and ok false for any other.
func (inst Value) Text() (s string, ok bool) { return inst.text, inst.kind == valueKindText }

// String spells the value for a person: a number as the shortest decimal
// that reads back to it, a text quoted.
func (inst Value) String() string {
	switch inst.kind {
	case valueKindNumber:
		return strconv.FormatFloat(inst.num, 'g', -1, 64)
	case valueKindText:
		return strconv.Quote(inst.text)
	}
	return "none"
}

// maxExactInteger is the largest whole number below which every whole
// number is a float64.
const maxExactInteger = 1 << 53

func isExactInteger(v float64) bool {
	return v >= -maxExactInteger && v <= maxExactInteger && v == math.Trunc(v)
}

// MarshalJSONTo writes the value as a JSON number or string. A number that
// is not finite has no JSON form and is refused.
func (inst Value) MarshalJSONTo(enc *jsontext.Encoder) error {
	switch inst.kind {
	case valueKindNumber:
		if !isFinite(inst.num) {
			return eb.Build().Float64("value", inst.num).Errorf("a parameter value that is not finite has no JSON form")
		}
		return enc.WriteToken(jsontext.Float(inst.num))
	case valueKindText:
		return enc.WriteToken(jsontext.String(inst.text))
	}
	return eb.Build().Errorf("an empty parameter value has no JSON form")
}

// UnmarshalJSONFrom reads a JSON number or string; anything else is refused.
func (inst *Value) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	switch tok.Kind() {
	case '0':
		v, ferr := tok.Float()
		if ferr != nil || !isFinite(v) {
			return eb.Build().Str("number", tok.String()).Errorf("a parameter value is outside the range of a float64")
		}
		*inst = Number(v)
	case '"':
		*inst = Text(tok.String())
	default:
		return eb.Build().Str("kind", tok.Kind().String()).Errorf("a parameter value is a JSON number or string")
	}
	return nil
}

// Params are parameter values by name. In JSON they are an object whose
// members a record writes in name order.
type Params map[string]Value

// Clone returns a copy; nil for nil.
func (inst Params) Clone() Params { return maps.Clone(inst) }

func (inst Params) number(name string) float64 {
	v, _ := inst[name].Number()
	return v
}

func (inst Params) integer(name string) int64 {
	v, _ := inst[name].Integer()
	return v
}

func (inst Params) text(name string) string {
	v, _ := inst[name].Text()
	return v
}
