//go:build llm_generated_opus47

package marshallreflect

import (
	"reflect"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallgen"
)

// RowComposer drives the per-row stacked-entity emit pattern from
// ADR-0008 D1. Each row opens with BeginRow(plainOwner) — which
// writes plain columns and the plainOwner DTO's sections — then
// accepts zero or more AddSections(row) calls contributing additional
// DTOs' sections to the same entity, and closes with CommitRow.
//
// Unlike the original one-shot MarshalStack, the composer doesn't
// require batches-of-rows rectangles up front: the caller iterates
// row indices (or any other ordering) and composes each entity from
// arbitrary DTO instances. Plain-column ownership is explicit per
// row — only the plainOwner argument's DTO drives plain emission;
// other DTOs' plain declarations are ignored.
//
// The composer enforces a Initial → InRow → Initial state machine.
// BeginRow on an already-open row, AddSections / CommitRow before
// BeginRow, or any other mis-sequenced call returns a clear error
// without touching the DML.
type RowComposer struct {
	dml    reflect.Value
	lookup LookupI
	inRow  bool
}

// NewRowComposer wraps `dml` and `lookup` for repeated per-row
// stacked emits. `dml`'s method set must satisfy the reflective
// contract Marshal expects (BeginEntity / SetId / SetTimestamp /
// SetLifecycle / GetSection<X> / CommitEntity). Pass NoLookup{} for
// `lookup` if every DTO field uses a verbatim membership channel.
func NewRowComposer(dml any, lookup LookupI) *RowComposer {
	if lookup == nil {
		lookup = NoLookup{}
	}
	return &RowComposer{
		dml:    reflect.ValueOf(dml),
		lookup: lookup,
	}
}

// BeginRow opens an entity frame and writes plain columns plus
// sections from `plainOwner`'s DTO. Subsequent AddSections calls
// contribute more DTOs' sections to the same entity until CommitRow.
//
// Returns an error if the composer is already inside a row, or if
// plainOwner's plan resolution / plain-column emit fails. Errors
// surface before any DML method is called when the cause is
// composer-state related; plan / DML errors propagate from the
// underlying emit.
func (c *RowComposer) BeginRow(plainOwner any) (err error) {
	if c.inRow {
		err = eb.Build().Errorf("marshallreflect: BeginRow called while already inside a row — call CommitRow first")
		return
	}
	rowVal, plan, err := resolvePlan(plainOwner)
	if err != nil {
		return
	}
	mustCall(c.dml, "BeginEntity")
	c.inRow = true

	if err = marshalPlain(c.dml, rowVal, plan); err != nil {
		return
	}
	err = marshalRowSections(c.dml, rowVal, plan, c.lookup)
	return
}

// AddSections contributes `row`'s sections to the currently open
// entity. Plain columns declared on row's DTO are ignored — only
// the plainOwner passed to BeginRow drives plain emission.
//
// Returns an error if no row is open (BeginRow not called or
// CommitRow already called for this entity), or if plan resolution
// fails.
func (c *RowComposer) AddSections(row any) (err error) {
	return c.addSectionsFiltered(row, cardFilterAll, "AddSections")
}

// AddSingleValueAttributes contributes `row`'s sections to the
// currently open entity, emitting only attributes whose value-
// cardinality is exactly 1 at runtime. Concretely:
//
//   - scalar fields (T, Option[T] with Has=true, consts): always size 1
//   - container / roaring fields whose runtime length is exactly 1
//   - explode-shaped fields (each element produces a size-1 attribute)
//   - multi-sub-column scalar sections (one tuple per row)
//
// Container / roaring fields with runtime length > 1 are skipped;
// they belong to AddMultiValueAttributes. Sections whose fields all
// fail to match this filter open no BeginSection frame.
//
// Pair this with AddMultiValueAttributes on the same row to drive
// the per-section `1,1,…,>1,>1,…` attribute ordering described in
// ADR-0008 D2 (the field-class partition is static; this method
// extends it to per-attribute runtime cardinality).
func (c *RowComposer) AddSingleValueAttributes(row any) (err error) {
	return c.addSectionsFiltered(row, cardFilterSingleValue, "AddSingleValueAttributes")
}

// AddMultiValueAttributes contributes `row`'s sections to the
// currently open entity, emitting only attributes whose value-
// cardinality exceeds 1 at runtime. Only container / roaring fields
// without `,explode` and with runtime length > 1 reach the wire;
// scalar / Option / explode / const / multi-sub-column emits are
// skipped (they belong to AddSingleValueAttributes).
//
// Sections that produce no matching attribute open no BeginSection
// frame.
func (c *RowComposer) AddMultiValueAttributes(row any) (err error) {
	return c.addSectionsFiltered(row, cardFilterMultiValue, "AddMultiValueAttributes")
}

func (c *RowComposer) addSectionsFiltered(row any, filter cardFilter, callerName string) (err error) {
	if !c.inRow {
		err = eb.Build().Str("call", callerName).Errorf("marshallreflect: %s called outside of a row — call BeginRow first", callerName)
		return
	}
	rowVal, plan, err := resolvePlan(row)
	if err != nil {
		return
	}
	err = marshalRowSectionsFiltered(c.dml, rowVal, plan, c.lookup, filter)
	return
}

