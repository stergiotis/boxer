package marshallgen

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

// The write side: the derived per-section AttrI / SecI + <Kind>EntityI
// constraint interfaces, and the <Kind>BuildEntities / <Kind>AddSections
// drivers that walk them.

func writeBuildHelper(sb *strings.Builder, plan *mappingplan.Plan) (err error) {
	groups := goplan.ComputeGroups(plan)

	line(sb, 0, "// --- Composed-interface BuildEntities helper (schema-agnostic). ---\n//")
	linef(sb, 0, "// %sBuildEntities walks the SoA columns and emits one entity per", plan.KindType)
	line(sb, 0, "// row through dml.BeginEntity / per-section BeginAttribute* /")
	line(sb, 0, "// AddMembershipLowCardRefP / AddToContainerP / EndAttributeP /")
	line(sb, 0, "// EndSection / CommitEntity. dml is generic — any leeway-DML class")
	line(sb, 0, "// whose method shapes satisfy the derived interfaces qualifies;")
	line(sb, 0, "// Go's type inference binds the type parameters at the call site.\n//")
	line(sb, 0, "// Callers drain via dml.TransferRecords (or schema-specific")
	line(sb, 0, "// equivalents) — left outside the helper because the record type")
	line(sb, 0, "// varies by target.\n")

	for _, g := range groups {
		err = writeSectionInterfaces(sb, plan, g, EmitModeCodec)
		if err != nil {
			return
		}
	}
	err = writeEntityInterface(sb, plan, groups, EmitModeCodec)
	if err != nil {
		return
	}
	err = writeBuildEntitiesFunc(sb, plan, groups)
	if err != nil {
		return
	}
	err = writeAddSectionsFunc(sb, plan, groups, EmitModeCodec)
	return
}

// elemType reports the per-attribute / per-element argument type the
// section's value column accepts. For `*roaring.Bitmap` the wire
// element is uint32; for `[N]byte` re-sliced into a blob the element
// is `[]byte`. For scalar fixed-width arrays kept as-is, the type is
// the array literal (the DML expects `[N]byte` directly only on plain
// columns — tagged-value fixed-width blobs always re-slice into
// `[]byte`).
func elemType(f mappingplan.TaggedField) string {
	if f.IsRoaring() {
		return "uint32"
	}
	if goplan.IsFixedByteArray(f.GoType()) {
		return "[]byte"
	}
	return f.GoType()
}

