package marshallreflect

import (
	"reflect"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	dmlruntime "github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
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
//
// # Sections are buffered, not written as they arrive
//
// Several stacked DTOs may contribute to the SAME section — a fact is not
// written once by one owner, and later stages fuse and enrich entities without
// knowing every component involved. A section frame is opened once per entity
// and never reopened, so those contributions have to share one frame. The
// composer therefore buffers each DTO's per-section attributes and writes them
// at CommitRow, one frame per section carrying every contribution in call
// order.
//
// The cost is error timing: a failure inside the attribute emit (a co-container
// zip mismatch, say) surfaces from CommitRow rather than from the AddSections
// call that supplied it, and names the section. Everything decided from the DTO
// alone — an unresolvable plan, a non-struct row — is still reported by the
// call that passed it.
type RowComposer struct {
	dml    reflect.Value
	lookup LookupI
	inRow  bool
	// The open row's section contributions, buffered until CommitRow. A
	// section frame is closed for good by EndSection, so several DTOs
	// contributing to one section must share that frame — which means the
	// close cannot happen as each DTO finishes.
	//
	// The buffer is dml/runtime's, shared with the generated typed builders
	// (ADR-0183 D4). This composer's own buffer was its specification, so what
	// changed here is who owns the order and the frame invariants, not what
	// they are.
	buf dmlruntime.DeferredSectionBuffer
	// open caches the section handles this row has asked the DML for. A
	// generated DML returns the same instance every time, but a mock records
	// the calls, and one GetSection per section per entity is what the frame
	// contract looks like from outside.
	open map[string]reflect.Value
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
		open:   map[string]reflect.Value{},
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
	c.buf.Reset()
	clear(c.open)

	if err = marshalPlain(c.dml, rowVal, plan); err != nil {
		return
	}
	err = c.buffer(plan, rowVal, groups)
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
	err = c.buffer(plan, rowVal, groups)
	return
}

// buffer records a DTO's per-section contributions for the open row.
//
// The kind is stated so the buffer can hold the one-contribution-per-kind
// invariant and attribute a failure at flush; a DTO with no kind tag (a plain
// struct used as a section carrier) contributes under its Go type name, which
// is the only name it has.
func (c *RowComposer) buffer(plan *mappingplan.Plan, rowVal reflect.Value, groups []goplan.SectionGroup) (err error) {
	kind := plan.KindName
	if kind == "" {
		kind = plan.KindType
	}
	if err = c.buf.StartKind(kind); err != nil {
		return
	}
	for _, g := range groups {
		c.buf.Enqueue(g.Section, kind, func() error {
			return emitSectionAttributes(c.section(g.Section), rowVal, g, c.lookup)
		})
	}
	return
}

// flushSections writes the buffered contributions: one frame per section,
// carrying every DTO that contributed to it, in call order.
func (c *RowComposer) flushSections() (err error) {
	return c.buf.Flush(func(section string) error {
		closeSection(c.section(section))
		return nil
	})
}

// section returns this row's handle for a section, asking the DML once.
func (c *RowComposer) section(name string) (sec reflect.Value) {
	sec, has := c.open[name]
	if has {
		return
	}
	sec = openSection(c.dml, name)
	c.open[name] = sec
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
	if err = c.flushSections(); err != nil {
		return
	}
	c.buf.Reset()
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
	if rowVal.Kind() == reflect.Pointer {
		rowVal = rowVal.Elem()
	}
	if rowVal.Kind() != reflect.Struct {
		err = eb.Build().Str("type", reflect.TypeOf(row).String()).Stringer("kind", rowVal.Kind()).Errorf("row must be a struct (or *struct)")
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
