package goplan

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// The front-end-agnostic value-type classification (FieldShape) and the
// Go ↔ canonical helpers around it. Both front-ends fill a FieldShape from
// their own view of a field's type — go/ast or reflect — and everything
// downstream reads the derived Go shape (mappingplan.DeriveGoShape).

// FieldShape is the front-end-agnostic classification of a DTO field's
// value type. The codegen front-end (ParsePlan, walking go/ast) and the
// reflect front-end (marshallreflect.buildPlan, walking reflect.Type)
// each classify a field into this shape; every validation rule applied
// afterwards is shared via PlanBuilder, so the two front-ends cannot
// drift on what they accept.
//
// The shape is canonical-native: a field's value type is authored as a
// leeway Canonical (canonicaltypes.PrimitiveAstNodeI). PlanBuilder derives
// the Go-facing fields (GoType / IsSlice / IsRoaring on PlainCol /
// TaggedField) from Canonical once via the canonical→Go rule (see
// DeriveGoShape); the rest of the pipeline keeps reading those derived
// fields.
type FieldShape struct {
	// Canonical is the leeway canonical type the field's value type maps to.
	// For a multi-element membership it carries the HomogenousArray scalar
	// modifier ([]T) or Set modifier (*roaring.Bitmap); for a ZeroToOne
	// field IsOption is set alongside a scalar Canonical. "" for a carrier
	// field (its CarrierType drives the carrier path instead).
	Canonical canonicaltypes.PrimitiveAstNodeI
	IsOption  bool // option.Option[T] wrapper

	// Unit marks the ,unit / BeginAttributeSingle shape (an lw.Single[T] field):
	// a container sub-column carrying exactly one element, supplied as the scalar
	// Canonical. AddField folds it into FieldFlags.Unit so the field behaves like
	// a flat `,unit` field.
	Unit bool

	// IsMembership marks a nested-model membership marker field (lw.Ref /
	// lw.Verbatim / …) — a per-attribute membership rather than a value
	// sub-column. MembershipChannel is its channel; the membership value's Go
	// type (uint64 / string) is Canonical's, and a HomogenousArray Canonical
	// marks a repeated ([]lw.Ref) membership. Consumed by AddTupleSliceField in
	// place of the `@membership` tag.
	IsMembership      bool
	MembershipChannel mappingplan.MembershipChannel

	// MarkerGoType is the as-written lw.* marker Go type (e.g. "lw.Ref",
	// "lw.Verbatim", "lw.Single[uint64]", "lw.IPv4"), or "" for a plain field.
	// The reflect codec bridges markers off the live reflect.Type and IGNORES
	// this; the codegen codec has no live type and needs it to emit the newtype
	// conversions (uint64(x) / lw.Ref(v) / .Val / …). Additive — the marker's
	// wire representation is its Canonical, unchanged.
	MarkerGoType string

	// CarrierType is the marshalltypes carrier struct name (e.g.
	// "MixedLowCardRef") when the field's Go type is a Cut-2 carrier, or ""
	// otherwise. Both front-ends set it by recognising the marshalltypes
	// package + struct name; PlanBuilder pairs the carrier with its value
	// sibling. A carrier field's other shape bits are unused.
	CarrierType string
}

// ScalarCanonicalForGoType maps a scalar Go-type spelling to its leeway
// canonical node — the Go→canonical half of the front-end classifiers,
// shared so the go/ast and reflect paths cannot drift. It is the exact
// inverse of DeriveGoShape's scalar (None-modifier) case: for every goType
// it returns, GenerateGoCode(canonical, EmptyAspectSet) reproduces goType,
// which keeps emitted codecs byte-identical.
//
// The front-ends handle multiplicity themselves: a `[]T` element-slice
// promotes the returned scalar with ScalarModifierHomogenousArray, and a
// roaring bitmap promotes a u32 scalar with ScalarModifierSet. The byte
// shapes `[]byte` (variable blob) and `[N]byte` (fixed array) are scalar
// byte-strings handled here directly, not slice promotions.
func ScalarCanonicalForGoType(goType string) (c canonicaltypes.PrimitiveAstNodeI, err error) {
	switch goType {
	case "uint8":
		c = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 8}
	case "uint16":
		c = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 16}
	case "uint32":
		c = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 32}
	case "uint64":
		c = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 64}
	case "int8":
		c = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericSigned, Width: 8}
	case "int16":
		c = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericSigned, Width: 16}
	case "int32":
		c = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericSigned, Width: 32}
	case "int64":
		c = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericSigned, Width: 64}
	case "float32":
		c = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericFloat, Width: 32}
	case "float64":
		c = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericFloat, Width: 64}
	case "bool":
		c = canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBool}
	case "string":
		c = canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8}
	case "[]byte":
		// Scalar variable-length byte-string, NOT a HomogenousArray of u8.
		c = canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBytes}
	case "time.Time":
		c = canonicaltypes.TemporalTypeAstNode{BaseType: canonicaltypes.BaseTypeTemporalUtcDatetime, Width: 64}
	default:
		if n, ok := FixedByteArrayLen(goType); ok {
			// `[N]byte` — a fixed-width byte-string.
			c = canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBytes, WidthModifier: canonicaltypes.WidthModifierFixed, Width: canonicaltypes.Width(n)}
			return
		}
		err = eb.Build().Str("goType", goType).Errorf("no leeway canonical type for Go type")
	}
	return
}