// writeSectionInterfaces emits the per-section AttrI + SecI interface
// pair for one section group. AttrI lists only the methods this DTO's
// fields use (membership P op, container-append P op, EndAttribute P
// op); SecI lists the BeginAttribute method shapes any field needs
// plus EndSection.
//
// All AttrI methods are P-variants (void return) — no F-bounded
// `[Self]` parameter is needed. SecI keeps `[Attr, Ent]` parameters
// because BeginAttribute* still return Attr (caller needs the handle)
// and EndSection returns Ent for the chain back to the entity.
func writeSectionInterfaces(sb *strings.Builder, plan *mappingplan.Plan, g goplan.SectionGroup, mode EmitModeE) (err error) {
	kind := kindIdent(plan.KindType, mode)
	method := methodFor(g.Section)

	// Survey which Begin* shapes and container-append the SecI / AttrI
	// must expose, based on the union of field shapes in this group.
	needContainerOpen := false // shape: sec.BeginAttribute() (no args, opens container)
	needBeginSingleVal := ""   // shape: sec.BeginAttributeSingle(value T) — element type
	needBeginScalarVal := ""   // shape: sec.BeginAttribute(value T) — element type
	needAddToContainer := ""   // attr.AddToContainerP(v T) — element type (single-sub-column container)
	multiSubColAttr := false   // multi-sub-column — BeginAttribute(<scalars…>) + zipped co-containers (ADR-0101)
	multiSubColScalars := []argPair{}
	multiSubColContainers := []argPair{}

	// A dynamic-membership tuple section (ADR-0103) drives the same
	// per-attribute call shape — BeginAttribute(<scalars…>) + zipped
	// co-containers — at ANY sub-column count, so it routes through the
	// multi-sub-column survey even with a single sub-column.
	_, isTuple := g.TupleSpec()
	if isTuple || len(g.SubColumns) > 1 {
		multiSubColAttr = true
		for _, sc := range g.SubColumns {
			// Backstops only — PlanBuilder.Finish rejects these shapes for
			// both front-ends (ADR-0101 D3); hand-built plans still hit them.
			if len(sc.Fields) != 1 {
				err = eb.Build().Str("section", g.Section).Str("column", sc.Name).Errorf("multi-field sub-column in multi-sub-column section not supported")
				return
			}
			f := sc.Fields[0]
			if f.IsOption || f.IsRoaring() {
				err = eb.Build().Str("section", g.Section).Str("field", f.GoFieldName).Errorf("Option / roaring field in multi-sub-column section not supported")
				return
			}
		}
		// Scalar class → BeginAttribute arguments; container class →
		// AddToContainerP / AddToCoContainersP arguments (declaration order
		// within each class, matching the DML generator's per-class rule).
		for _, sc := range g.ScalarSubColumns() {
			multiSubColScalars = append(multiSubColScalars, argPair{Name: sc.Name, Type: sc.Fields[0].GoType()})
		}
		for _, sc := range g.ContainerSubColumns() {
			multiSubColContainers = append(multiSubColContainers, argPair{Name: sc.Name, Type: elemType(sc.Fields[0])})
		}
	} else {
		for _, f := range g.SubColumns[0].Fields {
			switch goplan.ClassifyBegin(f) {
			case goplan.ShapeScalarBegin:
				needBeginScalarVal = elemType(f)
			case goplan.ShapeScalarBeginSingle:
				needBeginSingleVal = elemType(f)
			case goplan.ShapeContainer:
				needContainerOpen = true
				needAddToContainer = elemType(f)
			}
		}
	}

	// --- AttrI. ---
	linef(sb, 0, "// %s%sAttrI is the InAttr-side view of the %s section. P-variants only —", kind, method, g.Section)
	line(sb, 0, "// every method returns void so no F-bounded `[Self]` parameter is")
	line(sb, 0, "// needed.")
	linef(sb, 0, "type %s%sAttrI interface {", kind, method)
	if ts, ok := g.TupleSpec(); ok && len(ts.Memberships) > 0 {
		// A DYNAMIC tuple element may carry memberships on several channels
		// (ADR-0109 D4); the AttrI embeds one InAttributeMembership<Channel>PI
		// per channel.
		for _, ch := range ts.Channels() {
			linef(sb, 1, "dmlruntime.InAttributeMembership%sPI", ch.AddMethodSuffix())
		}
	} else {
		// A plain section OR a STATIC nested section carries one section
		// membership (g.Channel()); the static tuple resolves it exactly like a
		// flat section.
		linef(sb, 1, "dmlruntime.InAttributeMembership%sPI", g.Channel().AddMethodSuffix())
	}
	if needAddToContainer != "" {
		linef(sb, 1, "AddToContainerP(value %s)", needAddToContainer)
	}
	if multiSubColAttr && len(multiSubColContainers) > 0 {
		argDecls := make([]string, 0, len(multiSubColContainers))
		for _, p := range multiSubColContainers {
			argDecls = append(argDecls, fmt.Sprintf("%s %s", p.Name, p.Type))
		}
		linef(sb, 1, "%sP(%s)", goplan.ContainerAddMethod(len(multiSubColContainers)), strings.Join(argDecls, ", "))
	}
	line(sb, 1, "EndAttributeP()")
	line(sb, 0, "}\n")

	// --- SecI. ---
	linef(sb, 0, "// %s%sSecI is the Section-side view: opens an attribute and closes", kind, method)
	line(sb, 0, "// the section. Attr and Ent are bound at the call site by inference.")
	linef(sb, 0, "type %s%sSecI[Attr any, Ent any] interface {", kind, method)
	switch {
	case multiSubColAttr:
		argDecls := make([]string, 0, len(multiSubColScalars))
		for _, p := range multiSubColScalars {
			argDecls = append(argDecls, fmt.Sprintf("%s %s", p.Name, p.Type))
		}
		linef(sb, 1, "BeginAttribute(%s) Attr", strings.Join(argDecls, ", "))
	default:
		if needContainerOpen {
			line(sb, 1, "BeginAttribute() Attr")
		}
		if needBeginScalarVal != "" && !needContainerOpen {
			linef(sb, 1, "BeginAttribute(value %s) Attr", needBeginScalarVal)
		} else if needBeginScalarVal != "" {
			// container path + per-element scalar Begin both exist on the
			// same section: would require Go overloading; not supported.
			err = eb.Build().Str("section", g.Section).Errorf("section mixes container and scalar field shapes — BeginAttribute cannot both open a container (0 args) and take a value (1 arg); give the fields separate sections, or make them all containers")
			return
		}
		if needBeginSingleVal != "" {
			linef(sb, 1, "BeginAttributeSingle(value %s) Attr", needBeginSingleVal)
		}
	}
	line(sb, 1, "EndSection() Ent")
	line(sb, 0, "}\n")
	return
}

