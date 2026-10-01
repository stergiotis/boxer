// Package opjson is the model edge of the app operations contract
// (ADR-0269 §SD10): the JSON a model reads and writes for an operation's
// arguments and results, and the JSON Schema that describes it. Both are
// derived from the same Go types by one reflection walk, so a schema and the
// codec that enforces it cannot disagree.
//
// The rules, per Go kind:
//
//   - struct → object. A field's name is its `json` tag name, else the Go
//     name in snake_case; `json:"-"` and unexported fields are skipped,
//     embedded fields are refused. A `desc` tag becomes the description.
//     Every field is required unless it is a pointer, which may also be
//     null, or its `json` tag says omitempty or omitzero, when a missing
//     key leaves its zero value. Unknown keys are refused.
//   - int8…int32, uint8…uint32 → integer within the type's range.
//   - int, int64, uint, uint64 → a decimal string: a JSON number cannot
//     carry 64 bits exactly.
//   - float32, float64 → number; NaN and infinities are refused.
//   - []byte → a lowercase hex string.
//   - other slices and arrays → array; a nil slice is written as [].
//   - map with string keys → object.
//   - time.Time → RFC 3339 string; time.Duration → Go duration string.
//   - an integer type implementing [EnumI] → its names, as a string enum.
//
// Interfaces, channels, functions, complex numbers and maps with other key
// types are refused when the schema is derived, which is when a catalog is
// validated.
package opjson

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// SchemaError is a Go type the model edge cannot carry. Its message names
// the type and the field, since it becomes a catalog's diagnostic.
type SchemaError struct {
	Type    string
	Field   string
	Problem string
}

func (inst *SchemaError) Error() (s string) {
	s = "opjson: " + inst.Type
	if inst.Field != "" {
		s += "." + inst.Field
	}
	s += ": " + inst.Problem
	return
}

// ValueError is a JSON value that does not fit its schema. Path locates it
// from the root ($); the message goes back to the model as is.
type ValueError struct {
	Path    string
	Problem string
}

func (inst *ValueError) Error() (s string) { return "opjson: " + inst.Path + ": " + inst.Problem }

// EnumI is implemented by an integer type whose values the model sees by
// name. OpEnumNames lists the names by value: value i is named names[i].
type EnumI interface {
	OpEnumNames() (names []string)
}

var (
	typeTime     = reflect.TypeFor[time.Time]()
	typeDuration = reflect.TypeFor[time.Duration]()
	typeEnum     = reflect.TypeFor[EnumI]()
)

// field is one struct field as the model sees it.
type field struct {
	index    int
	name     string
	desc     string
	optional bool
}

// fields lists the model-visible fields of a struct type.
func fields(t reflect.Type) (out []field, err error) {
	seen := make(map[string]string, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous {
			err = &SchemaError{Type: t.String(), Field: f.Name, Problem: "embedded fields are not supported"}
			return
		}
		if !f.IsExported() {
			continue
		}
		name := ""
		omittable := false
		if tag, ok := f.Tag.Lookup("json"); ok {
			var opts string
			name, opts, _ = strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			for _, o := range strings.Split(opts, ",") {
				if o == "omitempty" || o == "omitzero" {
					omittable = true
				}
			}
		}
		if name == "" {
			name = SnakeCase(f.Name)
		}
		if prev, dup := seen[name]; dup {
			err = &SchemaError{Type: t.String(), Field: f.Name, Problem: "shares the name " + name + " with " + prev}
			return
		}
		seen[name] = f.Name
		out = append(out, field{index: i, name: name, desc: f.Tag.Get("desc"), optional: f.Type.Kind() == reflect.Pointer || omittable})
	}
	return
}

// FieldNames lists the model-visible field names of a struct type, in
// declaration order. A catalog uses it to check the arguments it declares as
// references.
func FieldNames(t reflect.Type) (names []string, err error) {
	if t == nil || t.Kind() != reflect.Struct {
		err = eh.Errorf("opjson: field names of a non-struct type")
		return
	}
	fs, err := fields(t)
	if err != nil {
		return
	}
	names = make([]string, 0, len(fs))
	for _, f := range fs {
		names = append(names, f.name)
	}
	return
}

