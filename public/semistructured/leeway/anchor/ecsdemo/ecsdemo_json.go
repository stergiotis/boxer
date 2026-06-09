package ecsdemo

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// kindAny marks a field whose value kind we do not constrain (interfaces and
// the like): any incoming token kind is accepted by the approximate check.
const kindAny jsontext.Kind = 0

// fieldSpec is the reflected, json-visible description of one struct field used
// by the shape checks. It is derived once per type and cached.
type fieldSpec struct {
	name     string        // json member name
	kind     jsontext.Kind // expected top-level token kind of the value
	optional bool          // omitzero/omitempty/pointer ⇒ the key may be absent
	typ      reflect.Type  // concrete Go type, for Subset's structural comparison
}

// typeSpec is the cached shape of a component struct type.
type typeSpec struct {
	byName       map[string]fieldSpec // json name -> field
	fields       []fieldSpec          // declaration order
	mandatoryLen int                  // count of non-optional fields
}

var specCache sync.Map // reflect.Type -> *typeSpec

// specOf returns the cached shape of component type T. T must be a struct.
func specOf[T any]() *typeSpec {
	t := reflect.TypeFor[T]()
	if t.Kind() != reflect.Struct {
		panic("ecsdemo: component type must be a struct, got " + t.String())
	}
	if s, ok := specCache.Load(t); ok {
		return s.(*typeSpec)
	}
	s := buildSpec(t)
	specCache.Store(t, s)
	return s
}

func buildSpec(t reflect.Type) *typeSpec {
	s := &typeSpec{byName: make(map[string]fieldSpec, t.NumField())}
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		name, opts, skip := parseJSONTag(f)
		if skip {
			continue
		}
		fs := fieldSpec{
			name:     name,
			kind:     expectedKind(f.Type),
			optional: isOptional(f.Type, opts),
			typ:      f.Type,
		}
		s.fields = append(s.fields, fs)
		s.byName[name] = fs
		if !fs.optional {
			s.mandatoryLen++
		}
	}
	return s
}

// parseJSONTag extracts the json member name and tag options. A `json:"-"` tag
// (without a trailing comma) marks the field as not serialized.
func parseJSONTag(f reflect.StructField) (name string, opts []string, skip bool) {
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return f.Name, nil, false
	}
	if tag == "-" {
		return "", nil, true
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = f.Name
	}
	return name, parts[1:], false
}

// expectedKind maps a Go type to the JSON token kind json/v2 emits for it. A
// boolean is reported as KindTrue and treated as {true,false} by kindCompatible.
// []byte / [N]byte encode as base64 strings, not arrays.
func expectedKind(t reflect.Type) jsontext.Kind {
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
			return jsontext.KindString // base64
		}
		return jsontext.KindBeginArray
	case reflect.Map, reflect.Struct:
		return jsontext.KindBeginObject
	default:
		return kindAny
	}
}

// isOptional reports whether a field's key may be absent: pointers are always
// optional, and the omitzero / omitempty tag options make any field optional.
func isOptional(t reflect.Type, opts []string) bool {
	if t.Kind() == reflect.Pointer {
		return true
	}
	for _, o := range opts {
		if o == "omitzero" || o == "omitempty" {
			return true
		}
	}
	return false
}

// kindCompatible reports whether an observed value kind could decode into the
// field. It accepts null universally because json/v2 accepts null for any type
// (resetting it to the zero value), so rejecting null would make the
// approximate check unsound (it would report a false where the exact check
// succeeds).
func kindCompatible(fs fieldSpec, k jsontext.Kind) bool {
	switch {
	case k == jsontext.KindNull:
		return true
	case fs.kind == kindAny:
		return true
	case fs.kind == jsontext.KindTrue: // boolean
		return k == jsontext.KindTrue || k == jsontext.KindFalse
	default:
		return k == fs.kind
	}
}

// Presence is the APPROXIMATE per-component check: it reports whether data could
// be unserialized into component T using a single jsontext token scan, without
// ever materializing a T. It confirms data is a JSON object whose top-level
// members include every mandatory field of T with a compatible value kind.
//
// The guarantee is one-sided, like leeway's ADR-0066 presence prefilter: if
// Presence returns false, Validate[T] is guaranteed to fail too; if it returns
// true, Validate[T] may still fail (a nested object may be malformed, a number
// may overflow, an unknown member may be present).
func Presence[T any](data []byte) bool {
	spec := specOf[T]()
	dec := jsontext.NewDecoder(bytes.NewReader(data))

	tok, err := dec.ReadToken()
	if err != nil || tok.Kind() != jsontext.KindBeginObject {
		return false
	}
	seen := 0
	for {
		nameTok, err := dec.ReadToken()
		if err != nil {
			return false // malformed, or a duplicate member name (rejected by default)
		}
		if nameTok.Kind() == jsontext.KindEndObject {
			break
		}
		fs, isField := spec.byName[nameTok.String()]
		valueKind := dec.PeekKind()
		if isField && !fs.optional && kindCompatible(fs, valueKind) {
			seen++
		}
		if err := dec.SkipValue(); err != nil {
			return false
		}
	}
	return seen == spec.mandatoryLen
}

