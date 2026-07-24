package goplan

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// The struct-shaped section entry points: a DTO field whose Go type is a
// struct (or a slice of one) maps a whole section, its sub-columns being the
// struct's fields. Two spellings reach the same Plan shape — the flat
// `@membership` tuple (ADR-0103/0109) and the nested attribute model
// (ADR-0113) — and the rules they share live here.

// TupleElem is one field of a tuple element struct as seen by a
// front-end: the Go field name, its raw lw: tag, and the classified
// FieldShape. The front-ends walk the element struct (go/ast or
// reflect) and hand the fields here in declaration order; every
// validation rule lives in AddTupleSliceField so the two front-ends
// accept exactly the same tuple shapes.
type TupleElem struct {
	GoFieldName string
	LWTag       string
	Shape       FieldShape
}

// AddTupleSliceField validates a dynamic-membership tuple field
// (ADR-0103): a slice-of-struct DTO field — `Texts []LabeledText` tagged
// `lw:"<section>"` — whose elements each emit ONE attribute of that
// section, carrying its own membership. structTypeName is the element
// struct's type name (rendered by the codegen front-end); elems are the
// element struct's fields in declaration order.
//
// Element grammar (SplitTupleElemLW): exactly one `@membership` field — a
// string or []byte scalar with an explicit verbatim channel flag — plus
// one value field per sub-column (`<section>:<column>`, scalars as T,
// containers as []T, `,ct=` composes). Ref and carrier channels are
// rejected: a dynamic membership embeds its value on the wire, while ref
// ids resolve through compile-time kindXxx symbols the generated
// BuildEntities cannot parameterise per element. Option / roaring / unit
// / const are rejected like in any multi-sub-column section (ADR-0101 D2).
func (b *PlanBuilder) AddTupleSliceField(goFieldName, lwTag, structTypeName string, elems []TupleElem) (err error) {
	section, err := SplitTupleOuterLW(lwTag)
	if err != nil {
		err = eb.Build().Str("field", goFieldName).Errorf("parse tuple field tag: %w", err)
		return
	}
	if structTypeName == "" {
		err = eb.Build().Str("field", goFieldName).Errorf("tuple element type must be a named struct type")
		return
	}
	if len(elems) == 0 {
		err = eb.Build().Str("field", goFieldName).Errorf("tuple element struct has no fields")
		return
	}

	type valueField struct {
		goField   string
		column    string
		canonical canonicaltypes.PrimitiveAstNodeI
		flags     mappingplan.FieldFlags
	}
	memberships := make([]mappingplan.TupleMembership, 0, len(elems))
	values := make([]valueField, 0, len(elems))
	usedCols := map[string]string{}

	for _, e := range elems {
		ctx := eb.Build().Str("field", goFieldName).Str("elemField", e.GoFieldName)
		var pt ParsedTupleElemTag
		pt, err = SplitTupleElemLW(e.LWTag)
		if err != nil {
			err = ctx.Errorf("parse tuple element tag: %w", err)
			return
		}
		if e.Shape.CarrierType != "" {
			err = ctx.Errorf("marshalltypes carrier not supported inside a tuple element — carrier channels cannot reach a tuple section")
			return
		}
		if e.Shape.IsOption {
			err = ctx.Errorf("Option[T] not supported inside a tuple element — the tuple attribute has no per-sub-column presence (ADR-0101 D2)")
			return
		}
		var goType string
		var isSlice, isRoaring bool
		goType, isSlice, isRoaring, err = mappingplan.DeriveGoShape(e.Shape.Canonical)
		if err != nil {
			err = ctx.Errorf("derive Go type from canonical: %w", err)
			return
		}

		if pt.IsMembership {
			// A tuple element may declare MORE THAN ONE `@membership` field
			// (repeated fixed fields and/or one repeated slice field per
			// channel) so an attribute carries several memberships
			// (`membership-card > 1`), possibly on heterogeneous channels
			// (ADR-0109 (a)). The channel is per-field.
			if pt.Flags.Unit || pt.Flags.HasConst || pt.Flags.CanonicalType != "" {
				err = ctx.Errorf("`%s` field takes only a channel flag (no unit / const / ct=)", TupleMembershipMarker)
				return
			}
			if isRoaring {
				err = ctx.Errorf("`%s` field cannot be a *roaring.Bitmap — use a scalar or a `[]T` for a repeated membership", TupleMembershipMarker)
				return
			}
			ch := pt.Flags.Channel
			if ch.UsesCarrier() {
				err = ctx.Str("channel", ch.String()).Errorf("`%s` cannot use a carrier / parametrized channel — its identity is per-row carrier data, not an element field; use a verbatim or ref channel", TupleMembershipMarker)
				return
			}
			// Type ↔ channel: a verbatim channel embeds the literal name
			// (string / []byte); a ref channel carries the id directly as a
			// uint64 — no lookup, no compile-time kindXxx symbol (ADR-0109 (b)).
			// A repeated field ([]T) sets IsSlice; goType is then the element type.
			switch goType {
			case "string", "[]byte":
				if !ch.EmbedsLiteralName() {
					err = ctx.Str("channel", ch.String()).Errorf("`%s` on a string / []byte field requires an explicit verbatim channel flag (`,verbatim` / `,lowCardVerbatim` / `,highCardVerbatim`) — the literal name embeds on the wire; a ref channel takes a uint64 id", TupleMembershipMarker)
					return
				}
			case "uint64":
				if !ch.NeedsKindVar() {
					err = ctx.Str("channel", ch.String()).Errorf("`%s` on a uint64 field requires a ref channel flag (`,lowCardRef` / `,highCardRef`) — the id is carried directly; a verbatim channel takes a string / []byte name", TupleMembershipMarker)
					return
				}
			default:
				err = ctx.Str("goType", goType).Errorf("`%s` field must be a string / []byte (verbatim) or uint64 (ref) value, or a `[]T` of them", TupleMembershipMarker)
				return
			}
			memberships = append(memberships, mappingplan.TupleMembership{
				GoField: e.GoFieldName,
				GoType:  goType,
				Channel: ch,
				IsSlice: isSlice,
			})
			continue
		}

		// Value field — one sub-column of the tuple's section.
		if pt.Section != section {
			err = ctx.Str("tupleSection", section).Str("elemSection", pt.Section).Errorf("tuple element targets a different section than its tuple field")
			return
		}
		col := pt.Column
		if col == "" {
			col = "value"
		}
		if prev, dup := usedCols[col]; dup {
			err = ctx.Str("column", col).Str("first", prev).Errorf("sub-column appears on two tuple element fields")
			return
		}
		usedCols[col] = e.GoFieldName
		if pt.Flags.Unit || pt.Flags.HasConst {
			err = ctx.Errorf("`unit` / `const` not supported inside a tuple element — each element is one tuple attribute plus zipped co-containers")
			return
		}
		if pt.Flags.Channel != mappingplan.MembershipChannelLowCardRef {
			err = ctx.Str("flag", pt.Flags.Channel.String()).Errorf("channel flag belongs on the `%s` field, not on a tuple value field", TupleMembershipMarker)
			return
		}
		fieldCanonical := e.Shape.Canonical
		if pt.Flags.CanonicalType != "" {
			fieldCanonical, err = resolveCanonicalOverride(e.GoFieldName, pt.Flags.CanonicalType, goType, isSlice, isRoaring)
			if err != nil {
				return
			}
			goType, isSlice, isRoaring, err = mappingplan.DeriveGoShape(fieldCanonical)
			if err != nil {
				err = ctx.Errorf("derive Go type from `,ct=` canonical: %w", err)
				return
			}
		}
		if isRoaring {
			err = ctx.Errorf("*roaring.Bitmap not supported inside a tuple element — no stable element index to zip with the co-containers; use []T")
			return
		}
		if isSlice {
			if err = checkSliceElemType(e.GoFieldName, goType); err != nil {
				return
			}
		}
		values = append(values, valueField{goField: e.GoFieldName, column: col, canonical: fieldCanonical, flags: pt.Flags})
	}

	if len(memberships) == 0 {
		err = eb.Build().Str("field", goFieldName).Errorf("tuple element struct needs at least one `%s` field carrying a per-attribute membership", TupleMembershipMarker)
		return
	}
	if len(values) == 0 {
		err = eb.Build().Str("field", goFieldName).Errorf("tuple element struct needs at least one value field (`<section>:<column>`)")
		return
	}

	if err = checkTupleMembershipArity(goFieldName, memberships); err != nil {
		return
	}

	for _, v := range values {
		b.plan.Fields = append(b.plan.Fields, mappingplan.TaggedField{
			GoFieldName:  v.goField,
			Canonical:    v.canonical,
			LWMembership: "", // dynamic — per-element data, not a static tag
			LWSection:    section,
			LWColumn:     v.column,
			// The value fields carry the first membership's channel only so
			// g.Channel() and the per-section channel-uniformity check stay
			// well-defined; every tuple channel site dispatches on
			// TupleMemberships instead (the memberships may be heterogeneous).
			Flags:            mappingplan.FieldFlags{Channel: memberships[0].Channel, CanonicalType: v.flags.CanonicalType},
			TupleField:       goFieldName,
			TupleStructType:  structTypeName,
			TupleMemberships: memberships,
		})
	}
	return
}

