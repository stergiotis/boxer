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

// emitter carries Input plus the emission-time facts derived from the
// schema — scratch state stays off the public config struct.
type emitter struct {
	Input
	// keyGoType is the Key column's derived Go type ("uint64" or
	// "string").
	keyGoType string
}

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

// secUse is one distinct tagged section a component set touches: the
// decode-side reader variable it binds to.
type secUse struct {
	varN string
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
// presence-gated decode — the shape pinned by the recordstore/example
// round-trip test.
//
// Role gates (ADR-0100 SD2): Key = EntityId (u64 or string), Order =
// EntityTimestamp (z64); the state view emits only when an
// EntityLifecycle (u8) column exists; any other plain column is a
// generation error — pass-through envelope fields are deferred (ADR-0100
// Update 2026-07-04). Component decode coverage is gated by
// marshallgen.ReadRowSupported (carrier channels and exploded fields
// remain uncovered).
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

	em := emitter{Input: inst, keyGoType: key.goType}
	var sb strings.Builder
	em.emitStoreHeader(&sb, key, order, lifecycle, stateView)
	em.emitEntityBag(&sb, comps, stateView)
	em.emitStoreType(&sb)
	em.emitBuilder(&sb, comps, stateView)
	err = em.emitIngest(&sb, comps)
	if err != nil {
		return
	}
	em.emitFlush(&sb)
	em.emitCacheView(&sb, stateView)
	em.emitQueryVerbs(&sb, comps, stateView)
	em.emitDecode(&sb, comps, stateView)

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
		case common.PlainItemTypeNone:
			// Tagged and other non-plain columns report None under this
			// convention; plain columns always carry a concrete item type
			// (the IR defines plain-ness as != None).
		default:
			// A non-role plain column would be silently zero-written by
			// every Begin and dropped by the decode — refuse until
			// pass-through envelope fields exist (ADR-0100 Update
			// 2026-07-04).
			err = eh.Errorf("plain column %s carries item type %v — only the role-bearing EntityId/EntityTimestamp/EntityLifecycle plain columns are supported; pass-through envelope fields are deferred (ADR-0100 Update 2026-07-04)", cr.PhysicalColumn.String(), itemType)
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

func (inst Input) dmlType() string         { return "InEntity" + upperFirst(inst.TableName) + "Table" }
func (inst Input) raPrefix() string        { return "ReadAccess" + upperFirst(inst.TableName) + "Table" }
func (inst Input) entityType() string      { return inst.StoreName + "Entity" }
func (inst Input) storeType() string       { return inst.StoreName + "Store" }
func (inst Input) builderType() string     { return inst.StoreName + "EntityBuilder" }
func (inst Input) cacheType() string       { return inst.StoreName + "Cache" }
func (inst Input) cacheConfigType() string { return inst.StoreName + "CacheConfig" }

// lowQ qualifies DML/RA scaffolding references from the store file:
// empty in the Flat layout, "lowlevel." otherwise.
func (inst Input) lowQ() string {
	if inst.Flat {
		return ""
	}
	return "lowlevel."
}

func (inst emitter) emitStoreHeader(sb *strings.Builder, key, order, lifecycle envelopeCol, stateView bool) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// Code generated by github.com/stergiotis/boxer/public/storage/recordstore/gen — DO NOT EDIT.")
	p("//")
	p("// %s composes the generated %s building blocks per ADR-0100:", inst.storeType(), inst.TableName)
	p("// append-only primitives and iterator query verbs%s; batched", map[bool]string{true: " plus the state view", false: ""}[stateView])
	p("// cached retrieval is the attachable %s view. Reads see only", inst.cacheType())
	p("// flushed rows: buffered writes are invisible until Flush returns.")
	p("")
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
	p("\t%q", "github.com/apache/arrow-go/v18/arrow/array")
	p("\t%q", "github.com/apache/arrow-go/v18/arrow/memory")
	p("\t%q", "github.com/stergiotis/boxer/public/caching")
	if inst.keyGoType == "string" {
		p("\t%q", "github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling")
	}
	p("\t%q", "github.com/stergiotis/boxer/public/functional")
	p("\t%q", "github.com/stergiotis/boxer/public/functional/option")
	p("\t%q", "github.com/stergiotis/boxer/public/observability/eh")
	p("\t%q", "github.com/stergiotis/boxer/public/storage/recordstore")
	if !inst.Flat {
		p("\t%q", inst.ImportPath+"/internal/lowlevel")
	}
	p("\traruntime %q", "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime")
	p(")")
	p("")
	p("// The complete CREATE TABLE composed at generation time through the")
	p("// ADR-0102 table-clause seam (engine, ORDER BY over the envelope")
	p("// roles, indexes, settings — physical names resolved via the IR).")
	p("//go:embed %s_ddl_clickhouse.out.sql", inst.TableName)
	p("var %sDDLCreate string", inst.TableName)
	p("")
	p("// %sTableName is the ClickHouse table this store binds.", inst.StoreName)
	p("const %sTableName = %q", inst.StoreName, inst.TableName)
	p("")
	p("// Physical (encoded, quoted) names of the envelope role columns,")
	p("// derived from the IR at generation time — exported so consumers can")
	p("// address them in ScanOpts.ExtraPredicate and their own SQL.")
	p("const (")
	p("\t%sColKey = `\"%s\"`", inst.StoreName, key.physical)
	p("\t%sColOrder = `\"%s\"`", inst.StoreName, order.physical)
	if stateView {
		p("\t%sColLifecycle = `\"%s\"`", inst.StoreName, lifecycle.physical)
	}
	p(")")
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
}

