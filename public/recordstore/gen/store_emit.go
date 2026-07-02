package gen

import (
	"fmt"
	"go/format"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
)

// storeComponent is the per-component emission model: the parsed plan plus
// the derived names and generation-time artefacts the template needs.
type storeComponent struct {
	Kind   string // Go type, e.g. "Identity"
	plan   *mappingplan.Plan
	groups []goplan.SectionGroup
	// filter is the baked ADR-0066 Filter artefact (presence prefilter AND
	// exact validator) identifying rows that carry a conforming component —
	// the WHERE body of the Scan verb.
	filter string
}

// secUse is one distinct tagged section a component set touches: its
// PascalCase method name and the decode-side reader variable.
type secUse struct {
	method string
	varN   string
}

// envelopeCol is one plain envelope column: its physical (encoded) name,
// its derived Go type, and the fixed DML setter the PlainItemTypeE lane
// provides.
type envelopeCol struct {
	physical string
	goType   string
}

// emitStore renders the store glue file: entity bag, builder (Add* over
// the generated <Kind>AddSections), verbs, cache fetcher and
// presence-gated decode — the shape pinned by the hand-written reference
// recordstore/example/device_store.go and its round-trip test.
//
// v1 scope (ADR-0100 S1): the Key role must be a u64 EntityId plain
// column; Order = EntityTimestamp; the state view emits only when an
// EntityLifecycle column exists. Component decode supports the scalar,
// scalar-single (unit) and slice-container shapes; other shapes
// (multi-sub-column, carriers, options, roaring, explode) are a
// generation-time error until the presence-gated read helpers move into
// marshallgen (S2).
func (inst Input) emitStore(ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, plans []*mappingplan.Plan) (code []byte, err error) {
	info := readback.NewInformationRetrieval(conv)
	err = info.LoadTable(ir, inst.RowConfig)
	if err != nil {
		err = eh.Errorf("load readback IR: %w", err)
		return
	}
	key, order, lifecycle, err := inst.envelope(info, conv)
	if err != nil {
		return
	}
	if key.physical == "" {
		err = eh.Errorf("schema has no EntityId plain column — the Key role is required")
		return
	}
	if order.physical == "" {
		err = eh.Errorf("schema has no EntityTimestamp plain column — Latest/Replay need the Order role")
		return
	}
	switch key.goType {
	case "uint64", "string":
		inst.keyGoType = key.goType
	default:
		err = eh.Errorf("Key column Go type %q not supported (uint64 and string are; ADR-0100 SD2)", key.goType)
		return
	}
	if order.goType != "time.Time" {
		err = eh.Errorf("Order column Go type %q not supported — Replay and the decode assume the z64 timestamp lane (DateTime64(9)); declare the EntityTimestamp column as ctabb.Z64", order.goType)
		return
	}
	stateView := lifecycle.physical != ""
	if stateView && lifecycle.goType != "uint8" {
		err = eh.Errorf("Lifecycle column Go type %q not supported — the state view assumes a u8 live/tombstone marker (ctabb.U8)", lifecycle.goType)
		return
	}

	comps := make([]storeComponent, 0, len(plans))
	for _, plan := range plans {
		var sc storeComponent
		sc, err = classifyComponent(plan, info)
		if err != nil {
			return
		}
		comps = append(comps, sc)
	}
	// Components must own disjoint sections: membership ids are assigned
	// per kind (each numbering from 1), so two kinds writing the same
	// section would alias each other's memberships — the presence-gated
	// decode and the baked Scan filters would silently cross-read them.
	sectionOwner := map[string]string{}
	for _, sc := range comps {
		for _, g := range sc.groups {
			if owner, taken := sectionOwner[g.Section]; taken && owner != sc.Kind {
				err = eh.Errorf("components %s and %s both bind section %q — components must own disjoint sections (ADR-0100 SD6)", owner, sc.Kind, g.Section)
				return
			}
			sectionOwner[g.Section] = sc.Kind
		}
	}

	var sb strings.Builder
	inst.emitStoreHeader(&sb, key, order, lifecycle, stateView)
	inst.emitEntityBag(&sb, comps, stateView)
	inst.emitStoreType(&sb)
	inst.emitBuilder(&sb, comps, stateView)
	err = inst.emitIngest(&sb, comps)
	if err != nil {
		return
	}
	inst.emitFlush(&sb)
	inst.emitCacheVerbs(&sb)
	inst.emitQueryVerbs(&sb, comps, stateView)
	err = inst.emitDecode(&sb, comps, stateView)
	if err != nil {
		return
	}

	raw := []byte(sb.String())
	code, err = format.Source(raw)
	if err != nil {
		err = eh.Errorf("gofmt rejected store output: %w; emitted:\n%s", err, string(raw))
	}
	return
}

