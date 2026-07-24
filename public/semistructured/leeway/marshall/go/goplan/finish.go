package goplan

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// The whole-DTO checks: the rules that can only be decided once every field
// has been added — completeness, tuple-section exclusivity, per-section
// channel uniformity, kindXxx symbol collisions, carrier pairing, and the
// multi-sub-column structural rules.

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
		// DYNAMIC tuple value fields carry a ref channel only to keep
		// g.Channel() well-defined; their memberships are per-element ids (or
		// names) declared on the element's `@membership` fields, not a static
		// kindXxx symbol (ADR-0109), so this identifier rule does not apply to
		// them. A STATIC nested section (TupleField set, TupleMemberships empty)
		// DOES resolve its membership through a kindXxx symbol like any flat
		// section, so it must satisfy the identifier rule.
		if f.TupleField != "" && len(f.TupleMemberships) > 0 {
			continue
		}
		if f.Flags.Channel.NeedsKindVar() && !mappingplan.IsIdentifierLike(f.LWMembership) {
			err = eb.Build().Str("membership", f.LWMembership).Errorf("ref-channel membership must be a Go identifier (ASCII letters, digits, underscores) — it becomes the emitted kindXxx symbol; use a verbatim channel for an arbitrary wire label")
			return
		}
	}

	// Tuple-section exclusivity (ADR-0103). A section mapped by a
	// dynamic-membership tuple field belongs to it entirely: its attribute
	// count and memberships are per-element data, so a static field, const
	// or second tuple field on the same section could not be disambiguated
	// on read (the same rationale as the carrier one-membership rule).
	// Checked before the channel-uniformity rule so a shared section is
	// reported as the sharing itself, not as a downstream channel mix.
	tupleOwner := map[string]string{}
	for _, f := range b.plan.Fields {
		if f.TupleField == "" {
			continue
		}
		if owner, ok := tupleOwner[f.LWSection]; ok && owner != f.TupleField {
			err = eb.Build().Str("section", f.LWSection).Str("first", owner).Str("second", f.TupleField).Errorf("two tuple fields map one section")
			return
		}
		tupleOwner[f.LWSection] = f.TupleField
	}
	if len(tupleOwner) > 0 {
		for _, f := range b.plan.Fields {
			owner, ok := tupleOwner[f.LWSection]
			if !ok || f.TupleField == owner {
				continue
			}
			name := f.GoFieldName
			if f.IsConst {
				name = "const " + f.LWMembership
			}
			err = eb.Build().Str("section", f.LWSection).Str("tupleField", owner).Str("field", name).Errorf("section is mapped by a tuple field — no other field may target it")
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
		// Carriers are scalar-only: one marshalltypes.X per attribute,
		// whatever the value shape (scalar / Option / container). The
		// element-wise slice pairing went with `,explode` (ADR-0113 D1).
		f.CarrierField = c.goField
		f.CarrierType = c.carrierType
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
	}

	plan = b.plan
	return
}