func (inst emitter) emitEntityBag(sb *strings.Builder, comps []storeComponent, stateView bool) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// %s is the entity bag (ADR-0100 SD5): the envelope plus one option", inst.entityType())
	p("// per bound component. Arrow-free — safe to hold in the cache.")
	p("// Entities returned by cached reads are shared with the cache (and")
	p("// every later reader): treat them as immutable.")
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
	if stateView {
		p("// IsTombstone reports whether this row is a state-view deletion")
		p("// marker — what the tombstone-blind verbs (Latest, Replay, the")
		p("// cache's Get) hand back for a deleted key.")
		p("func (inst *%s) IsTombstone() bool {", inst.entityType())
		p("\treturn inst.Lifecycle == recordstore.LifecycleTombstone")
		p("}")
		p("")
	}
}

func (inst emitter) emitStoreType(sb *strings.Builder) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("type %sStoreConfig struct {", inst.StoreName)
	p("\t// DDLTail is a raw suffix appended verbatim after the composed")
	p("\t// CREATE TABLE at EnsureTable time — the escape hatch for clauses")
	p("\t// the generation-time table options (ADR-0102) do not carry.")
	p("\tDDLTail string")
	p("}")
	p("")
	p("// %s is single-goroutine, like every part it composes. Batched", inst.storeType())
	p("// cached retrieval is not built in — attach a %s view.", inst.cacheType())
	p("type %s struct {", inst.storeType())
	p("\texec recordstore.ExecutorI")
	p("\talloc memory.Allocator")
	p("\tcfg %sStoreConfig", inst.StoreName)
	p("\tdml *%s%s", inst.lowQ(), inst.dmlType())
	p("\tbuffered int")
	p("\t// pending holds transferred-but-uninserted records after a failed")
	p("\t// Flush; the next Flush ships them (DiscardPending drops them).")
	p("\tpending []arrow.RecordBatch")
	p("\t// dirty tracks locally-written keys between Commit/Delete and the")
	p("\t// next successful Flush (or DiscardPending); attached cache views")
	p("\t// refuse to cache them so a fetch can never re-cache the pre-write")
	p("\t// row.")
	p("\tdirty map[%s]struct{}", inst.keyGoType)
	p("\t// onWrite holds the local-write invalidation hooks of attached")
	p("\t// cache views (New%s registers one per view).", inst.cacheType())
	p("\tonWrite []func(%s)", inst.keyGoType)
	p("}")
	p("")
	p("// New%s wires the store. A nil alloc selects the Go allocator.", inst.storeType())
	p("func New%s(exec recordstore.ExecutorI, alloc memory.Allocator, cfg %sStoreConfig) (inst *%s) {", inst.storeType(), inst.StoreName, inst.storeType())
	p("\tif alloc == nil {")
	p("\t\talloc = memory.NewGoAllocator()")
	p("\t}")
	p("\tinst = &%s{exec: exec, alloc: alloc, cfg: cfg, dml: %sNew%s(alloc, 64), dirty: make(map[%s]struct{})}", inst.storeType(), inst.lowQ(), inst.dmlType(), inst.keyGoType)
	p("\treturn")
	p("}")
	p("")
	p("// invalidate fires the registered local-write hooks so attached cache")
	p("// views evict the key — a local write never leaves a stale L1 value")
	p("// behind.")
	p("func (inst *%s) invalidate(key %s) {", inst.storeType(), inst.keyGoType)
	p("\tfor _, f := range inst.onWrite {")
	p("\t\tf(key)")
	p("\t}")
	p("}")
	p("")
	p("// EnsureTable applies the composed CREATE TABLE (plus the DDLTail")
	p("// suffix, when configured). Idempotent (CREATE TABLE IF NOT EXISTS).")
	p("func (inst *%s) EnsureTable(ctx context.Context) (err error) {", inst.storeType())
	p("\tsql := %sDDLCreate", inst.TableName)
	p("\tif inst.cfg.DDLTail != \"\" {")
	p("\t\tsql += \" \" + inst.cfg.DDLTail")
	p("\t}")
	p("\terr = inst.exec.Exec(ctx, sql)")
	p("\tif err != nil {")
	p("\t\terr = eh.Errorf(\"ensure table %%s: %%w\", %sTableName, err)", inst.StoreName)
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// VerifySchema compares the live table's columns — names and order —")
	p("// against the generated schema. EnsureTable alone cannot detect drift")
	p("// on an existing table (IF NOT EXISTS succeeds against any old shape),")
	p("// and the decode is positional, so drift fails late or, for same-typed")
	p("// column swaps, silently: run VerifySchema at startup after")
	p("// EnsureTable.")
	p("func (inst *%s) VerifySchema(ctx context.Context) (err error) {", inst.storeType())
	p("\tlive := make([]string, 0, 64)")
	p("\tfor rec, rerr := range inst.exec.QueryArrow(ctx, \"DESCRIBE TABLE \"+%sTableName+%sArrowOutputSettings) {", inst.StoreName, inst.TableName)
	p("\t\tif rerr != nil {")
	p("\t\t\terr = eh.Errorf(\"describe table %%s: %%w\", %sTableName, rerr)", inst.StoreName)
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\tnames, ok := rec.Column(0).(*array.String)")
	p("\t\tif !ok {")
	p("\t\t\terr = eh.Errorf(\"describe table %%s: name column is %%s, not a string\", %sTableName, rec.Column(0).DataType())", inst.StoreName)
	p("\t\t\trec.Release()")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\tfor i := range int(rec.NumRows()) {")
	p("\t\t\tlive = append(live, names.Value(i))")
	p("\t\t}")
	p("\t\trec.Release()")
	p("\t}")
	p("\twant := %sCreateSchema%sTable().Fields()", inst.lowQ(), upperFirst(inst.TableName))
	p("\tif len(live) != len(want) {")
	p("\t\terr = eh.Errorf(\"schema drift on %%s: table has %%d columns, the generated schema expects %%d — regenerated code against an old table (or vice versa); migrate or regenerate\", %sTableName, len(live), len(want))", inst.StoreName)
	p("\t\treturn")
	p("\t}")
	p("\tfor i, f := range want {")
	p("\t\tif live[i] != f.Name {")
	p("\t\t\terr = eh.Errorf(\"schema drift on %%s: column %%d is %%q, the generated schema expects %%q — the decode is positional; migrate or regenerate\", %sTableName, i, live[i], f.Name)", inst.StoreName)
	p("\t\t\treturn")
	p("\t\t}")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
}

