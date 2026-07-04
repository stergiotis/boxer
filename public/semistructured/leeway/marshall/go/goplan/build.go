package goplan

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

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

	// CarrierType is the marshalltypes carrier struct name (e.g.
	// "MixedLowCardRef") when the field's Go type is a Cut-2 carrier, or ""
	// otherwise. Both front-ends set it by recognising the marshalltypes
	// package + struct name; PlanBuilder pairs the carrier with its value
	// sibling. A carrier field's other shape bits are unused.
	CarrierType string

	// CarrierIsSlice is true when the carrier field's Go type is a slice of
	// the carrier struct (`[]marshalltypes.X`) rather than a scalar
	// (`marshalltypes.X`). A slice carrier pairs with an exploded value field
	// — one carrier element per emitted attribute; a scalar carrier pairs with
	// every other value shape (one carrier for the single / container
	// attribute). PlanBuilder.Finish checks this matches the value shape.
	CarrierIsSlice bool
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

// PlanBuilder accumulates validated fields into a Plan. It centralises
// the per-field semantic checks shared between the two front-ends:
// plain-column constraints, the slice-element allowlist, in-DTO
// (membership, sub-column) uniqueness, flag×shape consistency, the
// `_`-field const-declaration grammar, and the whole-DTO completeness +
// per-section channel-uniformity rules. Front-ends differ only in how
// they turn a field's Go type into a FieldShape; everything downstream
// of that lives here so the codegen and reflect paths accept exactly
// the same DTOs.
//
// Typical use:
//
//	b := NewPlanBuilder(inputPath, pkgName, kindType)
//	for each field {
//	    if underscoreField {
//	        err = b.AddUnderscoreField(kindTag, plainTag, lwTag)
//	    } else {
//	        err = b.AddField(goFieldName, lwTag, shape)
//	    }
//	}
//	plan, err := b.Finish()
type PlanBuilder struct {
	plan            *mappingplan.Plan
	usedPlainCols   map[string]string
	usedMemberships map[string]string
	// carriers holds Cut-2 carrier fields awaiting pairing with their value
	// sibling, keyed by membership+"\x00"+section. Resolved in Finish.
	carriers map[string]carrierInfo
}

// carrierInfo records a parsed carrier field pending pairing in Finish.
type carrierInfo struct {
	goField     string
	carrierType string
	channel     mappingplan.MembershipChannel
	isSlice     bool // []marshalltypes.X (pairs with an exploded value)
}

// NewPlanBuilder returns a builder seeded with the plan-level identity.
// inputPath is a source locator used only for error context (a file
// path for the codegen front-end, a type path for the reflect one).
func NewPlanBuilder(inputPath, packageName, kindType string) *PlanBuilder {
	return &PlanBuilder{
		plan: &mappingplan.Plan{
			InputPath:   inputPath,
			PackageName: packageName,
			KindType:    kindType,
		},
		usedPlainCols:   map[string]string{},
		usedMemberships: map[string]string{},
		carriers:        map[string]carrierInfo{},
	}
}