// Validate is the EXACT per-component check: it returns nil exactly when data is
// a JSON object that (a) carries every mandatory field of T — the Presence
// precondition — and (b) strictly unmarshals into a T with no unknown members.
//
// Presence runs first, so it is a strict sub-computation: when the approximate
// check fails, Validate fails without attempting the full decode. This is the
// Validator ⊇ Presence layering of ADR-0066.
func Validate[T any](data []byte) error {
	if !Presence[T](data) {
		return fmt.Errorf("ecsdemo: presence check failed: data is not an object carrying every mandatory field of %s", typeName[T]())
	}
	var v T
	if err := json.Unmarshal(data, &v, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("ecsdemo: strict unmarshal into %s: %w", typeName[T](), err)
	}
	return nil
}

// Unmarshal is the PROJECTION: it deserializes data into a component T with the
// same strict options Validate uses. It does not enforce mandatory-field
// presence; pair it with Validate for the exact gate.
func Unmarshal[T any](data []byte) (T, error) {
	var v T
	if err := json.Unmarshal(data, &v, json.RejectUnknownMembers(true)); err != nil {
		return v, fmt.Errorf("ecsdemo: unmarshal into %s: %w", typeName[T](), err)
	}
	return v, nil
}

// Subset reports whether component A is a structural (field) subset of component
// B: every json-visible field of A appears in B under the same json name and
// with an identical Go type (A ⊆ B). It is the pure-reflection analogue of
// leeway's common.TableOperations.Subset, and the field-level granularity that
// Archetype.SubsetOf lifts to the component-set level.
func Subset[A, B any]() bool {
	sa, sb := specOf[A](), specOf[B]()
	for _, fa := range sa.fields {
		fb, ok := sb.byName[fa.name]
		if !ok || fb.typ != fa.typ {
			return false
		}
	}
	return true
}

func typeName[T any]() string {
	return reflect.TypeFor[T]().Name()
}

// --- archetype-level checks: the per-component pair, lifted to component sets ---

// rawComponent is one top-level member of an entity document: its raw JSON bytes
// and value kind, captured in a single scan.
type rawComponent struct {
	value []byte
	kind  jsontext.Kind
}

// entityMembers scans an entity document into its component members keyed by
// kind, dropping the "id" member (the entity's join key, not a component).
func entityMembers(doc []byte) (map[ComponentKind]rawComponent, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(doc))
	tok, err := dec.ReadToken()
	if err != nil || tok.Kind() != jsontext.KindBeginObject {
		return nil, fmt.Errorf("ecsdemo: entity document is not a JSON object")
	}
	out := make(map[ComponentKind]rawComponent)
	for {
		nameTok, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}
		if nameTok.Kind() == jsontext.KindEndObject {
			break
		}
		name := nameTok.String()
		kind := dec.PeekKind()
		val, err := dec.ReadValue()
		if err != nil {
			return nil, err
		}
		if name == "id" {
			continue
		}
		out[ComponentKind(name)] = rawComponent{value: append([]byte(nil), val...), kind: kind}
	}
	return out, nil
}

// componentValidators binds each component kind to its exact per-component check,
// so the archetype checks can validate components generically.
var componentValidators = map[ComponentKind]func([]byte) error{
	KindIdentity: Validate[Identity],
	KindBattery:  Validate[Battery],
	KindLocated:  Validate[Located],
	KindTasked:   Validate[Tasked],
}

// ArchetypePresence is the APPROXIMATE archetype check: it reports whether doc
// could be an entity of archetype arch, by confirming every required component
// appears as a top-level object member. It does not look inside the components,
// so it is necessary but not sufficient — the per-component Presence guarantee,
// lifted to the component-set level.
func ArchetypePresence(doc []byte, arch Archetype) bool {
	m, err := entityMembers(doc)
	if err != nil {
		return false
	}
	for _, k := range arch {
		rc, ok := m[k]
		if !ok {
			return false
		}
		if rc.kind != jsontext.KindBeginObject && rc.kind != jsontext.KindNull {
			return false
		}
	}
	return true
}

// ArchetypeValidate is the EXACT archetype check: doc is a valid entity of
// archetype arch iff every required component is present and individually Valid,
// and no component outside arch is present (reject-unknown lifted to the
// component-set level). It returns nil on success, else the first failure.
//
// As with the per-component pair, ArchetypePresence is the necessary precheck:
// when it fails, ArchetypeValidate fails without decoding any component.
func ArchetypeValidate(doc []byte, arch Archetype) error {
	if !ArchetypePresence(doc, arch) {
		return fmt.Errorf("ecsdemo: archetype %v: presence check failed", arch)
	}
	m, err := entityMembers(doc)
	if err != nil {
		return err
	}
	want := make(map[ComponentKind]bool, len(arch))
	for _, k := range arch {
		want[k] = true
		validate, known := componentValidators[k]
		if !known {
			return fmt.Errorf("ecsdemo: archetype %v: no validator for component %q", arch, k)
		}
		if err := validate(m[k].value); err != nil {
			return fmt.Errorf("ecsdemo: archetype %v: component %q: %w", arch, k, err)
		}
	}
	for k := range m {
		if !want[k] {
			return fmt.Errorf("ecsdemo: archetype %v: unexpected component %q", arch, k)
		}
	}
	return nil
}
