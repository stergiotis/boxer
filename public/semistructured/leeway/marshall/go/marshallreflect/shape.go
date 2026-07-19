package marshallreflect

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

// optionPkgPath is the import path of the new boxer-side Option type.
// Recognised by classifyReflectType so DTO authors can use
// option.Option[T] without the reflect parser needing to scan the source.
const optionPkgPath = "github.com/stergiotis/boxer/public/functional/option"

// roaringPkgPath is the import path of the *roaring.Bitmap type — the
// one pointer shape marshallgen / marshallreflect both allow on a DTO
// field.
const roaringPkgPath = "github.com/RoaringBitmap/roaring"

// marshalltypesPkgPath is the import path of the Cut-2 carrier structs
// (marshalltypes.MixedLowCardRef, …). A struct field from this package is
// a carrier, paired with its value sibling by goplan.PlanBuilder.
const marshalltypesPkgPath = "github.com/stergiotis/boxer/public/semistructured/leeway/marshall/marshalltypes"

// lwPkgPath is the import path of the leeway nested-model marker package. A
// field whose type is from this package is a marker — a value-shape marker
// (lw.Single, the unit shape) or a canonical lane (lw.IPv4 / lw.IPv6).
const lwPkgPath = "github.com/stergiotis/boxer/public/semistructured/leeway/marshall/lw"

// laneCanonical maps an lw lane type's name to the canonical string it relabels
// its (type-fixed) bytes as — the type-safe form of a `,ct=` override.
var laneCanonical = map[string]string{
	"IPv4":       "v",
	"IPv6":       "w",
	"IPv4Prefix": "vc",
	"IPv6Prefix": "wc",
}

// membershipMarkerChannel maps an lw membership marker type's name to its
// leeway channel — the type-safe form of the tuple `@membership,<channel>` tag.
var membershipMarkerChannel = map[string]mappingplan.MembershipChannel{
	"Ref":          mappingplan.MembershipChannelLowCardRef,
	"HighRef":      mappingplan.MembershipChannelHighCardRef,
	"Verbatim":     mappingplan.MembershipChannelLowCardVerbatim,
	"HighVerbatim": mappingplan.MembershipChannelHighCardVerbatim,
}

// classifyLwMarker classifies a field whose type is from the lw marker package.
// lw.Single[T] is the unit shape — a container sub-column carrying one element
// supplied as the scalar T (routed to BeginAttributeSingle via the Unit flag);
// a lane type (lw.IPv4 / lw.IPv6) relabels its fixed bytes to a network
// canonical, exactly like `,ct=`.
func classifyLwMarker(rt reflect.Type) (s goplan.FieldShape, err error) {
	name := rt.Name()
	// The as-written marker type ("lw.Ref", "lw.Single[uint64]", "lw.IPv4") —
	// carried through the shared Plan for the codegen codec's newtype bridging;
	// the reflect codec ignores it (it converts off the live reflect.Type).
	s.MarkerGoType = rt.String()
	if strings.HasPrefix(name, "Single[") {
		vf, ok := rt.FieldByName("Val")
		if !ok {
			err = eb.Build().Str("type", rt.String()).Errorf("lw.Single without Val field — wrong shape")
			return
		}
		s.Canonical, err = goplan.ScalarCanonicalForGoType(reflectGoTypeName(vf.Type))
		s.Unit = true
		return
	}
	if ct, ok := laneCanonical[name]; ok {
		s.Canonical, err = canonicaltypes.NewParser().ParsePrimitiveTypeAst(ct)
		if err != nil {
			err = eb.Build().Str("type", rt.String()).Str("canonical", ct).Errorf("lw lane canonical failed to parse: %w", err)
		}
		return
	}
	if ch, ok := membershipMarkerChannel[name]; ok {
		// A membership marker: the value type is uint64 (ref id) or string
		// (verbatim name); AddTupleSliceField reads MembershipChannel + the
		// Canonical to build the TupleMembership.
		s.IsMembership = true
		s.MembershipChannel = ch
		goType := "uint64"
		if ch.EmbedsLiteralName() {
			goType = "string"
		}
		s.Canonical, err = goplan.ScalarCanonicalForGoType(goType)
		return
	}
	err = eb.Build().Str("type", rt.String()).Errorf("unknown lw marker type (recognised: Single[T], IPv4, IPv6, IPv4Prefix, IPv6Prefix, Ref, HighRef, Verbatim, HighVerbatim)")
	return
}

// isLwSingleType reports whether t is an lw.Single[T] wrapper.
func isLwSingleType(t reflect.Type) bool {
	return t.Kind() == reflect.Struct && t.PkgPath() == lwPkgPath && strings.HasPrefix(t.Name(), "Single[")
}