// SnakeCase converts a Go identifier to snake_case, keeping acronyms
// together: "ResultRef" → "result_ref", "SQLText" → "sql_text".
func SnakeCase(s string) (out string) {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		if unicode.IsUpper(r) {
			if i > 0 && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1]) ||
				(i+1 < len(rs) && unicode.IsLower(rs[i+1]) && unicode.IsUpper(rs[i-1]))) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	out = b.String()
	return
}

func enumNames(t reflect.Type) (names []string, ok bool) {
	if !t.Implements(typeEnum) {
		return
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
	default:
		return
	}
	names = reflect.Zero(t).Interface().(EnumI).OpEnumNames()
	ok = true
	return
}

// Schema derives the JSON Schema for t. A nil t is the schema of an
// operation without arguments or result: an empty object.
func Schema(t reflect.Type) (schema jsontext.Value, err error) {
	var node map[string]any
	if t == nil {
		node = map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
	} else {
		node, err = schemaOf(t, make(map[reflect.Type]bool))
		if err != nil {
			return
		}
	}
	b, err := json.Marshal(node, json.Deterministic(true))
	if err != nil {
		err = eh.Errorf("opjson: marshal schema: %w", err)
		return
	}
	schema = jsontext.Value(b)
	return
}

func schemaOf(t reflect.Type, active map[reflect.Type]bool) (node map[string]any, err error) {
	if names, ok := enumNames(t); ok {
		node = map[string]any{"type": "string", "enum": names}
		return
	}
	switch t {
	case typeTime:
		node = map[string]any{"type": "string", "format": "date-time"}
		return
	case typeDuration:
		node = map[string]any{"type": "string", "description": "a Go duration, e.g. 1.5s"}
		return
	}
	switch t.Kind() {
	case reflect.Bool:
		node = map[string]any{"type": "boolean"}
	case reflect.String:
		node = map[string]any{"type": "string"}
	case reflect.Int8, reflect.Int16, reflect.Int32:
		bits := t.Bits()
		node = map[string]any{"type": "integer", "minimum": -(int64(1) << (bits - 1)), "maximum": int64(1)<<(bits-1) - 1}
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		node = map[string]any{"type": "integer", "minimum": 0, "maximum": uint64(1)<<t.Bits() - 1}
	case reflect.Int, reflect.Int64:
		node = map[string]any{"type": "string", "pattern": "^-?[0-9]+$", "description": "a 64-bit integer as a decimal string"}
	case reflect.Uint, reflect.Uint64:
		node = map[string]any{"type": "string", "pattern": "^[0-9]+$", "description": "a 64-bit unsigned integer as a decimal string"}
	case reflect.Float32, reflect.Float64:
		node = map[string]any{"type": "number"}
	case reflect.Pointer:
		var inner map[string]any
		inner, err = schemaOf(t.Elem(), active)
		if err != nil {
			return
		}
		node = map[string]any{"anyOf": []any{inner, map[string]any{"type": "null"}}}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 && t.Kind() == reflect.Slice {
			node = map[string]any{"type": "string", "pattern": "^([0-9a-f]{2})*$", "description": "bytes as lowercase hex"}
			return
		}
		var items map[string]any
		items, err = schemaOf(t.Elem(), active)
		if err != nil {
			return
		}
		node = map[string]any{"type": "array", "items": items}
		if t.Kind() == reflect.Array {
			node["minItems"] = t.Len()
			node["maxItems"] = t.Len()
		}
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			err = &SchemaError{Type: t.String(), Problem: "map keys must be strings"}
			return
		}
		var vals map[string]any
		vals, err = schemaOf(t.Elem(), active)
		if err != nil {
			return
		}
		node = map[string]any{"type": "object", "additionalProperties": vals}
	case reflect.Struct:
		if active[t] {
			err = &SchemaError{Type: t.String(), Problem: "recursive types are not supported"}
			return
		}
		active[t] = true
		defer delete(active, t)
		var fs []field
		fs, err = fields(t)
		if err != nil {
			return
		}
		props := make(map[string]any, len(fs))
		required := make([]string, 0, len(fs))
		for _, f := range fs {
			var p map[string]any
			p, err = schemaOf(t.Field(f.index).Type, active)
			if err != nil {
				return
			}
			if f.desc != "" {
				p["description"] = f.desc
			}
			props[f.name] = p
			if !f.optional {
				required = append(required, f.name)
			}
		}
		node = map[string]any{"type": "object", "properties": props, "additionalProperties": false}
		if len(required) > 0 {
			node["required"] = required
		}
	default:
		err = &SchemaError{Type: t.String(), Problem: "unsupported kind " + t.Kind().String()}
	}
	return
}