func (inst emitter) emitBuilder(sb *strings.Builder, comps []storeComponent, stateView bool) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// %s assembles one entity: envelope from Begin, components", inst.builderType())
	p("// via Add*, direct attribute manipulation via Raw, then Commit.")
	p("type %s struct {", inst.builderType())
	p("\tstore *%s", inst.storeType())
	p("\tkey %s", inst.keyGoType)
	p("}")
	p("")
	p("// Begin opens one entity with the envelope roles as typed arguments")
	p("// (Key, Order)%s.", map[bool]string{true: " and a live lifecycle", false: ""}[stateView])
	p("func (inst *%s) Begin(id %s, ts time.Time) *%s {", inst.storeType(), inst.keyGoType, inst.builderType())
	if stateView {
		p("\tinst.dml.BeginEntity().SetId(id).SetTimestamp(ts).SetLifecycle(recordstore.LifecycleLive)")
	} else {
		p("\tinst.dml.BeginEntity().SetId(id).SetTimestamp(ts)")
	}
	p("\treturn &%s{store: inst, key: id}", inst.builderType())
	p("}")
	p("")
	for _, c := range comps {
		p("// Add%s contributes the %s component to the open entity via the", c.Kind, c.Kind)
		p("// generated entity-frame-free section driver (ADR-0100 SD6).")
		p("func (inst *%s) Add%s(row %s) *%s {", inst.builderType(), c.Kind, c.Kind, inst.builderType())
		p("\terr := %sAddSections(inst.store.dml, row)", c.Kind)
		p("\tif err != nil {")
		p("\t\tinst.store.dml.AppendError(err)")
		p("\t}")
		p("\treturn inst")
		p("}")
		p("")
	}
	p("// Raw exposes the underlying DML entity for direct attribute")
	p("// manipulation within the same entity frame.%s", map[bool]string{true: "", false: " The type lives in"}[inst.Flat])
	if !inst.Flat {
		p("// internal/lowlevel: callers outside the generated package hold the")
		p("// returned value by inference (raw := b.Raw()) and chain its")
		p("// methods, but cannot name the type in their own signatures.")
	}
	p("func (inst *%s) Raw() *%s%s {", inst.builderType(), inst.lowQ(), inst.dmlType())
	p("\treturn inst.store.dml")
	p("}")
	p("")
	p("// Commit finishes the open entity and buffers the row. Attached cache")
	p("// views evict the entity's key — a local write never leaves a stale")
	p("// L1 value behind (external writers remain the caller's problem; see")
	p("// ADR-0100 Deferred). A failed Commit rolls the frame back — the")
	p("// entity is discarded and the store stays usable.")
	p("func (inst *%s) Commit() (err error) {", inst.builderType())
	p("\terr = inst.store.dml.CommitEntity()")
	p("\tif err != nil {")
	p("\t\t_ = inst.store.dml.RollbackEntity() // no-op error when no frame is open")
	p("\t\treturn")
	p("\t}")
	p("\tinst.store.buffered++")
	p("\tinst.store.dirty[inst.key] = struct{}{}")
	p("\tinst.store.invalidate(inst.key)")
	p("\treturn")
	p("}")
	p("")
	p("// Rollback abandons the open entity frame without committing it;")
	p("// already-buffered rows and the store remain usable.")
	p("func (inst *%s) Rollback() (err error) {", inst.builderType())
	p("\treturn inst.store.dml.RollbackEntity()")
	p("}")
	p("")
}