// writeEntityInterface emits the entity-level interface — section
// getters + entity-lifecycle methods. Per-section: one Attr + one Sec
// type parameter (no F-bounded recursion on Attr). Ent stays on the
// EntityI (BeginEntity / SetId / SetTimestamp / SetLifecycle return
// it; CommitEntity returns error).
func writeEntityInterface(sb *strings.Builder, plan *mappingplan.Plan, groups []goplan.SectionGroup, mode EmitModeE) (err error) {
	kind := kindIdent(plan.KindType, mode)

	linef(sb, 0, "// %sEntityI is the entity-builder surface %sAddSections drives.", kind, kind)
	line(sb, 0, "// It always lists the per-section getters; the entity-frame methods")
	line(sb, 0, "// (BeginEntity / plain setters / CommitEntity) are added only for the")
	line(sb, 0, "// full codec's BuildEntities. AddSections stacks sections onto a frame")
	line(sb, 0, "// the caller already owns, so it needs none of them — which lets a")
	line(sb, 0, "// store drive it with a builder whose frame control is unexported")
	line(sb, 0, "// (ADR-0100 SD6). Ent is the builder pointer.")
	linef(sb, 0, "type %sEntityI[", kind)
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "%sAttr %s%sAttrI,", method, kind, method)
		linef(sb, 1, "%sSec %s%sSecI[%sAttr, Ent],", method, kind, method, method)
	}
	line(sb, 1, "Ent any,")
	line(sb, 0, "] interface {")

	idCol := planIdCol(plan)
	if idCol == nil {
		err = eb.Build().Errorf("plain spec missing required `id` column")
		return
	}
	// Frame-lifecycle methods are what BuildEntities drives; AddSections
	// never calls them, so the store-support product omits them and its
	// constraint stays satisfiable by a builder with unexported control.
	if mode != EmitModeStoreSupport {
		line(sb, 1, "BeginEntity() Ent")
		if nkCol := planNaturalKeyCol(plan); nkCol != nil {
			linef(sb, 1, "SetId(id %s, naturalKey %s) Ent", idCol.GoType(), nkCol.GoType())
		} else {
			linef(sb, 1, "SetId(id %s) Ent", idCol.GoType())
		}
		if tsCol := planTsCol(plan); tsCol != nil {
			linef(sb, 1, "SetTimestamp(ts %s) Ent", tsCol.GoType())
		}
		if lcCol := planExpiresAtCol(plan); lcCol != nil {
			linef(sb, 1, "SetLifecycle(expiresAt %s) Ent", lcCol.GoType())
		}
	}
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "GetSection%s() %sSec", method, method)
	}
	if mode != EmitModeStoreSupport {
		line(sb, 1, "CommitEntity() (err error)")
	}
	line(sb, 0, "}\n")
	return
}