// envelope finds the physical (encoded) names of the role-bearing plain
// columns by walking the Plan⋈IR join readback maintains. Each role binds
// at most once — a second matching column is a schema error, not a silent
// override (ADR-0100 SD2; explicit role configuration stays deferred).
func (inst Input) envelope(info *readback.InformationRetrieval, conv common.NamingConventionI) (key, order, lifecycle envelopeCol, err error) {
	bind := func(dst *envelopeCol, role string, physical string, ct canonicaltypes.PrimitiveAstNodeI) error {
		if dst.physical != "" {
			return eh.Errorf("plain columns %s and %s both carry the %s role — roles must be unambiguous (ADR-0100 SD2)", dst.physical, physical, role)
		}
		dst.physical = physical
		gt, _, _, derr := mappingplan.DeriveGoShape(ct)
		if derr != nil {
			return eh.Errorf("derive %s column Go type: %w", role, derr)
		}
		dst.goType = gt
		return nil
	}
	for cr := range info.IterateAll() {
		var itemType common.PlainItemTypeE
		itemType, err = conv.ExtractPlainItemType(cr.PhysicalColumn)
		if err != nil {
			// Not a plain column under this convention; skip.
			err = nil
			continue
		}
		switch itemType {
		case common.PlainItemTypeEntityId:
			err = bind(&key, "Key", cr.PhysicalColumn.String(), cr.CanonicalType)
		case common.PlainItemTypeEntityTimestamp:
			err = bind(&order, "Order", cr.PhysicalColumn.String(), cr.CanonicalType)
		case common.PlainItemTypeEntityLifecycle:
			err = bind(&lifecycle, "Lifecycle", cr.PhysicalColumn.String(), cr.CanonicalType)
		}
		if err != nil {
			return
		}
	}
	return
}

// classifyComponent validates a component against the store's decode
// coverage — exactly marshallgen's ReadRow gate, so the store generator
// and the codec emission cannot disagree — and bakes the component's
// ADR-0066 Filter artefact for the Scan verb.
func classifyComponent(plan *mappingplan.Plan, info *readback.InformationRetrieval) (sc storeComponent, err error) {
	sc.Kind = plan.KindType
	sc.plan = plan
	sc.groups = goplan.ComputeGroups(plan)
	if ok, reason := marshallgen.ReadRowSupported(plan); !ok {
		err = eh.Errorf("component %s: %s — <Kind>ReadRow is not emitted for this shape (ADR-0100 Deferred)", sc.Kind, reason)
		return
	}
	g := readback.NewGenerator(info, readback.NewLookupResolver(mapIdLookup(marshallgen.MembershipIds(plan))))
	artefacts, err := g.Generate(plan)
	if err != nil {
		err = eh.Errorf("component %s: generate read-back artefacts: %w", sc.Kind, err)
		return
	}
	sc.filter = artefacts.Filter
	return
}

// mapIdLookup adapts marshallgen's package-local membership-id assignment
// to the readback resolver's IdLookup.
type mapIdLookup map[string]uint64

func (inst mapIdLookup) LookupMembership(name string) (id uint64, err error) {
	id, ok := inst[name]
	if !ok {
		err = eh.Errorf("membership %q not found in the generated kind-id assignment", name)
	}
	return
}

// --- emission helpers. The emitted shapes mirror example/device_store.go. ---

func (inst Input) dmlType() string     { return "InEntity" + upperFirst(inst.TableName) + "Table" }
func (inst Input) raPrefix() string    { return "ReadAccess" + upperFirst(inst.TableName) + "Table" }
func (inst Input) entityType() string  { return inst.StoreName + "Entity" }
func (inst Input) storeType() string   { return inst.StoreName + "Store" }
func (inst Input) builderType() string { return inst.StoreName + "EntityBuilder" }

