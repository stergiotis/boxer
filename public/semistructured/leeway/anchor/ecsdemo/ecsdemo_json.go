package ecsdemo

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"reflect"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// invalidKind is jsontext's zero Kind; kindOf returns it for types whose value
// kind we do not constrain (interfaces).
const invalidKind jsontext.Kind = 0

// jsonField is the json-visible shape of one struct field.
type jsonField struct {
	name     string
	required bool          // not a pointer and no omitzero/omitempty
	kind     jsontext.Kind // value kind json/v2 emits for the Go type
	typ      reflect.Type  // for Subset's structural comparison
}

// jsonFields reflects T's json-visible fields. T must be a struct.
func jsonFields[T any]() (out []jsonField) {
	t := reflect.TypeFor[T]()
	out = make([]jsonField, 0, t.NumField())
	for f := range t.Fields() {
		name, opts, ok := jsonName(f)
		if !f.IsExported() || !ok {
			continue
		}
		out = append(out, jsonField{
			name:     name,
			required: !isOptional(f.Type, opts),
			kind:     kindOf(f.Type),
			typ:      f.Type,
		})
	}
	return
}

// jsonName returns f's json member name and tag options; ok is false for json:"-".
func jsonName(f reflect.StructField) (name string, opts []string, ok bool) {
	tag, tagged := f.Tag.Lookup("json")
	if !tagged {
		return f.Name, nil, true
	}
	if tag == "-" {
		return "", nil, false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = f.Name
	}
	return name, parts[1:], true
}

// isOptional reports whether a field's key may be absent (pointer, omitzero or
// omitempty) — the json/v2 analogue of leeway option.Option.
func isOptional(t reflect.Type, opts []string) bool {
	return t.Kind() == reflect.Pointer || slices.Contains(opts, "omitzero") || slices.Contains(opts, "omitempty")
}

// kindOf is the json/v2 token kind a value of type t serializes to. Bool is
// reported as KindTrue (kindOK accepts either literal); []byte/[N]byte encode as
// base64 strings.
func kindOf(t reflect.Type) jsontext.Kind {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return jsontext.KindString
	case reflect.Bool:
		return jsontext.KindTrue
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return jsontext.KindNumber
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return jsontext.KindString
		}
		return jsontext.KindBeginArray
	case reflect.Map, reflect.Struct:
		return jsontext.KindBeginObject
	default:
		return invalidKind
	}
}

// kindOK reports whether an observed value kind could decode into a field whose
// type maps to want. null is always accepted (json/v2 resets any type to its
// zero value), so the approximate check never rejects what the exact check admits.
func kindOK(want, got jsontext.Kind) bool {
	switch {
	case got == jsontext.KindNull, want == invalidKind:
		return true
	case want == jsontext.KindTrue:
		return got == jsontext.KindTrue || got == jsontext.KindFalse
	default:
		return want == got
	}
}

// Presence is the APPROXIMATE per-component check: a single jsontext scan
// reporting whether data is an object carrying every mandatory field of
// component T with a compatible value kind, without materializing a T.
//
// One-sided, like leeway ADR-0066's presence prefilter: a false result
// guarantees Validate[T] also fails; a true result does not guarantee it passes
// (a nested object may be malformed, a number may overflow, a member unknown).
func Presence[T any](data []byte) bool {
	dec := jsontext.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != jsontext.KindBeginObject {
		return false
	}
	present := make(map[string]jsontext.Kind, 8)
	for dec.PeekKind() != jsontext.KindEndObject {
		name, err := dec.ReadToken()
		if err != nil {
			return false
		}
		key := name.String() // capture before the next decoder call voids the token
		present[key] = dec.PeekKind()
		if dec.SkipValue() != nil {
			return false
		}
	}
	for _, f := range jsonFields[T]() {
		if !f.required {
			continue
		}
		if k, ok := present[f.name]; !ok || !kindOK(f.kind, k) {
			return false
		}
	}
	return true
}

// Validate is the EXACT per-component check: nil iff data carries every
// mandatory field of T (the Presence precondition) and strictly unmarshals into
// a T with no unknown members. Presence runs first, so a failing approximate
// check short-circuits — the Validator ⊇ Presence layering of ADR-0066.
func Validate[T any](data []byte) error {
	if !Presence[T](data) {
		return eb.Build().Str("type", typeName[T]()).Errorf("document lacks a mandatory field")
	}
	var v T
	if err := json.Unmarshal(data, &v, json.RejectUnknownMembers(true)); err != nil {
		return eb.Build().Str("type", typeName[T]()).Errorf("strict unmarshal: %w", err)
	}
	return nil
}

