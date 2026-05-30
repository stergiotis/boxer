package marshallgen

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// FieldShape is the front-end-agnostic classification of a DTO field's
// Go type. The codegen front-end (ParsePlan, walking go/ast) and the
// reflect front-end (marshallreflect.buildPlan, walking reflect.Type)
// each classify a field into this shape; every validation rule applied
// afterwards is shared via PlanBuilder, so the two front-ends cannot
// drift on what they accept.
type FieldShape struct {
	GoType    string // inner element type, source-form ("uint64", "time.Time", "[4]byte", "[]byte", "*roaring.Bitmap")
	IsOption  bool   // option.Option[T] wrapper
	IsSlice   bool   // []T element-slice (top-level, non-byte)
	IsRoaring bool   // *roaring.Bitmap
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
	plan            *Plan
	usedPlainCols   map[string]string
	usedMemberships map[string]string
}

// NewPlanBuilder returns a builder seeded with the plan-level identity.
// inputPath is a source locator used only for error context (a file
// path for the codegen front-end, a type path for the reflect one).
func NewPlanBuilder(inputPath, packageName, kindType string) *PlanBuilder {
	return &PlanBuilder{
		plan: &Plan{
			InputPath:   inputPath,
			PackageName: packageName,
			KindType:    kindType,
		},
		usedPlainCols:   map[string]string{},
		usedMemberships: map[string]string{},
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
	b.plan.Fields = append(b.plan.Fields, TaggedField{
		GoFieldName:  "", // synthetic — no Go field
		GoType:       "string",
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
		if flags.Unit || flags.Explode || flags.HasConst || flags.Channel != MembershipChannelLowCardRef {
			err = eb.Build().Str("field", goFieldName).Errorf("plain field cannot carry channel / `unit` / `explode` / `const` flags (flags apply to tagged-value attributes only)")
			return
		}
		if shape.IsOption || shape.IsRoaring || shape.IsSlice {
			// Top-level `[]byte` is recognised by the classifier as
			// IsSlice=false GoType="[]byte", so naturalKey still passes.
			err = eb.Build().Str("field", goFieldName).Errorf("plain field must be a scalar T (no Option / no slice / no roaring; top-level `[]byte` for naturalKey is allowed)")
			return
		}
		if prev, dup := b.usedPlainCols[section]; dup {
			err = eb.Build().Str("column", section).Str("first", prev).Str("second", goFieldName).Errorf("plain column declared on two DTO fields")
			return
		}
		b.usedPlainCols[section] = goFieldName
		err = ValidatePlainColumnShape(section, shape.GoType)
		if err != nil {
			err = eb.Build().Str("field", goFieldName).Errorf("%w", err)
			return
		}
		b.plan.PlainCols = append(b.plan.PlainCols, PlainCol{
			Column:  section,
			GoField: goFieldName,
			GoType:  shape.GoType,
		})
		return
	}

	// Tagged-value field. Slice element allowlist is shape-level only
	// (per-element identity conversion in the emitted code); schema-
	// specific section compatibility is the Go compiler's job at the
	// BuildEntities call site.
	if shape.IsSlice {
		switch shape.GoType {
		case "string",
			"uint8", "uint16", "uint32", "uint64",
			"int8", "int16", "int32", "int64",
			"float32", "float64", "bool",
			"[]byte":
			// OK — identity-conversion primitives, plus [][]byte.
		default:
			err = eb.Build().Str("field", goFieldName).Str("elemType", shape.GoType).Errorf("slice element type not yet supported")
			return
		}
	}

	// In-DTO uniqueness: (membership, sub-column) is the key. Two fields
	// can share a membership iff they target distinct sub-columns of a
	// multi-column section (u32Range with beginIncl + endExcl).
	dupKey := membership
	if column != "" {
		dupKey = membership + ":" + column
	}
	if prev, dup := b.usedMemberships[dupKey]; dup {
		err = eb.Build().Str("membership", membership).Str("column", column).Str("first", prev).Str("second", goFieldName).Errorf("membership+column appears on two DTO fields")
		return
	}
	b.usedMemberships[dupKey] = goFieldName

	// Flag × shape consistency.
	isMulti := shape.IsSlice || shape.IsRoaring
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

	b.plan.Fields = append(b.plan.Fields, TaggedField{
		GoFieldName:  goFieldName,
		GoType:       shape.GoType,
		IsOption:     shape.IsOption,
		IsSlice:      shape.IsSlice,
		IsRoaring:    shape.IsRoaring,
		LWMembership: membership,
		LWSection:    section,
		LWColumn:     column,
		Flags:        flags,
	})
	return
}

// Finish runs the whole-DTO completeness + per-section channel
// uniformity checks and returns the assembled plan.
func (b *PlanBuilder) Finish() (plan *Plan, err error) {
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

	// Per-section membership-channel uniformity check: all fields
	// targeting the same section must agree on Channel (the read-side
	// dispatch iterates a per-section channel; mixed channels would
	// require two separate decode passes). Generalised by ADR-0008 D3
	// from the original "all Verbatim or all Ref" bool.
	bySection := map[string]MembershipChannel{}
	bySectionFirst := map[string]string{}
	for _, f := range b.plan.Fields {
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
	plan = b.plan
	return
}