func (inst Input) emitStoreHeader(sb *strings.Builder, key, order, lifecycle envelopeCol, stateView bool) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// Code generated by github.com/stergiotis/boxer/public/recordstore/gen — DO NOT EDIT.")
	p("//")
	p("// %s composes the generated %s building blocks per ADR-0100:", inst.storeType(), inst.TableName)
	p("// append-only primitives plus batched cached retrieval%s.", map[bool]string{true: " and the state view", false: ""}[stateView])
	p("package %s", inst.PackageName)
	p("")
	p("import (")
	p("\t%q", "context")
	p("\t_ %q", "embed")
	p("\t%q", "iter")
	p("\t%q", "strconv")
	p("\t%q", "strings")
	p("\t%q", "time")
	p("")
	p("\t%q", "github.com/apache/arrow-go/v18/arrow")
	p("\t%q", "github.com/apache/arrow-go/v18/arrow/memory")
	p("\t%q", "github.com/stergiotis/boxer/public/caching")
	if inst.keyGoType == "string" {
		p("\t%q", "github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling")
	}
	p("\t%q", "github.com/stergiotis/boxer/public/functional")
	p("\t%q", "github.com/stergiotis/boxer/public/functional/option")
	p("\t%q", "github.com/stergiotis/boxer/public/observability/eh")
	p("\t%q", "github.com/stergiotis/boxer/public/recordstore")
	p("\traruntime %q", "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime")
	p(")")
	p("")
	p("//go:embed %s_ddl_clickhouse.out.sql", inst.TableName)
	p("var %sDDLColumnBody string", inst.TableName)
	p("")
	p("const %sTableName = %q", inst.TableName, inst.TableName)
	p("")
	p("// Physical (encoded) plain-column names, derived from the IR at")
	p("// generation time.")
	p("const (")
	p("\t%sColKey = `\"%s\"`", inst.TableName, key.physical)
	p("\t%sColOrder = `\"%s\"`", inst.TableName, order.physical)
	if stateView {
		p("\t%sColLifecycle = `\"%s\"`", inst.TableName, lifecycle.physical)
	}
	p(")")
	p("")
	p("// Default DDL tail: durable engine, Key leading ORDER BY (point-lookup")
	p("// guidance), Order second. Override via %sStoreConfig.DDLTail.", inst.StoreName)
	p("const %sDefaultDDLTail = \"ENGINE = MergeTree() ORDER BY (\" + %sColKey + \", \" + %sColOrder + \") SETTINGS allow_suspicious_low_cardinality_types=1\"",
		inst.TableName, inst.TableName, inst.TableName)
	p("")
	p("// Arrow output shape the read-access classes expect.")
	p("const %sArrowOutputSettings = \" SETTINGS output_format_arrow_string_as_string=1, output_format_arrow_low_cardinality_as_dictionary=0\"", inst.TableName)
	p("")
	p("// %sKeyLiteral renders a Key value as a ClickHouse SQL literal.", inst.TableName)
	if inst.keyGoType == "string" {
		p("func %sKeyLiteral(k string) string { return marshalling.EscapeString(k) }", inst.TableName)
	} else {
		p("func %sKeyLiteral(k uint64) string { return strconv.FormatUint(k, 10) }", inst.TableName)
	}
	p("")
	if stateView {
		p("const (")
		p("\t%sLifecycleLive uint8 = 0", inst.TableName)
		p("\t%sLifecycleTombstone uint8 = 1", inst.TableName)
		p(")")
		p("")
	}
}

func (inst Input) emitEntityBag(sb *strings.Builder, comps []storeComponent, stateView bool) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// %s is the entity bag (ADR-0100 SD5): the envelope plus one option", inst.entityType())
	p("// per bound component. Arrow-free — safe to hold in the cache.")
	p("type %s struct {", inst.entityType())
	p("\tID %s", inst.keyGoType)
	p("\tTs time.Time")
	if stateView {
		p("\tLifecycle uint8")
	}
	for _, c := range comps {
		p("\t%s option.Option[%s]", c.Kind, c.Kind)
	}
	p("}")
	p("")
	p("// Archetype reports which components the entity carries, in schema order.")
	p("func (inst *%s) Archetype() (a []string) {", inst.entityType())
	for _, c := range comps {
		p("\tif inst.%s.Has {", c.Kind)
		p("\t\ta = append(a, %q)", lowerFirst(c.Kind))
		p("\t}")
	}
	p("\treturn")
	p("}")
	p("")
}

func (inst Input) emitStoreType(sb *strings.Builder) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("type %sStoreConfig struct {", inst.StoreName)
	p("\t// DDLTail is appended after the generated column body at EnsureTable")
	p("\t// time (engine, ORDER BY, SETTINGS). Empty selects the default.")
	p("\tDDLTail string")
	p("\t// CacheCapacity is the L1 capacity of the read-through cache.")
	p("\tCacheCapacity int")
	p("\t// FetchCriteria are the cache's batch-flush thresholds.")
	p("\tFetchCriteria caching.FetchCriteria")
	p("}")
	p("")
	p("// %s is single-goroutine, like every part it composes.", inst.storeType())
	p("type %s[W comparable] struct {", inst.storeType())
	p("\texec recordstore.ExecutorI")
	p("\talloc memory.Allocator")
	p("\tcfg %sStoreConfig", inst.StoreName)
	p("\tdml *%s", inst.dmlType())
	p("\tbuffered int")
	p("\t// pending holds transferred-but-uninserted records after a failed")
	p("\t// Flush; the next Flush ships them (DiscardPending drops them).")
	p("\tpending []arrow.RecordBatch")
	p("\t// dirty tracks locally-written keys between Commit/Delete and the")
	p("\t// next successful Flush (or DiscardPending); the fetcher refuses to")
	p("\t// cache them so a fetch can never re-cache the pre-write row.")
	p("\tdirty map[%s]struct{}", inst.keyGoType)
	p("\tcache *caching.ReadThroughCache[%s, *%s, W]", inst.keyGoType, inst.entityType())
	p("}")
	p("")
	p("func New%s[W comparable](exec recordstore.ExecutorI, alloc memory.Allocator, cfg %sStoreConfig) (inst *%s[W]) {", inst.storeType(), inst.StoreName, inst.storeType())
	p("\tif alloc == nil {")
	p("\t\talloc = memory.NewGoAllocator()")
	p("\t}")
	p("\tif cfg.CacheCapacity <= 0 {")
	p("\t\tcfg.CacheCapacity = 1024")
	p("\t}")
	p("\tinst = &%s[W]{exec: exec, alloc: alloc, cfg: cfg, dml: New%s(alloc, 64), dirty: make(map[%s]struct{})}", inst.storeType(), inst.dmlType(), inst.keyGoType)
	p("\tinst.cache = caching.NewReadThroughCache[%s, *%s, W](cfg.CacheCapacity, inst, cfg.FetchCriteria)", inst.keyGoType, inst.entityType())
	p("\treturn")
	p("}")
	p("")
	p("// EnsureTable applies the generated DDL column body plus the configured")
	p("// tail. Idempotent (CREATE TABLE IF NOT EXISTS).")
	p("func (inst *%s[W]) EnsureTable(ctx context.Context) (err error) {", inst.storeType())
	p("\ttail := inst.cfg.DDLTail")
	p("\tif tail == \"\" {")
	p("\t\ttail = %sDefaultDDLTail", inst.TableName)
	p("\t}")
	p("\terr = inst.exec.Exec(ctx, %sDDLColumnBody+\" \"+tail)", inst.TableName)
	p("\tif err != nil {")
	p("\t\terr = eh.Errorf(\"ensure table %%s: %%w\", %sTableName, err)", inst.TableName)
	p("\t}")
	p("\treturn")
	p("}")
	p("")
}