// CommitRow closes the open entity by calling CommitEntity on the
// DML. The entity-level error returned by CommitEntity (if any) is
// surfaced. After CommitRow the composer is ready for the next
// BeginRow.
func (c *RowComposer) CommitRow() (err error) {
	if !c.inRow {
		err = eb.Build().Errorf("marshallreflect: CommitRow called outside of a row — call BeginRow first")
		return
	}
	c.inRow = false
	rets := mustCall(c.dml, "CommitEntity")
	if len(rets) == 1 && !rets[0].IsNil() {
		err = rets[0].Interface().(error)
	}
	return
}

// resolvePlan inspects a row value, ensuring it's a struct (not a
// slice / map / pointer), and returns its reflect.Value plus the
// cached Plan for its type.
func resolvePlan(row any) (rowVal reflect.Value, plan *marshallgen.Plan, err error) {
	rowVal = reflect.ValueOf(row)
	if rowVal.Kind() == reflect.Ptr {
		rowVal = rowVal.Elem()
	}
	if rowVal.Kind() != reflect.Struct {
		err = eb.Build().Str("type", reflect.TypeOf(row).String()).Errorf("marshallreflect: row must be a struct (or *struct), got %s", rowVal.Kind())
		return
	}
	plan, err = planForType(rowVal.Type())
	if err != nil {
		err = eb.Build().Str("type", rowVal.Type().String()).Errorf("marshallreflect: plan for row type: %w", err)
		return
	}
	return
}

// marshalRowSections emits every section in plan against rowVal
// using the existing computeGroups / marshalSection machinery shared
// with Marshal. Equivalent to marshalRowSectionsFiltered with
// cardFilterAll; kept as the unfiltered entry point used by BeginRow.
func marshalRowSections(dml, rowVal reflect.Value, plan *marshallgen.Plan, lookup LookupI) (err error) {
	return marshalRowSectionsFiltered(dml, rowVal, plan, lookup, cardFilterAll)
}

func marshalRowSectionsFiltered(dml, rowVal reflect.Value, plan *marshallgen.Plan, lookup LookupI, filter cardFilter) (err error) {
	groups := computeGroups(plan)
	for _, g := range groups {
		err = marshalSection(dml, rowVal, g, lookup, filter)
		if err != nil {
			err = eb.Build().Str("section", g.Section).Errorf("marshallreflect: section %s: %w", g.Section, err)
			return
		}
	}
	return
}

// cardFilter partitions per-attribute emit by runtime value
// cardinality. Used by RowComposer.AddSingleValueAttributes /
// AddMultiValueAttributes to drive the `1,1,…,>1,>1,…` per-section
// attribute ordering from ADR-0008 D2 at the per-attribute grain.
type cardFilter uint8

const (
	cardFilterAll cardFilter = iota
	cardFilterSingleValue
	cardFilterMultiValue
)

// sectionHasMatchingField reports whether any field in the section
// would emit at least one attribute matching `filter`, given the
// current row's values. Used to decide whether to open a
// BeginSection frame for a filtered emit.
func sectionHasMatchingField(row reflect.Value, g sectionGroup, filter cardFilter) bool {
	if filter == cardFilterAll {
		return true
	}
	if len(g.SubColumns) > 1 {
		// Multi-sub-column = one tuple per row, single-value attribute.
		return filter == cardFilterSingleValue
	}
	for _, f := range g.SubColumns[0].Fields {
		if fieldEmitsForFilter(row, f, filter) {
			return true
		}
	}
	return false
}

// fieldEmitsForFilter reports whether the field will emit at least
// one attribute matching the cardinality filter, given the row's
// current values. Returns true for cardFilterAll so callers can use
// the same predicate uniformly.
func fieldEmitsForFilter(row reflect.Value, f marshallgen.TaggedField, filter cardFilter) bool {
	if filter == cardFilterAll {
		return true
	}
	if f.IsConst {
		return filter == cardFilterSingleValue
	}
	if f.IsOption {
		if filter != cardFilterSingleValue {
			return false
		}
		return row.FieldByName(f.GoFieldName).FieldByName("Has").Bool()
	}
	isMulti := f.IsSlice || f.IsRoaring
	if !isMulti {
		return filter == cardFilterSingleValue
	}
	size, hasData := containerSize(row, f)
	if !hasData {
		return false
	}
	if f.Flags.Explode {
		// Each element emits a size-1 attribute. Never multi-value.
		return filter == cardFilterSingleValue
	}
	if size == 1 {
		return filter == cardFilterSingleValue
	}
	return filter == cardFilterMultiValue
}

// containerSize returns the element count of a slice / roaring
// field and a bool indicating whether the field has any data at
// all. Used to decide cardFilter matching for non-explode multi-
// shape fields.
func containerSize(row reflect.Value, f marshallgen.TaggedField) (n int, hasData bool) {
	fld := row.FieldByName(f.GoFieldName)
	if f.IsRoaring {
		if fld.IsNil() {
			return 0, false
		}
		if mustCall(fld, "IsEmpty")[0].Bool() {
			return 0, false
		}
		card := mustCall(fld, "GetCardinality")[0].Uint()
		return int(card), card > 0
	}
	if f.IsSlice {
		n = fld.Len()
		return n, n > 0
	}
	return 0, false
}
