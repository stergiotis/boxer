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
		if f.TupleField != "" {
			return false, fmt.Sprintf("field %s is a dynamic-membership tuple", f.TupleField)
		}
		if f.Flags.Channel.UsesCarrier() {
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
// present=false; a duplicated scalar field is an error, while duplicated
// container memberships concatenate. Fields bound to plain columns are
// left at their zero value — the caller owns the envelope.
func writeReadRowHelper(sb *strings.Builder, plan *mappingplan.Plan, mode EmitModeE) (err error) {
	kind := kindIdent(plan.KindType, mode)
	if ok, reason := ReadRowSupported(plan); !ok {
		linef(sb, 0, "// %sReadRow is not emitted: %s.\n", kind, reason)
		return
	}
	groups := goplan.ComputeGroups(plan)

	linef(sb, 0, "// %sReadRow reads row i as one optional %s component: presence-", kind, plan.KindType)
	line(sb, 0, "// gated (a row carrying none of the kind's memberships yields")
	line(sb, 0, "// present=false), membership-matched. A duplicated scalar field is")
	line(sb, 0, "// an error; duplicated container memberships concatenate. Plain-")
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
// absence; error on duplicate occurrences of a scalar-shaped field.
func writeReadRowFieldFinish(sb *strings.Builder, f mappingplan.TaggedField, prefix string) {
	switch {
	case f.IsOption:
		linef(sb, 1, "if %s%sCount > 1 {", prefix, f.GoFieldName)
		linef(sb, 2, "err = eb.Build().Int(\"row\", i).Str(\"field\", %q).Errorf(\"occurs more than once on the row\")", f.GoFieldName)
		line(sb, 2, "return\n\t}")
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
		linef(sb, 1, "if %s%sCount > 1 {", prefix, f.GoFieldName)
		linef(sb, 2, "err = eb.Build().Int(\"row\", i).Str(\"field\", %q).Errorf(\"occurs more than once on the row\")", f.GoFieldName)
		line(sb, 2, "return\n\t}")
		linef(sb, 1, "if %s%sCount == 1 {", prefix, f.GoFieldName)
		linef(sb, 2, "row.%s = %s%sVal", f.GoFieldName, prefix, f.GoFieldName)
		line(sb, 2, "present = true")
		line(sb, 1, "}")
	}
}