func (inst Input) emitBuilder(sb *strings.Builder, comps []storeComponent, stateView bool) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// %s assembles one entity: envelope from Begin, components", inst.builderType())
	p("// via Add*, direct attribute manipulation via Raw, then Commit.")
	p("type %s[W comparable] struct {", inst.builderType())
	p("\tstore *%s[W]", inst.storeType())
	p("\tkey %s", inst.keyGoType)
	p("}")
	p("")
	p("// Begin opens one entity with the envelope roles as typed arguments")
	p("// (Key, Order)%s.", map[bool]string{true: " and a live lifecycle", false: ""}[stateView])
	p("func (inst *%s[W]) Begin(id %s, ts time.Time) *%s[W] {", inst.storeType(), inst.keyGoType, inst.builderType())
	if stateView {
		p("\tinst.dml.BeginEntity().SetId(id).SetTimestamp(ts).SetLifecycle(%sLifecycleLive)", inst.TableName)
	} else {
		p("\tinst.dml.BeginEntity().SetId(id).SetTimestamp(ts)")
	}
	p("\treturn &%s[W]{store: inst, key: id}", inst.builderType())
	p("}")
	p("")
	for _, c := range comps {
		p("// Add%s contributes the %s component to the open entity via the", c.Kind, c.Kind)
		p("// generated entity-frame-free section driver (ADR-0100 SD6).")
		p("func (inst *%s[W]) Add%s(row %s) *%s[W] {", inst.builderType(), c.Kind, c.Kind, inst.builderType())
		p("\terr := %sAddSections(inst.store.dml, row)", c.Kind)
		p("\tif err != nil {")
		p("\t\tinst.store.dml.AppendError(err)")
		p("\t}")
		p("\treturn inst")
		p("}")
		p("")
	}
	p("// Raw exposes the underlying DML entity for direct attribute")
	p("// manipulation within the same entity frame.")
	p("func (inst *%s[W]) Raw() *%s {", inst.builderType(), inst.dmlType())
	p("\treturn inst.store.dml")
	p("}")
	p("")
	p("// Commit finishes the open entity and buffers the row. The cache")
	p("// entry for the entity's key, if any, is invalidated — a local write")
	p("// never leaves a stale L1 value behind (external writers remain the")
	p("// caller's problem; see ADR-0100 Deferred). A failed Commit rolls the")
	p("// frame back — the entity is discarded and the store stays usable.")
	p("func (inst *%s[W]) Commit() (err error) {", inst.builderType())
	p("\terr = inst.store.dml.CommitEntity()")
	p("\tif err != nil {")
	p("\t\t_ = inst.store.dml.RollbackEntity() // no-op error when no frame is open")
	p("\t\treturn")
	p("\t}")
	p("\tinst.store.buffered++")
	p("\tinst.store.dirty[inst.key] = struct{}{}")
	p("\tinst.store.cache.Delete(inst.key)")
	p("\treturn")
	p("}")
	p("")
	p("// Rollback abandons the open entity frame without committing it;")
	p("// already-buffered rows and the store remain usable.")
	p("func (inst *%s[W]) Rollback() (err error) {", inst.builderType())
	p("\treturn inst.store.dml.RollbackEntity()")
	p("}")
	p("")
}