// writeBuildEntitiesFunc emits the generic function that loops the
// SoA columns and drives the entity-builder calls. Schema-free — every
// call routes through the derived interfaces; shape-driven per-field
// dispatch picks the BeginAttribute / BeginAttributeSingle / container
// pattern (goplan.ClassifyBegin).
func writeBuildEntitiesFunc(sb *strings.Builder, plan *mappingplan.Plan, groups []goplan.SectionGroup) (err error) {
	kind := plan.KindType

	linef(sb, 0, "// %sBuildEntities walks c row-by-row, drives dml's entity / section", kind)
	line(sb, 0, "// chain, and returns once every row has been committed. The dml")
	line(sb, 0, "// argument's concrete type binds every type parameter via Go's")
	line(sb, 0, "// type inference at the call site.")
	linef(sb, 0, "func %sBuildEntities[", kind)
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "%sAttr %s%sAttrI,", method, kind, method)
		linef(sb, 1, "%sSec %s%sSecI[%sAttr, Ent],", method, kind, method, method)
	}
	line(sb, 1, "Ent any,")
	linef(sb, 1, "DML %sEntityI[", kind)
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 2, "%sAttr, %sSec,", method, method)
	}
	line(sb, 2, "Ent,")
	line(sb, 1, "],")
	linef(sb, 0, "](dml DML, c *%sColumns) (err error) {", kind)
	line(sb, 1, "n := c.Len()")
	line(sb, 1, "for i := 0; i < n; i++ {")

	idCol := planIdCol(plan)
	nkCol := planNaturalKeyCol(plan)
	tsCol := planTsCol(plan)
	lcCol := planExpiresAtCol(plan)
	if idCol == nil {
		err = eb.Build().Errorf("plain spec missing required `id` column")
		return
	}
	line(sb, 2, "dml.BeginEntity()")
	if nkCol != nil {
		linef(sb, 2, "dml.SetId(c.%s[i], c.%s[i])", idCol.GoField, nkCol.GoField)
	} else {
		linef(sb, 2, "dml.SetId(c.%s[i])", idCol.GoField)
	}
	if tsCol != nil {
		linef(sb, 2, "dml.SetTimestamp(c.%s[i])", tsCol.GoField)
	}
	if lcCol != nil {
		linef(sb, 2, "dml.SetLifecycle(c.%s[i])", lcCol.GoField)
	}

	for _, g := range groups {
		err = writeSectionDriver(sb, g, soaValueSrc())
		if err != nil {
			return
		}
	}

	line(sb, 2, "err = dml.CommitEntity()")
	line(sb, 2, "if err != nil {")
	line(sb, 3, "err = eh.Errorf(\"commit row %d: %w\", i, err)")
	line(sb, 3, "return")
	line(sb, 2, "}")
	line(sb, 1, "}")
	line(sb, 1, "return")
	line(sb, 0, "}\n")
	return
}

// writeAddSectionsFunc emits the entity-frame-free variant of
// BuildEntities (ADR-0100 SD6): the same section drivers over one row
// value, without BeginEntity / plain setters / CommitEntity. A composer
// that owns the entity frame (e.g. a recordstore builder assembling one
// entity from several components) calls it between BeginEntity and
// CommitEntity; sections from several kinds stack on one row the way
// marshallreflect's RowComposer stacks DTOs (ADR-0070).
func writeAddSectionsFunc(sb *strings.Builder, plan *mappingplan.Plan, groups []goplan.SectionGroup, mode EmitModeE) (err error) {
	kind := kindIdent(plan.KindType, mode)

	linef(sb, 0, "// %sAddSections contributes this kind's tagged sections to the OPEN", kind)
	line(sb, 0, "// entity on dml — the BuildEntities body without the entity frame.")
	line(sb, 0, "// The caller owns BeginEntity / plain setters / CommitEntity.")
	linef(sb, 0, "func %sAddSections[", kind)
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "%sAttr %s%sAttrI,", method, kind, method)
		linef(sb, 1, "%sSec %s%sSecI[%sAttr, Ent],", method, kind, method, method)
	}
	line(sb, 1, "Ent any,")
	linef(sb, 1, "DML %sEntityI[", kind)
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 2, "%sAttr, %sSec,", method, method)
	}
	line(sb, 2, "Ent,")
	line(sb, 1, "],")
	linef(sb, 0, "](dml DML, row %s) (err error) {", plan.KindType)

	for _, g := range groups {
		err = writeSectionDriver(sb, g, rowValueSrc())
		if err != nil {
			return
		}
	}

	line(sb, 1, "return")
	line(sb, 0, "}\n")
	return
}

// valueSrc renders access to a kind's field values in emitted driver code.
// BuildEntities reads the SoA columns at row i (`c.X[i]`, options split
// into `c.XVal[i]` / `c.XHas[i]`); AddSections reads a single row value
// (`row.X`, options nested as `row.X.Val` / `row.X.Has`).
type valueSrc struct {
	field     func(goField string) string
	optionVal func(goField string) string
	optionHas func(goField string) string
	// rowErrCtx is the eb context fragment naming the row in error
	// messages — `.Int("row", i)` for the SoA loop, empty for row shape.
	rowErrCtx string
}

