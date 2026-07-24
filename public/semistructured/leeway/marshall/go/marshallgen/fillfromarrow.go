package marshallgen

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

// The read side: the derived per-section AttrsReadI / MembsReadI constraint
// interfaces and the <Kind>FillFromArrow decode that walks them, batch-strict
// (a row missing a mandatory field is an error).

func writeFillHelper(sb *strings.Builder, plan *mappingplan.Plan) (err error) {
	groups := goplan.ComputeGroups(plan)

	line(sb, 0, "// --- Composed-interface FillFromArrow helper (schema-agnostic). ---\n//")
	linef(sb, 0, "// %sFillFromArrow walks the Arrow record row-by-row and appends", plan.KindType)
	line(sb, 0, "// each entity's plain + tagged-section values into c. Plain columns")
	line(sb, 0, "// enter as concrete Arrow accessors (uniform across schemas);")
	line(sb, 0, "// per-section Attrs + Membs bind through type-parameter interfaces.")
	blank(sb)

	for _, g := range groups {
		err = writeSectionReadInterfaces(sb, plan, g, EmitModeCodec)
		if err != nil {
			return
		}
	}
	err = writeFillFromArrowFunc(sb, plan, groups)
	return
}

// argPair is a parsed (name, type) pair, used for multi-sub-column
// section read interfaces (one accessor per sub-column).
type argPair struct{ Name, Type string }

func writeSectionReadInterfaces(sb *strings.Builder, plan *mappingplan.Plan, g goplan.SectionGroup, mode EmitModeE) (err error) {
	kind := kindIdent(plan.KindType, mode)
	method := methodFor(g.Section)

	// Classify which read-side accessors this DTO's fields require for
	// this section. Section is scalar (only GetAttrValueValue T) iff
	// every field uses a scalar-section write shape (no Unit, no
	// container); non-scalar otherwise (GetAttrValueValue iter.Seq[T]
	// for container shapes, GetAttrValueSingleOrDefault T for Unit
	// single-value shapes).
	var hasScalarValue, hasSingleVal, hasIterVal bool
	for _, sc := range g.SubColumns {
		for _, f := range sc.Fields {
			switch goplan.ClassifyBegin(f) {
			case goplan.ShapeScalarBegin:
				hasScalarValue = true
			case goplan.ShapeScalarBeginSingle:
				hasSingleVal = true
			case goplan.ShapeContainer:
				hasIterVal = true
			}
		}
	}
	if len(g.SubColumns) == 1 && hasScalarValue && (hasSingleVal || hasIterVal) {
		// Single-sub-column only: two field shapes would contend for one
		// GetAttrValueValue signature. A multi-sub-column section reads each
		// sub-column through its own accessor, so mixed shapes are fine
		// there (ADR-0101 D5).
		err = eb.Build().Str("section", g.Section).Errorf("section mixes scalar-section field shape with non-scalar-section field shape — disambiguate via flags so the read API resolves to one method set")
		return
	}

	linef(sb, 0, "// %s%sAttrsReadI is the Attributes-side view of the %s section.", kind, method, g.Section)
	linef(sb, 0, "type %s%sAttrsReadI interface {", kind, method)
	// A tuple section reads every sub-column through its own named
	// accessor at any sub-column count (its decode addresses columns by
	// name, and a lone sub-column may not be named "value") — ADR-0103.
	_, isTuple := g.TupleSpec()
	if isTuple || len(g.SubColumns) > 1 {
		// Per-sub-column accessor, each shaped to its own subtype: scalar
		// sub-columns read the value directly, container sub-columns drain
		// an iter.Seq (the RA generator emits exactly this pair of shapes).
		for _, sc := range g.SubColumns {
			f := sc.Fields[0]
			if f.IsSlice() {
				linef(sb, 1, "GetAttrValue%s(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq[%s]", mappingplan.UpperFirst(sc.Name), elemType(f))
			} else {
				linef(sb, 1, "GetAttrValue%s(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) %s", mappingplan.UpperFirst(sc.Name), f.GoType())
			}
		}
	} else {
		f := g.SubColumns[0].Fields[0]
		vt := elemType(f)
		switch {
		case hasScalarValue:
			// Scalar section — GetAttrValueValue returns the value directly.
			linef(sb, 1, "GetAttrValueValue(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) %s", vt)
		default:
			// Non-scalar section — expose what the fields actually use.
			if hasIterVal {
				linef(sb, 1, "GetAttrValueValue(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq[%s]", vt)
			}
			if hasSingleVal {
				linef(sb, 1, "GetAttrValueSingleOrDefault(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) %s", vt)
			}
		}
	}
	line(sb, 1, "GetNumberOfAttributes(entityIdx raruntime.EntityIdx) int64")
	line(sb, 0, "}\n")

	linef(sb, 0, "// %s%sMembsReadI is the Memberships-side view of the %s section.", kind, method, g.Section)
	linef(sb, 0, "type %s%sMembsReadI interface {", kind, method)
	if ts, ok := g.TupleSpec(); ok {
		// A tuple element may read memberships on several channels (ADR-0109
		// D4); expose one GetMembValue<Channel> per channel (all simple
		// channels — a plain Seq of the id / name).
		for _, tch := range ts.Channels() {
			linef(sb, 1, "GetMembValue%s(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq[%s]", tch.AddMethodSuffix(), tch.ReadIterElemType())
		}
		line(sb, 0, "}\n")
		return
	}
	ch := g.Channel()
	switch {
	case ch.UsesCarrier() && ch.CarrierValueField() != "":
		// Mixed channel: the combined Seq2 accessor yields the per-row
		// membership value (id/name) + params together.
		linef(sb, 1, "GetMembValue%s(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq2[%s]", ch.CarrierReadMethodSuffix(), ch.CarrierReadSeq2Types())
	case ch.UsesCarrier():
		// Parametrized channel: a single Seq of the opaque params blob.
		linef(sb, 1, "GetMembValue%s(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq[[]byte]", ch.CarrierReadMethodSuffix())
	default:
		linef(sb, 1, "GetMembValue%s(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq[%s]", ch.AddMethodSuffix(), ch.ReadIterElemType())
	}
	line(sb, 0, "}\n")
	return
}