// emitIngest renders the per-kind whole-entity ingest verbs. A kind that
// does not bind the plain id column gets no Ingest verb (the Begin
// builder with an explicit key still ingests it); a kind whose id field
// type disagrees with the Key column is a generation error.
func (inst Input) emitIngest(sb *strings.Builder, comps []storeComponent) (err error) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	for _, c := range comps {
		idCol := goplan.FindPlainCol(c.plan, "id")
		if idCol == nil {
			p("// Ingest%s is not emitted: %s does not bind the plain id column;", c.Kind, c.Kind)
			p("// ingest rows through Begin(key, ts).Add%s(row).Commit() instead.", c.Kind)
			p("")
			continue
		}
		if gt := idCol.GoType(); gt != inst.keyGoType {
			err = eh.Errorf("component %s: plain id field %s has Go type %s but the Key column is %s — Ingest%s cannot be emitted", c.Kind, idCol.GoField, gt, inst.keyGoType, c.Kind)
			return
		}
		p("// Ingest%s appends one whole entity per row carrying only the", c.Kind)
		p("// %s component, all stamped with ts.", c.Kind)
		p("func (inst *%s[W]) Ingest%s(ts time.Time, rows []%s) (err error) {", inst.storeType(), c.Kind, c.Kind)
		p("\tfor i := range rows {")
		p("\t\terr = inst.Begin(rows[i].%s, ts).Add%s(rows[i]).Commit()", idCol.GoField, c.Kind)
		p("\t\tif err != nil {")
		p("\t\t\terr = eh.Errorf(\"ingest %s row %%d: %%w\", i, err)", lowerFirst(c.Kind))
		p("\t\t\treturn")
		p("\t\t}")
		p("\t}")
		p("\treturn")
		p("}")
		p("")
	}
	return
}

func (inst Input) emitFlush(sb *strings.Builder) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// Buffered reports the number of committed-but-unflushed rows.")
	p("func (inst *%s[W]) Buffered() int { return inst.buffered }", inst.storeType())
	p("")
	p("// Flush drains the buffered rows to ClickHouse (Arrow IPC, ADR-0089")
	p("// pivot). Rows are durable when Flush returns, engine permitting. On")
	p("// insert failure the transferred records are retained and the next")
	p("// Flush ships them — Flush is retryable; DiscardPending drops them")
	p("// instead. An open (uncommitted) entity frame makes Flush error.")
	p("func (inst *%s[W]) Flush(ctx context.Context) (n int, err error) {", inst.storeType())
	p("\tif inst.buffered == 0 && len(inst.pending) == 0 {")
	p("\t\treturn")
	p("\t}")
	p("\trecords, err := inst.dml.TransferRecords(nil)")
	p("\tif err != nil {")
	p("\t\terr = eh.Errorf(\"transfer records: %%w\", err)")
	p("\t\treturn")
	p("\t}")
	p("\trecords = append(inst.pending, records...)")
	p("\tinst.pending = nil")
	p("\tif len(records) > 0 {")
	p("\t\terr = inst.exec.InsertArrow(ctx, %sTableName, records)", inst.TableName)
	p("\t\tif err != nil {")
	p("\t\t\tinst.pending = records")
	p("\t\t\terr = eh.Errorf(\"insert into %%s: %%w\", %sTableName, err)", inst.TableName)
	p("\t\t\treturn")
	p("\t\t}")
	p("\t}")
	p("\tfor _, rec := range records {")
	p("\t\trec.Release()")
	p("\t}")
	p("\tn = inst.buffered")
	p("\tinst.buffered = 0")
	p("\tclear(inst.dirty) // flushed — ClickHouse now serves the written state")
	p("\treturn")
	p("}")
	p("")
	p("// DiscardPending drops every committed-but-unflushed row: records")
	p("// retained by a failed Flush, rows still in the DML builder, and an")
	p("// open (uncommitted) entity frame. It gives a failed Flush \"never")
	p("// happened\" semantics — ClickHouse state is the truth afterwards.")
	p("func (inst *%s[W]) DiscardPending() {", inst.storeType())
	p("\t_ = inst.dml.RollbackEntity() // no-op error when no frame is open")
	p("\tif records, err := inst.dml.TransferRecords(nil); err == nil {")
	p("\t\tfor _, rec := range records {")
	p("\t\t\trec.Release()")
	p("\t\t}")
	p("\t}")
	p("\tfor _, rec := range inst.pending {")
	p("\t\trec.Release()")
	p("\t}")
	p("\tinst.pending = nil")
	p("\tinst.buffered = 0")
	p("\tclear(inst.dirty) // nothing local remains — ClickHouse is the truth")
	p("}")
	p("")
}