// isLwMembershipType reports whether t (or its slice element, for a repeated
// membership) is an lw membership marker (lw.Ref / lw.Verbatim / …). Used by the
// tuple detector to route a `[]S` whose element carries marker-typed memberships
// to the dynamic-tuple path.
func isLwMembershipType(t reflect.Type) bool {
	if t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.PkgPath() != lwPkgPath {
		return false
	}
	_, ok := membershipMarkerChannel[t.Name()]
	return ok
}

// unwrapLwSingle returns v.Val when v is an lw.Single[T] wrapper (so the codec
// passes the wrapped scalar to the DML), else v unchanged. A no-op for every
// non-marker field.
func unwrapLwSingle(v reflect.Value) reflect.Value {
	if isLwSingleType(v.Type()) {
		return v.FieldByName("Val")
	}
	return v
}

// setScalarField sets a scalar Go field from a decoded value, bridging the
// nested-model marker types whose Go type differs from the canonical's plain Go
// type: an lw.Single[T] wrapper receives the value in its Val field; a lane
// newtype (lw.IPv4 = [4]byte, …) receives it via a Convert. Plain fields (the
// value type already matches) take the fast path.
func setScalarField(fld, val reflect.Value) {
	ft := fld.Type()
	switch {
	case isLwSingleType(ft):
		fld.FieldByName("Val").Set(val)
	case val.Type() != ft && val.Type().ConvertibleTo(ft):
		fld.Set(val.Convert(ft))
	default:
		fld.Set(val)
	}
}

// classifyReflectType inspects rt and returns the corresponding shared
// goplan.FieldShape (consumed by goplan.PlanBuilder). It is
// canonical-native: the shape's value type is a leeway Canonical (the
// Go-facing GoType / IsSlice / IsRoaring are derived from it by
// PlanBuilder). Forbids the same Go shapes the codegen classifier forbids:
// Option[[]T] (except Option[[]byte]), []Option[T], arbitrary pointers
// other than *roaring.Bitmap, nested generics other than option.Option.
// The Go-side spelling each reflect kind maps to is the same go-type token
// the AST classifier produces ("uint64", "time.Time", "[4]byte", "[]byte",
// …); both front-ends funnel those tokens through
// goplan.ScalarCanonicalForGoType so they cannot drift.
func classifyReflectType(rt reflect.Type) (s goplan.FieldShape, err error) {
	// lw marker types (nested-model value-shape markers + lanes) are recognised
	// by package, ahead of the structural switch — lw.Single[T] is a struct that
	// would otherwise be misread, and the lanes are named byte types.
	if rt.PkgPath() == lwPkgPath {
		return classifyLwMarker(rt)
	}
	switch rt.Kind() {

	case reflect.Ptr:
		elem := rt.Elem()
		if elem.PkgPath() == roaringPkgPath && elem.Name() == "Bitmap" {
			// A roaring bitmap is a Set of uint32 in the canonical model.
			s.Canonical = canonicaltypes.PromoteScalarPrim(goplan.RoaringElemCanonical(), canonicaltypes.ScalarModifierSet)
			return
		}
		err = eb.Build().Str("type", rt.String()).Errorf("pointer types forbidden except *roaring.Bitmap — use option.Option[T] for ZeroToOne fields")
		return

	case reflect.Struct:
		// marshalltypes carrier (Cut-2): recognised by package + name;
		// paired with its value sibling in goplan.PlanBuilder.
		if rt.PkgPath() == marshalltypesPkgPath {
			s.CarrierType = rt.Name()
			return
		}
		// option.Option[T] is the only struct shape the codec accepts
		// on a tagged field. Pure-value types like time.Time get a
		// fast-path further down via reflectGoTypeName.
		if rt.PkgPath() == optionPkgPath {
			s.IsOption = true
			valField, ok := rt.FieldByName("Val")
			if !ok {
				err = eb.Build().Str("type", rt.String()).Errorf("option.Option without Val field — wrong shape")
				return
			}
			// Reject option.Option[[]T] (except option.Option[[]byte]).
			vt := valField.Type
			if vt.Kind() == reflect.Slice {
				if vt.Elem().Kind() == reflect.Uint8 {
					s.Canonical, err = goplan.ScalarCanonicalForGoType("[]byte")
					return
				}
				err = eb.Build().Str("type", rt.String()).Errorf("option.Option[[]T] is forbidden — use []T for multi-element membership (option.Option[[]byte] is allowed as a scalar blob)")
				return
			}
			s.Canonical, err = goplan.ScalarCanonicalForGoType(reflectGoTypeName(vt))
			return
		}
		// time.Time and any other pure-value struct routes through
		// reflectGoTypeName below.
		s.Canonical, err = goplan.ScalarCanonicalForGoType(reflectGoTypeName(rt))
		return

	case reflect.Slice:
		elem := rt.Elem()
		// []lw.Ref etc. — a repeated membership marker (one attribute, many
		// memberships). Only membership markers may be sliced this way.
		if elem.PkgPath() == lwPkgPath {
			s, err = classifyLwMarker(elem)
			if err != nil {
				return
			}
			if !s.IsMembership {
				err = eb.Build().Str("type", rt.String()).Errorf("only lw membership markers (lw.Ref / lw.Verbatim / …) may be sliced — []lw.Single / []lw.<lane> is not supported")
				return
			}
			s.Canonical = canonicaltypes.PromoteScalarPrim(s.Canonical, canonicaltypes.ScalarModifierHomogenousArray)
			return
		}
		// []byte: scalar blob lane (necessarily also []uint8 — the
		// identical Go type; the u8 homogenous-array lane is selected
		// explicitly via `,ct=u8h`, matching the go/ast front-end —
		// ADR-0101 OQ2).
		if elem.Kind() == reflect.Uint8 {
			// A named []byte (e.g. json.RawMessage) is not a plain []byte; the
			// AST front-end rejects its source spelling, so the reflect path
			// must too rather than accept-then-panic at marshal (review E-2).
			if rt.PkgPath() != "" {
				err = eb.Build().Str("type", rt.String()).Errorf("named []byte type is not supported; use plain []byte")
				return
			}
			s.Canonical, err = goplan.ScalarCanonicalForGoType("[]byte")
			return
		}
		// []marshalltypes.X — the former element-wise slice carrier, removed
		// with `,explode` (ADR-0113 D1; mirrors the AST classifier). Carriers
		// are scalar-only: one marshalltypes.X per attribute.
		if elem.Kind() == reflect.Struct && elem.PkgPath() == marshalltypesPkgPath {
			err = eb.Build().Str("carrier", elem.Name()).Errorf("slice carriers (`[]marshalltypes.%s`) were removed with `,explode` (ADR-0113 D1) — a carrier is a scalar `marshalltypes.%s`, one per attribute", elem.Name(), elem.Name())
			return
		}
		// []option.Option[T] forbidden.
		if elem.Kind() == reflect.Struct && elem.PkgPath() == optionPkgPath {
			err = eb.Build().Str("type", rt.String()).Errorf("[]option.Option[T] is forbidden — option.Option[T] is only allowed as a scalar field")
			return
		}
		// A homogenous-array membership: classify the element to a scalar
		// canonical, then promote it with the HomogenousArray modifier.
		var scalar canonicaltypes.PrimitiveAstNodeI
		scalar, err = goplan.ScalarCanonicalForGoType(reflectGoTypeName(elem))
		if err != nil {
			return
		}
		s.Canonical = canonicaltypes.PromoteScalarPrim(scalar, canonicaltypes.ScalarModifierHomogenousArray)
		return

	case reflect.Array:
		elem := rt.Elem()
		if elem.Kind() != reflect.Uint8 {
			err = eb.Build().Str("type", rt.String()).Errorf("only fixed-length `[N]byte` arrays supported (e.g. [4]byte, [16]byte)")
			return
		}
		s.Canonical, err = goplan.ScalarCanonicalForGoType(fmt.Sprintf("[%d]byte", rt.Len()))
		return

	default:
		s.Canonical, err = goplan.ScalarCanonicalForGoType(reflectGoTypeName(rt))
	}
	return
}