// splitNestedElemLW parses the optional lw: tag on a field INSIDE a nested
// attribute struct: `lw:"<column>[,ct=<canonical>]"`. Unlike a tuple element
// (SplitTupleElemLW) it carries neither an `@membership` marker nor a
// `<section>:` prefix — the section comes from the outer section-field tag and
// the column is the bare head token (empty ⇒ the caller defaults it to "value",
// the flat single-sub-column default). Only `,ct=` is meaningful here; the
// channel lives on the section field and unit / const have no per-sub-column
// meaning (AddNestedSliceField rejects them).
func splitNestedElemLW(tag string) (column string, flags mappingplan.FieldFlags, err error) {
	if strings.TrimSpace(tag) == "" {
		return
	}
	parts := strings.Split(tag, ",")
	column = strings.TrimSpace(parts[0])
	if strings.IndexByte(column, ':') >= 0 {
		err = eb.Build().Str("tag", tag).Errorf("nested sub-column tag names a bare column, not `section:column` (the section is on the struct field)")
		return
	}
	err = parseFlagTokens(parts[1:], &flags)
	return
}

// AddNestedSliceField validates a NESTED, static-membership section field
// (Slice A): an attribute-struct-typed DTO field — e.g. `Window rangeWindow`
// tagged `lw:"<membership>,<section>"` — whose struct fields are the section's
// sub-columns, emitting `card` attributes per row (One / Optional / Many). It is
// the static-membership sibling of AddTupleSliceField: the membership is a
// compile-time tag (not per-element `@membership` fields), so the emitted
// TaggedFields carry a normal LWMembership / Flags.Channel and an EMPTY
// TupleMemberships — the codec's static-vs-dynamic discriminator. structTypeName
// is the element struct type; elems are its fields in declaration order.
//
// Slice-A scope: value sub-columns only (scalars / `[]T` containers) — no
// `@membership` fields, no lw.Single, no bundled co-container element list, no
// carriers; later steps widen it.
func (b *PlanBuilder) AddNestedSliceField(goFieldName, outerTag, structTypeName string, elems []TupleElem, card mappingplan.AttrCardinalityE) (err error) {
	if structTypeName == "" {
		err = eb.Build().Str("field", goFieldName).Errorf("nested section element type must be a named struct type")
		return
	}
	if len(elems) == 0 {
		err = eb.Build().Str("field", goFieldName).Errorf("nested section struct has no fields")
		return
	}

	// Partition the element fields: marker-typed memberships (lw.Ref / lw.Verbatim
	// — the DYNAMIC case, per-attribute memberships) versus value sub-columns. A
	// struct with ≥1 membership marker is dynamic; without one it is STATIC (its
	// single membership is on the outer tag).
	type valueField struct {
		goField   string
		column    string
		canonical canonicaltypes.PrimitiveAstNodeI
		ct        string
	}
	memberships := make([]mappingplan.TupleMembership, 0, len(elems))
	values := make([]valueField, 0, len(elems))
	usedCols := map[string]string{}

	for _, e := range elems {
		ctx := eb.Build().Str("field", goFieldName).Str("elemField", e.GoFieldName)

		if e.Shape.IsMembership {
			// A per-attribute membership marker: the channel is the type, the
			// value's Go type (uint64 / string) is the canonical, and a
			// HomogenousArray canonical marks a repeated ([]lw.Ref) membership.
			if e.LWTag != "" {
				err = ctx.Errorf("lw membership marker field takes no lw: tag — the channel is the type")
				return
			}
			var mGoType string
			var mIsSlice bool
			mGoType, mIsSlice, _, err = mappingplan.DeriveGoShape(e.Shape.Canonical)
			if err != nil {
				err = ctx.Errorf("derive Go type from membership marker: %w", err)
				return
			}
			memberships = append(memberships, mappingplan.TupleMembership{
				GoField:      e.GoFieldName,
				GoType:       mGoType,
				Channel:      e.Shape.MembershipChannel,
				IsSlice:      mIsSlice,
				MarkerGoType: e.Shape.MarkerGoType, // "lw.Ref" / "lw.Verbatim" for the codegen bridge
			})
			continue
		}

		if e.Shape.CarrierType != "" {
			err = ctx.Errorf("marshalltypes carrier not supported inside a nested section — carrier channels cannot reach a nested section")
			return
		}
		if e.Shape.IsOption {
			err = ctx.Errorf("Option[T] not supported as a nested sub-column — the attribute has no per-sub-column presence")
			return
		}
		if e.Shape.Unit {
			err = ctx.Errorf("lw.Single (unit) not yet supported as a nested sub-column — use it at the entity level for now (Slice-A Step 4)")
			return
		}
		var column string
		var flags mappingplan.FieldFlags
		column, flags, err = splitNestedElemLW(e.LWTag)
		if err != nil {
			err = ctx.Errorf("parse nested sub-column tag: %w", err)
			return
		}
		if column == "" {
			// The flat single-sub-column default. A multi-sub-column nested
			// section must give each field a distinct `lw:"<column>"` tag — two
			// untagged fields would both claim "value" and collide below.
			column = "value"
		}
		if flags.Unit || flags.HasConst || flags.Channel != mappingplan.MembershipChannelLowCardRef {
			err = ctx.Errorf("nested sub-column tag takes only `ct=` (no unit / const / channel — the channel is on the membership)")
			return
		}
		if prev, dup := usedCols[column]; dup {
			err = ctx.Str("column", column).Str("first", prev).Errorf("sub-column appears on two nested fields")
			return
		}
		usedCols[column] = e.GoFieldName

		var goType string
		var isSlice, isRoaring bool
		goType, isSlice, isRoaring, err = mappingplan.DeriveGoShape(e.Shape.Canonical)
		if err != nil {
			err = ctx.Errorf("derive Go type from canonical: %w", err)
			return
		}
		fieldCanonical := e.Shape.Canonical
		if flags.CanonicalType != "" {
			fieldCanonical, err = resolveCanonicalOverride(e.GoFieldName, flags.CanonicalType, goType, isSlice, isRoaring)
			if err != nil {
				return
			}
			goType, isSlice, isRoaring, err = mappingplan.DeriveGoShape(fieldCanonical)
			if err != nil {
				err = ctx.Errorf("derive Go type from `,ct=` canonical: %w", err)
				return
			}
		}
		if isRoaring {
			err = ctx.Errorf("*roaring.Bitmap not supported as a nested sub-column — no stable element index to zip; use []T")
			return
		}
		if isSlice {
			if err = checkSliceElemType(e.GoFieldName, goType); err != nil {
				return
			}
		}
		values = append(values, valueField{goField: e.GoFieldName, column: column, canonical: fieldCanonical, ct: flags.CanonicalType})
	}

	if len(values) == 0 {
		err = eb.Build().Str("field", goFieldName).Errorf("nested section struct needs at least one sub-column field")
		return
	}

	dynamic := len(memberships) > 0
	var membership, section string
	var channel mappingplan.MembershipChannel

	if dynamic {
		// The outer tag is the bare section; memberships are per-attribute, so a
		// dynamic section must be a slice (`[]S`).
		if card != mappingplan.AttrCardinalityMany {
			err = eb.Build().Str("field", goFieldName).Errorf("a nested section with per-attribute lw.* membership fields must be a slice (`[]S`) — One / Optional dynamic-membership sections are not yet supported")
			return
		}
		section, err = SplitTupleOuterLW(outerTag)
		if err != nil {
			err = eb.Build().Str("field", goFieldName).Errorf("parse dynamic nested section tag: %w", err)
			return
		}
		if err = checkTupleMembershipArity(goFieldName, memberships); err != nil {
			return
		}
	} else {
		var pt ParsedLWTag
		pt, err = SplitLW(outerTag)
		if err != nil {
			err = eb.Build().Str("field", goFieldName).Errorf("parse nested section tag: %w", err)
			return
		}
		if err = rejectReservedMembership(pt.Membership); err != nil {
			err = eb.Build().Str("field", goFieldName).Errorf("%w", err)
			return
		}
		if pt.Membership == "" || pt.Section == "" {
			err = eb.Build().Str("field", goFieldName).Str("tag", outerTag).Errorf("nested section field needs a static membership and section (`lw:\"<membership>,<section>\"`) or per-attribute lw.* membership fields")
			return
		}
		if pt.Column != "" {
			err = eb.Build().Str("field", goFieldName).Errorf("nested section field names the whole section, not a sub-column (`:<col>`) — the sub-columns are the struct's fields")
			return
		}
		if pt.Flags.Unit || pt.Flags.HasConst || pt.Flags.CanonicalType != "" {
			err = eb.Build().Str("field", goFieldName).Errorf("nested section field tag takes only a channel flag (no unit / const / ct=)")
			return
		}
		if pt.Flags.Channel.UsesCarrier() {
			err = eb.Build().Str("field", goFieldName).Str("channel", pt.Flags.Channel.String()).Errorf("nested section cannot use a carrier / parametrized channel — a carrier's identity is per-row data, not a static membership")
			return
		}
		membership, section, channel = pt.Membership, pt.Section, pt.Flags.Channel
	}

	for _, v := range values {
		tf := mappingplan.TaggedField{
			GoFieldName:      v.goField,
			Canonical:        v.canonical,
			LWSection:        section,
			LWColumn:         v.column,
			TupleField:       goFieldName,
			TupleStructType:  structTypeName,
			TupleCardinality: card,
		}
		if dynamic {
			// Per-attribute memberships: the value fields carry no static
			// membership; the first membership's channel keeps g.Channel()
			// well-defined (memberships may be heterogeneous — ADR-0109 D4).
			tf.LWMembership = ""
			tf.TupleMemberships = memberships
			tf.Flags = mappingplan.FieldFlags{Channel: memberships[0].Channel, CanonicalType: v.ct}
		} else {
			tf.LWMembership = membership
			tf.TupleMemberships = nil // empty ⇒ static membership (codec discriminator)
			tf.Flags = mappingplan.FieldFlags{Channel: channel, CanonicalType: v.ct}
		}
		b.plan.Fields = append(b.plan.Fields, tf)
	}
	return
}