func (inst Input) emitCacheVerbs(sb *strings.Builder) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// Get retrieves an entity by Key through the cache. A miss queues the")
	p("// key for the next batch fetch (the caching suspend/replay contract);")
	p("// Get is intended for immutable-by-key access — see ADR-0100 SD4.")
	p("func (inst *%s[W]) Get(key %s) (has bool, ent *%s) {", inst.storeType(), inst.keyGoType, inst.entityType())
	p("\treturn inst.cache.Get(key)")
	p("}")
	p("")
	p("// WorkItem marks the current work item for the cache's miss bookkeeping.")
	p("func (inst *%s[W]) WorkItem(w W) iter.Seq[functional.NilIteratorValueType] {", inst.storeType())
	p("\treturn inst.cache.WorkItem(w)")
	p("}")
	p("")
	p("// IterateReadyWorkItems flushes the queued keys when the fetch criteria")
	p("// are met and replays the work items that had misses.")
	p("func (inst *%s[W]) IterateReadyWorkItems(ctx context.Context) iter.Seq[W] {", inst.storeType())
	p("\treturn inst.cache.IterateReadyWorkItems(ctx)")
	p("}")
	p("")
	p("// IterateRestWorkItems forces a fetch of all queued keys and replays")
	p("// the pending work items.")
	p("func (inst *%s[W]) IterateRestWorkItems(ctx context.Context) iter.Seq[W] {", inst.storeType())
	p("\treturn inst.cache.IterateRestWorkItems(ctx)")
	p("}")
	p("")
	p("// AdvanceEpoch advances the cache's pinning epoch — call once per")
	p("// frame / batch so untouched L1 entries become evictable.")
	p("func (inst *%s[W]) AdvanceEpoch() {", inst.storeType())
	p("\tinst.cache.AdvanceEpoch()")
	p("}")
	p("")
	p("// DeterminePartition implements caching.ItemFetcherI. Single partition")
	p("// in v1 (one table, one server).")
	p("func (inst *%s[W]) DeterminePartition(key %s) uint64 { return 0 }", inst.storeType(), inst.keyGoType)
	p("")
	p("// FetchItemSinglePartition implements caching.ItemFetcherI: one batched")
	p("// point lookup per fetch. Duplicate versions collapse newest-first.")
	p("// Dirty keys (written locally, not yet flushed) are fetched but not")
	p("// cached — caching them would resurrect the pre-write row.")
	p("func (inst *%s[W]) FetchItemSinglePartition(ctx context.Context, partition uint64, keys []%s, target caching.ItemTargetI[%s, *%s]) (err error) {", inst.storeType(), inst.keyGoType, inst.keyGoType, inst.entityType())
	p("\tvar sb strings.Builder")
	p("\tsb.WriteString(\"SELECT * FROM \")")
	p("\tsb.WriteString(%sTableName)", inst.TableName)
	p("\tsb.WriteString(\" WHERE \" + %sColKey + \" IN (\")", inst.TableName)
	p("\tfor i, k := range keys {")
	p("\t\tif i > 0 {")
	p("\t\t\tsb.WriteByte(',')")
	p("\t\t}")
	p("\t\tsb.WriteString(%sKeyLiteral(k))", inst.TableName)
	p("\t}")
	p("\tsb.WriteString(\") ORDER BY \" + %sColOrder + \" DESC LIMIT 1 BY \" + %sColKey)", inst.TableName, inst.TableName)
	p("\tsb.WriteString(%sArrowOutputSettings)", inst.TableName)
	p("\tents, err := inst.queryEntities(ctx, sb.String())")
	p("\tif err != nil {")
	p("\t\treturn")
	p("\t}")
	p("\tfor _, ent := range ents {")
	p("\t\tif _, d := inst.dirty[ent.ID]; d {")
	p("\t\t\tcontinue")
	p("\t\t}")
	p("\t\ttarget.AddItem(ent.ID, ent)")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
}

