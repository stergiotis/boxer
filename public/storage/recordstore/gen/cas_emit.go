package gen

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
)

// casField is one field of a component a compare-and-set can rewrite in
// place: a scalar on a section of its own, whose one attribute under one
// static membership every row carries, so the value column's single
// element is the whole of what an update changes.
type casField struct {
	goField  string // DTO field name, e.g. "Version"
	goType   string // its Go type, e.g. "uint32"
	physical string // the section's value column, e.g. tv:runVersion:value:…
}

// casComponent is a component's in-place-rewritable fields, in DTO order.
type casComponent struct {
	kind   string
	fields []casField
}

// casLiteral is the Go expression rendering v of goType as a ClickHouse
// literal, or "" for a type the compare-and-set helpers do not render.
func casLiteral(goType string, v string) string {
	switch goType {
	case "string":
		return "marshalling.EscapeString(" + v + ")"
	case "uint8", "uint16", "uint32", "uint64":
		return "strconv.FormatUint(uint64(" + v + "), 10)"
	case "int8", "int16", "int32", "int64":
		return "strconv.FormatInt(int64(" + v + "), 10)"
	case "bool":
		return "strconv.FormatBool(" + v + ")"
	case "time.Time":
		return "\"fromUnixTimestamp64Nano(\" + strconv.FormatInt(" + v + ".UnixNano(), 10) + \")\""
	}
	return ""
}

// casComponents classifies every component for Input.Cas: each field must
// be rewritable in place, and no section may carry two components' fields —
// an update that set a shared section's value column would overwrite the
// other component's value too. Anything else is refused at generation, so a
// Cas store either rewrites a row faithfully or does not exist.
func (inst Input) casComponents(comps []storeComponent, info *readback.InformationRetrieval) (r []casComponent, err error) {
	valueCol := map[string]string{}
	for cr := range info.IterateAll() {
		if cr.ColumnContext.SectionName != "" && cr.Role == common.ColumnRoleValue && cr.Name == "value" {
			valueCol[string(cr.ColumnContext.SectionName)] = cr.PhysicalColumn.String()
		}
	}
	sectionOwner := map[string]string{}
	for _, c := range comps {
		cc := casComponent{kind: c.Kind}
		for _, g := range c.groups {
			if owner, taken := sectionOwner[g.Section]; taken {
				err = eb.Build().Str("component", c.Kind).Str("otherComponent", owner).Str("section", g.Section).Errorf("Cas: two components share a section — an in-place update of one would overwrite the other's value")
				return
			}
			sectionOwner[g.Section] = c.Kind
			if len(g.SubColumns) != 1 || g.SubColumns[0].Name != "value" || len(g.SubColumns[0].Fields) != 1 {
				err = eb.Build().Str("component", c.Kind).Str("section", g.Section).Errorf("Cas: a section must carry exactly one field in its value column to be rewritten in place")
				return
			}
			f := g.SubColumns[0].Fields[0]
			if err = casFieldOk(c.Kind, g.Section, f); err != nil {
				return
			}
			phys, ok := valueCol[g.Section]
			if !ok {
				err = eb.Build().Str("component", c.Kind).Str("section", g.Section).Errorf("Cas: no value column found for the section")
				return
			}
			cc.fields = append(cc.fields, casField{goField: f.GoFieldName, goType: f.GoType(), physical: phys})
		}
		r = append(r, cc)
	}
	return
}

func casFieldOk(kind, section string, f mappingplan.TaggedField) (err error) {
	switch {
	case f.IsConst, f.IsOption, f.TupleField != "", f.IsSlice(), f.IsRoaring():
		err = eb.Build().Str("component", kind).Str("section", section).Str("field", f.GoFieldName).Errorf("Cas: only plain scalar fields are rewritten in place — not const, optional, tuple or collection fields")
	case f.Flags.Channel != mappingplan.MembershipChannelLowCardRef && f.Flags.Channel != mappingplan.MembershipChannelLowCardVerbatim:
		err = eb.Build().Str("component", kind).Str("section", section).Str("field", f.GoFieldName).Errorf("Cas: only a static low-cardinality ref or verbatim membership is rewritten in place — the membership columns are not updated")
	case casLiteral(f.GoType(), "v") == "":
		err = eb.Build().Str("component", kind).Str("section", section).Str("field", f.GoFieldName).Str("goType", f.GoType()).Errorf("Cas: no SQL literal rendering for this field type")
	}
	return
}