// checkTupleMembershipArity enforces the per-channel arity rule (ADR-0109 D3).
// On one channel the memberships are read back by draining that channel's
// per-attribute Seq positionally, so it must carry EITHER any number of fixed
// (scalar) memberships — each one membership, assigned in declaration order —
// OR exactly one repeated (slice) membership that takes the whole Seq. A slice
// mixed with any other membership on one channel (or two slices) could not be
// split back unambiguously. Checked in declaration order for a deterministic
// error. Shared by both spellings: the `@membership` tuple element
// (AddTupleSliceField) and the lw.* marker fields of a nested attribute struct
// (AddNestedSliceField).
func checkTupleMembershipArity(goFieldName string, memberships []mappingplan.TupleMembership) (err error) {
	seenSliceOnChannel := map[mappingplan.MembershipChannel]bool{}
	seenAnyOnChannel := map[mappingplan.MembershipChannel]bool{}
	for _, m := range memberships {
		if seenSliceOnChannel[m.Channel] || (m.IsSlice && seenAnyOnChannel[m.Channel]) {
			err = eb.Build().Str("field", goFieldName).Str("channel", m.Channel.String()).Errorf("a repeated (slice) membership must be the only membership on its channel — a slice cannot be split from the channel's other memberships; put them on different channels")
			return
		}
		if m.IsSlice {
			seenSliceOnChannel[m.Channel] = true
		}
		seenAnyOnChannel[m.Channel] = true
	}
	return nil
}