func writeFillFromArrowFunc(sb *strings.Builder, plan *mappingplan.Plan, groups []goplan.SectionGroup) (err error) {
	kind := plan.KindType

	idCol := planIdCol(plan)
	nkCol := planNaturalKeyCol(plan)
	tsCol := planTsCol(plan)
	lcCol := planExpiresAtCol(plan)
	if idCol == nil {
		err = eb.Build().Errorf("plain spec missing required `id` column")
		return
	}

	linef(sb, 0, "// %sFillFromArrow walks rec row-by-row and appends each entity's", kind)
	line(sb, 0, "// plain + tagged-section values into c. Plain columns enter as")
	line(sb, 0, "// concrete Arrow accessors; per-section Attrs + Membs bind through")
	line(sb, 0, "// type-parameter interfaces.")
	if len(groups) == 0 {
		// Plain-only entity (no tagged sections): FillFromArrow is a plain,
		// non-generic func. An empty type-parameter list `[]` is invalid Go,
		// so the `[ … ]` block is emitted only when there is at least one
		// per-section reader type parameter to put in it.
		linef(sb, 0, "func %sFillFromArrow(", kind)
	} else {
		linef(sb, 0, "func %sFillFromArrow[", kind)
		for _, g := range groups {
			method := methodFor(g.Section)
			linef(sb, 1, "%sAttrs %s%sAttrsReadI,", method, kind, method)
			linef(sb, 1, "%sMembs %s%sMembsReadI,", method, kind, method)
		}
		line(sb, 0, "](")
	}
	linef(sb, 1, "c *%sColumns,", kind)
	line(sb, 1, "n int,")
	linef(sb, 1, "idCol %s,", plainArrowParam(idCol))
	if nkCol != nil {
		linef(sb, 1, "nkCol %s,", plainArrowParam(nkCol))
	}
	if tsCol != nil {
		linef(sb, 1, "tsCol %s,", plainArrowParam(tsCol))
	}
	if lcCol != nil {
		linef(sb, 1, "lcCol %s,", plainArrowParam(lcCol))
	}
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "%sAttrs %sAttrs,", lowerFirst(method), method)
		linef(sb, 1, "%sMembs %sMembs,", lowerFirst(method), method)
	}
	line(sb, 0, ") (err error) {")
	line(sb, 1, "for i := 0; i < n; i++ {")

	writePlainRead(sb, 2, idCol, "idCol")
	if nkCol != nil {
		writePlainRead(sb, 2, nkCol, "nkCol")
	}
	if tsCol != nil {
		writePlainRead(sb, 2, tsCol, "tsCol")
	}
	if lcCol != nil {
		writePlainRead(sb, 2, lcCol, "lcCol")
	}

	for _, g := range groups {
		err = writeSectionDecode(sb, g)
		if err != nil {
			return
		}
	}

	line(sb, 1, "}")
	line(sb, 1, "return\n}\n")
	return
}

func writeSectionDecode(sb *strings.Builder, g goplan.SectionGroup) (err error) {
	method := methodFor(g.Section)
	attrsVar := lowerFirst(method) + "Attrs"
	membsVar := lowerFirst(method) + "Membs"
	prefix := lowerFirst(method)

	linef(sb, 2, "// --- %s. ---", g.Section)

	if ts, ok := g.TupleSpec(); ok {
		writeTupleSectionDecode(sb, g, ts, attrsVar, membsVar, prefix)
		return
	}

	if len(g.SubColumns) > 1 {
		return writeMultiSubColumnDecode(sb, g, attrsVar, membsVar, prefix)
	}

	if g.Channel().UsesCarrier() {
		return writeCarrierSectionDecode(sb, g, attrsVar, membsVar, prefix)
	}

	fields := writeSectionMatchLoops(sb, g, attrsVar, membsVar, prefix)
	for _, f := range fields {
		writeFieldAppend(sb, f, prefix)
	}
	return
}