func soaValueSrc() valueSrc {
	return valueSrc{
		field:     func(goField string) string { return "c." + goField + "[i]" },
		optionVal: func(goField string) string { return "c." + goField + "Val[i]" },
		optionHas: func(goField string) string { return "c." + goField + "Has[i]" },
		rowErrCtx: `.Int("row", i)`,
	}
}

func rowValueSrc() valueSrc {
	return valueSrc{
		field:     func(goField string) string { return "row." + goField },
		optionVal: func(goField string) string { return "row." + goField + ".Val" },
		optionHas: func(goField string) string { return "row." + goField + ".Has" },
	}
}

func writeSectionDriver(sb *strings.Builder, g goplan.SectionGroup, src valueSrc) (err error) {
	method := methodFor(g.Section)
	secVar := lowerFirst(method) + "Sec"
	linef(sb, 2, "// --- %s. ---", g.Section)
	linef(sb, 2, "%s := dml.GetSection%s()", secVar, method)

	if ts, ok := g.TupleSpec(); ok {
		writeTupleSectionDriver(sb, g, ts, secVar, src)
		linef(sb, 2, "%s.EndSection()", secVar)
		return
	}
	if len(g.SubColumns) > 1 {
		err = writeMultiSubColumnDriver(sb, g, secVar, src)
		if err != nil {
			return
		}
		linef(sb, 2, "%s.EndSection()", secVar)
		return
	}
	for _, f := range g.SubColumns[0].Fields {
		err = writeFieldDriver(sb, f, secVar, src)
		if err != nil {
			return
		}
	}
	linef(sb, 2, "%s.EndSection()", secVar)
	return
}

// writeTupleSectionDriver emits the write driver for a tuple-family section —
// a dynamic-membership tuple (ADR-0103/0109) or a nested static-membership
// section (Slice A/C). One attribute per element the row contributes:
// BeginAttribute(<scalar sub-columns…>), the zipped co-containers, the
// membership(s), then EndAttributeP. The per-element sequence mirrors
// marshallreflect.marshalTupleSection exactly (the byte-identity invariant).
// Two axes generalise the original Many/dynamic tuple:
//
//   - Cardinality (ts.Cardinality): Many → each slice element; One → the struct
//     value once; Optional → the present-gated option.Option[S] value. A One /
//     Optional all-container element whose containers are all empty splices to
//     zero attributes (the S=0 rule), matching the flat multi-sub-column driver.
//   - Membership source: a DYNAMIC tuple (ts.Memberships non-empty) emits one
//     AddMembership<Channel>P per `@membership` field (one per element for a
//     repeated field). A STATIC nested section (ts.Memberships empty) resolves
//     its one section membership through writeMembershipAdd — the ref lookup
//     symbol / verbatim literal — exactly like a flat section.
func writeTupleSectionDriver(sb *strings.Builder, g goplan.SectionGroup, ts goplan.TupleSpec, secVar string, src valueSrc) {
	scalars := g.ScalarSubColumns()
	containers := g.ContainerSubColumns()
	elemVar := secVar + "Elem"
	attrVar := secVar + "Attr"
	// Element fields are reached through the loop / block variable; reusing the
	// valueSrc contract lets the scalar / blob render helpers apply unchanged.
	// Options cannot occur inside a tuple element (PlanBuilder).
	elemSrc := valueSrc{
		field:     func(goField string) string { return elemVar + "." + goField },
		rowErrCtx: src.rowErrCtx,
	}

	// Enumerate the attribute element(s) by cardinality, binding elemVar; the
	// per-element body runs at depth 3.
	switch ts.Cardinality {
	case mappingplan.AttrCardinalityOne:
		line(sb, 2, "{")
		linef(sb, 3, "%s := %s", elemVar, src.field(ts.GoField))
	case mappingplan.AttrCardinalityOptional:
		linef(sb, 2, "if %s {", src.optionHas(ts.GoField))
		linef(sb, 3, "%s := %s", elemVar, src.optionVal(ts.GoField))
	default: // Many
		linef(sb, 2, "for _, %s := range %s {", elemVar, src.field(ts.GoField))
	}

	if len(containers) > 1 {
		for _, sc := range containers[1:] {
			linef(sb, 3, "if len(%s) != len(%s) {", elemSrc.field(sc.Fields[0].GoFieldName), elemSrc.field(containers[0].Fields[0].GoFieldName))
			linef(sb, 4, "err = eb.Build()%s.Str(\"section\", %q).Str(\"field\", %q).Errorf(\"co-container slices have different lengths\")", src.rowErrCtx, g.Section, sc.Fields[0].GoFieldName)
			line(sb, 4, "return")
			line(sb, 3, "}")
		}
	}

	// S=0 splice (H2): a One / Optional all-container element with every
	// container empty emits no attribute. A Many element always emits.
	depth := 3
	if ts.Cardinality != mappingplan.AttrCardinalityMany && len(scalars) == 0 && len(containers) > 0 {
		linef(sb, 3, "if len(%s) > 0 {", elemSrc.field(containers[0].Fields[0].GoFieldName))
		depth = 4
	}

	args := make([]string, 0, len(scalars))
	for _, sc := range scalars {
		args = append(args, scalarValueExpr(sc.Fields[0], elemSrc))
	}
	linef(sb, depth, "%s := %s.BeginAttribute(%s)", attrVar, secVar, strings.Join(args, ", "))
	if len(containers) > 0 {
		elems := make([]string, 0, len(containers))
		for _, sc := range containers {
			f := sc.Fields[0]
			elems = append(elems, sliceElemExpr(f, elemSrc.field(f.GoFieldName)+"[k]"))
		}
		linef(sb, depth, "for k := range %s {", elemSrc.field(containers[0].Fields[0].GoFieldName))
		linef(sb, depth+1, "%s.%sP(%s)", attrVar, goplan.ContainerAddMethod(len(containers)), strings.Join(elems, ", "))
		line(sb, depth, "}")
	}
	if len(ts.Memberships) == 0 {
		writeMembershipAdd(sb, strings.Repeat("\t", depth), attrVar, g.Memberships[0], elemSrc)
	} else {
		for _, m := range ts.Memberships {
			suffix := m.Channel.AddMethodSuffix()
			if m.IsSlice {
				linef(sb, depth, "for _, mv := range %s {", elemSrc.field(m.GoField))
				linef(sb, depth+1, "%s.AddMembership%sP(%s)", attrVar, suffix, tupleMembExpr("mv", m))
				line(sb, depth, "}")
			} else {
				linef(sb, depth, "%s.AddMembership%sP(%s)", attrVar, suffix, tupleMembExpr(elemSrc.field(m.GoField), m))
			}
		}
	}
	linef(sb, depth, "%s.EndAttributeP()", attrVar)
	if depth == 4 {
		line(sb, 3, "}")
	}
	line(sb, 2, "}")
}