// emitIngest renders the per-kind whole-entity ingest verbs. A kind that
// does not bind the plain id column gets no Ingest verb (the Begin
// builder with an explicit key still ingests it); a kind whose id field
// type disagrees with the Key column is a generation error.
func (inst emitter) emitIngest(sb *strings.Builder, comps []storeComponent) (err error) {
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
		p("// Ingest%s buffers one whole entity per row carrying only the", c.Kind)
		p("// %s component, all stamped with ts — rows ship on the next Flush,", c.Kind)
		p("// like every write. Keys must be distinct within one call (rows")
		p("// share ts, so duplicates would tie on Order): a duplicate returns")
		p("// recordstore.ErrDuplicateIngestKey. On any error the rows buffered")
		p("// so far remain buffered — Flush ships them, DiscardPending drops")
		p("// them.")
		p("func (inst *%s) Ingest%s(ts time.Time, rows []%s) (err error) {", inst.storeType(), c.Kind, c.Kind)
		p("\tseen := make(map[%s]struct{}, len(rows))", inst.keyGoType)
		p("\tfor i := range rows {")
		p("\t\tif _, dup := seen[rows[i].%s]; dup {", idCol.GoField)
		p("\t\t\terr = eh.Errorf(\"ingest %s row %%d: %%w: key %%v\", i, recordstore.ErrDuplicateIngestKey, rows[i].%s)", lowerFirst(c.Kind), idCol.GoField)
		p("\t\t\treturn")
		p("\t\t}")
		p("\t\tseen[rows[i].%s] = struct{}{}", idCol.GoField)
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

func (inst emitter) emitFlush(sb *strings.Builder) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// Buffered reports the number of committed-but-unflushed rows.")
	p("func (inst *%s) Buffered() int { return inst.buffered }", inst.storeType())
	p("")
	p("// Flush drains the buffered rows to ClickHouse (Arrow IPC, ADR-0089")
	p("// pivot). Rows are durable when Flush returns, engine permitting. On")
	p("// insert failure the transferred records are retained and the next")
	p("// Flush ships them — Flush is retryable; DiscardPending drops them")
	p("// instead. An open (uncommitted) entity frame makes Flush error.")
	p("func (inst *%s) Flush(ctx context.Context) (n int, err error) {", inst.storeType())
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
	p("\t\terr = inst.exec.InsertArrow(ctx, %sTableName, records)", inst.StoreName)
	p("\t\tif err != nil {")
	p("\t\t\tinst.pending = records")
	p("\t\t\terr = eh.Errorf(\"insert into %%s: %%w\", %sTableName, err)", inst.StoreName)
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
	p("func (inst *%s) DiscardPending() {", inst.storeType())
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
	p("// Close discards everything unflushed and releases the store's Arrow")
	p("// builder; the store must not be used afterwards. Required for a")
	p("// clean shutdown under tracking/checked allocators — the default Go")
	p("// allocator needs no Close.")
	p("func (inst *%s) Close() {", inst.storeType())
	p("\tinst.DiscardPending()")
	p("\tinst.dml.Builder().Release()")
	p("}")
	p("")
}