// AddUnderscoreField handles a `_` blank-identifier field. kindTag /
// plainTag / lwTag are the raw struct-tag values (any may be ""). It
// records the entity kind, rejects the retired `plain:` map, and — when
// an lw: tag is present — validates the `,const=<value>` declaration and
// appends a const TaggedField. Multiple `_` fields are allowed; at most
// one may carry `kind:`.
func (b *PlanBuilder) AddUnderscoreField(kindTag, plainTag, lwTag string) (err error) {
	if kindTag != "" {
		if b.plan.KindName != "" {
			err = eb.Build().Str("input", b.plan.InputPath).Errorf("multiple `_` fields carry `kind:` — only one entity-level kind name allowed per DTO")
			return
		}
		b.plan.KindName = kindTag
	}
	if plainTag != "" {
		err = eb.Build().Str("input", b.plan.InputPath).Errorf("`_` field's `plain:` map is retired — declare plain columns per-field via `lw:\",<col>\"` (e.g. `Id uint64` with `lw:\",id\"`)")
		return
	}
	if lwTag == "" {
		return
	}
	// Constant declaration on the `_` field.
	var pt ParsedLWTag
	pt, err = SplitLW(lwTag)
	if err != nil {
		err = eb.Build().Str("tag", lwTag).Errorf("parse `_` lw tag: %w", err)
		return
	}
	if !pt.Flags.HasConst {
		err = eb.Build().Str("tag", lwTag).Errorf("`_` field's lw: tag must declare `,const=<value>` — bare memberships belong on Go fields")
		return
	}
	if pt.Membership == "" {
		err = eb.Build().Str("tag", lwTag).Errorf("const declaration requires non-empty membership name")
		return
	}
	if pt.Section == "" {
		err = eb.Build().Str("tag", lwTag).Errorf("const declaration requires a section name")
		return
	}
	if pt.Column != "" {
		err = eb.Build().Str("tag", lwTag).Errorf("const declaration cannot target a sub-column")
		return
	}
	if pt.Flags.Explode {
		err = eb.Build().Str("tag", lwTag).Errorf("const declaration cannot combine with `explode`")
		return
	}
	b.plan.Fields = append(b.plan.Fields, mappingplan.TaggedField{
		GoFieldName:  "", // synthetic — no Go field
		LWMembership: pt.Membership,
		LWSection:    pt.Section,
		Flags:        pt.Flags,
		IsConst:      true,
		ConstValue:   pt.Flags.ConstValue,
	})
	return
}

