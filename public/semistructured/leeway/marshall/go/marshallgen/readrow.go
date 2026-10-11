package marshallgen

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

// ReadRow: the presence-gated single-row twin of FillFromArrow (ADR-0100 S2),
// for a kind carried as an optional component of a FAT table rather than as a
// kind-homogeneous batch. It reuses the fill side's match loops and attaches
// presence-tolerant tails.

// ReadRowSupported reports whether <Kind>ReadRow is emitted for the plan,
// and the reason when it is not. Shared with downstream generators
// (recordstore/gen) so the store generator and this emission cannot
// disagree about coverage. Carrier (mixed / parametrized) channels are not
// covered yet; a plain-only kind has no sections to read, and a const-only
// kind is rejected because the match loops skip consts — presence could
// never be set, so the component would read back permanently absent.
func ReadRowSupported(plan *mappingplan.Plan) (ok bool, reason string) {
	if len(plan.Fields) == 0 {
		return false, "plain-only kind (no tagged sections)"
	}
	if !plan.HasNonConstField() {
		return false, "const-only kind (no non-const field can set presence)"
	}
	for _, f := range plan.Fields {
		// A tuple field reads by claiming its attributes (ADR-0326 §SD4); a
		// flat field on a carrier channel pairs with a carrier ReadRow does
		// not decode.
		if f.TupleField == "" && f.Flags.Channel.UsesCarrier() {
			return false, fmt.Sprintf("field %s uses a carrier channel", f.GoFieldName)
		}
	}
	return true, ""
}

// writeReadRowHelper emits <Kind>ReadRow: the presence-gated single-row
// twin of FillFromArrow. Where FillFromArrow decodes kind-homogeneous
// batches (a row lacking a scalar/unit field is an error), ReadRow reads
// one row of a FAT table on which the kind is an optional component
// (ADR-0075): a row carrying none of the kind's memberships yields
// present=false, while a slot carrying more attributes than the DTO's shape
// admits is an error — for every shape, containers included (ADR-0146 D4;
// duplicated container memberships used to concatenate silently). Fields
// bound to plain columns are left at their zero value — the caller owns the
// envelope.
func writeReadRowHelper(sb *strings.Builder, plan *mappingplan.Plan, mode EmitModeE) (err error) {
	kind := kindIdent(plan.KindType, mode)
	if ok, reason := ReadRowSupported(plan); !ok {
		linef(sb, 0, "// %sReadRow is not emitted: %s.\n", kind, reason)
		return
	}
	groups := goplan.ComputeGroups(plan)

	linef(sb, 0, "// %sReadRow reads row i as one optional %s component: presence-", kind, plan.KindType)
	line(sb, 0, "// gated (a row carrying none of the kind's memberships yields")
	line(sb, 0, "// present=false), membership-matched. A slot carrying more")
	line(sb, 0, "// attributes than this kind's shape admits is an error, for every")
	line(sb, 0, "// shape including containers. Plain-")
	line(sb, 0, "// bound fields stay zero — the caller owns the envelope. The")
	line(sb, 0, "// Attrs/Membs readers bind by type inference at the call site, as")
	line(sb, 0, "// with FillFromArrow.")
	linef(sb, 0, "func %sReadRow[", kind)
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "%sAttrs %s%sAttrsReadI,", method, kind, method)
		linef(sb, 1, "%sMembs %s%sMembsReadI,", method, kind, method)
	}
	line(sb, 0, "](")
	line(sb, 1, "i int,")
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "%sAttrs %sAttrs,", lowerFirst(method), method)
		linef(sb, 1, "%sMembs %sMembs,", lowerFirst(method), method)
	}
	linef(sb, 0, ") (row %s, present bool, err error) {", plan.KindType)

	for _, g := range groups {
		method := methodFor(g.Section)
		attrsVar := lowerFirst(method) + "Attrs"
		membsVar := lowerFirst(method) + "Membs"
		prefix := lowerFirst(method)
		linef(sb, 1, "// --- %s. ---", g.Section)
		if ts, ok := g.TupleSpec(); ok {
			writeReadRowTuple(sb, g, ts, attrsVar, membsVar, prefix)
			continue
		}
		if len(g.SubColumns) > 1 {
			var subs []multiSub
			var memb mappingplan.TaggedField
			subs, memb, err = writeMultiSubMatchLoops(sb, g, attrsVar, membsVar, prefix)
			if err != nil {
				return
			}
			linef(sb, 1, "if %s%sCount > 1 {", prefix, memb.GoFieldName)
			linef(sb, 2, "err = eb.Build().Int(\"row\", i).Str(\"membership\", %q).Errorf(\"occurs more than once on the row\")", memb.LWMembership)
			line(sb, 2, "return\n\t}")
			linef(sb, 1, "if %s%sCount == 1 {", prefix, memb.GoFieldName)
			for _, s := range subs {
				linef(sb, 2, "row.%s = %s%sVal", s.Field.GoFieldName, prefix, s.Field.GoFieldName)
			}
			line(sb, 2, "present = true")
			line(sb, 1, "}")
			continue
		}
		fields := writeSectionMatchLoops(sb, g, attrsVar, membsVar, prefix)
		for _, f := range fields {
			writeReadRowFieldFinish(sb, f, prefix)
		}
	}
	line(sb, 1, "return")
	line(sb, 0, "}\n")
	return
}