func (inst emitter) emitCacheView(sb *strings.Builder, stateView bool) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// %s parameterizes an attached read-through cache view.", inst.cacheConfigType())
	p("type %s struct {", inst.cacheConfigType())
	p("\t// Capacity is the L1 capacity in entries, not bytes — budget")
	p("\t// memory as Capacity × the largest expected entity payload. Zero")
	p("\t// or negative selects the default (1024).")
	p("\tCapacity int")
	p("\t// FetchCriteria are the cache's batch-flush thresholds.")
	p("\tFetchCriteria caching.FetchCriteria")
	p("}")
	p("")
	p("// %s is the batched read-through KV view over a %s", inst.cacheType(), inst.storeType())
	p("// (ADR-0100 SD5): misses queue under work items and flush as one")
	p("// IN (…) lookup. Get is exact for immutable-by-key access — see SD4.")
	p("// Local writes on the store evict the written key from every attached")
	p("// view, so under this process's single writer the cached entries are")
	p("// also the latest state; only EXTERNAL writers can leave the view")
	p("// stale, and they need a caller-provided signal: MarkStale /")
	p("// Invalidate / InvalidateAll (a freshness TTL is a recorded")
	p("// follow-up). Like the store it wraps, a view is single-goroutine.")
	p("// W is the work-item type (use struct{} when the suspend/replay")
	p("// machinery is not needed).")
	p("type %s[W comparable] struct {", inst.cacheType())
	p("\tst *%s", inst.storeType())
	p("\tcfg %s", inst.cacheConfigType())
	p("\tcache *caching.ReadThroughCache[%s, *%s, W]", inst.keyGoType, inst.entityType())
	p("}")
	p("")
	p("// New%s attaches a read-through cache view to st, registering its", inst.cacheType())
	p("// local-write invalidation hook with the store. Views attach for the")
	p("// store's lifetime — there is no detach.")
	p("func New%s[W comparable](st *%s, cfg %s) (inst *%s[W]) {", inst.cacheType(), inst.storeType(), inst.cacheConfigType(), inst.cacheType())
	p("\tif cfg.Capacity <= 0 {")
	p("\t\tcfg.Capacity = 1024")
	p("\t}")
	p("\tinst = &%s[W]{st: st, cfg: cfg}", inst.cacheType())
	p("\tinst.rebuild()")
	p("\t// The hook closes over the view, not the cache instance, so it")
	p("\t// stays correct across InvalidateAll's rebuild.")
	p("\tst.onWrite = append(st.onWrite, func(k %s) { inst.cache.Delete(k) })", inst.keyGoType)
	p("\treturn")
	p("}")
	p("")
	p("func (inst *%s[W]) rebuild() {", inst.cacheType())
	p("\tinst.cache = caching.NewReadThroughCache[%s, *%s, W](inst.cfg.Capacity, &%sFetcher{st: inst.st}, inst.cfg.FetchCriteria)", inst.keyGoType, inst.entityType(), inst.TableName)
	p("}")
	p("")
	p("// Get retrieves an entity by Key through the cache. A miss queues the")
	p("// key for the next batch fetch (the caching suspend/replay contract).")
	p("// A miss can also mean the batched fetch errored (misses swallow")
	p("// fetch errors; the circuit breaker backs off) — GetFetch surfaces")
	p("// the error instead, and the store's Latest stays the authoritative")
	p("// check. The returned entity is shared with the cache: treat it as")
	p("// immutable.")
	p("func (inst *%s[W]) Get(key %s) (ent *%s, found bool) {", inst.cacheType(), inst.keyGoType, inst.entityType())
	p("\treturn inst.cache.Get(key)")
	p("}")
	p("")
	p("// GetFetch is the single-lookup read: the cached entity when present,")
	p("// otherwise one immediate batched point fetch — fetch errors surface")
	p("// instead of reading as misses, so found=false with err=nil is the")
	p("// authoritative absent. The fetched row is cached unless the key is")
	p("// in the dirty write window. Prefer Get plus the work-item protocol")
	p("// when batching lookups across a frame; the initial miss here also")
	p("// queues the key, so a later batch fetch may include it redundantly")
	p("// (harmless).")
	p("func (inst *%s[W]) GetFetch(ctx context.Context, key %s) (ent *%s, found bool, err error) {", inst.cacheType(), inst.keyGoType, inst.entityType())
	p("\tent, found = inst.cache.Get(key)")
	p("\tif found {")
	p("\t\treturn")
	p("\t}")
	p("\tents, err := inst.st.queryEntities(ctx, inst.st.fetchLatestSQL([]%s{key}))", inst.keyGoType)
	p("\tif err != nil {")
	p("\t\terr = eh.Errorf(\"get-fetch: %%w\", err)")
	p("\t\treturn")
	p("\t}")
	p("\tfor _, e := range ents {")
	p("\t\tif e.ID != key {")
	p("\t\t\tcontinue")
	p("\t\t}")
	p("\t\tent = e")
	p("\t\tfound = true")
	p("\t\tif _, d := inst.st.dirty[key]; !d {")
	p("\t\t\tinst.cache.AddItem(key, e)")
	p("\t\t}")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// WorkItem marks the current work item for the cache's miss bookkeeping.")
	p("func (inst *%s[W]) WorkItem(w W) iter.Seq[functional.NilIteratorValueType] {", inst.cacheType())
	p("\treturn inst.cache.WorkItem(w)")
	p("}")
	p("")
	p("// IterateReadyWorkItems flushes the queued keys when the fetch criteria")
	p("// are met and replays the work items that had misses.")
	p("func (inst *%s[W]) IterateReadyWorkItems(ctx context.Context) iter.Seq[W] {", inst.cacheType())
	p("\treturn inst.cache.IterateReadyWorkItems(ctx)")
	p("}")
	p("")
	p("// IterateRestWorkItems forces a fetch of all queued keys and replays")
	p("// the pending work items.")
	p("func (inst *%s[W]) IterateRestWorkItems(ctx context.Context) iter.Seq[W] {", inst.cacheType())
	p("\treturn inst.cache.IterateRestWorkItems(ctx)")
	p("}")
	p("")
	p("// AdvanceEpoch advances the cache's pinning epoch — call once per")
	p("// frame / batch so untouched L1 entries become evictable.")
	p("func (inst *%s[W]) AdvanceEpoch() {", inst.cacheType())
	p("\tinst.cache.AdvanceEpoch()")
	p("}")
	p("")
	p("// MarkStale flags the key's cached entry as stale — the external-writer")
	p("// signal: the next strict read misses and queues a refetch, while")
	p("// accept-stale reads keep serving the old value until it lands.")
	p("func (inst *%s[W]) MarkStale(key %s) {", inst.cacheType(), inst.keyGoType)
	p("\tinst.cache.MarkAsStale(key)")
	p("}")
	p("")
	p("// Invalidate drops the key's cached entry (L1 and stash).")
	p("func (inst *%s[W]) Invalidate(key %s) {", inst.cacheType(), inst.keyGoType)
	p("\tinst.cache.Delete(key)")
	p("}")
	p("")
	p("// InvalidateAll drops every cached entry by rebuilding the underlying")
	p("// cache — the bulk external-writer signal (e.g. after an import).")
	p("// In-flight miss bookkeeping (queued keys, pending work items) is")
	p("// dropped with it: call between frames, not with suspended work.")
	p("func (inst *%s[W]) InvalidateAll() {", inst.cacheType())
	p("\tinst.rebuild()")
	p("}")
	p("")
	if stateView {
		p("// GetLive is the cached state-view read: the cache's newest row for")
		p("// the key with the tombstone read as absent — exact under this")
		p("// process's single writer (local writes invalidate); external writers")
		p("// need MarkStale / Invalidate. The store's uncached GetLive stays")
		p("// the authoritative read. A miss queues the batch fetch like Get.")
		p("func (inst *%s[W]) GetLive(key %s) (ent *%s, found bool) {", inst.cacheType(), inst.keyGoType, inst.entityType())
		p("\tent, found = inst.cache.Get(key)")
		p("\tif found && ent.IsTombstone() {")
		p("\t\tent = nil")
		p("\t\tfound = false")
		p("\t}")
		p("\treturn")
		p("}")
		p("")
		p("// GetLiveAcceptStale is the stale-while-revalidate state-view read:")
		p("// a stale entry is served immediately (stale=true) while the refetch")
		p("// queues in the background — pair with the work-item replay loop.")
		p("// Tombstones read as absent; stale then reports whether that verdict")
		p("// came from a stale entry.")
		p("func (inst *%s[W]) GetLiveAcceptStale(key %s) (ent *%s, found bool, stale bool) {", inst.cacheType(), inst.keyGoType, inst.entityType())
		p("\tent, found, stale = inst.cache.GetAcceptStale(key)")
		p("\tif found && ent.IsTombstone() {")
		p("\t\tent = nil")
		p("\t\tfound = false")
		p("\t}")
		p("\treturn")
		p("}")
		p("")
	}
	p("// fetchLatestSQL is the batched newest-row-per-key point lookup shared")
	p("// by the cache fetcher and GetFetch.")
	p("func (inst *%s) fetchLatestSQL(keys []%s) string {", inst.storeType(), inst.keyGoType)
	p("\tvar sb strings.Builder")
	p("\tsb.WriteString(\"SELECT * FROM \")")
	p("\tsb.WriteString(%sTableName)", inst.StoreName)
	p("\tsb.WriteString(\" WHERE \" + %sColKey + \" IN (\")", inst.StoreName)
	p("\tfor i, k := range keys {")
	p("\t\tif i > 0 {")
	p("\t\t\tsb.WriteByte(',')")
	p("\t\t}")
	p("\t\tsb.WriteString(%sKeyLiteral(k))", inst.TableName)
	p("\t}")
	p("\tsb.WriteString(\") ORDER BY \" + %sColOrder + \" DESC LIMIT 1 BY \" + %sColKey)", inst.StoreName, inst.StoreName)
	p("\tsb.WriteString(%sArrowOutputSettings)", inst.TableName)
	p("\treturn sb.String()")
	p("}")
	p("")
	p("// %sFetcher implements caching.ItemFetcherI for attached cache views —", inst.TableName)
	p("// an unexported shim so the fetch plumbing stays off the store's")
	p("// public method set.")
	p("type %sFetcher struct{ st *%s }", inst.TableName, inst.storeType())
	p("")
	p("var _ caching.ItemFetcherI[%s, *%s] = (*%sFetcher)(nil)", inst.keyGoType, inst.entityType(), inst.TableName)
	p("")
	p("// DeterminePartition implements caching.ItemFetcherI. Single partition")
	p("// in v1 (one table, one server).")
	p("func (inst *%sFetcher) DeterminePartition(key %s) uint64 { return 0 }", inst.TableName, inst.keyGoType)
	p("")
	p("// FetchItemSinglePartition implements caching.ItemFetcherI: one batched")
	p("// point lookup per fetch. Duplicate versions collapse newest-first.")
	p("// Dirty keys (written locally, not yet flushed) are fetched but not")
	p("// cached — caching them would resurrect the pre-write row.")
	p("func (inst *%sFetcher) FetchItemSinglePartition(ctx context.Context, partition uint64, keys []%s, target caching.ItemTargetI[%s, *%s]) (err error) {", inst.TableName, inst.keyGoType, inst.keyGoType, inst.entityType())
	p("\tents, err := inst.st.queryEntities(ctx, inst.st.fetchLatestSQL(keys))")
	p("\tif err != nil {")
	p("\t\treturn")
	p("\t}")
	p("\tfor _, ent := range ents {")
	p("\t\tif _, d := inst.st.dirty[ent.ID]; d {")
	p("\t\t\tcontinue")
	p("\t\t}")
	p("\t\ttarget.AddItem(ent.ID, ent)")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
}