// tupleMembExpr renders the AddMembership<Channel>P argument from a tuple
// element's membership value expression (a field access, or a slice-loop var):
// `[]byte(x)` for a verbatim string field, the expression as-is for a []byte or
// a ref uint64 field.
func tupleMembExpr(valExpr string, m mappingplan.TupleMembership) string {
	if m.Channel.EmbedsLiteralName() {
		// verbatim: []byte(name). A string field or an lw.Verbatim newtype both
		// convert directly; a []byte field passes as-is.
		if m.GoType == "string" {
			return "[]byte(" + valExpr + ")"
		}
		return valExpr
	}
	// ref: the uint64 id. An lw.Ref marker newtype needs an explicit conversion
	// to the DML method's plain uint64 parameter.
	if m.MarkerGoType != "" {
		return "uint64(" + valExpr + ")"
	}
	return valExpr
}

func writeMultiSubColumnDriver(sb *strings.Builder, g goplan.SectionGroup, secVar string, src valueSrc) (err error) {
	if len(g.Memberships) != 1 {
		err = eb.Build().Str("section", g.Section).Errorf("multi-sub-column section with multiple memberships not supported")
		return
	}
	scalars := g.ScalarSubColumns()
	containers := g.ContainerSubColumns()
	args := make([]string, 0, len(scalars))
	for _, sc := range scalars {
		args = append(args, src.field(sc.Fields[0].GoFieldName))
	}
	memb := g.Memberships[0]

	// Zip-length agreement across the container class (ADR-0101 D2): all
	// container sub-columns advance in lockstep through one
	// AddTo(Co)Container(s)P call per element, so unequal lengths are a
	// caller bug surfaced as an error, never silent truncation.
	if len(containers) > 1 {
		for _, sc := range containers[1:] {
			linef(sb, 2, "if len(%s) != len(%s) {", src.field(sc.Fields[0].GoFieldName), src.field(containers[0].Fields[0].GoFieldName))
			linef(sb, 3, "err = eb.Build()%s.Str(\"section\", %q).Str(\"field\", %q).Errorf(\"co-container slices have different lengths\")", src.rowErrCtx, g.Section, sc.Fields[0].GoFieldName)
			line(sb, 3, "return")
			line(sb, 2, "}")
		}
	}

	// S = 0 splice: an all-container tuple with every container empty emits
	// no attribute — the lone-container splice rule generalised. With S ≥ 1
	// the scalar tuple is the presence signal and the attribute always
	// emits, containers possibly empty (ADR-0101 D2).
	depth := 2
	if len(scalars) == 0 && len(containers) > 0 {
		linef(sb, 2, "if len(%s) > 0 {", src.field(containers[0].Fields[0].GoFieldName))
		depth = 3
	}
	indent := strings.Repeat("\t", depth)
	linef(sb, depth, "%sAttr := %s.BeginAttribute(%s)", secVar, secVar, strings.Join(args, ", "))
	if len(containers) > 0 {
		elems := make([]string, 0, len(containers))
		for _, sc := range containers {
			f := sc.Fields[0]
			elems = append(elems, sliceElemExpr(f, src.field(f.GoFieldName)+"[k]"))
		}
		linef(sb, depth, "for k := range %s {", src.field(containers[0].Fields[0].GoFieldName))
		linef(sb, depth+1, "%sAttr.%sP(%s)", secVar, goplan.ContainerAddMethod(len(containers)), strings.Join(elems, ", "))
		linef(sb, depth, "}")
	}
	writeMembershipAdd(sb, indent, secVar+"Attr", memb, src)
	linef(sb, depth, "%sAttr.EndAttributeP()", secVar)
	if depth == 3 {
		line(sb, 2, "}")
	}
	return
}