func (inst Input) emitQueryVerbs(sb *strings.Builder, comps []storeComponent, stateView bool) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }

	// Scan (ADR-0100 SD4 / ADR-0066): per component, the Filter artefact —
	// presence prefilter AND exact validator, membership ids baked as SQL
	// literals at generation time — is the WHERE body.
	p("// Baked ADR-0066 Filter artefacts: rows carrying a conforming")
	p("// component. Generated from Plan ⋈ IR; membership ids are literals.")
	p("const (")
	for _, c := range comps {
		p("\t%sScan%sFilter = %q", inst.TableName, c.Kind, c.filter)
	}
	p(")")
	p("")
	for _, c := range comps {
		p("// Scan%s returns the entities whose rows carry a conforming %s", c.Kind, c.Kind)
		p("// component, ordered by (Order, Key) — deterministic across ties.")
		p("// extraPredicate (raw SQL over the physical columns; empty for none)")
		p("// further restricts the scan. The Filter artefact uses ClickHouse")
		p("// built-ins only, so this is a single SELECT — no helper UDFs, no")
		p("// multi-statement script (the ExecutorI contract).")
		p("func (inst *%s[W]) Scan%s(ctx context.Context, extraPredicate string) (ents []*%s, err error) {", inst.storeType(), c.Kind, inst.entityType())
		p("\twhere := %sScan%sFilter", inst.TableName, c.Kind)
		p("\tif extraPredicate != \"\" {")
		p("\t\twhere = \"(\" + where + \") AND (\" + extraPredicate + \")\"")
		p("\t}")
		p("\tsql := \"SELECT * FROM \" + %sTableName +", inst.TableName)
		p("\t\t\" WHERE \" + where +")
		p("\t\t\" ORDER BY \" + %sColOrder + \" ASC, \" + %sColKey + \" ASC\" + %sArrowOutputSettings", inst.TableName, inst.TableName, inst.TableName)
		p("\treturn inst.queryEntities(ctx, sql)")
		p("}")
		p("")
	}
	p("// Latest returns the newest row for key, tombstone-blind (the raw")
	p("// primitive).")
	p("func (inst *%s[W]) Latest(ctx context.Context, key %s) (ent *%s, found bool, err error) {", inst.storeType(), inst.keyGoType, inst.entityType())
	p("\tsql := \"SELECT * FROM \" + %sTableName +", inst.TableName)
	p("\t\t\" WHERE \" + %sColKey + \" = \" + %sKeyLiteral(key) +", inst.TableName, inst.TableName)
	p("\t\t\" ORDER BY \" + %sColOrder + \" DESC LIMIT 1\" + %sArrowOutputSettings", inst.TableName, inst.TableName)
	p("\tents, err := inst.queryEntities(ctx, sql)")
	p("\tif err != nil || len(ents) == 0 {")
	p("\t\treturn")
	p("\t}")
	p("\tent = ents[0]")
	p("\tfound = true")
	p("\treturn")
	p("}")
	p("")
	p("// Replay returns the rows for key with the order column >= fromOrder in")
	p("// ascending order — the event-replay primitive. Buffered in v1.")
	p("func (inst *%s[W]) Replay(ctx context.Context, key %s, fromOrder time.Time) (ents []*%s, err error) {", inst.storeType(), inst.keyGoType, inst.entityType())
	p("\tsql := \"SELECT * FROM \" + %sTableName +", inst.TableName)
	p("\t\t\" WHERE \" + %sColKey + \" = \" + %sKeyLiteral(key) +", inst.TableName, inst.TableName)
	p("\t\t\" AND \" + %sColOrder + \" >= fromUnixTimestamp64Nano(\" + strconv.FormatInt(fromOrder.UnixNano(), 10) + \")\" +", inst.TableName)
	p("\t\t\" ORDER BY \" + %sColOrder + \" ASC\" + %sArrowOutputSettings", inst.TableName, inst.TableName)
	p("\treturn inst.queryEntities(ctx, sql)")
	p("}")
	p("")
	if !stateView {
		return
	}
	p("// --- state view (Put / Delete / GetLatest; ADR-0100 SD4). ---")
	p("")
	p("// Put appends a new version of the entity — Begin under its state-view")
	p("// name.")
	p("func (inst *%s[W]) Put(id %s, ts time.Time) *%s[W] {", inst.storeType(), inst.keyGoType, inst.builderType())
	p("\treturn inst.Begin(id, ts)")
	p("}")
	p("")
	p("// Delete appends a tombstone row for id (no components; lifecycle marks")
	p("// the deletion) and invalidates the cache entry. GetLatest reads it as")
	p("// absent.")
	p("func (inst *%s[W]) Delete(id %s, ts time.Time) (err error) {", inst.storeType(), inst.keyGoType)
	p("\tinst.dml.BeginEntity().SetId(id).SetTimestamp(ts).SetLifecycle(%sLifecycleTombstone)", inst.TableName)
	p("\terr = inst.dml.CommitEntity()")
	p("\tif err != nil {")
	p("\t\t_ = inst.dml.RollbackEntity() // discard the failed frame; the store stays usable")
	p("\t\treturn")
	p("\t}")
	p("\tinst.buffered++")
	p("\tinst.dirty[id] = struct{}{}")
	p("\tinst.cache.Delete(id)")
	p("\treturn")
	p("}")
	p("")
	p("// GetLatest is Latest plus tombstone interpretation: newest row wins, a")
	p("// tombstone reads as absent. Uncached in v1 (ADR-0100 Deferred).")
	p("func (inst *%s[W]) GetLatest(ctx context.Context, key %s) (ent *%s, found bool, err error) {", inst.storeType(), inst.keyGoType, inst.entityType())
	p("\tent, found, err = inst.Latest(ctx, key)")
	p("\tif err != nil || !found {")
	p("\t\treturn")
	p("\t}")
	p("\tif ent.Lifecycle == %sLifecycleTombstone {", inst.TableName)
	p("\t\tent = nil")
	p("\t\tfound = false")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
}