func (inst emitter) emitQueryVerbs(sb *strings.Builder, comps []storeComponent, stateView bool) {
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
		p("// Scan%s iterates the entities whose rows carry a conforming %s", c.Kind, c.Kind)
		p("// component, ordered by (Order, Key) — deterministic across ties.")
		p("// opts.ExtraPredicate (trusted raw SQL over the physical columns —")
		p("// never untrusted input) further restricts the scan; opts.Limit")
		p("// caps the row count. The Filter artefact uses ClickHouse")
		p("// built-ins only, so this is a single SELECT — no helper UDFs, no")
		p("// multi-statement script (the ExecutorI contract). The sequence is")
		p("// single-use; ctx must stay valid until iteration completes; an")
		p("// error ends it as a final (nil, err) pair. Scans see only flushed")
		p("// rows.")
		p("func (inst *%s) Scan%s(ctx context.Context, opts recordstore.ScanOpts) iter.Seq2[*%s, error] {", inst.storeType(), c.Kind, inst.entityType())
		p("\twhere := %sScan%sFilter", inst.TableName, c.Kind)
		p("\tif opts.ExtraPredicate != \"\" {")
		p("\t\twhere = \"(\" + where + \") AND (\" + opts.ExtraPredicate + \")\"")
		p("\t}")
		p("\tsql := \"SELECT * FROM \" + %sTableName +", inst.StoreName)
		p("\t\t\" WHERE \" + where +")
		p("\t\t\" ORDER BY \" + %sColOrder + \" ASC, \" + %sColKey + \" ASC\"", inst.StoreName, inst.StoreName)
		p("\tif opts.Limit > 0 {")
		p("\t\tsql += \" LIMIT \" + strconv.Itoa(opts.Limit)")
		p("\t}")
		p("\tsql += %sArrowOutputSettings", inst.TableName)
		p("\treturn inst.iterateEntities(ctx, sql)")
		p("}")
		p("")
	}
	p("// Latest returns the newest row for key, tombstone-blind (the raw")
	p("// row-level primitive — a deleted key still returns its tombstone")
	p("// row; GetLive is the interpreted state-view read). Reads see only")
	p("// flushed rows.")
	p("func (inst *%s) Latest(ctx context.Context, key %s) (ent *%s, found bool, err error) {", inst.storeType(), inst.keyGoType, inst.entityType())
	p("\tsql := \"SELECT * FROM \" + %sTableName +", inst.StoreName)
	p("\t\t\" WHERE \" + %sColKey + \" = \" + %sKeyLiteral(key) +", inst.StoreName, inst.TableName)
	p("\t\t\" ORDER BY \" + %sColOrder + \" DESC LIMIT 1\" + %sArrowOutputSettings", inst.StoreName, inst.TableName)
	p("\tents, err := inst.queryEntities(ctx, sql)")
	p("\tif err != nil || len(ents) == 0 {")
	p("\t\treturn")
	p("\t}")
	p("\tent = ents[0]")
	p("\tfound = true")
	p("\treturn")
	p("}")
	p("")
	p("// Replay iterates the rows for key with the order column >= fromOrder")
	p("// in ascending order — the event-replay primitive. A zero fromOrder")
	p("// replays everything (zero time.Time has no defined UnixNano;")
	p("// recordstore.SeqTs(0) is the equivalent explicit bound);")
	p("// opts.To bounds the replay exclusively (\"state as of To\") and")
	p("// opts.Limit caps the row count. The sequence is single-use; ctx")
	p("// must stay valid until iteration completes; the query may execute")
	p("// at call time or lazily during iteration (buffered in v1 — a")
	p("// streaming executor changes nothing visible); an error ends the")
	p("// sequence as a final (nil, err) pair. Reads see only flushed rows.")
	p("func (inst *%s) Replay(ctx context.Context, key %s, fromOrder time.Time, opts recordstore.ReplayOpts) iter.Seq2[*%s, error] {", inst.storeType(), inst.keyGoType, inst.entityType())
	p("\tsql := \"SELECT * FROM \" + %sTableName +", inst.StoreName)
	p("\t\t\" WHERE \" + %sColKey + \" = \" + %sKeyLiteral(key)", inst.StoreName, inst.TableName)
	p("\tif !fromOrder.IsZero() {")
	p("\t\tsql += \" AND \" + %sColOrder + \" >= fromUnixTimestamp64Nano(\" + strconv.FormatInt(fromOrder.UnixNano(), 10) + \")\"", inst.StoreName)
	p("\t}")
	p("\tif !opts.To.IsZero() {")
	p("\t\tsql += \" AND \" + %sColOrder + \" < fromUnixTimestamp64Nano(\" + strconv.FormatInt(opts.To.UnixNano(), 10) + \")\"", inst.StoreName)
	p("\t}")
	p("\tsql += \" ORDER BY \" + %sColOrder + \" ASC\"", inst.StoreName)
	p("\tif opts.Limit > 0 {")
	p("\t\tsql += \" LIMIT \" + strconv.Itoa(opts.Limit)")
	p("\t}")
	p("\tsql += %sArrowOutputSettings", inst.TableName)
	p("\treturn inst.iterateEntities(ctx, sql)")
	p("}")
	p("")
	if !stateView {
		return
	}
	p("// --- state view (Delete / GetLive; ADR-0100 SD4). Versioned writes")
	p("// go through Begin — appending a new version IS the update. ---")
	p("")
	p("// Delete appends a tombstone row for id (no components; lifecycle marks")
	p("// the deletion); attached cache views evict the key. GetLive reads")
	p("// it as absent.")
	p("func (inst *%s) Delete(id %s, ts time.Time) (err error) {", inst.storeType(), inst.keyGoType)
	p("\tinst.dml.BeginEntity().SetId(id).SetTimestamp(ts).SetLifecycle(recordstore.LifecycleTombstone)")
	p("\terr = inst.dml.CommitEntity()")
	p("\tif err != nil {")
	p("\t\t_ = inst.dml.RollbackEntity() // discard the failed frame; the store stays usable")
	p("\t\treturn")
	p("\t}")
	p("\tinst.buffered++")
	p("\tinst.dirty[id] = struct{}{}")
	p("\tinst.invalidate(id)")
	p("\treturn")
	p("}")
	p("")
	p("// GetLive is Latest plus tombstone interpretation: newest row wins, a")
	p("// tombstone reads as absent — the state-view read (the cache view")
	p("// carries the cached twin).")
	p("func (inst *%s) GetLive(ctx context.Context, key %s) (ent *%s, found bool, err error) {", inst.storeType(), inst.keyGoType, inst.entityType())
	p("\tent, found, err = inst.Latest(ctx, key)")
	p("\tif err != nil || !found {")
	p("\t\treturn")
	p("\t}")
	p("\tif ent.IsTombstone() {")
	p("\t\tent = nil")
	p("\t\tfound = false")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
}

