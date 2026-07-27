package marshallreflect

import (
	"reflect"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
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
	// sections records which sections the open row has already emitted, and
	// which DTO kind emitted each — see claimSections.
	sections map[string]string
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
		dml:      reflect.ValueOf(dml),
		lookup:   lookup,
		sections: map[string]string{},
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
	defer recoverContract(&err)
	if c.inRow {
		err = eb.Build().Errorf("BeginRow called while already inside a row — call CommitRow first")
		return
	}
	rowVal, plan, groups, err := resolvePlan(plainOwner)
	if err != nil {
		return
	}
	mustCall(c.dml, "BeginEntity")
	c.inRow = true
	clear(c.sections)

	if err = marshalPlain(c.dml, rowVal, plan); err != nil {
		return
	}
	if err = c.claimSections(groups, plan.KindName); err != nil {
		return
	}
	err = marshalRowSections(c.dml, rowVal, groups, c.lookup)
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
	defer recoverContract(&err)
	if !c.inRow {
		err = eb.Build().Str("call", "AddSections").Errorf("AddSections called outside of a row — call BeginRow first")
		return
	}
	rowVal, plan, groups, err := resolvePlan(row)
	if err != nil {
		return
	}
	if err = c.claimSections(groups, plan.KindName); err != nil {
		return
	}
	err = marshalRowSections(c.dml, rowVal, groups, c.lookup)
	return
}

// claimSections enforces one section visit per entity (ADR-0146 D6). A section
// is opened once per BeginEntity and closed once; the generated DML does not
// reopen it, so a second DTO touching a section another already emitted fails
// deep inside the DML as a bare "invalid state transition" that names neither
// the section nor the DTO. Catching it here says what actually happened.
func (c *RowComposer) claimSections(groups []goplan.SectionGroup, kind string) (err error) {
	for _, g := range groups {
		if owner, taken := c.sections[g.Section]; taken {
			return eb.Build().Str("section", g.Section).Str("first", owner).Errorf("section %s was already emitted in this row by %s — a section is opened once per entity, so stacked DTOs must not overlap on one (ADR-0070 D3 retracted by ADR-0146 D6)", g.Section, owner)
		}
	}
	for _, g := range groups {
		c.sections[g.Section] = kind
	}
	return
}

// CommitRow closes the open entity by calling CommitEntity on the
// DML. The entity-level error returned by CommitEntity (if any) is
// surfaced. After CommitRow the composer is ready for the next
// BeginRow.
func (c *RowComposer) CommitRow() (err error) {
	defer recoverContract(&err)
	if !c.inRow {
		err = eb.Build().Errorf("CommitRow called outside of a row — call BeginRow first")
		return
	}
	c.inRow = false
	clear(c.sections)
	rets := mustCall(c.dml, "CommitEntity")
	if len(rets) == 1 && !rets[0].IsNil() {
		err = rets[0].Interface().(error)
	}
	return
}

// resolvePlan inspects a row value, ensuring it's a struct (not a
// slice / map / pointer), and returns its reflect.Value plus the cached
// Plan + section grouping for its type.
func resolvePlan(row any) (rowVal reflect.Value, plan *mappingplan.Plan, groups []goplan.SectionGroup, err error) {
	rowVal = reflect.ValueOf(row)
	if rowVal.Kind() == reflect.Ptr {
		rowVal = rowVal.Elem()
	}
	if rowVal.Kind() != reflect.Struct {
		err = eb.Build().Str("type", reflect.TypeOf(row).String()).Errorf("row must be a struct (or *struct), got %s", rowVal.Kind())
		return
	}
	r, rerr := resolveForType(rowVal.Type())
	if rerr != nil {
		err = eb.Build().Str("type", rowVal.Type().String()).Errorf("plan for row type: %w", rerr)
		return
	}
	plan = r.plan
	groups = r.groups
	return
}

func marshalRowSections(dml, rowVal reflect.Value, groups []goplan.SectionGroup, lookup LookupI) (err error) {
	for _, g := range groups {
		err = marshalSection(dml, rowVal, g, lookup)
		if err != nil {
			err = eb.Build().Str("section", g.Section).Errorf("section %s: %w", g.Section, err)
			return
		}
	}
	return
}