// AddField validates one non-`_` field given its Go name, raw lw: tag,
// and classified shape, appending the resulting PlainCol (empty
// membership) or TaggedField (membership present).
func (b *PlanBuilder) AddField(goFieldName, lwTag string, shape FieldShape) (err error) {
	var pt ParsedLWTag
	pt, err = SplitLW(lwTag)
	if err != nil {
		err = eb.Build().Str("tag", lwTag).Errorf("parse lw tag: %w", err)
		return
	}
	membership, section, column, flags := pt.Membership, pt.Section, pt.Column, pt.Flags

	// Cut-2 carrier field — recognised by its Go type (a marshalltypes
	// struct), not by the lw: tag. It rides alongside a value field sharing
	// the same (membership, section); recorded here and paired in Finish.
	// It claims no value sub-column and emits no attribute of its own.
	if shape.CarrierType != "" {
		return b.addCarrierField(goFieldName, membership, section, column, flags, shape)
	}

	// Canonical-native: derive the Go-facing shape (element type +
	// multiplicity) once from the field's canonical value type. Every check
	// and the appended PlainCol / TaggedField below read these derived
	// locals; the canonical itself is carried through verbatim.
	goType, isSlice, isRoaring, err := mappingplan.DeriveGoShape(shape.Canonical)
	if err != nil {
		// shape.Canonical may be nil here (an unclassified field), so the
		// error context must not stringify it; the wrapped error explains it.
		err = eb.Build().Str("field", goFieldName).Errorf("derive Go type from canonical: %w", err)
		return
	}

	// Empty membership ⇒ plain row column. The section slot names the
	// fact-row column (id / ts / naturalKey / expiresAt). Shape is
	// constrained per-column; flags are not allowed (plain columns have
	// no BeginAttribute call to switch).
	if membership == "" {
		if section == "" {
			err = eb.Build().Str("tag", lwTag).Errorf("empty membership AND empty section — plain field needs `lw:\",<col>\"` (id/ts/naturalKey/expiresAt)")
			return
		}
		if column != "" {
			err = eb.Build().Str("tag", lwTag).Errorf("plain field cannot carry sub-column (`:<col>`)")
			return
		}
		if flags.Unit || flags.Explode || flags.HasConst || flags.CanonicalType != "" || flags.Channel != mappingplan.MembershipChannelLowCardRef {
			err = eb.Build().Str("field", goFieldName).Errorf("plain field cannot carry channel / `unit` / `explode` / `const` / `ct=` flags (flags apply to tagged-value attributes only)")
			return
		}
		if shape.IsOption || isRoaring || isSlice {
			// Top-level `[]byte` is recognised by the classifier as a scalar
			// byte-string (isSlice=false, goType="[]byte"), so naturalKey still
			// passes.
			err = eb.Build().Str("field", goFieldName).Errorf("plain field must be a scalar T (no Option / no slice / no roaring; top-level `[]byte` for naturalKey is allowed)")
			return
		}
		if prev, dup := b.usedPlainCols[section]; dup {
			err = eb.Build().Str("column", section).Str("first", prev).Str("second", goFieldName).Errorf("plain column declared on two DTO fields")
			return
		}
		b.usedPlainCols[section] = goFieldName
		err = ValidatePlainColumnShape(section, goType)
		if err != nil {
			err = eb.Build().Str("field", goFieldName).Errorf("%w", err)
			return
		}
		b.plan.PlainCols = append(b.plan.PlainCols, mappingplan.PlainCol{
			Column:    section,
			GoField:   goFieldName,
			Canonical: shape.Canonical,
		})
		return
	}

	// Resolve the field's canonical, applying a `,ct=<canonical>` override if
	// present. The override must reproduce the field's Go/wire shape — it
	// relabels the canonical (e.g. a [N]byte field as IPv4, or a []byte blob
	// as the u8 homogenous array — the same Go type) without changing the
	// bytes, so Plan-consuming tooling sees the richer type and both
	// front-ends stay wire-compatible. Resolved before the shape checks
	// below so the allowlist and the flag × shape rules see the field's
	// effective shape (e.g. `,ct=u8h,explode` composes: the override makes
	// the field multi-element).
	fieldCanonical := shape.Canonical
	if flags.CanonicalType != "" {
		fieldCanonical, err = resolveCanonicalOverride(goFieldName, flags.CanonicalType, goType, isSlice, isRoaring)
		if err != nil {
			return
		}
		goType, isSlice, isRoaring, err = mappingplan.DeriveGoShape(fieldCanonical)
		if err != nil {
			err = eb.Build().Str("field", goFieldName).Errorf("derive Go type from `,ct=` canonical: %w", err)
			return
		}
	}

	// Tagged-value field. Slice element allowlist is shape-level only
	// (per-element identity conversion in the emitted code); schema-
	// specific section compatibility is the Go compiler's job at the
	// BuildEntities call site.
	if isSlice {
		switch goType {
		case "string",
			"uint8", "uint16", "uint32", "uint64",
			"int8", "int16", "int32", "int64",
			"float32", "float64", "bool",
			"[]byte":
			// OK — identity-conversion primitives, plus [][]byte.
		default:
			err = eb.Build().Str("field", goFieldName).Str("elemType", goType).Errorf("slice element type not yet supported")
			return
		}
	}

	// In-DTO uniqueness: (membership, sub-column) is the key. Two fields
	// can share a membership iff they target distinct sub-columns of a
	// multi-column section (u32Range with beginIncl + endExcl). The
	// separator is NUL, not ":", so a colon inside a (verbatim) membership
	// name cannot alias a membership+column pair — e.g. membership "a:b"
	// vs membership "a" column "b" both keyed "a:b" under a ":" separator,
	// which false-rejects the second valid field. Matches the carriers
	// map key below.
	dupKey := membership + "\x00" + column
	if prev, dup := b.usedMemberships[dupKey]; dup {
		err = eb.Build().Str("membership", membership).Str("column", column).Str("first", prev).Str("second", goFieldName).Errorf("membership+column appears on two DTO fields")
		return
	}
	b.usedMemberships[dupKey] = goFieldName

	// Flag × shape consistency.
	isMulti := isSlice || isRoaring
	if flags.Explode && !isMulti {
		err = eb.Build().Str("field", goFieldName).Str("flag", "explode").Errorf("`explode` requires a multi-element shape (`[]T`, `*roaring.Bitmap`, `[][]byte`)")
		return
	}
	if flags.Unit && isMulti && !flags.Explode {
		err = eb.Build().Str("field", goFieldName).Str("flag", "unit").Errorf("`unit` on a multi-element shape requires `explode` (otherwise the default container shape has no per-element call to switch)")
		return
	}
	if flags.HasConst {
		err = eb.Build().Str("field", goFieldName).Errorf("`,const=<value>` only valid on `_` blank-identifier fields (carries no Go-side data)")
		return
	}
	if flags.Channel.UsesCarrier() {
		if column != "" {
			// A carrier channel carries the section's value column; a `:<col>`
			// sub-column (only meaningful for multi-sub-column sections like
			// u32Range) would mis-shape the emit and panic at marshal time.
			err = eb.Build().Str("field", goFieldName).Str("channel", flags.Channel.String()).Errorf("mixed/parametrized value field cannot target a sub-column (`:<col>`)")
			return
		}
		if isRoaring {
			// A roaring set iterates in sorted order with no stable element
			// index, so there is no well-defined element-wise pairing with a
			// carrier slice. Scalar / Option / []T (incl. `,explode`) are
			// supported; roaring is not. (ADR-0008 OQ#4 lift.)
			err = eb.Build().Str("field", goFieldName).Str("channel", flags.Channel.String()).Errorf("mixed/parametrized value field cannot be a roaring bitmap — no stable element index to pair with the carrier; use []T with `,explode`")
			return
		}
	}

	b.plan.Fields = append(b.plan.Fields, mappingplan.TaggedField{
		GoFieldName:  goFieldName,
		IsOption:     shape.IsOption,
		Canonical:    fieldCanonical,
		LWMembership: membership,
		LWSection:    section,
		LWColumn:     column,
		Flags:        flags,
	})
	return
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

// addCarrierField records a Cut-2 carrier field (its Go type is a
// marshalltypes carrier struct) for pairing with its value sibling in
// Finish. The carrier names the channel's membership-side data (id /
// params); it occupies no value sub-column and emits no attribute.
func (b *PlanBuilder) addCarrierField(goFieldName, membership, section, column string, flags mappingplan.FieldFlags, shape FieldShape) (err error) {
	if membership == "" || section == "" {
		err = eb.Build().Str("field", goFieldName).Str("carrier", shape.CarrierType).Errorf("carrier field needs a membership and section in its lw: tag")
		return
	}
	if column != "" {
		err = eb.Build().Str("field", goFieldName).Errorf("carrier field cannot target a sub-column (`:<col>`)")
		return
	}
	if !flags.Channel.UsesCarrier() {
		err = eb.Build().Str("field", goFieldName).Str("carrier", shape.CarrierType).Errorf("a marshalltypes carrier field requires a mixed/parametrized channel flag (e.g. `,mixedLowCardRef`)")
		return
	}
	if want := flags.Channel.CarrierTypeName(); want != shape.CarrierType {
		err = eb.Build().Str("field", goFieldName).Str("carrier", shape.CarrierType).Str("channel", flags.Channel.String()).Str("wantCarrier", want).Errorf("carrier type does not match the channel flag")
		return
	}
	if flags.Unit || flags.Explode || flags.HasConst || flags.CanonicalType != "" {
		err = eb.Build().Str("field", goFieldName).Errorf("carrier field cannot carry `unit` / `explode` / `const` / `ct=` flags")
		return
	}
	key := membership + "\x00" + section
	if prev, dup := b.carriers[key]; dup {
		err = eb.Build().Str("membership", membership).Str("section", section).Str("first", prev.goField).Str("second", goFieldName).Errorf("two carrier fields share one membership+section")
		return
	}
	b.carriers[key] = carrierInfo{goField: goFieldName, carrierType: shape.CarrierType, channel: flags.Channel, isSlice: shape.CarrierIsSlice}
	return
}

// Finish runs the whole-DTO completeness + per-section channel
// uniformity checks and returns the assembled plan.
func (b *PlanBuilder) Finish() (plan *mappingplan.Plan, err error) {
	if b.plan.KindName == "" {
		err = eb.Build().Str("input", b.plan.InputPath).Errorf("DTO struct is missing the `_` entity-level field with `kind:\"…\"`")
		return
	}
	if len(b.plan.PlainCols) == 0 {
		err = eb.Build().Str("input", b.plan.InputPath).Errorf("DTO declares no plain columns; at least an `id` plain column (`Id uint64` with `lw:\",id\"`) is required")
		return
	}
	if _, ok := b.usedPlainCols["id"]; !ok {
		err = eb.Build().Str("input", b.plan.InputPath).Errorf("DTO missing required plain column `id` (`lw:\",id\"`)")
		return
	}

	// A ref-channel field's membership becomes a Go identifier
	// kind<UpperFirst(memb)> in the marshallgen core emit (for every target;
	// membership-keyed so kind vars stay unique when several kinds are
	// generated into one package — see mappingplan.TaggedField.KindVar), so
	// a non-identifier name yields code that does not compile. The reflect
	// front-end would instead accept it (it resolves the membership as a
	// lookup-map key, never an identifier), so rejecting it in this shared
	// builder keeps the two front-ends accepting the same DTOs. The facts
	// target additionally maps every ref membership to vdd.Memb<memb> and
	// re-validates itself (factswrapper). Verbatim / carrier memberships are
	// never identifiers (literal wire label / per-row carrier data), so
	// their names may be arbitrary.
	for _, f := range b.plan.Fields {
		if f.Flags.Channel.NeedsKindVar() && !mappingplan.IsIdentifierLike(f.LWMembership) {
			err = eb.Build().Str("membership", f.LWMembership).Errorf("ref-channel membership must be a Go identifier (ASCII letters, digits, underscores) — it becomes the emitted kindXxx symbol; use a verbatim channel for an arbitrary wire label")
			return
		}
	}

	// Per-section membership-channel uniformity check: all fields
	// targeting the same section must agree on Channel (the read-side
	// dispatch iterates a per-section channel; mixed channels would
	// require two separate decode passes). Generalised by ADR-0008 D3
	// from the original "all Verbatim or all Ref" bool.
	bySection := map[string]mappingplan.MembershipChannel{}
	bySectionFirst := map[string]string{}
	membsBySection := map[string]map[string]bool{}
	for _, f := range b.plan.Fields {
		if membsBySection[f.LWSection] == nil {
			membsBySection[f.LWSection] = map[string]bool{}
		}
		membsBySection[f.LWSection][f.LWMembership] = true

		seen, ok := bySection[f.LWSection]
		if !ok {
			bySection[f.LWSection] = f.Flags.Channel
			bySectionFirst[f.LWSection] = f.GoFieldName
			continue
		}
		if seen != f.Flags.Channel {
			err = eb.Build().Str("section", f.LWSection).Str("field", f.GoFieldName).Str("firstField", bySectionFirst[f.LWSection]).Str("firstChannel", seen.String()).Str("secondChannel", f.Flags.Channel.String()).Errorf("section mixes membership channels — pick one channel per section")
			return
		}
	}

	// KindVar keying guard. A const field keys its kindXxx on the membership
	// name (so several consts on one membership share a symbol); a value field
	// keys it on its Go field name. If a ref-channel membership is claimed by
	// both a const and a value field, the two spellings differ — KindVars /
	// uniqueMemberships declares one and the other reference is undefined, so
	// the generated code would not compile. Reject it here with a clear
	// message instead. Verbatim / parametrized channels declare no kindXxx, so
	// the collision cannot arise there; sharing a *section* (different
	// memberships) is fine — only a shared membership collides.
	constRefMemb := map[string]bool{}
	for _, f := range b.plan.Fields {
		if f.IsConst && f.Flags.Channel.NeedsKindVar() {
			constRefMemb[f.LWMembership] = true
		}
	}
	for _, f := range b.plan.Fields {
		if !f.IsConst && f.Flags.Channel.NeedsKindVar() && constRefMemb[f.LWMembership] {
			err = eb.Build().Str("membership", f.LWMembership).Str("valueField", f.GoFieldName).Errorf("a const and a value field share a ref-channel membership — their kindXxx symbols would collide; give them distinct memberships or use a verbatim channel")
			return
		}
	}

	// Cut-2: resolve each carrier-channel value field with its sibling
	// carrier and enforce one membership per carrier (mixed/parametrized)
	// section. Such a section's attributes carry per-row membership data
	// (id/params), so a second membership could not be disambiguated on read.
	for i := range b.plan.Fields {
		f := &b.plan.Fields[i]
		if !f.Flags.Channel.UsesCarrier() {
			continue
		}
		if len(membsBySection[f.LWSection]) > 1 {
			err = eb.Build().Str("section", f.LWSection).Str("channel", f.Flags.Channel.String()).Errorf("a carrier (mixed/parametrized) section may carry only one membership — its per-row attributes cannot be disambiguated on read")
			return
		}
		key := f.LWMembership + "\x00" + f.LWSection
		c, ok := b.carriers[key]
		if !ok {
			err = eb.Build().Str("field", f.GoFieldName).Str("channel", f.Flags.Channel.String()).Str("wantCarrier", f.Flags.Channel.CarrierTypeName()).Errorf("mixed/parametrized field needs a sibling carrier field with the same lw: membership+section")
			return
		}
		// The value field and its carrier must agree on the channel. They
		// are paired by (membership, section) only — and the carrier is not
		// a plan.Field, so the per-section channel-uniformity check above
		// never sees it. Without this guard a mispaired channel (e.g. a
		// mixedLowCardVerbatim value with a MixedLowCardRef carrier) builds
		// clean and then panics / drops data at marshal time.
		if c.channel != f.Flags.Channel {
			err = eb.Build().Str("field", f.GoFieldName).Str("carrierField", c.goField).Str("valueChannel", f.Flags.Channel.String()).Str("carrierChannel", c.channel.String()).Errorf("value field and its carrier sibling declare different channels")
			return
		}
		// Carrier multiplicity must match the value shape: an exploded value
		// emits N attributes (one carrier each → []marshalltypes.X); every
		// other shape (scalar / Option / container) emits one carrier per
		// attribute (scalar marshalltypes.X). Roaring values were rejected at
		// AddField, so f.IsSlice fully determines the multi case here.
		valueIsExplode := f.IsSlice() && f.Flags.Explode
		if valueIsExplode != c.isSlice {
			want := "a scalar `marshalltypes." + c.carrierType + "`"
			if valueIsExplode {
				want = "a slice `[]marshalltypes." + c.carrierType + "`"
			}
			err = eb.Build().Str("field", f.GoFieldName).Str("carrierField", c.goField).Str("channel", f.Flags.Channel.String()).Errorf("carrier multiplicity must match the value shape: this value needs %s carrier", want)
			return
		}
		f.CarrierField = c.goField
		f.CarrierType = c.carrierType
		f.CarrierIsSlice = c.isSlice
		delete(b.carriers, key)
	}
	for key, c := range b.carriers {
		memb, sect, _ := strings.Cut(key, "\x00")
		err = eb.Build().Str("field", c.goField).Str("membership", memb).Str("section", sect).Errorf("carrier field has no value sibling on the same membership+section")
		return
	}

	// Multi-sub-column structural rules (ADR-0101 D3). A section whose
	// fields target more than one sub-column emits one tuple attribute per
	// row: BeginAttribute(<scalars…>) plus zipped co-containers via
	// AddTo(Co)Container(s)P. Validating here (not at marshallgen emit time)
	// means both front-ends and Validate[T] reject the same DTOs before any
	// DML method is reflected — previously the reflect path panicked
	// mid-marshal. Carrier channels cannot reach a multi-sub-column section
	// (AddField rejects `:<col>` on value and carrier fields of such
	// channels), so only the per-field shape/flag rules are checked.
	colFields := map[string]map[string][]string{} // section → column → field names, declaration order
	colCount := map[string]int{}                  // section → distinct column count
	for _, f := range b.plan.Fields {
		col := f.LWColumn
		if col == "" {
			col = "value"
		}
		if colFields[f.LWSection] == nil {
			colFields[f.LWSection] = map[string][]string{}
		}
		if _, ok := colFields[f.LWSection][col]; !ok {
			colCount[f.LWSection]++
		}
		colFields[f.LWSection][col] = append(colFields[f.LWSection][col], f.GoFieldName)
	}
	for _, f := range b.plan.Fields {
		if colCount[f.LWSection] < 2 {
			continue
		}
		col := f.LWColumn
		if col == "" {
			col = "value"
		}
		if len(colFields[f.LWSection][col]) > 1 {
			err = eb.Build().Str("section", f.LWSection).Str("column", col).Errorf("multi-field sub-column in multi-sub-column section not supported")
			return
		}
		if len(membsBySection[f.LWSection]) > 1 {
			err = eb.Build().Str("section", f.LWSection).Errorf("multi-sub-column section with multiple memberships not supported")
			return
		}
		if f.IsConst {
			err = eb.Build().Str("section", f.LWSection).Str("membership", f.LWMembership).Errorf("const field cannot share a multi-sub-column section — the tuple attribute has no slot for it")
			return
		}
		if f.IsOption {
			err = eb.Build().Str("section", f.LWSection).Str("field", f.GoFieldName).Errorf("Option[T] not supported in a multi-sub-column section — the tuple attribute has no per-sub-column presence")
			return
		}
		if f.IsRoaring() {
			err = eb.Build().Str("section", f.LWSection).Str("field", f.GoFieldName).Errorf("*roaring.Bitmap not supported in a multi-sub-column section — no stable element index to zip with the co-containers; use []T")
			return
		}
		if f.Flags.Unit {
			err = eb.Build().Str("section", f.LWSection).Str("field", f.GoFieldName).Errorf("`unit` not supported in a multi-sub-column section")
			return
		}
		if f.Flags.Explode {
			err = eb.Build().Str("section", f.LWSection).Str("field", f.GoFieldName).Errorf("`explode` not supported in a multi-sub-column section — the attribute is one tuple plus zipped co-containers per row")
			return
		}
	}

	plan = b.plan
	return
}

// ValidatePlainColumnShape checks that a plain column names a recognized
// entity-header role (id / naturalKey / ts / expiresAt) and carries a Go
// type the codec supports. Under the strict 1:1 mapping the Go type is
// the entity setter's argument type verbatim — the codec inserts no
// conversions — so the only constraint here is that the type round-trips
// through Arrow (see PlainArrowArrayType). Exported for the sibling
// marshallreflect package.
func ValidatePlainColumnShape(column, goType string) (err error) {
	switch column {
	case "id", "naturalKey", "ts", "expiresAt":
		// Recognized role; the Go type is the setter's arg type verbatim.
		if !IsSupportedPlainType(goType) {
			err = eb.Build().Str("column", column).Str("goType", goType).Errorf("unsupported plain column Go type (see goplan.PlainArrowArrayType for the supported set)")
		}
	default:
		err = eb.Build().Str("column", column).Errorf("unknown plain column (allowed: id, naturalKey, ts, expiresAt)")
	}
	return
}