// Unmarshal is the PROJECTION: a strict decode of data into component T. It does
// not enforce mandatory-field presence; gate with Validate for the exact check.
func Unmarshal[T any](data []byte) (v T, err error) {
	if err = json.Unmarshal(data, &v, json.RejectUnknownMembers(true)); err != nil {
		err = eb.Build().Str("type", typeName[T]()).Errorf("unmarshal: %w", err)
	}
	return
}

// Subset reports whether component A is a field-subset of B: every json field of
// A appears in B under the same name and Go type (A ⊆ B). It is the
// pure-reflection analogue of leeway's TableOperations.Subset, and the level
// Archetype.SubsetOf lifts to component sets.
func Subset[A, B any]() bool {
	fieldsB := jsonFields[B]()
	inB := make(map[string]reflect.Type, len(fieldsB))
	for _, f := range fieldsB {
		inB[f.name] = f.typ
	}
	for _, f := range jsonFields[A]() {
		if inB[f.name] != f.typ {
			return false
		}
	}
	return true
}

func typeName[T any]() string {
	return reflect.TypeFor[T]().Name()
}

// rawComponent is one entity-document member: its raw bytes and value kind.
type rawComponent struct {
	value []byte
	kind  jsontext.Kind
}

// entityMembers scans an entity document into its component members keyed by
// kind, dropping the "id" join key.
func entityMembers(doc []byte) (map[ComponentKindE]rawComponent, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(doc))
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != jsontext.KindBeginObject {
		return nil, eh.New("entity document is not a JSON object")
	}
	out := make(map[ComponentKindE]rawComponent, 4)
	for dec.PeekKind() != jsontext.KindEndObject {
		name, err := dec.ReadToken()
		if err != nil {
			return nil, eh.Errorf("read entity member: %w", err)
		}
		key := name.String() // capture before the next decoder call voids the token
		kind := dec.PeekKind()
		val, err := dec.ReadValue()
		if err != nil {
			return nil, eh.Errorf("read entity member value: %w", err)
		}
		if key != "id" {
			out[ComponentKindE(key)] = rawComponent{value: bytes.Clone(val), kind: kind}
		}
	}
	return out, nil
}

// componentValidators binds each kind to its exact per-component check.
var componentValidators = map[ComponentKindE]func([]byte) error{
	KindIdentity: Validate[Identity],
	KindBattery:  Validate[Battery],
	KindLocated:  Validate[Located],
	KindTasked:   Validate[Tasked],
}

// ArchetypePresence is the APPROXIMATE archetype check: every required component
// of arch appears as a top-level object member. Necessary, not sufficient — the
// per-component Presence guarantee lifted to the component set.
func ArchetypePresence(doc []byte, arch Archetype) bool {
	m, err := entityMembers(doc)
	if err != nil {
		return false
	}
	for _, k := range arch {
		rc, ok := m[k]
		if !ok || (rc.kind != jsontext.KindBeginObject && rc.kind != jsontext.KindNull) {
			return false
		}
	}
	return true
}

// ArchetypeValidate is the EXACT archetype check: nil iff every required
// component is present and individually Valid, with no component outside arch
// (reject-unknown lifted to the component set). ArchetypePresence is the
// necessary precheck.
func ArchetypeValidate(doc []byte, arch Archetype) error {
	if !ArchetypePresence(doc, arch) {
		return eh.New("approximate archetype presence check failed")
	}
	m, err := entityMembers(doc)
	if err != nil {
		return err
	}
	required := make(map[ComponentKindE]bool, len(arch))
	for _, k := range arch {
		required[k] = true
		validate, ok := componentValidators[k]
		if !ok {
			return eb.Build().Str("component", string(k)).Errorf("no validator for component")
		}
		if err := validate(m[k].value); err != nil {
			return eb.Build().Str("component", string(k)).Errorf("invalid component: %w", err)
		}
	}
	for k := range m {
		if !required[k] {
			return eb.Build().Str("component", string(k)).Errorf("unexpected component")
		}
	}
	return nil
}