func (inst Input) emitDecode(sb *strings.Builder, comps []storeComponent, stateView bool) (err error) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	ra := inst.raPrefix()

	// Collect the distinct tagged sections the components use.
	seen := map[string]secUse{}
	order := []string{}
	for _, c := range comps {
		for _, g := range c.groups {
			m := mappingplan.UpperFirst(g.Section)
			if _, ok := seen[m]; !ok {
				seen[m] = secUse{method: m, varN: lowerFirst(m) + "R"}
				order = append(order, m)
			}
		}
	}

	p("// --- decode (Arrow → entity bags). ---")
	p("")
	p("// %sSectionReaderI is the uniform slice of the generated read-access", inst.TableName)
	p("// readers.")
	p("type %sSectionReaderI interface {", inst.TableName)
	p("\tGetColumnIndices() []uint32")
	p("\tSetColumnIndices([]uint32) []uint32")
	p("\tLoadFromRecord(raruntime.RecordI) error")
	p("\tRelease()")
	p("}")
	p("")
	p("func (inst *%s[W]) queryEntities(ctx context.Context, sql string) (ents []*%s, err error) {", inst.storeType(), inst.entityType())
	p("\trecords, err := inst.exec.QueryArrow(ctx, sql)")
	p("\tif err != nil {")
	p("\t\terr = eh.Errorf(\"query entities: %%w\", err)")
	p("\t\treturn")
	p("\t}")
	p("\tdefer func() {")
	p("\t\tfor _, rec := range records {")
	p("\t\t\trec.Release()")
	p("\t\t}")
	p("\t}()")
	p("\tfor _, rec := range records {")
	p("\t\tvar batch []*%s", inst.entityType())
	p("\t\tbatch, err = decode%sRecord(rec)", inst.StoreName)
	p("\t\tif err != nil {")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\tents = append(ents, batch...)")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// decode%sRecord reads one fetched Arrow record into entity bags:", inst.StoreName)
	p("// envelope from the plain readers, components via presence-gated,")
	p("// membership-matched reads (fat rows carry optional components — the")
	p("// kind-homogeneous helpers cannot decode them).")
	p("func decode%sRecord(rec arrow.RecordBatch) (ents []*%s, err error) {", inst.StoreName, inst.entityType())
	p("\tidR := New%sPlainEntityIdAttributes()", ra)
	p("\ttsR := New%sPlainEntityTimestampAttributes()", ra)
	if stateView {
		p("\tlcR := New%sPlainEntityLifecycleAttributes()", ra)
	}
	for _, m := range order {
		p("\t%s := New%sTagged%s()", seen[m].varN, ra, m)
	}
	readerVars := []string{"idR", "tsR"}
	if stateView {
		readerVars = append(readerVars, "lcR")
	}
	for _, m := range order {
		readerVars = append(readerVars, seen[m].varN)
	}
	p("\treaders := []%sSectionReaderI{%s}", inst.TableName, strings.Join(readerVars, ", "))
	p("\tfor _, r := range readers {")
	p("\t\tr.SetColumnIndices(r.GetColumnIndices())")
	p("\t\terr = r.LoadFromRecord(rec)")
	p("\t\tif err != nil {")
	p("\t\t\terr = eh.Errorf(\"load %s reader from record: %%w\", err)", inst.TableName)
	p("\t\t\treturn")
	p("\t\t}")
	p("\t}")
	p("\tdefer func() {")
	p("\t\tfor _, r := range readers {")
	p("\t\t\tr.Release()")
	p("\t\t}")
	p("\t}()")
	p("")
	p("\tn := idR.ValueId.Len()")
	p("\ttsType, ok := tsR.ValueTs.DataType().(*arrow.TimestampType)")
	p("\tif !ok {")
	p("\t\terr = eh.Errorf(\"order column is not a timestamp (got %%s)\", tsR.ValueTs.DataType())")
	p("\t\treturn")
	p("\t}")
	p("\tents = make([]*%s, 0, n)", inst.entityType())
	p("\tfor i := range n {")
	p("\t\tent := &%s{", inst.entityType())
	p("\t\t\tID: idR.ValueId.Value(i),")
	p("\t\t\tTs: tsR.ValueTs.Value(i).ToTime(tsType.Unit).UTC(),")
	if stateView {
		p("\t\t\tLifecycle: lcR.ValueLifecycle.Value(i),")
	}
	p("\t\t}")
	// One presence-gated <Kind>ReadRow call per component (the generated
	// twin of FillFromArrow; the Attrs/Membs readers bind by inference).
	// Fields bound to plain columns come from the envelope afterwards.
	for _, c := range comps {
		args := make([]string, 0, 2*len(c.groups)+1)
		args = append(args, "i")
		for _, g := range c.groups {
			rv := seen[mappingplan.UpperFirst(g.Section)].varN
			args = append(args, rv+".GetAttributes()", rv+".GetMemberships()")
		}
		p("\t\t{")
		p("\t\t\trow, ok, e := %sReadRow(%s)", c.Kind, strings.Join(args, ", "))
		p("\t\t\tif e != nil {")
		p("\t\t\t\terr = eh.Errorf(\"read %s component: %%w\", e)", lowerFirst(c.Kind))
		p("\t\t\t\treturn")
		p("\t\t\t}")
		p("\t\t\tif ok {")
		if idCol := goplan.FindPlainCol(c.plan, "id"); idCol != nil {
			p("\t\t\t\trow.%s = ent.ID", idCol.GoField)
		}
		if tsCol := goplan.FindPlainCol(c.plan, "ts"); tsCol != nil {
			p("\t\t\t\trow.%s = ent.Ts", tsCol.GoField)
		}
		p("\t\t\t\tent.%s = option.Some(row)", c.Kind)
		p("\t\t\t}")
		p("\t\t}")
	}
	p("\t\tents = append(ents, ent)")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	return
}

func upperFirst(s string) string { return mappingplan.UpperFirst(s) }

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