// writeMembershipAdd emits the per-attribute membership push, choosing the
// AddMembership<Channel>P method per ADR-0008 D3. Ref channels push the
// lookup-resolved kindXxx symbol; Verbatim channels push the literal lw: name
// as []byte; carrier (mixed / parametrized) channels read the per-row
// membership data from the sibling carrier column — one scalar carrier per
// attribute, reached through src like any other field (ADR-0113 D1).
func writeMembershipAdd(sb *strings.Builder, indent, attrVar string, f mappingplan.TaggedField, src valueSrc) {
	ch := f.Flags.Channel
	method := "AddMembership" + ch.AddMethodSuffix() + "P"
	if ch.UsesCarrier() {
		// Cut-2: per-row membership data from the sibling carrier column —
		// one scalar carrier per attribute. Mixed channels pass (value field
		// Id/Name, Params); parametrized channels pass (Params) only. The
		// method suffix selects the channel.
		carrier := src.field(f.CarrierField)
		if vf := ch.CarrierValueField(); vf != "" {
			linef(sb, 0, "%s%s.%s(%s.%s, %s.Params)", indent, attrVar, method, carrier, vf, carrier)
		} else {
			linef(sb, 0, "%s%s.%s(%s.Params)", indent, attrVar, method, carrier)
		}
		return
	}
	if ch.EmbedsLiteralName() {
		linef(sb, 0, "%s%s.%s([]byte(%q))", indent, attrVar, method, f.LWMembership)
		return
	}
	linef(sb, 0, "%s%s.%s(%s)", indent, attrVar, method, f.KindVar())
}

