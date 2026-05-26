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
	if !c.inRow {
		err = eb.Build().Errorf("marshallreflect: AddSections called outside of a row — call BeginRow first")
		return
	}
	rowVal, plan, err := resolvePlan(row)
	if err != nil {
		return
	}
	err = marshalRowSections(c.dml, rowVal, plan, c.lookup)
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
// with Marshal.
func marshalRowSections(dml, rowVal reflect.Value, plan *marshallgen.Plan, lookup LookupI) (err error) {
	groups := computeGroups(plan)
	for _, g := range groups {
		err = marshalSection(dml, rowVal, g, lookup)
		if err != nil {
			err = eb.Build().Str("section", g.Section).Errorf("marshallreflect: section %s: %w", g.Section, err)
			return
		}
	}
	return
}