// Decode parses data as JSON and converts it to a value of type t under the
// rules of the package. A nil t accepts only an empty object and returns the
// zero Value. The returned value is addressable.
func Decode(data []byte, t reflect.Type) (v reflect.Value, err error) {
	var raw any
	if len(data) == 0 {
		data = []byte("{}")
	}
	err = json.Unmarshal(data, &raw)
	if err != nil {
		err = eh.Errorf("opjson: parse: %w", err)
		return
	}
	if t == nil {
		m, ok := raw.(map[string]any)
		if !ok || len(m) != 0 {
			err = &ValueError{Path: "$", Problem: "this operation takes no arguments; send {}"}
		}
		return
	}
	v = reflect.New(t).Elem()
	err = assign(v, raw, "$")
	return
}

func pathErr(path string, problem string) (err error) {
	err = &ValueError{Path: path, Problem: problem}
	return
}

func assign(v reflect.Value, raw any, path string) (err error) {
	t := v.Type()
	if names, ok := enumNames(t); ok {
		s, isStr := raw.(string)
		if !isStr {
			return pathErr(path, "expected one of "+strings.Join(names, ", "))
		}
		for i, n := range names {
			if n == s {
				if t.Kind() >= reflect.Uint && t.Kind() <= reflect.Uint64 {
					v.SetUint(uint64(i))
				} else {
					v.SetInt(int64(i))
				}
				return
			}
		}
		return pathErr(path, "unknown name "+strconv.Quote(s)+", expected one of "+strings.Join(names, ", "))
	}
	switch t {
	case typeTime:
		s, isStr := raw.(string)
		if !isStr {
			return pathErr(path, "expected an RFC 3339 time string")
		}
		ts, perr := time.Parse(time.RFC3339Nano, s)
		if perr != nil {
			return pathErr(path, "expected an RFC 3339 time string")
		}
		v.Set(reflect.ValueOf(ts))
		return
	case typeDuration:
		s, isStr := raw.(string)
		if !isStr {
			return pathErr(path, "expected a duration string such as 1.5s")
		}
		d, perr := time.ParseDuration(s)
		if perr != nil {
			return pathErr(path, "expected a duration string such as 1.5s")
		}
		v.SetInt(int64(d))
		return
	}
	switch t.Kind() {
	case reflect.Bool:
		b, ok := raw.(bool)
		if !ok {
			return pathErr(path, "expected a boolean")
		}
		v.SetBool(b)
	case reflect.String:
		s, ok := raw.(string)
		if !ok {
			return pathErr(path, "expected a string")
		}
		v.SetString(s)
	case reflect.Int8, reflect.Int16, reflect.Int32:
		f, ok := raw.(float64)
		if !ok || f != math.Trunc(f) || v.OverflowInt(int64(f)) {
			return pathErr(path, "expected an integer within "+t.String()+" range")
		}
		v.SetInt(int64(f))
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		f, ok := raw.(float64)
		if !ok || f != math.Trunc(f) || f < 0 || v.OverflowUint(uint64(f)) {
			return pathErr(path, "expected a non-negative integer within "+t.String()+" range")
		}
		v.SetUint(uint64(f))
	case reflect.Int, reflect.Int64:
		s, ok := raw.(string)
		if !ok {
			return pathErr(path, "expected a 64-bit integer as a decimal string")
		}
		n, perr := strconv.ParseInt(s, 10, t.Bits())
		if perr != nil {
			return pathErr(path, "expected a 64-bit integer as a decimal string")
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint64:
		s, ok := raw.(string)
		if !ok {
			return pathErr(path, "expected a 64-bit unsigned integer as a decimal string")
		}
		n, perr := strconv.ParseUint(s, 10, t.Bits())
		if perr != nil {
			return pathErr(path, "expected a 64-bit unsigned integer as a decimal string")
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, ok := raw.(float64)
		if !ok {
			return pathErr(path, "expected a number")
		}
		if v.OverflowFloat(f) {
			return pathErr(path, "number out of "+t.String()+" range")
		}
		v.SetFloat(f)
	case reflect.Pointer:
		if raw == nil {
			v.SetZero()
			return
		}
		p := reflect.New(t.Elem())
		err = assign(p.Elem(), raw, path)
		if err != nil {
			return
		}
		v.Set(p)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			s, ok := raw.(string)
			if !ok {
				return pathErr(path, "expected bytes as a lowercase hex string")
			}
			b, perr := decodeHex(s)
			if perr != nil {
				return pathErr(path, "expected bytes as a lowercase hex string")
			}
			v.SetBytes(b)
			return
		}
		arr, ok := raw.([]any)
		if !ok {
			return pathErr(path, "expected an array")
		}
		s := reflect.MakeSlice(t, len(arr), len(arr))
		for i, e := range arr {
			err = assign(s.Index(i), e, path+"["+strconv.Itoa(i)+"]")
			if err != nil {
				return
			}
		}
		v.Set(s)
	case reflect.Array:
		arr, ok := raw.([]any)
		if !ok || len(arr) != t.Len() {
			return pathErr(path, "expected an array of "+strconv.Itoa(t.Len()))
		}
		for i, e := range arr {
			err = assign(v.Index(i), e, path+"["+strconv.Itoa(i)+"]")
			if err != nil {
				return
			}
		}
	case reflect.Map:
		obj, ok := raw.(map[string]any)
		if !ok {
			return pathErr(path, "expected an object")
		}
		m := reflect.MakeMapWithSize(t, len(obj))
		for k, e := range obj {
			ev := reflect.New(t.Elem()).Elem()
			err = assign(ev, e, path+"."+k)
			if err != nil {
				return
			}
			m.SetMapIndex(reflect.ValueOf(k).Convert(t.Key()), ev)
		}
		v.Set(m)
	case reflect.Struct:
		obj, ok := raw.(map[string]any)
		if !ok {
			return pathErr(path, "expected an object")
		}
		var fs []field
		fs, err = fields(t)
		if err != nil {
			return
		}
		known := make(map[string]bool, len(fs))
		for _, f := range fs {
			known[f.name] = true
			e, present := obj[f.name]
			if !present {
				if !f.optional {
					return pathErr(path+"."+f.name, "required")
				}
				continue
			}
			err = assign(v.Field(f.index), e, path+"."+f.name)
			if err != nil {
				return
			}
		}
		for k := range obj {
			if !known[k] {
				return pathErr(path+"."+k, "unknown field")
			}
		}
	default:
		return pathErr(path, "unsupported type "+t.String())
	}
	return
}

func decodeHex(s string) (b []byte, err error) {
	if len(s)%2 != 0 {
		err = eh.Errorf("odd length")
		return
	}
	b = make([]byte, len(s)/2)
	for i := range b {
		hi, lo := hexNibble(s[2*i]), hexNibble(s[2*i+1])
		if hi < 0 || lo < 0 {
			err = eh.Errorf("not lowercase hex")
			return
		}
		b[i] = byte(hi<<4 | lo)
	}
	return
}

func hexNibble(c byte) (n int) {
	switch {
	case c >= '0' && c <= '9':
		n = int(c - '0')
	case c >= 'a' && c <= 'f':
		n = int(c-'a') + 10
	default:
		n = -1
	}
	return
}

const hexDigits = "0123456789abcdef"

// Encode writes v as JSON under the rules of the package. Object members
// come in field declaration order, map members in key order.
func Encode(v any) (data []byte, err error) {
	var b strings.Builder
	enc := jsontext.NewEncoder(&b)
	if v == nil {
		err = enc.WriteToken(jsontext.BeginObject)
		if err == nil {
			err = enc.WriteToken(jsontext.EndObject)
		}
	} else {
		err = write(enc, reflect.ValueOf(v), "$")
	}
	if err != nil {
		return
	}
	data = []byte(strings.TrimSpace(b.String()))
	return
}

func write(enc *jsontext.Encoder, v reflect.Value, path string) (err error) {
	t := v.Type()
	if names, ok := enumNames(t); ok {
		var i uint64
		if t.Kind() >= reflect.Uint && t.Kind() <= reflect.Uint64 {
			i = v.Uint()
		} else if v.Int() >= 0 {
			i = uint64(v.Int())
		} else {
			return pathErr(path, "enum value out of range")
		}
		if i >= uint64(len(names)) {
			return pathErr(path, "enum value out of range")
		}
		return enc.WriteToken(jsontext.String(names[i]))
	}
	switch t {
	case typeTime:
		return enc.WriteToken(jsontext.String(v.Interface().(time.Time).Format(time.RFC3339Nano)))
	case typeDuration:
		return enc.WriteToken(jsontext.String(time.Duration(v.Int()).String()))
	}
	switch t.Kind() {
	case reflect.Bool:
		err = enc.WriteToken(jsontext.Bool(v.Bool()))
	case reflect.String:
		err = enc.WriteToken(jsontext.String(v.String()))
	case reflect.Int8, reflect.Int16, reflect.Int32:
		err = enc.WriteToken(jsontext.Int(v.Int()))
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		err = enc.WriteToken(jsontext.Uint(v.Uint()))
	case reflect.Int, reflect.Int64:
		err = enc.WriteToken(jsontext.String(strconv.FormatInt(v.Int(), 10)))
	case reflect.Uint, reflect.Uint64:
		err = enc.WriteToken(jsontext.String(strconv.FormatUint(v.Uint(), 10)))
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return pathErr(path, "NaN and infinities have no JSON form")
		}
		err = enc.WriteToken(jsontext.Float(f))
	case reflect.Pointer:
		if v.IsNil() {
			return enc.WriteToken(jsontext.Null)
		}
		err = write(enc, v.Elem(), path)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			b := v.Bytes()
			out := make([]byte, 2*len(b))
			for i, c := range b {
				out[2*i], out[2*i+1] = hexDigits[c>>4], hexDigits[c&0xf]
			}
			return enc.WriteToken(jsontext.String(string(out)))
		}
		fallthrough
	case reflect.Array:
		if err = enc.WriteToken(jsontext.BeginArray); err != nil {
			return
		}
		for i := range v.Len() {
			if err = write(enc, v.Index(i), path+"["+strconv.Itoa(i)+"]"); err != nil {
				return
			}
		}
		err = enc.WriteToken(jsontext.EndArray)
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return pathErr(path, "map keys must be strings")
		}
		keys := v.MapKeys()
		strs := make([]string, len(keys))
		for i, k := range keys {
			strs[i] = k.String()
		}
		order := make([]int, len(keys))
		for i := range order {
			order[i] = i
		}
		sortByString(order, strs)
		if err = enc.WriteToken(jsontext.BeginObject); err != nil {
			return
		}
		for _, i := range order {
			if err = enc.WriteToken(jsontext.String(strs[i])); err != nil {
				return
			}
			if err = write(enc, v.MapIndex(keys[i]), path+"."+strs[i]); err != nil {
				return
			}
		}
		err = enc.WriteToken(jsontext.EndObject)
	case reflect.Struct:
		var fs []field
		fs, err = fields(t)
		if err != nil {
			return
		}
		if err = enc.WriteToken(jsontext.BeginObject); err != nil {
			return
		}
		for _, f := range fs {
			if err = enc.WriteToken(jsontext.String(f.name)); err != nil {
				return
			}
			if err = write(enc, v.Field(f.index), path+"."+f.name); err != nil {
				return
			}
		}
		err = enc.WriteToken(jsontext.EndObject)
	default:
		err = pathErr(path, "unsupported type "+t.String())
	}
	return
}

func sortByString(order []int, strs []string) {
	// Insertion sort: maps in operation results are small, and this keeps
	// the package free of a sort import for one call.
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && strs[order[j]] < strs[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
}