// writeFieldDriver emits the per-field BuildEntities lines for one
// field of a single-sub-column section. Flag-driven; never inspects
// section name. Const fields (IsConst) emit a literal-valued attribute
// per row instead of reading from a Go-side slot.
func writeFieldDriver(sb *strings.Builder, f mappingplan.TaggedField, secVar string, src valueSrc) (err error) {
	tag := f.GoFieldName
	if tag == "" {
		tag = mappingplan.UpperFirst(f.LWMembership) // const fields have no Go name
	}
	attrVar := secVar + "Attr_" + tag
	shape := goplan.ClassifyBegin(f)

	switch shape {
	case goplan.ShapeScalarBegin:
		valExpr := scalarValueExpr(f, src)
		if f.IsOption {
			linef(sb, 2, "if %s {", src.optionHas(f.GoFieldName))
			linef(sb, 3, "%s := %s.BeginAttribute(%s)", attrVar, secVar, valExpr)
			writeMembershipAdd(sb, "\t\t\t", attrVar, f, src)
			linef(sb, 3, "%s.EndAttributeP()", attrVar)
			line(sb, 2, "}")
			return
		}
		linef(sb, 2, "%s := %s.BeginAttribute(%s)", attrVar, secVar, valExpr)
		writeMembershipAdd(sb, "\t\t", attrVar, f, src)
		linef(sb, 2, "%s.EndAttributeP()", attrVar)

	case goplan.ShapeScalarBeginSingle:
		valExpr := scalarValueExpr(f, src)
		if f.IsOption {
			linef(sb, 2, "if %s {", src.optionHas(f.GoFieldName))
			linef(sb, 3, "%s := %s.BeginAttributeSingle(%s)", attrVar, secVar, valExpr)
			writeMembershipAdd(sb, "\t\t\t", attrVar, f, src)
			linef(sb, 3, "%s.EndAttributeP()", attrVar)
			line(sb, 2, "}")
			return
		}
		linef(sb, 2, "%s := %s.BeginAttributeSingle(%s)", attrVar, secVar, valExpr)
		writeMembershipAdd(sb, "\t\t", attrVar, f, src)
		linef(sb, 2, "%s.EndAttributeP()", attrVar)

	case goplan.ShapeContainer:
		// 1 attribute, N values via AddToContainerP, 1 carrier (if any). Empty
		// / nil skips (leeway splice semantics: empty non-scalars vanish — the
		// carrier of an empty container is therefore not emitted).
		switch {
		case f.IsRoaring():
			linef(sb, 2, "if %s != nil && !%s.IsEmpty() {", src.field(f.GoFieldName), src.field(f.GoFieldName))
			linef(sb, 3, "%s := %s.BeginAttribute()", attrVar, secVar)
			linef(sb, 3, "it := %s.Iterator()", src.field(f.GoFieldName))
			line(sb, 3, "for it.HasNext() {")
			linef(sb, 4, "%s.AddToContainerP(it.Next())", attrVar)
			line(sb, 3, "}")
			writeMembershipAdd(sb, "\t\t\t", attrVar, f, src)
			linef(sb, 3, "%s.EndAttributeP()", attrVar)
			line(sb, 2, "}")
		case f.IsSlice():
			linef(sb, 2, "if len(%s) > 0 {", src.field(f.GoFieldName))
			linef(sb, 3, "%s := %s.BeginAttribute()", attrVar, secVar)
			linef(sb, 3, "for _, v := range %s {", src.field(f.GoFieldName))
			linef(sb, 4, "%s.AddToContainerP(%s)", attrVar, sliceElemExpr(f, "v"))
			line(sb, 3, "}")
			writeMembershipAdd(sb, "\t\t\t", attrVar, f, src)
			linef(sb, 3, "%s.EndAttributeP()", attrVar)
			line(sb, 2, "}")
		default:
			err = eb.Build().Str("field", f.GoFieldName).Errorf("container shape on non-slice / non-roaring field — should have been caught by parser")
		}
	}
	return
}

// scalarValueExpr renders the BeginAttribute(value) argument for a
// scalar / Option field. For Option fields, the Has guard is emitted
// separately; this returns the raw value access. For const fields,
// returns the constant's Go literal (always a quoted string).
func scalarValueExpr(f mappingplan.TaggedField, src valueSrc) string {
	if f.IsConst {
		return fmt.Sprintf("%q", f.ConstValue)
	}
	if f.IsOption {
		return blobSliceMaybe(f, src.optionVal(f.GoFieldName))
	}
	return blobSliceMaybe(f, src.field(f.GoFieldName))
}

// sliceElemExpr renders the per-element expression inside a container
// loop. Re-slices fixed-width byte arrays so the AttrI's
// AddToContainerP / SecI's BeginAttribute (which take []byte for blob
// sections) accepts them.
func sliceElemExpr(f mappingplan.TaggedField, elemVar string) string {
	if goplan.IsFixedByteArray(f.GoType()) {
		return elemVar + "[:]"
	}
	return elemVar
}

// blobSliceMaybe re-slices a fixed-width byte array Go value into the
// []byte the blob-section BeginAttribute expects. No-op for any other
// type.
func blobSliceMaybe(f mappingplan.TaggedField, base string) string {
	if goplan.IsFixedByteArray(f.GoType()) {
		return base + "[:]"
	}
	return base
}
