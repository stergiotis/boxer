package mappingplan

import "sort"

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
	Name   string        // "value", "beginIncl", "endExcl"
	Fields []TaggedField // fields routing to this sub-column, DTO order
}

// SectionGroup is the per-section emission unit. Fields are grouped on
// two orthogonal axes: by sub-column (one value-array per sub-column on
// the wire) and by distinct membership (one lr+lrcard pair per
// membership).
type SectionGroup struct {
	Section     string
	SubColumns  []SubColumn
	Memberships []TaggedField // one per distinct LWMembership, in first-seen order
}

// Channel reports the (uniform per section) membership channel for
// this section's fields. ParsePlan enforces uniformity so any
// representative field's channel speaks for the whole section.
// Returns the zero value (LowCardRef) for empty groups.
func (g SectionGroup) Channel() MembershipChannel {
	if len(g.Memberships) == 0 {
		return MembershipChannelLowCardRef
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
func ComputeGroups(plan *Plan) (out []SectionGroup) {
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
func partitionScalarsFirst(fields []TaggedField) {
	sort.SliceStable(fields, func(i, j int) bool {
		return isScalarShape(fields[i]) && !isScalarShape(fields[j])
	})
}

// isScalarShape reports whether a tagged field emits as a single-value
// scalar attribute. Used to drive the within-section partition.
func isScalarShape(f TaggedField) bool {
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
func ClassifyBegin(f TaggedField) FieldBeginShape {
	isMulti := f.IsSlice || f.IsRoaring
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

// FindPlainCol returns the plan's plain column with the given wire name
// (id / ts / naturalKey / expiresAt), or nil if the DTO does not
// declare it.
func FindPlainCol(plan *Plan, col string) *PlainCol {
	for i := range plan.PlainCols {
		if plan.PlainCols[i].Column == col {
			return &plan.PlainCols[i]
		}
	}
	return nil
}

// UpperFirst upper-cases the first byte of s when it is an ASCII
// lower-case letter; every other input is returned unchanged. Used to
// derive PascalCase section / sub-column method names from the lw: tag
// strings (e.g. "u32Array" → "U32Array").
func UpperFirst(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}
