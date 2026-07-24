package goplan

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// PlanBuilder: the per-field validation and assembly both front-ends share,
// so the go/ast and reflect paths accept exactly the same DTOs. This file
// carries the flat field grammar — plain columns, tagged values, `_` consts
// and Cut-2 carriers. The struct-shaped section entry points live in
// buildstruct.go, and the whole-DTO checks in finish.go.

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
// Carriers are scalar-only — one `marshalltypes.X` per attribute (the
// `[]marshalltypes.X` slice form went with `,explode`, ADR-0113 D1).
type carrierInfo struct {
	goField     string
	carrierType string
	channel     mappingplan.MembershipChannel
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
	if err = rejectReservedMembership(pt.Membership); err != nil {
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
	if err = rejectRemovedValueChannel(pt.Flags.Channel, "", lwTag); err != nil {
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
	// An lw.Single[T] field carries the unit shape by TYPE, not a tag flag; fold
	// it in so the field behaves exactly like a flat `,unit` field.
	if shape.Unit {
		flags.Unit = true
	}
	if err = rejectReservedMembership(membership); err != nil {
		err = eb.Build().Str("field", goFieldName).Errorf("%w", err)
		return
	}

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
		if flags.Unit || flags.HasConst || flags.CanonicalType != "" || flags.Channel != mappingplan.MembershipChannelLowCardRef {
			err = eb.Build().Str("field", goFieldName).Errorf("plain field cannot carry channel / `unit` / `const` / `ct=` flags (flags apply to tagged-value attributes only)")
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
	// effective shape (e.g. `,ct=u8h` makes a `[]byte` field multi-element,
	// which the `,unit` × shape rule below must see).
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
		if err = checkSliceElemType(goFieldName, goType); err != nil {
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
	if flags.Unit && isMulti {
		err = eb.Build().Str("field", goFieldName).Str("flag", "unit").Errorf("`unit` requires a scalar shape — the container shape has no per-element call to switch")
		return
	}
	if err = rejectRemovedValueChannel(flags.Channel, goFieldName, lwTag); err != nil {
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
			// index, so there is no well-defined pairing with the carrier.
			// Scalar / Option / []T are supported; roaring is not.
			// (ADR-0008 OQ#4 lift.)
			err = eb.Build().Str("field", goFieldName).Str("channel", flags.Channel.String()).Errorf("mixed/parametrized value field cannot be a roaring bitmap — no stable element index to pair with the carrier; use []T")
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
	if flags.Unit || flags.HasConst || flags.CanonicalType != "" {
		err = eb.Build().Str("field", goFieldName).Errorf("carrier field cannot carry `unit` / `const` / `ct=` flags")
		return
	}
	key := membership + "\x00" + section
	if prev, dup := b.carriers[key]; dup {
		err = eb.Build().Str("membership", membership).Str("section", section).Str("first", prev.goField).Str("second", goFieldName).Errorf("two carrier fields share one membership+section")
		return
	}
	b.carriers[key] = carrierInfo{goField: goFieldName, carrierType: shape.CarrierType, channel: flags.Channel}
	return
}

// rejectRemovedValueChannel enforces ADR-0113 D1's grammar cull on value
// fields (and `_` consts): the `,highCardVerbatim` spelling is no longer
// authorable there. The channel itself survives — on tuple `@membership`
// fields and nested static memberships, and at the DML level — so the token
// stays in the shared flag vocabulary for those grammars.
func rejectRemovedValueChannel(ch mappingplan.MembershipChannel, goFieldName, lwTag string) (err error) {
	if ch == mappingplan.MembershipChannelHighCardVerbatim {
		err = eb.Build().Str("field", goFieldName).Str("tag", lwTag).Errorf("the `,highCardVerbatim` value-field spelling was removed (ADR-0113 D1) — the channel remains available on tuple `@membership` fields, nested memberships, and via hand-written DML")
	}
	return
}

// rejectReservedMembership errors on `@`-prefixed membership names in
// top-level lw: tags. The prefix is reserved for the tuple element
// grammar (`@membership`, SplitTupleElemLW) so a marker pasted onto a
// top-level field fails loudly instead of silently becoming a literal
// verbatim label (ADR-0103).
func rejectReservedMembership(membership string) (err error) {
	if strings.HasPrefix(membership, "@") {
		err = eb.Build().Str("membership", membership).Errorf("membership names starting with `@` are reserved for the tuple element grammar (`%s` inside a slice-of-struct element)", TupleMembershipMarker)
	}
	return
}

// checkSliceElemType enforces the slice-element allowlist shared by
// top-level `[]T` fields (AddField) and tuple element container fields
// (AddTupleSliceField): identity-conversion primitives plus [][]byte.
func checkSliceElemType(goFieldName, goType string) (err error) {
	switch goType {
	case "string",
		"uint8", "uint16", "uint32", "uint64",
		"int8", "int16", "int32", "int64",
		"float32", "float64", "bool",
		"[]byte":
		// OK — identity-conversion primitives, plus [][]byte.
	default:
		err = eb.Build().Str("field", goFieldName).Str("elemType", goType).Errorf("slice element type not yet supported")
	}
	return
}