// casNeedsMarshalling reports whether a helper renders a string literal.
func casNeedsMarshalling(cs []casComponent) bool {
	for _, c := range cs {
		for _, f := range c.fields {
			if f.goType == "string" {
				return true
			}
		}
	}
	return false
}

// emitCas writes the compare-and-set surface (ADR-0223 §SD3, through
// rowcas): the store's UpdateIf, and per component the value-column
// constants, typed setters and guards, and the whole-row set list.
func (inst emitter) emitCas(sb *strings.Builder, cs []casComponent) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// UpdateIf is the compare-and-set on key's row (ADR-0223 §SD3): one")
	p("// lightweight update, guarded by where on the row's current values, that")
	p("// applies sets. It does not report whether it won — the caller reads the")
	p("// row back and decides, since the read-back is the answer. where is SQL")
	p("// over the physical columns, trusted input only (the <Kind>Guard<Field>")
	p("// helpers render it); an empty one is refused, since an unguarded update")
	p("// is not a compare-and-set. A key with buffered, unflushed rows is")
	p("// refused too: the update sees only flushed rows. Attached cache views")
	p("// drop the key, so the next read refetches what the update left.")
	p("//")
	p("// The table lives under rowcas's conditions: the sequential update mode")
	p("// on every statement, the block-position settings (EnsureTable applies")
	p("// them), no heavyweight mutation ever, an immutable key, no async_insert")
	p("// on the claim path, and one server (ADR-0319 §SD1).")
	p("func (inst *%s) UpdateIf(ctx context.Context, key %s, where string, sets ...rowcas.Set) (err error) {", inst.storeType(), inst.keyGoType)
	p("\tif strings.TrimSpace(where) == \"\" {")
	p("\t\treturn eh.Errorf(\"compare-and-set without a guard\")")
	p("\t}")
	p("\tif len(sets) == 0 {")
	p("\t\treturn eh.Errorf(\"compare-and-set with nothing to set\")")
	p("\t}")
	p("\tif _, d := inst.dirty[key]; d {")
	p("\t\treturn eh.Errorf(\"compare-and-set on a key with buffered rows; Flush first\")")
	p("\t}")
	p("\tsql := rowcas.UpdateSQL(inst.writeTable(), sets, %sColKey+\" = \"+%sKeyLiteral(key)+\" AND (\"+where+\")\")", inst.StoreName, inst.TableName)
	p("\terr = inst.exec.Exec(ctx, sql)")
	p("\tinst.notifyWrite(key, nil)")
	p("\tif err != nil {")
	p("\t\terr = eh.Errorf(\"compare-and-set on %%s: %%w\", inst.writeTable(), err)")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	for _, c := range cs {
		p("// The physical value columns of %s's fields: each section holds one", c.kind)
		p("// attribute, so element [1] is the field's value (Input.Cas).")
		p("const (")
		for _, f := range c.fields {
			p("\t%sValueCol%s = `\"%s\"`", c.kind, f.goField, f.physical)
		}
		p(")")
		p("")
		for _, f := range c.fields {
			p("// %sSet%s rewrites %s.%s in place, for UpdateIf.", c.kind, f.goField, c.kind, f.goField)
			p("func %sSet%s(v %s) rowcas.Set {", c.kind, f.goField, f.goType)
			p("\treturn rowcas.Set{Column: %sValueCol%s, Expr: \"[\" + %s + \"]\"}", c.kind, f.goField, casLiteral(f.goType, "v"))
			p("}")
			p("")
			p("// %sGuard%s is the guard \"%s.%s equals v\", for UpdateIf.", c.kind, f.goField, c.kind, f.goField)
			p("func %sGuard%s(v %s) string {", c.kind, f.goField, f.goType)
			p("\treturn %sValueCol%s + \"[1] = \" + %s", c.kind, f.goField, casLiteral(f.goType, "v"))
			p("}")
			p("")
		}
		p("// %sSets rewrites every field of a %s in place: a whole-row update.", c.kind, c.kind)
		p("func %sSets(row %s) []rowcas.Set {", c.kind, c.kind)
		p("\treturn []rowcas.Set{")
		for _, f := range c.fields {
			p("\t\t%sSet%s(row.%s),", c.kind, f.goField, f.goField)
		}
		p("\t}")
		p("}")
		p("")
	}
}