// reflectGoTypeName produces the source-form Go type name that
// marshallgen's go/ast classifier would have emitted for the same type.
// Used so downstream comparisons (against "uint64", "time.Time",
// "[]byte", …) work without an extra translation table.
func reflectGoTypeName(rt reflect.Type) string {
	if rt.PkgPath() == "time" && rt.Name() == "Time" {
		return "time.Time"
	}
	// A named type from a package (e.g. `type Severity uint8`, time.Duration,
	// json.RawMessage) is not a builtin. The AST front-end rejects its source
	// spelling at plan-build, so the reflect front-end must too: mapping it to
	// the underlying-kind spelling here would accept it and then panic at
	// marshal time (reflect.Set with a non-assignable named type) — violating
	// the "both front-ends accept exactly the same DTOs" contract (review E-2).
	if rt.PkgPath() != "" {
		return rt.String()
	}
	switch rt.Kind() {
	case reflect.Uint8:
		return "uint8"
	case reflect.Uint16:
		return "uint16"
	case reflect.Uint32:
		return "uint32"
	case reflect.Uint64:
		return "uint64"
	case reflect.Int8:
		return "int8"
	case reflect.Int16:
		return "int16"
	case reflect.Int32:
		return "int32"
	case reflect.Int64:
		return "int64"
	case reflect.Float32:
		return "float32"
	case reflect.Float64:
		return "float64"
	case reflect.Bool:
		return "bool"
	case reflect.String:
		return "string"
	case reflect.Array:
		if rt.Elem().Kind() == reflect.Uint8 {
			return fmt.Sprintf("[%d]byte", rt.Len())
		}
	case reflect.Slice:
		if rt.Elem().Kind() == reflect.Uint8 {
			return "[]byte"
		}
	}
	// Fallback: reflect.Type.String() includes package qualifier
	// (e.g. "time.Time"). Same convention as ast renderers.
	return rt.String()
}