func (inst emitter) emitDecode(sb *strings.Builder, comps []storeComponent, stateView bool) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	ra := inst.raPrefix()

	// Collect the distinct tagged sections the components use.
	seen := map[string]secUse{}
	order := []string{}
	for _, c := range comps {
		for _, g := range c.groups {
			m := mappingplan.UpperFirst(g.Section)
			if _, ok := seen[m]; !ok {
				seen[m] = secUse{varN: lowerFirst(m) + "R"}
				order = append(order, m)
			}
		}
	}

	p("// --- decode (Arrow → entity bags). ---")
	p("")
	p("// %sSectionReaderI is the uniform slice of the generated read-access", inst.TableName)
	p("// readers. Column indices stay at their constructor defaults — the")
	p("// schema order a SELECT * returns.")
	p("type %sSectionReaderI interface {", inst.TableName)
	p("\tLoadFromRecord(raruntime.RecordI) error")
	p("\tRelease()")
	p("}")
	p("")
	p("func (inst *%s) queryEntities(ctx context.Context, sql string) (ents []*%s, err error) {", inst.storeType(), inst.entityType())
	p("\tfor rec, rerr := range inst.exec.QueryArrow(ctx, sql) {")
	p("\t\tif rerr != nil {")
	p("\t\t\terr = eh.Errorf(\"query entities: %%w\", rerr)")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\tbatch, derr := decode%sRecord(rec)", inst.StoreName)
	p("\t\trec.Release()")
	p("\t\tif derr != nil {")
	p("\t\t\terr = derr")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\tents = append(ents, batch...)")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// iterateEntities adapts the buffered query path to the single-use")
	p("// iterator contract shared by Replay and the Scan verbs: entities")
	p("// yield in query order; an error yields once as (nil, err) and ends")
	p("// the sequence.")
	p("func (inst *%s) iterateEntities(ctx context.Context, sql string) iter.Seq2[*%s, error] {", inst.storeType(), inst.entityType())
	p("\treturn func(yield func(*%s, error) bool) {", inst.entityType())
	p("\t\tents, err := inst.queryEntities(ctx, sql)")
	p("\t\tif err != nil {")
	p("\t\t\tyield(nil, err)")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\tfor _, ent := range ents {")
	p("\t\t\tif !yield(ent, nil) {")
	p("\t\t\t\treturn")
	p("\t\t\t}")
	p("\t\t}")
	p("\t}")
	p("}")
	p("")
	p("// decode%sRecord reads one fetched Arrow record into entity bags:", inst.StoreName)
	p("// envelope from the plain readers, components via presence-gated,")
	p("// membership-matched reads (fat rows carry optional components — the")
	p("// kind-homogeneous helpers cannot decode them).")
	p("func decode%sRecord(rec arrow.RecordBatch) (ents []*%s, err error) {", inst.StoreName, inst.entityType())
	p("\tidR := %sNew%sPlainEntityIdAttributes()", inst.lowQ(), ra)
	p("\ttsR := %sNew%sPlainEntityTimestampAttributes()", inst.lowQ(), ra)
	if stateView {
		p("\tlcR := %sNew%sPlainEntityLifecycleAttributes()", inst.lowQ(), ra)
	}
	for _, m := range order {
		p("\t%s := %sNew%sTagged%s()", seen[m].varN, inst.lowQ(), ra, m)
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
}

func upperFirst(s string) string { return mappingplan.UpperFirst(s) }

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