// writeReadRowFieldFinish emits the presence-tolerant tail for one field
// after writeSectionMatchLoops: assign into the row and mark the
// component present on a match; leave the zero value (never error) on
// absence; error on a slot carrying more attributes than the shape admits.
func writeReadRowFieldFinish(sb *strings.Builder, f mappingplan.TaggedField, prefix string) {
	// ReadRow reads an optional component, so absence is reported through
	// `present`, not as an error; only surplus is refused. Emitted for every
	// shape now — a container's duplicate memberships used to concatenate
	// silently (ADR-0146 D4).
	writeSlotArityCheck(sb, f, prefix, false, -1)
	switch {
	case f.IsOption:
		linef(sb, 1, "if %s%sCount == 1 {", prefix, f.GoFieldName)
		// Field assignment, not option.Some — the generated file does not
		// import the option package (same idiom as Row / Append).
		linef(sb, 2, "row.%s.Val = %s%sVal", f.GoFieldName, prefix, f.GoFieldName)
		linef(sb, 2, "row.%s.Has = true", f.GoFieldName)
		line(sb, 2, "present = true")
		line(sb, 1, "}")
	case f.IsSlice():
		linef(sb, 1, "if %s%sSlice != nil {", prefix, f.GoFieldName)
		linef(sb, 2, "row.%s = %s%sSlice", f.GoFieldName, prefix, f.GoFieldName)
		line(sb, 2, "present = true")
		line(sb, 1, "}")
	case f.IsRoaring():
		linef(sb, 1, "if %s%sBitmap != nil {", prefix, f.GoFieldName)
		linef(sb, 2, "row.%s = %s%sBitmap", f.GoFieldName, prefix, f.GoFieldName)
		line(sb, 2, "present = true")
		line(sb, 1, "}")
	default:
		// The surplus gate above covers the duplicate case.
		linef(sb, 1, "if %s%sCount == 1 {", prefix, f.GoFieldName)
		linef(sb, 2, "row.%s = %s%sVal", f.GoFieldName, prefix, f.GoFieldName)
		line(sb, 2, "present = true")
		line(sb, 1, "}")
	}
}

// writeReadRowTuple emits ReadRow's read of a tuple section on a table
// whose sections other kinds share (ADR-0326 §SD4). The tuple claims an
// attribute — a dynamic tuple, one carrying any membership on its
// channels; a static nested section, one carrying its membership — and
// decodes each claimed attribute to one element as FillFromArrow does. An
// unclaimed attribute is another kind's. Any claimed attribute makes the
// component present; the cardinality bounds how many may be.
func writeReadRowTuple(sb *strings.Builder, g goplan.SectionGroup, ts goplan.TupleSpec, attrsVar, membsVar, prefix string) {
	elemsVar := prefix + ts.GoField + "Elems"
	linef(sb, 1, "var %s []%s", elemsVar, ts.StructType)
	linef(sb, 1, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 1, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	claimVar := prefix + "Claimed"
	linef(sb, 2, "%s := false", claimVar)
	seq := func(ch mappingplan.MembershipChannel) (accessor string, pair bool) {
		suffix := ch.AddMethodSuffix()
		if ch.UsesCarrier() {
			suffix, pair = ch.CarrierReadMethodSuffix(), true
		}
		return fmt.Sprintf("%s.GetMembValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", membsVar, suffix), pair
	}
	if len(ts.Memberships) > 0 {
		for _, ch := range ts.Channels() {
			accessor, _ := seq(ch)
			linef(sb, 2, "for range %s {", accessor)
			linef(sb, 3, "%s = true", claimVar)
			line(sb, 3, "break")
			line(sb, 2, "}")
		}
	} else {
		f := sectionMembership(g)
		ch := f.Flags.Channel
		// A mixed channel's accessor yields (id or name, parameters); the
		// range binds the first.
		accessor, _ := seq(ch)
		match := "mv == " + f.KindVar()
		if ch.EmbedsLiteralName() || ch.CarrierValueIsBytes() {
			match = fmt.Sprintf("string(mv) == %q", f.LWMembership)
		}
		linef(sb, 2, "for mv := range %s {", accessor)
		linef(sb, 3, "if %s {", match)
		linef(sb, 4, "%s = true", claimVar)
		line(sb, 4, "break")
		line(sb, 3, "}")
		line(sb, 2, "}")
	}
	linef(sb, 2, "if !%s {", claimVar)
	line(sb, 3, "continue")
	line(sb, 2, "}")
	elemFields := writeTupleElemDecode(sb, 2, g, ts, attrsVar, membsVar, prefix)
	linef(sb, 2, "%s = append(%s, %s{", elemsVar, elemsVar, ts.StructType)
	for _, f := range elemFields {
		linef(sb, 3, "%s,", f)
	}
	line(sb, 2, "})")
	line(sb, 1, "}")

	switch ts.Cardinality {
	case mappingplan.AttrCardinalityOne, mappingplan.AttrCardinalityOptional:
		linef(sb, 1, "if len(%s) > 1 {", elemsVar)
		linef(sb, 2, `err = eb.Build().Int("row", i).Str("section", %q).Int("attrs", len(%s)).Errorf("a single nested section carries more than one attribute on the row")`, g.Section, elemsVar)
		line(sb, 2, "return")
		line(sb, 1, "}")
		linef(sb, 1, "if len(%s) == 1 {", elemsVar)
		if ts.Cardinality == mappingplan.AttrCardinalityOne {
			linef(sb, 2, "row.%s = %s[0]", ts.GoField, elemsVar)
		} else {
			linef(sb, 2, "row.%s.Val = %s[0]", ts.GoField, elemsVar)
			linef(sb, 2, "row.%s.Has = true", ts.GoField)
		}
		line(sb, 2, "present = true")
		line(sb, 1, "}")
	default: // Many
		linef(sb, 1, "if len(%s) > 0 {", elemsVar)
		linef(sb, 2, "row.%s = %s", ts.GoField, elemsVar)
		line(sb, 2, "present = true")
		line(sb, 1, "}")
	}
}
