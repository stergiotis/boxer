package goplan

import (
	"sort"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// --- Section grouping + field-shape classification (shared core). ---
//
// These types and helpers are the schema-agnostic grouping /
// classification vocabulary shared by the code generator (emit.go) and
// the runtime-reflection sibling package marshallreflect. They are
// exported so marshallreflect can drive the same section ordering,
// scalar-first partition, and Begin-shape classification without
// re-deriving them — the two packages MUST agree on this layout or
// their wire output diverges. Keep them here (not duplicated per
// consumer) so there is one source of truth.

// SubColumn groups fields targeting the same physical sub-column inside
// a section. Most sections have one sub-column ("value"); u32Range has
// two (beginIncl + endExcl) sharing the section's per-attribute support
// arrays.
type SubColumn struct {
	Name   string                    // "value", "beginIncl", "endExcl"
	Fields []mappingplan.TaggedField // fields routing to this sub-column, DTO order
}

// SectionGroup is the per-section emission unit. Fields are grouped on
// two orthogonal axes: by sub-column (one value-array per sub-column on
// the wire) and by distinct membership (one lr+lrcard pair per
// membership).
type SectionGroup struct {
	Section     string
	SubColumns  []SubColumn
	Memberships []mappingplan.TaggedField // one per distinct LWMembership, in first-seen order
}

// Channel reports the (uniform per section) membership channel for
// this section's fields. ParsePlan enforces uniformity so any
// representative field's channel speaks for the whole section.
// Returns the zero value (LowCardRef) for empty groups.
func (g SectionGroup) Channel() mappingplan.MembershipChannel {
	if len(g.Memberships) == 0 {
		return mappingplan.MembershipChannelLowCardRef
	}
	return g.Memberships[0].Flags.Channel
}

// ComputeGroups walks plan.Fields in DTO declaration order, bucketing
// each field by its lw: section name. The section order in the output
// is DTO declaration order — section-level layout matches three
// independent emit sites and must not drift:
//
//   - <Kind>EntityI's per-section type-parameter list (writeEntityInterface)
//   - <Kind>BuildEntities' type-parameter list and per-section call sequence
//   - <Kind>FillFromArrow's per-section reader-parameter list
//
// Within each section the fields are partitioned scalar-first
// (ShapeScalarBegin / ShapeScalarBeginSingle / consts) ahead of
// non-scalars (ShapeContainer / ShapeExplodeBegin*). The partition is
// stable: declaration order is preserved within each class. Per
// ADR-0008 D2.
//
// Wrappers that emit per-section helpers (e.g. FactsWrapper's Reader
// struct + Unmarshal-call argument list) MUST iterate sections in the
// same DTO-declaration order so the type-parameter binding at the
// call site lines up. A wrapper that walks sections in
// dml-index-sorted order (or alphabetical) silently passes the wrong
// readers to FillFromArrow.
func ComputeGroups(plan *mappingplan.Plan) (out []SectionGroup) {
	seen := map[string]int{}
	for _, f := range plan.Fields {
		gIdx, ok := seen[f.Section()]
		if !ok {
			seen[f.Section()] = len(out)
			gIdx = len(out)
			out = append(out, SectionGroup{Section: f.Section()})
		}
		g := &out[gIdx]

		colName := f.LWColumn
		if colName == "" {
			colName = "value"
		}
		scIdx := -1
		for i := range g.SubColumns {
			if g.SubColumns[i].Name == colName {
				scIdx = i
				break
			}
		}
		if scIdx < 0 {
			g.SubColumns = append(g.SubColumns, SubColumn{Name: colName})
			scIdx = len(g.SubColumns) - 1
		}
		g.SubColumns[scIdx].Fields = append(g.SubColumns[scIdx].Fields, f)
	}

	for gi := range out {
		g := &out[gi]
		for sci := range g.SubColumns {
			partitionScalarsFirst(g.SubColumns[sci].Fields)
		}
		rebuildMemberships(g)
	}
	return
}

// partitionScalarsFirst reorders fields in-place so all scalar-shaped
// fields (ShapeScalarBegin / ShapeScalarBeginSingle, which includes
// consts since they classify as scalar) precede non-scalar fields
// (ShapeContainer / ShapeExplodeBegin / ShapeExplodeBeginSingle).
// Declaration order is preserved within each class via a stable sort.
func partitionScalarsFirst(fields []mappingplan.TaggedField) {
	sort.SliceStable(fields, func(i, j int) bool {
		return isScalarShape(fields[i]) && !isScalarShape(fields[j])
	})
}

// isScalarShape reports whether a tagged field emits as a single-value
// scalar attribute. Used to drive the within-section partition.
func isScalarShape(f mappingplan.TaggedField) bool {
	switch ClassifyBegin(f) {
	case ShapeScalarBegin, ShapeScalarBeginSingle:
		return true
	default:
		return false
	}
}

// rebuildMemberships repopulates the section's distinct-membership
// slice from the (now scalar-first-partitioned) sub-column fields so
// the first-seen order reflects the post-partition layout.
func rebuildMemberships(g *SectionGroup) {
	g.Memberships = g.Memberships[:0]
	seen := map[string]bool{}
	for sci := range g.SubColumns {
		for _, f := range g.SubColumns[sci].Fields {
			if seen[f.LWMembership] {
				continue
			}
			seen[f.LWMembership] = true
			g.Memberships = append(g.Memberships, f)
		}
	}
}

// ScalarSubColumns returns the section's sub-columns whose (single)
// field is scalar-shaped, in declaration order. Meaningful for
// multi-sub-column sections only (PlanBuilder.Finish enforces exactly
// one field per sub-column there); the scalar class supplies the
// BeginAttribute argument list. Per ADR-0101 D3.
func (g SectionGroup) ScalarSubColumns() (out []SubColumn) {
	for _, sc := range g.SubColumns {
		if isScalarShape(sc.Fields[0]) {
			out = append(out, sc)
		}
	}
	return
}

// ContainerSubColumns returns the section's sub-columns whose (single)
// field is container-shaped, in declaration order. The container class
// supplies the AddToContainerP / AddToCoContainersP argument list — all
// containers advance in lockstep (zipped co-containers, one shared
// per-attribute length). Per ADR-0101 D1/D3.
func (g SectionGroup) ContainerSubColumns() (out []SubColumn) {
	for _, sc := range g.SubColumns {
		if !isScalarShape(sc.Fields[0]) {
			out = append(out, sc)
		}
	}
	return
}

// TupleSpec describes a section driven by a dynamic-membership tuple
// field (ADR-0103, extended by ADR-0109): the outer slice-of-struct DTO
// field, its element struct type, and the element's `@membership` fields —
// one or more, each an id-or-name value on a verbatim / ref channel, in
// declaration order. Derived from the sub-column fields' shared Tuple*
// metadata — every sub-field of one tuple carries identical copies
// (PlanBuilder.AddTupleSliceField).
type TupleSpec struct {
	GoField     string // outer DTO field, e.g. "Strings"
	StructType  string // element struct type name, e.g. "LabeledText"
	Memberships []mappingplan.TupleMembership
}

// Channels returns the distinct membership channels the tuple's elements
// carry, in first-seen (declaration) order. The AttrI exposes one
// AddMembership<Channel>P per channel and Validate checks the DML has each —
// an element may mix verbatim and ref channels (ADR-0109 D4).
func (ts TupleSpec) Channels() (out []mappingplan.MembershipChannel) {
	seen := map[mappingplan.MembershipChannel]bool{}
	for _, m := range ts.Memberships {
		if seen[m.Channel] {
			continue
		}
		seen[m.Channel] = true
		out = append(out, m.Channel)
	}
	return
}

// TupleSpec reports whether the section is tuple-driven, and its spec.
// A tuple section emits N attributes per row — one per element of the
// outer slice, each with its own membership — instead of the static
// shapes' fixed per-field attribute layout. Every emit / drive /
// validate / read site must dispatch on this BEFORE the sub-column-count
// shape split, so both front-ends route tuples identically.
func (g SectionGroup) TupleSpec() (ts TupleSpec, ok bool) {
	if len(g.SubColumns) == 0 || len(g.SubColumns[0].Fields) == 0 {
		return
	}
	f := g.SubColumns[0].Fields[0]
	if f.TupleField == "" {
		return
	}
	return TupleSpec{
		GoField:     f.TupleField,
		StructType:  f.TupleStructType,
		Memberships: f.TupleMemberships,
	}, true
}

// ContainerAddMethod returns the DML container-append method name for a
// section with containerCount container sub-columns, mirroring the DML
// generator's count rule (lw_dml_generator.go): "AddToContainer" for
// exactly one, "AddToCoContainers" for two or more. Callers append "P"
// for the void sibling. Shared by the codegen emitter, the reflect
// codec and Validate so the three cannot drift. Per ADR-0101 SD3.
func ContainerAddMethod(containerCount int) string {
	if containerCount == 1 {
		return "AddToContainer"
	}
	return "AddToCoContainers"
}

// FieldBeginShape classifies how one field opens its attribute on the
// wire — drives the SecI's exposed methods AND the per-field call
// pattern in BuildEntities. Independent of any registry; derived from
// (Go shape, FieldFlags) alone.
type FieldBeginShape int

const (
	// ShapeScalarBegin: T or Option[T] without `unit`. Section's
	// BeginAttribute takes the value directly (`sec.BeginAttribute(v)`).
	ShapeScalarBegin FieldBeginShape = iota
	// ShapeScalarBeginSingle: T or Option[T] with `unit`. Section's
	// BeginAttributeSingle takes the value (`sec.BeginAttributeSingle(v)`).
	ShapeScalarBeginSingle
	// ShapeContainer: []T / *roaring.Bitmap / [][]byte default. Section's
	// BeginAttribute opens a container (no args); AttrI carries
	// AddToContainerP for per-value append.
	ShapeContainer
	// ShapeExplodeBegin: []T,explode. Per-element loop with
	// `sec.BeginAttribute(v)` (scalar BeginAttribute signature).
	ShapeExplodeBegin
	// ShapeExplodeBeginSingle: []T,explode,unit. Per-element loop with
	// `sec.BeginAttributeSingle(v)`.
	ShapeExplodeBeginSingle
)

// ClassifyBegin maps a TaggedField to its FieldBeginShape.
func ClassifyBegin(f mappingplan.TaggedField) FieldBeginShape {
	isMulti := f.IsMulti()
	switch {
	case isMulti && f.Flags.Explode && f.Flags.Unit:
		return ShapeExplodeBeginSingle
	case isMulti && f.Flags.Explode:
		return ShapeExplodeBegin
	case isMulti:
		return ShapeContainer
	case f.Flags.Unit:
		return ShapeScalarBeginSingle
	default:
		return ShapeScalarBegin
	}
}

// SingleValueReadAccessor returns the RA accessor that yields a field's
// single per-attribute value: GetAttrValueValue for the scalar-section
// shapes (ShapeScalarBegin / ShapeExplodeBegin, whose section exposes the
// value directly), GetAttrValueSingleOrDefault otherwise (the HA /
// single-slot sections). Both back-ends route their single-value reads
// through here — the codegen emitter prints the returned name, the reflect
// codec calls it via mustCall — so the accessor choice cannot drift between
// them (it previously lived as four hand-copied switches, two of which
// silently omitted ShapeExplodeBegin).
func SingleValueReadAccessor(f mappingplan.TaggedField) string {
	switch ClassifyBegin(f) {
	case ShapeScalarBegin, ShapeExplodeBegin:
		return "GetAttrValueValue"
	default:
		return "GetAttrValueSingleOrDefault"
	}
}

// FindPlainCol returns the plan's plain column with the given wire name
// (id / ts / naturalKey / expiresAt), or nil if the DTO does not
// declare it.
func FindPlainCol(plan *mappingplan.Plan, col string) *mappingplan.PlainCol {
	for i := range plan.PlainCols {
		if plan.PlainCols[i].Column == col {
			return &plan.PlainCols[i]
		}
	}
	return nil
}