// RoaringElemCanonical is the scalar element a `*roaring.Bitmap` field's
// canonical promotes from: an unsigned 32-bit machine number (a roaring
// bitmap is a set of uint32). PromoteScalarPrim(…, ScalarModifierSet) over
// this is the canonical the front-ends record for roaring fields.
func RoaringElemCanonical() canonicaltypes.PrimitiveAstNodeI {
	return canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 32}
}

// FixedByteArrayLen reports the N in a fixed-length byte-array source-form
// type name `[N]byte`, or (0, false) for anything else (including the
// variable-length blob `[]byte`). It is the single point of truth for
// recognising fixed-byte fields, which the codec carries on the wire as a
// `[]byte` blob — resliced on write, copied back into the array on read.
// Any decimal length N is supported; the read/write paths generalise over
// N, so callers must not special-case particular sizes.
func FixedByteArrayLen(goType string) (n int, ok bool) {
	const suffix = "]byte"
	if !strings.HasPrefix(goType, "[") || !strings.HasSuffix(goType, suffix) {
		return 0, false
	}
	digits := goType[1 : len(goType)-len(suffix)]
	if digits == "" {
		return 0, false // "[]byte" is the variable-length blob, not a fixed array
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// IsFixedByteArray reports whether goType is a fixed-length byte array
// (`[N]byte`). See FixedByteArrayLen for the supported forms.
func IsFixedByteArray(goType string) bool {
	_, ok := FixedByteArrayLen(goType)
	return ok
}

// CopyStratE names how a value of a given Go type is lifted out of an Arrow
// buffer on the read side. The type→strategy decision is shared by both
// back-ends so it lives in one place: the codegen emitter switches on it to
// emit the right text; the reflect codec switches on it to perform the copy.
// (The reflect plain-column reader keeps its own type→Arrow-accessor switch
// in readPlainArrow — that is per-type value dispatch, a different concern.)
type CopyStratE uint8

const (
	// CopyNone assigns the Arrow value straight through (scalars).
	CopyNone CopyStratE = iota
	// CopyBytes defensively copies a []byte out of the Arrow buffer (the
	// buffer is reused across rows, so the value must be copied to survive).
	CopyBytes
	// CopyFixedByte copies the wire blob into a fresh [N]byte array.
	CopyFixedByte
	// CopyTime reconstructs a time.Time from Arrow's physical int64-nanos
	// timestamp (Arrow has no native time.Time).
	CopyTime
)

// CopyStrategy reports how a value of source-form Go type goType is lifted
// out of its Arrow buffer on read. See CopyStratE.
func CopyStrategy(goType string) CopyStratE {
	switch {
	case goType == "time.Time":
		return CopyTime
	case goType == "[]byte":
		return CopyBytes
	case IsFixedByteArray(goType):
		return CopyFixedByte
	default:
		return CopyNone
	}
}

// resolveCanonicalOverride parses a `,ct=<canonical>` string and checks it
// reproduces the field's Go type. It may only relabel the canonical — e.g. a
// [4]byte field as IPv4, or a []byte field as the u8 homogenous array
// (`,ct=u8h`) — never reshape it, so the codegen / reflect front-ends stay
// wire-compatible. The check compares the effective *rendered* Go types, not
// the (element, multiplicity) components: a scalar blob and a u8 array are
// distinct components but the identical Go type `[]byte` ≡ `[]uint8`, and the
// override exists precisely to pick the wire lane for such ambiguous types
// (ADR-0101 OQ2 resolution). The roaring axis stays strict — a bitmap is a
// different Go type from any slice.
func resolveCanonicalOverride(goFieldName, ctStr, goType string, isSlice, isRoaring bool) (out canonicaltypes.PrimitiveAstNodeI, err error) {
	out, err = canonicaltypes.NewParser().ParsePrimitiveTypeAst(ctStr)
	if err != nil {
		err = eb.Build().Str("field", goFieldName).Str("ct", ctStr).Errorf("parse `,ct=` canonical: %w", err)
		return
	}
	ovGoType, ovIsSlice, ovIsRoaring, derr := mappingplan.DeriveGoShape(out)
	if derr != nil {
		err = eb.Build().Str("field", goFieldName).Str("ct", ctStr).Errorf("`,ct=` canonical has no Go representation: %w", derr)
		return
	}
	if effectiveGoType(ovGoType, ovIsSlice) != effectiveGoType(goType, isSlice) || ovIsRoaring != isRoaring {
		out = nil
		err = eb.Build().Str("field", goFieldName).Str("ct", ctStr).Str("ctGoType", ovGoType).Str("fieldGoType", goType).Errorf("`,ct=` canonical's Go shape does not match the field's — the override may only relabel, not reshape")
		return
	}
	return
}

// effectiveGoType renders an (element type, multiplicity) pair to the field's
// full Go type, folding the `[]uint8` alias: a scalar blob ("[]byte") and a
// u8 homogenous array (element "uint8", slice) are the same Go type.
func effectiveGoType(goType string, isSlice bool) string {
	t := goType
	if isSlice {
		t = "[]" + t
	}
	if t == "[]uint8" {
		return "[]byte"
	}
	return t
}