// writeTupleSectionDecode emits the FillFromArrow decode of a
// dynamic-membership tuple section (ADR-0103, extended by ADR-0109). Every
// attribute of the row belongs to the tuple field (PlanBuilder.Finish
// guarantees the section carries no other field), so there is no membership
// match: each attribute decodes to ONE element — its sub-column values read
// positionally and its memberships distributed to the element's `@membership`
// fields per channel (fixed fields positional, a repeated field taking the
// whole channel Seq). Zero attributes decode to a nil element slice. Mirrors
// marshallreflect.unmarshalTupleSection.
func writeTupleSectionDecode(sb *strings.Builder, g goplan.SectionGroup, ts goplan.TupleSpec, attrsVar, membsVar, prefix string) {
	elemsVar := prefix + ts.GoField + "Elems"
	linef(sb, 2, "var %s []%s", elemsVar, ts.StructType)
	linef(sb, 2, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 2, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	for _, sc := range g.SubColumns {
		f := sc.Fields[0]
		localVar := prefix + f.GoFieldName + "Local"
		accessor := fmt.Sprintf("%s.GetAttrValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", attrsVar, mappingplan.UpperFirst(sc.Name))
		if !f.IsSlice() {
			// Scalar sub-column: read with the field's copy strategy — the
			// element is retained inside the tuple slice, so a []byte value
			// must not alias the reused Arrow buffer.
			linef(sb, 3, "var %s %s", localVar, f.GoType())
			writeCarrierValueRead(sb, 3, f, localVar, accessor)
			continue
		}
		// Container sub-column: drain the per-attribute Seq into a fresh
		// slice (nil for an empty container — an N = 0 attribute reads
		// back as a nil slice, ADR-0101 D5).
		linef(sb, 3, "var %s []%s", localVar, f.GoType())
		linef(sb, 3, "for v := range %s {", accessor)
		if goplan.CopyStrategy(f.GoType()) == goplan.CopyBytes {
			line(sb, 4, "cp := make([]byte, len(v))")
			line(sb, 4, "copy(cp, v)")
			linef(sb, 4, "%s = append(%s, cp)", localVar, localVar)
		} else {
			linef(sb, 4, "%s = append(%s, v)", localVar, localVar)
		}
		line(sb, 3, "}")
	}

	// Memberships, one channel at a time (ADR-0109 D3). membExpr[goField] is the
	// element-literal expression for each `@membership` field.
	membExpr := map[string]string{}
	for _, ch := range ts.Channels() {
		suffix := ch.AddMethodSuffix()
		accessor := fmt.Sprintf("%s.GetMembValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", membsVar, suffix)
		var chFields []mappingplan.TupleMembership
		for _, m := range ts.Memberships {
			if m.Channel == ch {
				chFields = append(chFields, m)
			}
		}
		if len(chFields) == 1 && chFields[0].IsSlice {
			// Slice-mode: the sole field on this channel takes the whole Seq.
			f := chFields[0]
			sliceVar := prefix + f.GoField + "Membs"
			linef(sb, 3, "var %s []%s", sliceVar, membElemGoType(f))
			linef(sb, 3, "for mv := range %s {", accessor)
			linef(sb, 4, "%s = append(%s, %s)", sliceVar, sliceVar, tupleMembDecodeElem("mv", f))
			line(sb, 3, "}")
			membExpr[f.GoField] = sliceVar
			continue
		}
		// Fixed-mode: collect the channel's values, require the exact count, then
		// index one per field in declaration order.
		rawVar := prefix + suffix + "Membs"
		wireElem := "uint64"
		if ch.EmbedsLiteralName() {
			wireElem = "[]byte"
		}
		linef(sb, 3, "var %s []%s", rawVar, wireElem)
		linef(sb, 3, "for mv := range %s {", accessor)
		linef(sb, 4, "%s = append(%s, mv)", rawVar, rawVar)
		line(sb, 3, "}")
		linef(sb, 3, "if len(%s) != %d {", rawVar, len(chFields))
		linef(sb, 4, `err = eb.Build().Str("section", %q).Str("channel", %q).Int("got", len(%s)).Int("want", %d).Errorf("membership count mismatch on read")`, g.Section, suffix, rawVar, len(chFields))
		line(sb, 4, "return")
		line(sb, 3, "}")
		for idx, f := range chFields {
			membExpr[f.GoField] = tupleMembDecodeElem(fmt.Sprintf("%s[%d]", rawVar, idx), f)
		}
	}

	linef(sb, 3, "%s = append(%s, %s{", elemsVar, elemsVar, ts.StructType)
	for _, m := range ts.Memberships {
		linef(sb, 4, "%s: %s,", m.GoField, membExpr[m.GoField])
	}
	for _, sc := range g.SubColumns {
		f := sc.Fields[0]
		linef(sb, 4, "%s: %s%sLocal,", f.GoFieldName, prefix, f.GoFieldName)
	}
	line(sb, 3, "})")
	line(sb, 2, "}")

	// Project the decoded elements onto the SoA column by cardinality (mirrors
	// marshallreflect.unmarshalTupleSection). One: exactly one attribute per row
	// (zero only when the section is all-container and spliced away); Optional:
	// at most one (Val/Has); Many: the whole element slice (nil when empty).
	switch ts.Cardinality {
	case mappingplan.AttrCardinalityOne:
		cond := fmt.Sprintf("len(%s) != 1", elemsVar)
		if len(g.ScalarSubColumns()) == 0 {
			cond = fmt.Sprintf("len(%s) > 1", elemsVar) // all-container: 0 or 1
		}
		linef(sb, 2, "if %s {", cond)
		linef(sb, 3, `err = eb.Build().Str("section", %q).Int("attrs", len(%s)).Errorf("cardinality-One nested section must carry exactly one attribute per row")`, g.Section, elemsVar)
		line(sb, 3, "return")
		line(sb, 2, "}")
		oneVar := prefix + ts.GoField + "One"
		linef(sb, 2, "var %s %s", oneVar, ts.StructType)
		linef(sb, 2, "if len(%s) == 1 {", elemsVar)
		linef(sb, 3, "%s = %s[0]", oneVar, elemsVar)
		line(sb, 2, "}")
		linef(sb, 2, "c.%s = append(c.%s, %s)", ts.GoField, ts.GoField, oneVar)
	case mappingplan.AttrCardinalityOptional:
		linef(sb, 2, "if len(%s) > 1 {", elemsVar)
		linef(sb, 3, `err = eb.Build().Str("section", %q).Int("attrs", len(%s)).Errorf("Optional nested section must carry at most one attribute per row")`, g.Section, elemsVar)
		line(sb, 3, "return")
		line(sb, 2, "}")
		valVar := prefix + ts.GoField + "Val"
		hasVar := prefix + ts.GoField + "Has"
		linef(sb, 2, "var %s %s", valVar, ts.StructType)
		linef(sb, 2, "%s := len(%s) == 1", hasVar, elemsVar)
		linef(sb, 2, "if %s {", hasVar)
		linef(sb, 3, "%s = %s[0]", valVar, elemsVar)
		line(sb, 2, "}")
		linef(sb, 2, "c.%sVal = append(c.%sVal, %s)", ts.GoField, ts.GoField, valVar)
		linef(sb, 2, "c.%sHas = append(c.%sHas, %s)", ts.GoField, ts.GoField, hasVar)
	default: // Many
		linef(sb, 2, "c.%s = append(c.%s, %s)", ts.GoField, ts.GoField, elemsVar)
	}
}

// tupleMembDecodeElem renders the expression converting one wire membership
// value (a loop var or an indexed raw value) to a tuple element field's element
// type: the uint64 id verbatim for a ref channel; string(x) for a string field;
// a defensive []byte copy for a []byte field (the value aliases the reused Arrow
// buffer and is retained inside the tuple slice).
func tupleMembDecodeElem(src string, m mappingplan.TupleMembership) string {
	var expr string
	switch m.GoType {
	case "uint64":
		expr = src
	case "[]byte":
		expr = "append([]byte(nil), " + src + "...)"
	default: // "string"
		expr = "string(" + src + ")"
	}
	// Wrap in the lw.* marker newtype (lw.Ref(v) / lw.Verbatim(string(v))); a
	// plain @membership field takes the underlying type directly.
	if m.MarkerGoType != "" {
		expr = m.MarkerGoType + "(" + expr + ")"
	}
	return expr
}

// membElemGoType is the Go type of one membership value in a decoded slice: the
// lw.* marker newtype when set (so a `[]lw.Ref` field is declared `[]lw.Ref`),
// else the plain underlying type.
func membElemGoType(m mappingplan.TupleMembership) string {
	if m.MarkerGoType != "" {
		return m.MarkerGoType
	}
	return m.GoType
}

// writeSectionMatchLoops emits the shared middle of a non-carrier
// single-sub-column section decode: per-field accumulator declarations,
// the attribute loop and the membership-match switch filling them. Both
// FillFromArrow (strict, SoA-appending tails) and ReadRow (presence-
// tolerant, row-assigning tails) build on it. Returns the non-const
// fields the caller must finish.
func writeSectionMatchLoops(sb *strings.Builder, g goplan.SectionGroup, attrsVar, membsVar, prefix string) (fields []mappingplan.TaggedField) {
	for _, f := range g.SubColumns[0].Fields {
		if f.IsConst {
			continue
		}
		fields = append(fields, f)
	}
	for _, f := range fields {
		writeFieldAccumulatorDecl(sb, f, prefix)
	}
	linef(sb, 2, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 2, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	if g.Channel().EmbedsLiteralName() {
		linef(sb, 3, "for membBytes := range %s.GetMembValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", membsVar, g.Channel().AddMethodSuffix())
		line(sb, 4, "switch string(membBytes) {")
	} else {
		linef(sb, 3, "for membID := range %s.GetMembValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", membsVar, g.Channel().AddMethodSuffix())
		line(sb, 4, "switch membID {")
	}
	for _, f := range fields {
		writeFieldMembCase(sb, f, prefix, attrsVar)
	}
	line(sb, 4, "}")
	line(sb, 3, "}")
	line(sb, 2, "}")
	return
}

// writeCarrierSectionDecode emits FillFromArrow decode for a mixed /
// parametrized (carrier-channel) section. PlanBuilder guarantees one
// membership — one value+carrier field — so every attribute belongs to it and
// there is no membership-id switch. The value comes from the section value
// accessor; the per-row carrier (id/name + params) comes from the combined
// Seq2 (mixed) or Seq (parametrized) membership accessor. The value field's
// shape selects the decode (ADR-0008 OQ#4): scalar / Option pair a single
// value with a scalar carrier; a container []T pairs N values (one attribute)
// with a scalar carrier. Carriers are scalar-only — one marshalltypes.X per
// attribute (ADR-0113 D1).
func writeCarrierSectionDecode(sb *strings.Builder, g goplan.SectionGroup, attrsVar, membsVar, prefix string) (err error) {
	var f mappingplan.TaggedField
	found := false
	for _, ff := range g.SubColumns[0].Fields {
		if ff.Flags.Channel.UsesCarrier() {
			f = ff
			found = true
			break
		}
	}
	if !found {
		err = eb.Build().Str("section", g.Section).Errorf("carrier section has no value field")
		return
	}

	switch {
	case f.IsSlice():
		writeCarrierContainerDecode(sb, f, attrsVar, membsVar, prefix)
	case f.IsOption:
		writeCarrierOptionDecode(sb, f, attrsVar, membsVar, prefix)
	default:
		writeCarrierScalarDecode(sb, f, attrsVar, membsVar, prefix)
	}
	return
}

// carrierStructLiteral renders the marshalltypes carrier struct literal
// reconstructed from a per-attribute membership read. mvExpr is the
// membership-value loop expression for mixed channels; ignored for
// parametrized channels (whose CarrierValueField is "").
func carrierStructLiteral(f mappingplan.TaggedField, mvExpr, paramsExpr string) string {
	if vf := f.Flags.Channel.CarrierValueField(); vf != "" {
		return fmt.Sprintf("marshalltypes.%s{%s: %s, Params: append([]byte(nil), %s...)}", f.CarrierType, vf, mvExpr, paramsExpr)
	}
	return fmt.Sprintf("marshalltypes.%s{Params: append([]byte(nil), %s...)}", f.CarrierType, paramsExpr)
}

// carrierMembValExpr is the membership-value loop expression for a mixed
// channel ("mv", copied out of the Arrow buffer for verbatim []byte names),
// or "" for a parametrized channel (no separate membership value).
func carrierMembValExpr(f mappingplan.TaggedField) string {
	if f.Flags.Channel.CarrierValueField() == "" {
		return ""
	}
	if f.Flags.Channel.CarrierValueIsBytes() {
		return "append([]byte(nil), mv...)"
	}
	return "mv"
}

// writeCarrierMembLoopHeader emits, at the given depth, the `for … := range
// <membs>.<read>(…) {` line opening a per-attribute membership read. Mixed
// channels range over (mv, params); parametrized over (params) only.
func writeCarrierMembLoopHeader(sb *strings.Builder, depth int, f mappingplan.TaggedField, membsVar, readMethod string) {
	if f.Flags.Channel.CarrierValueField() == "" {
		linef(sb, depth, "for params := range %s.%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", membsVar, readMethod)
	} else {
		linef(sb, depth, "for mv, params := range %s.%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", membsVar, readMethod)
	}
}

// writeCarrierValueRead emits the read of a single section value into valVar at
// the given depth — a defensive copy for []byte / fixed-byte, straight
// assignment otherwise.
func writeCarrierValueRead(sb *strings.Builder, depth int, f mappingplan.TaggedField, valVar, valRead string) {
	switch goplan.CopyStrategy(f.GoType()) {
	case goplan.CopyFixedByte:
		linef(sb, depth, "copy(%s[:], %s)", valVar, valRead)
	case goplan.CopyBytes:
		linef(sb, depth, "%s = append([]byte(nil), %s...)", valVar, valRead)
	default:
		linef(sb, depth, "%s = %s", valVar, valRead)
	}
}

// writeCarrierScalarDecode decodes a scalar carrier value: exactly one
// attribute per row, value + carrier into scalar columns.
func writeCarrierScalarDecode(sb *strings.Builder, f mappingplan.TaggedField, attrsVar, membsVar, prefix string) {
	valVar := prefix + f.GoFieldName + "Val"
	carrierVar := prefix + f.GoFieldName + "Carrier"
	readMethod := "GetMembValue" + f.Flags.Channel.CarrierReadMethodSuffix()
	valRead := fmt.Sprintf("%s.%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", attrsVar, goplan.SingleValueReadAccessor(f))

	linef(sb, 2, "var %s %s", valVar, f.GoType())
	linef(sb, 2, "var %s marshalltypes.%s", carrierVar, f.CarrierType)
	linef(sb, 2, "%sCount := 0", prefix)
	linef(sb, 2, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 2, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	writeCarrierMembLoopHeader(sb, 3, f, membsVar, readMethod)
	writeCarrierValueRead(sb, 4, f, valVar, valRead)
	linef(sb, 4, "%s = %s", carrierVar, carrierStructLiteral(f, carrierMembValExpr(f), "params"))
	linef(sb, 4, "%sCount++", prefix)
	line(sb, 3, "}")
	line(sb, 2, "}")
	linef(sb, 2, "if %sCount != 1 {", prefix)
	linef(sb, 3, "err = eb.Build().Int(\"row\", i).Str(\"field\", %q).Errorf(\"expected exactly one occurrence per row\")", f.GoFieldName)
	line(sb, 3, "return")
	line(sb, 2, "}")
	linef(sb, 2, "c.%s = append(c.%s, %s)", f.GoFieldName, f.GoFieldName, valVar)
	linef(sb, 2, "c.%s = append(c.%s, %s)", f.CarrierField, f.CarrierField, carrierVar)
}

// writeCarrierOptionDecode decodes an Option carrier value: 0 or 1 attribute.
// The carrier column gets one entry per row (zero when absent) to stay in
// lockstep with the Val / Has columns.
func writeCarrierOptionDecode(sb *strings.Builder, f mappingplan.TaggedField, attrsVar, membsVar, prefix string) {
	valVar := prefix + f.GoFieldName + "Val"
	carrierVar := prefix + f.GoFieldName + "Carrier"
	readMethod := "GetMembValue" + f.Flags.Channel.CarrierReadMethodSuffix()
	valRead := fmt.Sprintf("%s.%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", attrsVar, goplan.SingleValueReadAccessor(f))

	linef(sb, 2, "var %s %s", valVar, f.GoType())
	linef(sb, 2, "var %s marshalltypes.%s", carrierVar, f.CarrierType)
	linef(sb, 2, "%sCount := 0", prefix)
	linef(sb, 2, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 2, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	writeCarrierMembLoopHeader(sb, 3, f, membsVar, readMethod)
	writeCarrierValueRead(sb, 4, f, valVar, valRead)
	linef(sb, 4, "%s = %s", carrierVar, carrierStructLiteral(f, carrierMembValExpr(f), "params"))
	linef(sb, 4, "%sCount++", prefix)
	line(sb, 3, "}")
	line(sb, 2, "}")
	linef(sb, 2, "if %sCount == 1 {", prefix)
	linef(sb, 3, "c.%sVal = append(c.%sVal, %s)", f.GoFieldName, f.GoFieldName, valVar)
	linef(sb, 3, "c.%sHas = append(c.%sHas, true)", f.GoFieldName, f.GoFieldName)
	line(sb, 2, "} else {")
	linef(sb, 3, "var zero %s", f.GoType())
	linef(sb, 3, "c.%sVal = append(c.%sVal, zero)", f.GoFieldName, f.GoFieldName)
	linef(sb, 3, "c.%sHas = append(c.%sHas, false)", f.GoFieldName, f.GoFieldName)
	line(sb, 2, "}")
	linef(sb, 2, "c.%s = append(c.%s, %s)", f.CarrierField, f.CarrierField, carrierVar)
}

// writeCarrierContainerDecode decodes a container ([]T) carrier value: one
// attribute carrying N values, paired with a single scalar carrier. An empty
// container produces no attribute (splice) — the row gets an empty slice and a
// zero carrier.
func writeCarrierContainerDecode(sb *strings.Builder, f mappingplan.TaggedField, attrsVar, membsVar, prefix string) {
	sliceVar := prefix + f.GoFieldName + "Slice"
	carrierVar := prefix + f.GoFieldName + "Carrier"
	readMethod := "GetMembValue" + f.Flags.Channel.CarrierReadMethodSuffix()

	linef(sb, 2, "var %s []%s", sliceVar, f.GoType())
	linef(sb, 2, "var %s marshalltypes.%s", carrierVar, f.CarrierType)
	linef(sb, 2, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 2, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	writeCarrierMembLoopHeader(sb, 3, f, membsVar, readMethod)
	linef(sb, 4, "%s = %s", carrierVar, carrierStructLiteral(f, carrierMembValExpr(f), "params"))
	line(sb, 3, "}")
	linef(sb, 3, "for v := range %s.GetAttrValueValue(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", attrsVar)
	if goplan.CopyStrategy(f.GoType()) == goplan.CopyBytes {
		line(sb, 4, "cp := make([]byte, len(v))")
		line(sb, 4, "copy(cp, v)")
		linef(sb, 4, "%s = append(%s, cp)", sliceVar, sliceVar)
	} else {
		linef(sb, 4, "%s = append(%s, v)", sliceVar, sliceVar)
	}
	line(sb, 3, "}")
	line(sb, 2, "}")
	linef(sb, 2, "c.%s = append(c.%s, %s)", f.GoFieldName, f.GoFieldName, sliceVar)
	linef(sb, 2, "c.%s = append(c.%s, %s)", f.CarrierField, f.CarrierField, carrierVar)
}

func writeFieldAccumulatorDecl(sb *strings.Builder, f mappingplan.TaggedField, prefix string) {
	switch {
	case f.IsSlice():
		linef(sb, 2, "var %s%sSlice []%s", prefix, f.GoFieldName, f.GoType())
	case f.IsRoaring():
		linef(sb, 2, "var %s%sBitmap *roaring.Bitmap", prefix, f.GoFieldName)
	default:
		// Scalar or Option value: a Val slot plus an occurrence Count
		// (Option's Has bool is written at append time, not here).
		linef(sb, 2, "var %s%sVal %s", prefix, f.GoFieldName, f.GoType())
		linef(sb, 2, "var %s%sCount int", prefix, f.GoFieldName)
	}
}

func writeFieldAppend(sb *strings.Builder, f mappingplan.TaggedField, prefix string) {
	switch {
	case f.IsOption:
		linef(sb, 2, "if %s%sCount == 1 {", prefix, f.GoFieldName)
		linef(sb, 3, "c.%sVal = append(c.%sVal, %s%sVal)", f.GoFieldName, f.GoFieldName, prefix, f.GoFieldName)
		linef(sb, 3, "c.%sHas = append(c.%sHas, true)", f.GoFieldName, f.GoFieldName)
		line(sb, 2, "} else {")
		linef(sb, 3, "var zero %s", f.GoType())
		linef(sb, 3, "c.%sVal = append(c.%sVal, zero)", f.GoFieldName, f.GoFieldName)
		linef(sb, 3, "c.%sHas = append(c.%sHas, false)", f.GoFieldName, f.GoFieldName)
		line(sb, 2, "}")
	case f.IsSlice():
		linef(sb, 2, "c.%s = append(c.%s, %s%sSlice)", f.GoFieldName, f.GoFieldName, prefix, f.GoFieldName)
	case f.IsRoaring():
		linef(sb, 2, "c.%s = append(c.%s, %s%sBitmap)", f.GoFieldName, f.GoFieldName, prefix, f.GoFieldName)
	default:
		linef(sb, 2, "if %s%sCount != 1 {", prefix, f.GoFieldName)
		linef(sb, 3, "err = eb.Build().Int(\"row\", i).Str(\"field\", %q).Errorf(\"expected exactly one occurrence per row\")", f.GoFieldName)
		line(sb, 3, "return\n\t\t}")
		linef(sb, 2, "c.%s = append(c.%s, %s%sVal)", f.GoFieldName, f.GoFieldName, prefix, f.GoFieldName)
	}
}

func writeFieldMembCase(sb *strings.Builder, f mappingplan.TaggedField, prefix, attrsVar string) {
	if f.Flags.Channel.EmbedsLiteralName() {
		linef(sb, 4, "case %q:", f.LWMembership)
	} else {
		linef(sb, 4, "case %s:", f.KindVar())
	}
	// Single-value read accessor chosen by field shape, shared with the
	// reflect codec via goplan.SingleValueReadAccessor so the two
	// front-ends cannot pick different accessors for the same shape.
	singleVal := func() string {
		return fmt.Sprintf("%s.%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", attrsVar, goplan.SingleValueReadAccessor(f))
	}
	switch {
	case f.IsSlice():
		linef(sb, 5, "for v := range %s.GetAttrValueValue(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", attrsVar)
		if goplan.CopyStrategy(f.GoType()) == goplan.CopyBytes {
			line(sb, 6, "cp := make([]byte, len(v))")
			line(sb, 6, "copy(cp, v)")
			linef(sb, 6, "%s%sSlice = append(%s%sSlice, cp)", prefix, f.GoFieldName, prefix, f.GoFieldName)
		} else {
			linef(sb, 6, "%s%sSlice = append(%s%sSlice, v)", prefix, f.GoFieldName, prefix, f.GoFieldName)
		}
		line(sb, 5, "}")
	case f.IsRoaring():
		linef(sb, 5, "for v := range %s.GetAttrValueValue(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", attrsVar)
		linef(sb, 6, "if %s%sBitmap == nil {", prefix, f.GoFieldName)
		linef(sb, 7, "%s%sBitmap = roaring.New()", prefix, f.GoFieldName)
		line(sb, 6, "}")
		linef(sb, 6, "%s%sBitmap.Add(v)", prefix, f.GoFieldName)
		line(sb, 5, "}")
	default:
		// Scalar or Option value. Option diverges from a bare scalar only at
		// append time (writeFieldAppend writes the Has bool); the
		// per-attribute fill into <prefix><Field>Val / Count is identical, so
		// both shapes share this arm.
		linef(sb, 5, "val := %s", singleVal())
		switch goplan.CopyStrategy(f.GoType()) {
		case goplan.CopyFixedByte:
			linef(sb, 5, "copy(%s%sVal[:], val)", prefix, f.GoFieldName)
		case goplan.CopyBytes:
			line(sb, 5, "cp := make([]byte, len(val))")
			line(sb, 5, "copy(cp, val)")
			linef(sb, 5, "%s%sVal = cp", prefix, f.GoFieldName)
		default:
			linef(sb, 5, "%s%sVal = val", prefix, f.GoFieldName)
		}
		linef(sb, 5, "%s%sCount++", prefix, f.GoFieldName)
	}
}

func writeMultiSubColumnDecode(sb *strings.Builder, g goplan.SectionGroup, attrsVar, membsVar, prefix string) (err error) {
	subs, memb, err := writeMultiSubMatchLoops(sb, g, attrsVar, membsVar, prefix)
	if err != nil {
		return
	}
	if len(g.ScalarSubColumns()) > 0 {
		linef(sb, 2, "if %s%sCount != 1 {", prefix, memb.GoFieldName)
		linef(sb, 3, "err = eb.Build().Int(\"row\", i).Str(\"membership\", %q).Errorf(\"expected exactly one occurrence per row\")", memb.LWMembership)
	} else {
		// S = 0 tuple: a spliced row (every container empty on the write
		// side) carries no attribute and decodes to nil slices — mirror the
		// lone-container tolerance (ADR-0101 D2/D5).
		linef(sb, 2, "if %s%sCount > 1 {", prefix, memb.GoFieldName)
		linef(sb, 3, "err = eb.Build().Int(\"row\", i).Str(\"membership\", %q).Errorf(\"occurs more than once on the row\")", memb.LWMembership)
	}
	line(sb, 3, "return\n\t\t}")
	for _, s := range subs {
		linef(sb, 2, "c.%s = append(c.%s, %s%sVal)", s.Field.GoFieldName, s.Field.GoFieldName, prefix, s.Field.GoFieldName)
	}
	return
}

// multiSub is one sub-column of a multi-sub-column section during decode
// emission.
type multiSub struct {
	Field   mappingplan.TaggedField
	ColName string
}

// writeMultiSubMatchLoops emits the shared middle of a multi-sub-column
// section decode: per-sub value accumulators, the attribute loop reading
// every sub-column accessor and the single-membership match filling them
// plus a `<prefix><Memb>Count` occurrence counter. FillFromArrow and
// ReadRow attach their own tails.
func writeMultiSubMatchLoops(sb *strings.Builder, g goplan.SectionGroup, attrsVar, membsVar, prefix string) (subs []multiSub, memb mappingplan.TaggedField, err error) {
	if len(g.Memberships) != 1 {
		err = eb.Build().Str("section", g.Section).Errorf("multi-sub-column section with multiple memberships not supported on read side")
		return
	}
	for _, sc := range g.SubColumns {
		subs = append(subs, multiSub{Field: sc.Fields[0], ColName: sc.Name})
	}
	memb = g.Memberships[0]
	for _, s := range subs {
		if s.Field.IsSlice() {
			linef(sb, 2, "var %s%sVal []%s", prefix, s.Field.GoFieldName, s.Field.GoType())
		} else {
			linef(sb, 2, "var %s%sVal %s", prefix, s.Field.GoFieldName, s.Field.GoType())
		}
	}
	linef(sb, 2, "var %s%sCount int", prefix, memb.GoFieldName)
	linef(sb, 2, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 2, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	for _, s := range subs {
		if !s.Field.IsSlice() {
			linef(sb, 3, "%s%sLocal := %s.GetAttrValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", prefix, s.Field.GoFieldName, attrsVar, mappingplan.UpperFirst(s.ColName))
			continue
		}
		// Container sub-column: drain the per-attribute Seq into a fresh
		// slice (nil for an empty container — an N=0 attribute reads back
		// as a nil slice, ADR-0101 D5). []byte elements are copied out of
		// the Arrow buffer like the lone-container fill path.
		linef(sb, 3, "var %s%sLocal []%s", prefix, s.Field.GoFieldName, s.Field.GoType())
		linef(sb, 3, "for v := range %s.GetAttrValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", attrsVar, mappingplan.UpperFirst(s.ColName))
		switch goplan.CopyStrategy(s.Field.GoType()) {
		case goplan.CopyBytes:
			line(sb, 4, "cp := make([]byte, len(v))")
			line(sb, 4, "copy(cp, v)")
			linef(sb, 4, "%s%sLocal = append(%s%sLocal, cp)", prefix, s.Field.GoFieldName, prefix, s.Field.GoFieldName)
		default:
			linef(sb, 4, "%s%sLocal = append(%s%sLocal, v)", prefix, s.Field.GoFieldName, prefix, s.Field.GoFieldName)
		}
		line(sb, 3, "}")
	}
	if memb.Flags.Channel.EmbedsLiteralName() {
		linef(sb, 3, "for membBytes := range %s.GetMembValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", membsVar, memb.Flags.Channel.AddMethodSuffix())
		linef(sb, 4, "if string(membBytes) == %q {", memb.LWMembership)
	} else {
		linef(sb, 3, "for membID := range %s.GetMembValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", membsVar, memb.Flags.Channel.AddMethodSuffix())
		linef(sb, 4, "if membID == %s {", memb.KindVar())
	}
	for _, s := range subs {
		linef(sb, 5, "%s%sVal = %s%sLocal", prefix, s.Field.GoFieldName, prefix, s.Field.GoFieldName)
	}
	linef(sb, 5, "%s%sCount++", prefix, memb.GoFieldName)
	line(sb, 4, "}")
	line(sb, 3, "}")
	line(sb, 2, "}")
	return
}
