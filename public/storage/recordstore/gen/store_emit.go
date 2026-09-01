package gen

import (
	"fmt"
	"go/format"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// needsSlices reports whether the decode uses slices.Collect — true when a
// pass-through column is array-shaped (its read-access accessor yields an
// iter.Seq the decode collects).
func (m plainModel) needsSlices() bool {
	for _, c := range m.passthrough {
		if c.isArray {
			return true
		}
	}
	return false
}

// emitter carries Input plus the emission-time facts derived from the
// schema — scratch state stays off the public config struct.
type emitter struct {
	Input
	// keyGoType is the Key column's derived Go type ("uint64" or
	// "string").
	keyGoType string
	// orderGoType is the Order column's derived Go type: "time.Time" for
	// the z64 timestamp lane, "uint64" for a declared plain-integer Order
	// (ADR-0100 Update 2026-08-30).
	orderGoType string
	// model is the enumerated plain-column backbone: the role bindings plus
	// the pass-through envelope columns and their PlainItemType grouping.
	model plainModel
	// hasComps records whether any component is bound. The option package
	// (and the typed Add/Scan/component-decode surface) is only reached when
	// a component exists — a bare backbone store binds none.
	hasComps bool
	// stampLane / stampLaneAsData drive the New<Store> stamping guards
	// (ADR-0112 SD2 lane hygiene). stampLane: some section declares a
	// HighCardRef membership column, so ambient stamps have somewhere to
	// land — without it, configured Stampers would record nothing,
	// silently. stampLaneAsData: a component reads that lane back as data
	// (a tuple `@membership` field on the highCardRef channel collects the
	// whole lane), so a stamp would decode as a spurious ref id. Kind
	// memberships riding the lane are fine — their decode matches known
	// ids and skips the rest. Today the ReadRowSupported gate refuses
	// tuple components before stampLaneAsData can matter; the guard is the
	// second wall for when that ADR-0100 deferral lifts.
	stampLane       bool
	stampLaneAsData bool
	// registryIds is set when the configured wrapper's ids are globally
	// unique (a caller-assigned registry snapshot) rather than per-plan
	// declaration order — it selects the emitted <Store>MembershipIds doc
	// text.
	registryIds bool
}

// plainRole classifies one plain (backbone) column. The three roles drive
// the store's SQL semantics (point lookup, ORDER BY, tombstone); every
// other plain column is carried verbatim through the envelope.
type plainRole int

const (
	rolePassThrough plainRole = iota // a verbatim envelope field
	roleKey                          // the leading EntityId column (point-lookup key)
	roleOrder                        // the EntityTimestamp column (version / ORDER BY)
	roleLifecycle                    // a u8 EntityLifecycle column (state view)
)

// plainCol is one enumerated plain column: the field/arg identifier and Go
// type the store surfaces, plus its role and PlainItemType. isArray marks
// the homogenous-array / set columns, whose DML setter takes []element and
// whose read-access accessor yields an iter.Seq.
type plainCol struct {
	itemType common.PlainItemTypeE
	name     string // leeway column name as authored (what Input.Roles names)
	pascal   string // UpperCamelCase identifier, e.g. "PartitioningKey"
	goType   string // field / setter-arg Go type, e.g. "uint64", "[]string"
	isArray  bool
	physical string // encoded physical column name (for messages)
	role     plainRole
}

// plainGroup is the ordered set of plain columns sharing one PlainItemType —
// the shape of the generated DML grouped setter (SetId, SetRouting, …). The
// column order within a group matches the DML setter's argument order and
// the read-access reader's field order (both derive from the same IR).
type plainGroup struct {
	itemType common.PlainItemTypeE
	cols     []plainCol
}

// plainModel is the enumerated backbone: every plain column grouped by
// PlainItemType in canonical order, the three role bindings (pointers into
// groups), and the pass-through columns in canonical order. stateView is
// set when a u8 EntityLifecycle column bound the Lifecycle role.
type plainModel struct {
	groups      []plainGroup
	key         *plainCol
	order       *plainCol
	lifecycle   *plainCol
	passthrough []plainCol
	stateView   bool
}

// storeComponent is the per-component emission model: the parsed plan plus
// the derived names and generation-time artefacts the template needs.
type storeComponent struct {
	Kind   string // Go type, e.g. "Identity"
	plan   *mappingplan.Plan
	groups []goplan.SectionGroup
	// ids is the membership → id assignment the configured wrapper states
	// for this plan — the one source the codec consts, the baked Scan
	// filter literals and the <Store>MembershipIds map all resolve from.
	ids map[string]uint64
	// filter is the baked ADR-0066 Filter artefact (presence prefilter AND
	// exact validator) identifying rows that carry a conforming component —
	// the WHERE body of the Scan verb.
	filter string
	// The remaining three ADR-0066 artefacts. Nothing in the store's own read
	// path uses them — Scan needs only the Filter — but they are published
	// through the componentsql Set so a SQL author can reach what the
	// component definition already generates (ADR-0189 §SD1). They cost
	// nothing to keep: Generate builds all four.
	presence   string
	validator  string
	projection string
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
// EntityLifecycle (u8) column exists. Any other plain column becomes a
// pass-through envelope field, promoted onto the entity through the
// embedded <Store>Envelope (shipped 2026-07-09; the ADR-0100 Update of
// 2026-07-04 that deferred them is superseded). Component decode coverage
// is gated by marshallgen.ReadRowSupported (carrier channels and
// tuple / nested sections remain uncovered).
func (inst Input) emitStore(ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, plans []*mappingplan.Plan) (code []byte, err error) {
	info := readback.NewInformationRetrieval(conv)
	err = info.LoadTable(ir, inst.RowConfig)
	if err != nil {
		err = eh.Errorf("load readback IR: %w", err)
		return
	}
	model, err := inst.enumeratePlain(info)
	if err != nil {
		return
	}
	if model.key == nil {
		err = eh.Errorf("schema has no EntityId plain column — the Key role is required")
		return
	}
	if model.order == nil {
		err = eh.Errorf("schema has no EntityTimestamp plain column — Latest/Replay need the Order role")
		return
	}
	switch model.key.goType {
	case "uint64", "string":
	default:
		err = eb.Build().Str("goType", model.key.goType).Errorf("Key column Go type not supported (uint64 and string are; ADR-0100 SD2)")
		return
	}
	switch {
	case model.order.goType == "time.Time":
	case model.order.goType == "uint64" && inst.Roles.Order != "":
		// The u64 Order regime (ADR-0100 Update 2026-08-30): reached only
		// through the explicit declaration — positional binding stays on
		// the timestamp lane, so undeclared schemas cannot drift into it.
	default:
		err = eb.Build().Str("goType", model.order.goType).Errorf("Order column Go type not supported — Replay and the decode assume the timestamp lane; declare the EntityTimestamp column as a temporal (ctabb.Z64 for nanosecond replay precision), or bind a uint64 column explicitly via Roles.Order")
		return
	}
	if inst.TombstoneView && model.stateView {
		err = eb.Build().Str("lifecycleColumn", model.lifecycle.name).Errorf("TombstoneView is set, but the schema binds a u8 EntityLifecycle role — the state view exists already, with the u8 marker as the tombstone pair's default binding")
		return
	}
	// stateView says the Delete/GetLive verb family (and its cache twins)
	// is emitted; model.stateView keeps saying the u8 Lifecycle role
	// exists. They differ exactly for TombstoneView stores, whose verbs
	// run on the configured pair alone (ADR-0100 Update 2026-08-30).
	stateView := model.stateView || inst.TombstoneView

	idSrc, hasIdSrc := inst.wrapper().(marshallgen.MembershipIdSourceI)
	if !hasIdSrc && len(plans) > 0 {
		// Generate gates this earlier with the same message; kept here so
		// emitStore is safe against a future direct caller.
		err = eb.Build().Type("wrapper", inst.wrapper()).Errorf("Wrapper does not provide generation-time membership ids (marshallgen.MembershipIdSourceI) — the store bakes ids into its Scan filter SQL and the <Store>MembershipIds map")
		return
	}
	comps := make([]storeComponent, 0, len(plans))
	for _, plan := range plans {
		var sc storeComponent
		sc, err = classifyComponent(plan, info, idSrc)
		if err != nil {
			return
		}
		comps = append(comps, sc)
	}
	// Membership-id coherence gates. The kind<Name> symbols are declared
	// once per generated package, so two kinds naming one membership would
	// redeclare them — refuse with the domain error instead of the Go
	// compile break. (Deliberate cross-kind slot sharing needs the reflect
	// path — ADR-0146 D5/D6.)
	membOwner := map[string]string{}
	// Literal-name channels carry no kind symbol, but the presence hazard is
	// the same: <Kind>ReadRow marks a component present on any matched
	// (section reader, membership value) slot, so two kinds naming one
	// verbatim membership in one section would decode as each other. The
	// match is scoped to the section, so the same name in two sections is
	// two slots (ADR-0100 SD6, update 2026-08-28).
	verbatimOwner := map[string]string{}
	for _, sc := range comps {
		for _, name := range sortedIdNames(sc.ids) {
			if owner, taken := membOwner[name]; taken && owner != sc.Kind {
				err = eb.Build().Str("component", owner).Str("otherComponent", sc.Kind).Str("membership", name).Errorf("two components use the same membership — its kind symbol is declared once per generated package, so two kinds cannot share a membership in one store")
				return
			}
			membOwner[name] = sc.Kind
		}
		for _, slot := range verbatimSlots(sc.plan) {
			key := slot.section + "@" + slot.name
			if owner, taken := verbatimOwner[key]; taken && owner != sc.Kind {
				err = eb.Build().Str("component", owner).Str("otherComponent", sc.Kind).Str("membership", slot.name).Str("section", slot.section).Errorf("two components name the same verbatim membership in one section — a component is present on any matched slot, so two kinds sharing one (section, name) would decode as each other")
				return
			}
			verbatimOwner[key] = sc.Kind
		}
	}
	if !idSrc.GloballyUniqueIds() {
		// Under package-local ids (each kind numbering from 1), two kinds'
		// distinct memberships can carry the same wire id, and the
		// membership match is scoped to a section's reader — so components
		// must bind disjoint sections or the presence-gated decode and the
		// baked Scan filters would silently cross-read. A precondition of
		// this id regime, not a component-model rule (ADR-0100 SD6 as
		// corrected 2026-08-10; leeway itself permits sharing, ADR-0146
		// D5); a globally-unique id source lifts it (ADR-0105 D2). Only
		// sections a component reaches through a ref channel take part:
		// a literal-name channel matches its name on its own lane and
		// cannot alias by id (update 2026-08-28), so all-verbatim
		// components may share sections under any id source.
		sectionOwner := map[string]string{}
		for _, sc := range comps {
			for _, g := range sc.groups {
				if !refBound(g) {
					continue
				}
				if owner, taken := sectionOwner[g.Section]; taken && owner != sc.Kind {
					err = eb.Build().Str("component", owner).Str("otherComponent", sc.Kind).Str("section", g.Section).Errorf("two components bind the same section — components must own disjoint sections (ADR-0100 SD6)")
					return
				}
				sectionOwner[g.Section] = sc.Kind
			}
		}
	} else {
		// Id-level disjointness: the wrapper claims two names never share
		// an id — verify it over the memberships this store actually uses,
		// so a caller-supplied map that repeats an id cannot silently
		// cross-read a shared section (ADR-0105 D2).
		type idClaim struct{ kind, name string }
		idOwner := map[uint64]idClaim{}
		for _, sc := range comps {
			for _, name := range sortedIdNames(sc.ids) {
				id := sc.ids[name]
				if prev, taken := idOwner[id]; taken && prev.name != name {
					err = eb.Build().Str("membership", prev.name).Str("component", prev.kind).Str("otherMembership", name).Str("otherComponent", sc.Kind).Uint64("id", id).Errorf("two memberships share one id — a globally-unique id source must assign distinct ids (id-level disjointness, ADR-0105 D2)")
					return
				}
				idOwner[id] = idClaim{kind: sc.Kind, name: name}
			}
		}
	}
	// A pass-through column surfaces as a promoted entity field (via the
	// embedded envelope struct) — refuse a name that collides with the
	// fixed entity fields/methods or a component field, since Go would
	// reject the generated type.
	if len(model.passthrough) > 0 {
		orderEntityField := "Ts"
		if model.order.goType == "uint64" {
			orderEntityField = "Ord"
		}
		reserved := map[string]bool{"ID": true, orderEntityField: true, "Lifecycle": true, "Archetype": true, "IsTombstone": true}
		for _, sc := range comps {
			reserved[sc.Kind] = true
		}
		for _, pt := range model.passthrough {
			if reserved[pt.pascal] {
				err = eb.Build().Str("column", pt.physical).Str("field", pt.pascal).Errorf("pass-through envelope column maps to an entity field that collides with a fixed field/method or a component — rename the column")
				return
			}
			reserved[pt.pascal] = true // also catches two columns styling to one name
		}
	}

	key := envelopeCol{physical: model.key.physical, goType: model.key.goType}
	order := envelopeCol{physical: model.order.physical, goType: model.order.goType}
	var lifecycle envelopeCol
	if model.lifecycle != nil {
		lifecycle = envelopeCol{physical: model.lifecycle.physical, goType: model.lifecycle.goType}
	}

	stampLane := false
	for _, sec := range inst.Table.TaggedValuesSections {
		if sec.MembershipSpec.HasHighCardRefOnly() {
			stampLane = true
			break
		}
	}
	stampLaneAsData := false
	for _, plan := range plans {
		for i := range plan.Fields {
			for _, tm := range plan.Fields[i].TupleMemberships {
				if tm.Channel == mappingplan.MembershipChannelHighCardRef {
					stampLaneAsData = true
				}
			}
		}
	}

	em := emitter{Input: inst, keyGoType: model.key.goType, orderGoType: model.order.goType,
		model: model, hasComps: len(comps) > 0,
		stampLane: stampLane, stampLaneAsData: stampLaneAsData,
		registryIds: hasIdSrc && idSrc.GloballyUniqueIds()}
	var sb strings.Builder
	em.emitStoreHeader(&sb, key, order, lifecycle, stateView)
	em.emitMembershipIds(&sb, comps)
	em.emitEnvelopeStruct(&sb)
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
	em.emitComponentSQL(&sb, comps)
	em.emitDecode(&sb, comps, stateView)

	raw := []byte(sb.String())
	code, err = format.Source(raw)
	if err != nil {
		err = eb.Build().Str("emitted", string(raw)).Errorf("gofmt rejected store output: %w", err)
	}
	return
}

// enumeratePlain walks the plain (backbone) columns in canonical order —
// the order the DML grouped setters take their arguments and the read-access
// readers expose their fields, both derived from the same IR — grouping them
// by PlainItemType and binding the three roles. The leading EntityId column
// is the Key, the EntityTimestamp column the Order, and the first u8
// EntityLifecycle column the state-view Lifecycle; every other EntityId /
// EntityRouting / EntityLifecycle column is a pass-through envelope field
// (ADR-0100 Update 2026-07-09). Transaction and Opaque plain columns remain
// deferred — they carry streaming-group / transaction semantics the store
// glue does not model yet.
func (inst Input) enumeratePlain(info *readback.InformationRetrieval) (m plainModel, err error) {
	byType := map[common.PlainItemTypeE]int{}
	for cr := range info.IterateAll() {
		it := cr.ColumnContext.PlainItemType
		if it == common.PlainItemTypeNone {
			// Tagged (and support) columns report None; skip them.
			continue
		}
		switch it {
		case common.PlainItemTypeEntityId, common.PlainItemTypeEntityTimestamp,
			common.PlainItemTypeEntityRouting, common.PlainItemTypeEntityLifecycle:
		default:
			err = eb.Build().Str("column", cr.PhysicalColumn.String()).Stringer("itemType", it).Errorf("plain column carries an unsupported item type — only the envelope item types (EntityId / EntityTimestamp / EntityRouting / EntityLifecycle) are supported; Transaction and Opaque plain columns are deferred")
			return
		}
		var col plainCol
		col.itemType = it
		col.name = cr.Name.String()
		col.physical = cr.PhysicalColumn.String()
		col.pascal = cr.Name.Convert(naming.UpperCamelCase).String()
		col.goType, col.isArray, err = fieldGoType(cr.CanonicalType)
		if err != nil {
			err = eb.Build().Str("physical", col.physical).Errorf("derive Go type for plain column: %w", err)
			return
		}
		gi, ok := byType[it]
		if !ok {
			gi = len(m.groups)
			m.groups = append(m.groups, plainGroup{itemType: it})
			byType[it] = gi
		}
		m.groups[gi].cols = append(m.groups[gi].cols, col)
	}
	// Bind roles onto the stored columns. The groups slice is not mutated
	// after this point, so the role pointers stay valid across the return.
	// Declared roles (Input.Roles) bind first; the positional defaults then
	// fill only the roles left undeclared, skipping already-bound columns —
	// so a column elected Order by name cannot also positionally become the
	// Key.
	err = inst.bindDeclaredRoles(&m)
	if err != nil {
		return
	}
	for gi := range m.groups {
		for ci := range m.groups[gi].cols {
			c := &m.groups[gi].cols[ci]
			if c.role != rolePassThrough {
				continue
			}
			switch c.itemType {
			case common.PlainItemTypeEntityId:
				if m.key == nil {
					c.role = roleKey
					m.key = c
				}
			case common.PlainItemTypeEntityTimestamp:
				if m.order == nil {
					c.role = roleOrder
					m.order = c
				}
			case common.PlainItemTypeEntityLifecycle:
				if m.lifecycle == nil && c.goType == "uint8" {
					c.role = roleLifecycle
					m.lifecycle = c
				}
			}
		}
	}
	m.stateView = m.lifecycle != nil
	for gi := range m.groups {
		for _, c := range m.groups[gi].cols {
			if c.role == rolePassThrough {
				m.passthrough = append(m.passthrough, c)
			}
		}
	}
	return
}

// bindDeclaredRoles binds the roles Input.Roles names explicitly (ADR-0100
// SD2's deferred explicit role configuration). Each declared name must
// resolve to a plain column that fits the role; two roles naming one column
// are refused — Latest/Replay compare the Key and Order as independent
// lanes, so a shared column cannot serve both.
func (inst Input) bindDeclaredRoles(m *plainModel) (err error) {
	find := func(name string) *plainCol {
		for gi := range m.groups {
			for ci := range m.groups[gi].cols {
				if m.groups[gi].cols[ci].name == name {
					return &m.groups[gi].cols[ci]
				}
			}
		}
		return nil
	}
	taken := func(role string, c *plainCol) error {
		return eb.Build().Str("role", role).Str("column", c.name).Errorf("a Roles entry names a column another declared role already binds — every role needs its own column (ADR-0100 SD2)")
	}
	if inst.Roles.Key != "" {
		c := find(inst.Roles.Key)
		if c == nil {
			err = eb.Build().Str("column", inst.Roles.Key).Errorf("Roles.Key names no plain column in the schema")
			return
		}
		if c.itemType != common.PlainItemTypeEntityId {
			err = eb.Build().Str("name", c.name).Stringer("itemType", c.itemType).Errorf("Roles.Key column carries the wrong item type — the Key role requires an EntityId plain column")
			return
		}
		c.role = roleKey
		m.key = c
	}
	if inst.Roles.Order != "" {
		c := find(inst.Roles.Order)
		if c == nil {
			err = eb.Build().Str("column", inst.Roles.Order).Errorf("Roles.Order names no plain column in the schema")
			return
		}
		if c.role != rolePassThrough {
			err = taken("Order", c)
			return
		}
		if c.itemType != common.PlainItemTypeEntityTimestamp && c.goType != "uint64" {
			err = eb.Build().Str("name", c.name).Stringer("itemType", c.itemType).Str("goType", c.goType).Errorf("Roles.Order column carries an unsupported item type — the Order role takes the EntityTimestamp z64 lane or a plain column deriving to uint64")
			return
		}
		c.role = roleOrder
		m.order = c
	}
	if inst.Roles.Lifecycle != "" {
		c := find(inst.Roles.Lifecycle)
		if c == nil {
			err = eb.Build().Str("column", inst.Roles.Lifecycle).Errorf("Roles.Lifecycle names no plain column in the schema")
			return
		}
		if c.role != rolePassThrough {
			err = taken("Lifecycle", c)
			return
		}
		if c.itemType != common.PlainItemTypeEntityLifecycle || c.goType != "uint8" {
			err = eb.Build().Str("name", c.name).Stringer("itemType", c.itemType).Str("goType", c.goType).Errorf("Roles.Lifecycle column carries the wrong item type — the Lifecycle role requires a u8 EntityLifecycle plain column")
			return
		}
		c.role = roleLifecycle
		m.lifecycle = c
	}
	return
}

// fieldGoType renders the Go type a plain column surfaces as — a store
// field, an envelope field and a DML setter argument. Scalars keep their
// derived type; homogenous-array and set columns become []element (the DML
// setter takes []element and the read-access accessor yields
// iter.Seq[element], regardless of the codec's internal carrier).
func fieldGoType(ct canonicaltypes.PrimitiveAstNodeI) (goType string, isArray bool, err error) {
	gt, isSlice, isRoaring, derr := mappingplan.DeriveGoShape(ct)
	if derr != nil {
		err = derr
		return
	}
	if isRoaring {
		// Set carrier is *roaring.Bitmap in the codec, but the DML setter /
		// read-access API is []element — recover the element scalar type.
		elem, _, _, e := mappingplan.DeriveGoShape(canonicaltypes.DemoteToScalarPrim(ct))
		if e != nil {
			err = e
			return
		}
		goType = "[]" + elem
		isArray = true
		return
	}
	if isSlice {
		goType = "[]" + gt
		isArray = true
		return
	}
	goType = gt
	return
}

// itemTypeToSetterName maps a PlainItemType to the generated DML grouped
// setter — the mirror of the leeway DML generator's own mapping, so the
// store glue and the DML scaffolding agree on the method names.
func itemTypeToSetterName(itemType common.PlainItemTypeE) string {
	switch itemType {
	case common.PlainItemTypeEntityId:
		return "SetId"
	case common.PlainItemTypeEntityTimestamp:
		return "SetTimestamp"
	case common.PlainItemTypeEntityRouting:
		return "SetRouting"
	case common.PlainItemTypeEntityLifecycle:
		return "SetLifecycle"
	case common.PlainItemTypeTransaction:
		return "SetTransaction"
	case common.PlainItemTypeOpaque:
		return "SetOpaque"
	}
	return ""
}

// plainReaderVar is the decode-side local variable holding a group's
// read-access reader; the token also forms the reader constructor
// (New<ra>Plain<Token>Attributes).
func plainReaderRoleToken(itemType common.PlainItemTypeE) string {
	return naming.MustBeValidStylableName(itemType.String()).Convert(naming.UpperCamelCase).String()
}

func plainReaderVar(itemType common.PlainItemTypeE) string {
	switch itemType {
	case common.PlainItemTypeEntityId:
		return "idR"
	case common.PlainItemTypeEntityTimestamp:
		return "tsR"
	case common.PlainItemTypeEntityLifecycle:
		return "lcR"
	case common.PlainItemTypeEntityRouting:
		return "rtR"
	}
	return lowerFirst(plainReaderRoleToken(itemType)) + "R"
}

// orderU64 reports the u64 Order regime (ADR-0100 Update 2026-08-30);
// false is the z64 timestamp lane.
func (inst emitter) orderU64() bool { return inst.orderGoType == "uint64" }

// stateViewOn reports whether the store emits the Delete/GetLive family:
// the u8 Lifecycle role binds, or TombstoneView declares the
// predicate-driven view (ADR-0100 Update 2026-08-30).
func (inst emitter) stateViewOn() bool { return inst.model.stateView || inst.TombstoneView }

// orderArg is the Begin/Delete/Ingest parameter name carrying the Order
// value: "ts" on the timestamp lane, "ord" under the u64 regime.
func (inst emitter) orderArg() string {
	if inst.orderU64() {
		return "ord"
	}
	return "ts"
}

// orderEntityField is the entity field carrying the Order value: Ts on the
// timestamp lane, Ord under the u64 regime.
func (inst emitter) orderEntityField() string {
	if inst.orderU64() {
		return "Ord"
	}
	return "Ts"
}

// dmlChain renders the ".SetId(…).SetTimestamp(…)…" plain-setter chain over
// every group in canonical order. The Key column is sourced from id, the
// Order from the order argument, the state-view Lifecycle from
// lifecycleExpr, and each pass-through column from envVar.<field> (envVar
// is unreferenced when a group carries no pass-through column, e.g. every
// group in a role-only schema).
func (inst emitter) dmlChain(lifecycleExpr, envVar string) string {
	var sb strings.Builder
	for _, g := range inst.model.groups {
		args := make([]string, 0, len(g.cols))
		for _, c := range g.cols {
			switch c.role {
			case roleKey:
				args = append(args, "id")
			case roleOrder:
				args = append(args, inst.orderArg())
			case roleLifecycle:
				args = append(args, lifecycleExpr)
			default:
				args = append(args, envVar+"."+c.pascal)
			}
		}
		fmt.Fprintf(&sb, ".%s(%s)", itemTypeToSetterName(g.itemType), strings.Join(args, ", "))
	}
	return sb.String()
}

// privateControl reports whether the DML builder emits its control surface
// unexported (ADR-0100 SD6) — the default internal/lowlevel + trimmed
// layout. Flat exports the whole builder into the consumer package, and
// FullCodecs needs the exported control set for <Kind>BuildEntities, so
// either one keeps the control surface exported and the store drives it by
// plain method calls.
func (inst Input) privateControl() bool { return !inst.Flat && !inst.FullCodecs }

// ctrlCall renders a call to an entity control method (frame lifecycle,
// record drain, plain setters): a plain method call when the control
// surface is exported, or the type-prefixed driver function call (living in
// the builder's package) when it is private. E.g. ctrlCall("inst.dml",
// "CommitEntity") is "inst.dml.CommitEntity()" exported, or
// "lowlevel.InEntity<Table>CommitEntity(inst.dml)" private.
func (inst emitter) ctrlCall(recv, method string, args ...string) string {
	if inst.privateControl() {
		all := append([]string{recv}, args...)
		return inst.lowQ() + inst.dmlType() + method + "(" + strings.Join(all, ", ") + ")"
	}
	return recv + "." + method + "(" + strings.Join(args, ", ") + ")"
}

// emitBeginFrame writes the open-frame + envelope-setter sequence over recv
// (a dml pointer expression): the fluent method chain when control is
// exported, or sequential driver-function calls when it is private (drivers
// do not chain — they take the builder as their first argument).
func (inst emitter) emitBeginFrame(p func(string, ...any), recv, lifecycleExpr, envVar string) {
	if !inst.privateControl() {
		p("\t%s.BeginEntity()%s", recv, inst.dmlChain(lifecycleExpr, envVar))
		return
	}
	p("\t%s", inst.ctrlCall(recv, "BeginEntity"))
	for _, g := range inst.model.groups {
		args := make([]string, 0, len(g.cols))
		for _, c := range g.cols {
			switch c.role {
			case roleKey:
				args = append(args, "id")
			case roleOrder:
				args = append(args, inst.orderArg())
			case roleLifecycle:
				args = append(args, lifecycleExpr)
			default:
				args = append(args, envVar+"."+c.pascal)
			}
		}
		p("\t%s", inst.ctrlCall(recv, itemTypeToSetterName(g.itemType), args...))
	}
}

// emitEnvelopeStruct renders the pass-through envelope carrier — one field
// per non-role plain column, in canonical order. Nothing is emitted for a
// role-only schema (the pre-pass-through shape).
// emitMembershipIds emits the membership-id assignment this store was
// generated under: per component, the ids the configured wrapper bakes
// into that component's codec and this generator bakes into its Scan
// filter literals — declaration-order 1..N under the default NoOpWrapper,
// the caller-assigned registry snapshot under a FixedIdsWrapper.
//
// It is emitted as readable data because nothing on the wire records it. The
// ids are values in the membership columns, indistinguishable from any other
// kind's ids in a FAT table, so a store pointed at rows written under a
// different assignment matches nothing and decodes every component as absent
// — without an error (see VerifySchema, which checks columns only). A
// migration or a startup check therefore needs the assignment in a form it can
// compare; this is that form.
func (inst emitter) emitMembershipIds(sb *strings.Builder, comps []storeComponent) {
	if len(comps) == 0 {
		return
	}
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// %sMembershipIds is the membership-id assignment this store was", inst.StoreName)
	p("// generated under: component kind -> membership name -> the uint64 id")
	p("// carried in the membership columns. Verbatim-channel memberships embed")
	p("// their literal name instead and are absent here.")
	p("//")
	if inst.registryIds {
		p("// The ids are caller-assigned (a registry-stable snapshot), baked into")
	} else {
		p("// The ids are declaration-order (1..N per component) and are baked into")
	}
	p("// both the component codecs and this store's Scan filters. Nothing on the")
	p("// wire records which assignment wrote a row, so rows written under a")
	p("// different one decode as ABSENT rather than failing — VerifySchema")
	p("// cannot see it. Compare this map against the writer's before pointing a")
	p("// regenerated store at existing rows.")
	p("var %sMembershipIds = map[string]map[string]uint64{", inst.StoreName)
	for _, c := range comps {
		ids := c.ids
		names := make([]string, 0, len(ids))
		for n := range ids {
			names = append(names, n)
		}
		// Deterministic emission: by assigned id, which is declaration order.
		sort.Slice(names, func(i, j int) bool { return ids[names[i]] < ids[names[j]] })
		p("\t%q: {", c.Kind)
		for _, n := range names {
			p("\t\t%q: %d,", n, ids[n])
		}
		p("\t},")
	}
	p("}")
	p("")
}

func (inst emitter) emitEnvelopeStruct(sb *strings.Builder) {
	if len(inst.model.passthrough) == 0 {
		return
	}
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("// %sEnvelope carries the pass-through backbone columns — every plain", inst.StoreName)
	p("// column that is not the Key, Order or state-view Lifecycle role. Pass")
	p("// one to Begin; it is written verbatim onto the row and read back onto")
	p("// the entity.")
	p("type %sEnvelope struct {", inst.StoreName)
	for _, c := range inst.model.passthrough {
		p("\t%s %s", c.pascal, c.goType)
	}
	p("}")
	p("")
}

// classifyComponent validates a component against the store's decode
// coverage — exactly marshallgen's ReadRow gate, so the store generator
// and the codec emission cannot disagree — and bakes the component's
// ADR-0066 Filter artefact for the Scan verb.
func classifyComponent(plan *mappingplan.Plan, info *readback.InformationRetrieval, idSrc marshallgen.MembershipIdSourceI) (sc storeComponent, err error) {
	sc.Kind = plan.KindType
	sc.plan = plan
	sc.groups = goplan.ComputeGroups(plan)
	if ok, reason := marshallgen.ReadRowSupported(plan); !ok {
		err = eb.Build().Str("kind", sc.Kind).Str("reason", reason).Errorf("<Kind>ReadRow is not emitted for this component shape (ADR-0100 Deferred)")
		return
	}
	sc.ids, err = idSrc.PlanMembershipIds(plan)
	if err != nil {
		err = eb.Build().Str("kind", sc.Kind).Errorf("component: resolve membership ids: %w", err)
		return
	}
	g := readback.NewGenerator(info, readback.NewLookupResolver(marshallreflect.MapLookup(sc.ids)))
	artefacts, err := g.Generate(plan)
	if err != nil {
		err = eb.Build().Str("kind", sc.Kind).Errorf("component: generate read-back artefacts: %w", err)
		return
	}
	sc.filter = artefacts.Filter
	sc.presence = artefacts.Presence
	sc.validator = artefacts.Validator
	sc.projection = artefacts.Projection
	return
}

// refBound reports whether a section group is bound through at least one
// ref channel — a membership resolved by uint64 id, the only kind that can
// alias across kinds under per-plan ids. A group without memberships is
// treated as ref-bound, the conservative side of the gate.
func refBound(g goplan.SectionGroup) bool {
	if len(g.Memberships) == 0 {
		return true
	}
	for _, m := range g.Memberships {
		if m.Flags.Channel.NeedsKindVar() {
			return true
		}
	}
	return false
}

// verbatimSlot is one (section, literal membership name) a plan binds.
type verbatimSlot struct{ section, name string }

// verbatimSlots returns the plan's literal-name memberships, one per
// distinct slot, sorted for deterministic gate iteration.
func verbatimSlots(plan *mappingplan.Plan) (slots []verbatimSlot) {
	seen := map[verbatimSlot]bool{}
	for _, f := range plan.Fields {
		if f.LWMembership == "" || f.Flags.Channel.NeedsKindVar() {
			continue
		}
		slot := verbatimSlot{section: f.Section(), name: f.LWMembership}
		if seen[slot] {
			continue
		}
		seen[slot] = true
		slots = append(slots, slot)
	}
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].section != slots[j].section {
			return slots[i].section < slots[j].section
		}
		return slots[i].name < slots[j].name
	})
	return
}

// sortedIdNames returns the id map's membership names sorted, for
// deterministic gate iteration and error selection.
func sortedIdNames(ids map[string]uint64) []string {
	names := make([]string, 0, len(ids))
	for n := range ids {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// --- emission helpers. The emitted shapes mirror example/device_store.go. ---

func (inst Input) dmlType() string { return "InEntity" + upperFirst(inst.TableName) + "Table" }

// raPrefix is the generated read-access class prefix. It encodes the
// StylableName the RA package was generated under — this generator's own
// (TableName + "_table"), or the bound package's when SharedRA states a
// different one.
func (inst Input) raPrefix() string {
	if inst.SharedRA != nil {
		return "ReadAccess" + upperFirst(inst.SharedRA.Stylable)
	}
	return "ReadAccess" + upperFirst(inst.TableName) + "Table"
}

func (inst Input) entityType() string      { return inst.StoreName + "Entity" }
func (inst Input) storeType() string       { return inst.StoreName + "Store" }
func (inst Input) builderType() string     { return inst.StoreName + "EntityBuilder" }
func (inst Input) cacheType() string       { return inst.StoreName + "Cache" }
func (inst Input) cacheConfigType() string { return inst.StoreName + "CacheConfig" }

// lowQ qualifies emitted-scaffolding references from the store file:
// empty in the Flat layout, "lowlevel." otherwise.
func (inst Input) lowQ() string {
	if inst.Flat {
		return ""
	}
	return "lowlevel."
}

// raQ qualifies read-access references, which part company with lowQ once
// SharedRA binds a package of their own.
func (inst Input) raQ() string {
	if inst.SharedRA != nil {
		return inst.SharedRA.Package + "."
	}
	return inst.lowQ()
}

// codecName renders a per-kind codec identifier the store calls
// (AddSections, ReadRow): exported under FullCodecs, unexported under
// the default trimmed store-support emission.
func (inst Input) codecName(kind, suffix string) string {
	if inst.FullCodecs {
		return kind + suffix
	}
	return lowerFirst(kind) + suffix
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
	if !inst.ExternallyProvisioned {
		p("\t_ %q", "embed")
	}
	p("\t%q", "iter")
	if inst.model.needsSlices() {
		p("\t%q", "slices")
	}
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
	if inst.hasComps {
		p("\t%q", "github.com/stergiotis/boxer/public/functional/option")
	}
	p("\t%q", "github.com/stergiotis/boxer/public/observability/eh")
	if inst.hasComps {
		// The published artefact Set (ADR-0189 §SD1); emitted only with
		// components, since a store without them publishes nothing.
		p("\t%q", "github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql")
	}
	// The entity builder always holds the deferred buffer: Commit flushes it
	// and Raw marks it, whether or not any component is bound.
	p("\tdmlruntime %q", "github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime")
	p("\t%q", "github.com/stergiotis/boxer/public/storage/recordstore")
	if !inst.Flat {
		p("\t%q", inst.ImportPath+"/internal/lowlevel")
	}
	if inst.SharedRA != nil {
		p("\t%q", inst.SharedRA.ImportPath)
	}
	p("\traruntime %q", "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime")
	p(")")
	p("")
	if !inst.ExternallyProvisioned {
		p("// The complete CREATE TABLE composed at generation time through the")
		p("// ADR-0102 table-clause seam (engine, ORDER BY over the envelope")
		p("// roles, indexes, settings — physical names resolved via the IR).")
		p("//go:embed %s_ddl_clickhouse.out.sql", inst.TableName)
		p("var %sDDLCreate string", inst.TableName)
		p("")
	}
	p("// %sTableName is the ClickHouse table this store binds — database-", inst.StoreName)
	p("// qualified (\"<db>.<table>\") when a Database was set at generation.")
	p("const %sTableName = %q", inst.StoreName, inst.qualifiedTableName())
	p("")
	p("// Physical (encoded, quoted) names of the envelope role columns,")
	p("// derived from the IR at generation time — exported so consumers can")
	p("// address them in ScanOpts.ExtraPredicate and their own SQL.")
	p("const (")
	p("\t%sColKey = `\"%s\"`", inst.StoreName, key.physical)
	p("\t%sColOrder = `\"%s\"`", inst.StoreName, order.physical)
	if inst.model.stateView {
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
	if inst.orderU64() {
		p("\t// Ord is the Order role value — the caller-supplied, per-key")
		p("\t// strictly monotonic sequence (ADR-0100 SD2).")
		p("\tOrd uint64")
	} else {
		p("\tTs time.Time")
	}
	if inst.model.stateView {
		p("\tLifecycle uint8")
	}
	if len(inst.model.passthrough) > 0 {
		p("\t// %sEnvelope is embedded: its pass-through columns read as", inst.StoreName)
		p("\t// promoted entity fields.")
		p("\t%sEnvelope", inst.StoreName)
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
	if inst.model.stateView {
		p("// IsTombstone reports whether this row is a state-view deletion")
		p("// marker — what the tombstone-blind verbs (Latest, Replay, the")
		p("// cache's Get) hand back for a deleted key. It is the tombstone")
		p("// pair's DEFAULT binding; the verbs consult the configured")
		p("// TombstoneDetect when one is set, and so must callers holding a")
		p("// store whose config overrides the pair.")
		p("func (inst *%s) IsTombstone() bool {", inst.entityType())
		p("\treturn inst.Lifecycle == recordstore.LifecycleTombstone")
		p("}")
		p("")
	}
}

func (inst emitter) emitStoreType(sb *strings.Builder) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	p("type %sStoreConfig struct {", inst.StoreName)
	p("\t// Table overrides the ClickHouse table this store binds — the baked")
	p("\t// %sTableName — for every statement it issues (DDL, DESCRIBE, INSERT,", inst.StoreName)
	p("\t// SELECT). Optionally database-qualified (\"<db>.<table>\"), unquoted-")
	p("\t// identifier shape only ([A-Za-z_][A-Za-z0-9_]* per part; the")
	p("\t// constructor panics otherwise). Empty (the default) binds the baked")
	p("\t// name. The schema is unchanged — this moves WHERE the rows land, not")
	p("\t// what they look like — so a scratch table for a test or a per-")
	p("\t// deployment table needs no regeneration.")
	p("\tTable string")
	if !inst.ExternallyProvisioned {
		p("\t// DDLTail is a raw suffix appended verbatim after the composed")
		p("\t// CREATE TABLE at EnsureTable time — the escape hatch for clauses")
		p("\t// the generation-time table options (ADR-0102) do not carry.")
		p("\tDDLTail string")
	}
	p("\t// Stampers are consulted on every Begin (ADR-0112 M1): each yields")
	p("\t// surrogate ids stamped as additive HighCardRef memberships onto the")
	p("\t// entity's attributes. Empty (the default) leaves the store unstamped")
	p("\t// and behaviour-identical. A stamper must not write to this store.")
	p("\t// The schema must carry the HighCardRef membership lane, and no")
	p("\t// component may read that lane back as data — the constructor")
	p("\t// panics otherwise (ADR-0112 SD2 lane hygiene).")
	p("\tStampers []recordstore.ReferenceStamper")
	p("\t// BestEffortStampFlush relaxes the ADR-0112 SD5 ordered flush: when")
	p("\t// true, Flush does NOT flush the stampers' dimension stores before its")
	p("\t// own insert, so a referencing row may become durable ahead of its")
	p("\t// descriptor fact (resolution self-heals on the dimension's own flush).")
	p("\t// The default keeps the descriptor durable no later than the row.")
	p("\tBestEffortStampFlush bool")
	if inst.stateViewOn() {
		p("\t// TombstoneDetect is the read half of the state-view tombstone pair")
		p("\t// (ADR-0100 Update 2026-08-30): GetLive — the store's and the cache")
		p("\t// views' — reads a detected row as absent. It may consult any entity")
		p("\t// content, envelope fields or component attributes. It runs Go-side")
		p("\t// on the newest row per key, which is all GetLive needs; it cannot")
		p("\t// push down into SQL, so Scan verbs and hand-written SQL apply their")
		p("\t// own discipline. Supply BOTH halves or neither: Delete writes what")
		p("\t// the predicate recognises, and a lone half would tear the pair.")
		if inst.model.stateView {
			p("\t// Nil (the default) binds the u8 Lifecycle marker")
			p("\t// (Entity.IsTombstone).")
		} else {
			p("\t// This store binds no u8 Lifecycle role (generated with")
			p("\t// TombstoneView), so the pair is required — the constructor")
			p("\t// panics on a nil half.")
		}
		p("\tTombstoneDetect func(*%s) bool", inst.entityType())
		p("\t// TombstoneWrite is the write half of the pair: Delete opens an")
		p("\t// entity frame for (key, Order) and hands the builder here to")
		p("\t// compose the marker row the configured TombstoneDetect recognises.")
		p("\t// The marker is an ordinary row: a Scan whose filter it satisfies")
		p("\t// returns it, so pick marker content the store's scans exclude.")
		if inst.model.stateView {
			p("\t// Nil (the default) appends the bare envelope row with")
			p("\t// Lifecycle = recordstore.LifecycleTombstone. When set, Delete's")
			p("\t// frame carries Lifecycle = recordstore.LifecycleLive — the u8")
			p("\t// column stops being the marker; the pair is the marker.")
		}
		p("\tTombstoneWrite func(*%s)", inst.builderType())
	}
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
	p("\t// next successful Flush (or DiscardPending). Attached cache views")
	p("\t// pin these keys until the flush lands (eviction cannot expose the")
	p("\t// pre-write row) and their fetchers refuse to cache them — the")
	p("\t// remaining guard for writes the views could not materialize (Raw")
	p("\t// commits) and for InvalidateAll inside a dirty window.")
	p("\tdirty map[%s]struct{}", inst.keyGoType)
	p("\t// onWrite/onFlush hold the write-through hooks of attached cache")
	p("\t// views (New%s registers one pair per view): onWrite populates", inst.cacheType())
	p("\t// and pins the committed entity (nil = not materializable or a")
	p("\t// discarded write — invalidate instead); onFlush releases the pin")
	p("\t// once the row is durable.")
	p("\tonWrite []func(%s, *%s)", inst.keyGoType, inst.entityType())
	p("\tonFlush []func(%s)", inst.keyGoType)
	p("\t// stampers is cfg.Stampers, consulted per Begin (ADR-0112 M1).")
	p("\tstampers []recordstore.ReferenceStamper")
	p("}")
	p("")
	p("// New%s wires the store. A nil alloc selects the Go allocator.", inst.storeType())
	if !inst.stampLane || inst.stampLaneAsData {
		p("// Configuring Stampers panics: see the field's doc — this schema")
		p("// cannot carry stamps soundly.")
	}
	p("func New%s(exec recordstore.ExecutorI, alloc memory.Allocator, cfg %sStoreConfig) (inst *%s) {", inst.storeType(), inst.StoreName, inst.storeType())
	// Lane-hygiene guards (ADR-0112 SD2): a wiring bug this static fails
	// at construction, not silently at write or corruptly at read. Emitted
	// only for schemas that cannot carry stamps, so a stampable store pays
	// nothing.
	if !inst.stampLane {
		p("\tif len(cfg.Stampers) > 0 {")
		p("\t\tpanic(\"%s: Stampers configured, but no section of the %s schema declares a HighCardRef membership column — stamps would be dropped silently; declare the channel (AddSectionMembership) or drop the stampers\")", inst.storeType(), inst.TableName)
		p("\t}")
	} else if inst.stampLaneAsData {
		p("\tif len(cfg.Stampers) > 0 {")
		p("\t\tpanic(\"%s: Stampers configured, but a component reads the HighCardRef membership lane back as data (a tuple @membership field on the highCardRef channel) — a stamp would decode as a spurious ref id; move the ref memberships to another lane or drop the stampers\")", inst.storeType())
		p("\t}")
	}
	if inst.stateViewOn() {
		p("\tif (cfg.TombstoneDetect == nil) != (cfg.TombstoneWrite == nil) {")
		p("\t\tpanic(\"%s: the tombstone pair comes whole — supply both TombstoneDetect and TombstoneWrite, or neither\")", inst.storeType())
		p("\t}")
		if !inst.model.stateView {
			p("\tif cfg.TombstoneDetect == nil {")
			p("\t\tpanic(\"%s: generated with TombstoneView and no u8 Lifecycle role — the tombstone pair (TombstoneDetect, TombstoneWrite) is required\")", inst.storeType())
			p("\t}")
		}
	}
	p("\tif cfg.Table != \"\" {")
	p("\t\tif terr := recordstore.CheckTableRef(cfg.Table); terr != nil {")
	p("\t\t\tpanic(\"%s: \" + terr.Error())", inst.storeType())
	p("\t\t}")
	p("\t}")
	p("\tif alloc == nil {")
	p("\t\talloc = memory.NewGoAllocator()")
	p("\t}")
	p("\tinst = &%s{exec: exec, alloc: alloc, cfg: cfg, dml: %sNew%s(alloc, 64), dirty: make(map[%s]struct{}), stampers: cfg.Stampers}", inst.storeType(), inst.lowQ(), inst.dmlType(), inst.keyGoType)
	p("\treturn")
	p("}")
	p("")
	p("// tableName is the table reference every statement uses: the configured")
	p("// override when set, else the baked %sTableName.", inst.StoreName)
	p("func (inst *%s) tableName() string {", inst.storeType())
	p("\tif inst.cfg.Table != \"\" {")
	p("\t\treturn inst.cfg.Table")
	p("\t}")
	p("\treturn %sTableName", inst.StoreName)
	p("}")
	p("")
	if inst.stateViewOn() {
		p("// isTombstone applies the tombstone pair's read half — the interpreted")
		p("// state-view verbs (GetLive here and on the cache views) route through")
		p("// it, so the pair and the verbs cannot disagree.")
		if inst.model.stateView {
			p("func (inst *%s) isTombstone(e *%s) bool {", inst.storeType(), inst.entityType())
			p("\tif inst.cfg.TombstoneDetect != nil {")
			p("\t\treturn inst.cfg.TombstoneDetect(e)")
			p("\t}")
			p("\treturn e.IsTombstone()")
			p("}")
		} else {
			p("func (inst *%s) isTombstone(e *%s) bool { return inst.cfg.TombstoneDetect(e) }", inst.storeType(), inst.entityType())
		}
		p("")
	}
	p("// applyStampers consults the configured stampers and pushes their surrogate")
	p("// ids as ambient HighCardRef memberships onto the open entity (ADR-0112 M1),")
	p("// returning the count so Commit/Rollback pop exactly that many.")
	p("// context.Background() bounds the interning — the in-memory interner ignores")
	p("// it; a ctx-carrying Begin is the future seam for a durable one. No stampers")
	p("// means no pushes: inert.")
	p("func (inst *%s) applyStampers() (pushed int) {", inst.storeType())
	p("\tfor _, s := range inst.stampers {")
	p("\t\tfor id, err := range s.Current(context.Background()) {")
	p("\t\t\tif err != nil {")
	p("\t\t\t\tinst.dml.AppendError(err)")
	p("\t\t\t\tcontinue")
	p("\t\t\t}")
	p("\t\t\tinst.dml.PushMembershipHighCardRef(uint64(id))")
	p("\t\t\tpushed++")
	p("\t\t}")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// notifyWrite fires the write-through hooks of attached cache views:")
	p("// ent is the just-committed entity (the views populate and pin it),")
	p("// or nil when the row is not faithfully materializable (Raw commits)")
	p("// or a buffered write is being discarded — the views then invalidate")
	p("// the key instead.")
	p("func (inst *%s) notifyWrite(key %s, ent *%s) {", inst.storeType(), inst.keyGoType, inst.entityType())
	p("\tfor _, f := range inst.onWrite {")
	p("\t\tf(key, ent)")
	p("\t}")
	p("}")
	p("")
	p("// notifyFlush releases the attached views' dirty-window pins after a")
	p("// successful Flush made the key durable.")
	p("func (inst *%s) notifyFlush(key %s) {", inst.storeType(), inst.keyGoType)
	p("\tfor _, f := range inst.onFlush {")
	p("\t\tf(key)")
	p("\t}")
	p("}")
	p("")
	if !inst.ExternallyProvisioned {
		p("// EnsureTable applies the composed CREATE TABLE (plus the DDLTail")
		p("// suffix, when configured). Idempotent (CREATE TABLE IF NOT EXISTS).")
		p("// The embedded script is issued one statement per Exec — the")
		p("// optional CREATE DATABASE, then the CREATE TABLE — because the")
		p("// ClickHouse HTTP interface rejects a multi-statement body; under a")
		p("// Table override the statements are re-pointed at the override")
		p("// (recordstore.ProvisioningStatements: header and database only, the")
		p("// column block stays byte-identical).")
		p("func (inst *%s) EnsureTable(ctx context.Context) (err error) {", inst.storeType())
		p("\tstmts, err := recordstore.ProvisioningStatements(%sDDLCreate, %sTableName, inst.cfg.Table)", inst.TableName, inst.StoreName)
		p("\tif err != nil {")
		p("\t\terr = eh.Errorf(\"ensure table %%s: %%w\", inst.tableName(), err)")
		p("\t\treturn")
		p("\t}")
		p("\tif inst.cfg.DDLTail != \"\" {")
		p("\t\tstmts[len(stmts)-1] += \" \" + inst.cfg.DDLTail")
		p("\t}")
		p("\tfor _, sql := range stmts {")
		p("\t\terr = inst.exec.Exec(ctx, sql)")
		p("\t\tif err != nil {")
		p("\t\t\terr = eh.Errorf(\"ensure table %%s: %%w\", inst.tableName(), err)")
		p("\t\t\treturn")
		p("\t\t}")
		p("\t}")
		p("\treturn")
		p("}")
		p("")
	}
	p("// VerifySchema compares the live table's columns — names and order —")
	if inst.ExternallyProvisioned {
		p("// against the generated schema. This store cannot create or repair")
		p("// its table (generated ExternallyProvisioned), so nothing here")
		p("// constrains the live shape, and the decode is positional: drift")
		p("// fails late or, for same-typed column swaps, silently. Run")
		p("// VerifySchema at startup, before the first read;")
		p("// %s_ddl_clickhouse.out.sql states the shape it expects.", inst.TableName)
	} else {
		p("// against the generated schema. EnsureTable alone cannot detect drift")
		p("// on an existing table (IF NOT EXISTS succeeds against any old shape),")
		p("// and the decode is positional, so drift fails late or, for same-typed")
		p("// column swaps, silently: run VerifySchema at startup after")
		p("// EnsureTable.")
	}
	p("//")
	p("// It checks the COLUMN contract only. The membership-id contract is not")
	p("// checked and cannot be from the schema alone: the ids live in the")
	p("// membership columns as ordinary values, and a FAT table legitimately")
	p("// carries other kinds' ids beside this store's. Rows written under a")
	p("// different id assignment therefore pass VerifySchema and then match")
	p("// nothing — every component decodes absent, with no error. Compare")
	p("// %sMembershipIds against the writer's assignment when pointing this", inst.StoreName)
	p("// store at rows it did not write.")
	p("//")
	p("// What it describes is the reader's own projection — DESCRIBE over")
	p("// `SELECT *`, not over the table — because that is what the positional")
	p("// decode consumes. DESCRIBE TABLE lists columns SELECT * does not return")
	p("// (MATERIALIZED, ALIAS, EPHEMERAL), so a table legitimately carrying one")
	p("// beside the generated shape — a derived column added by ALTER after")
	p("// EnsureTable, which is how a store gets a skip index over a value its")
	p("// leeway attributes only encode — would fail a check whose contract still")
	p("// held. Describing the projection also follows the asterisk_include_*")
	p("// settings, which decide what SELECT * returns and which nothing here")
	p("// pins: under those, a derived column IS in the decode, and this notices")
	p("// where a column-kind filter would have blessed the mis-decode.")
	p("func (inst *%s) VerifySchema(ctx context.Context) (err error) {", inst.storeType())
	p("\tlive := make([]string, 0, 64)")
	p("\tfor rec, rerr := range inst.exec.QueryArrow(ctx, \"DESCRIBE (SELECT * FROM \"+inst.tableName()+\")\"+%sArrowOutputSettings) {", inst.TableName)
	p("\t\tif rerr != nil {")
	p("\t\t\terr = eh.Errorf(\"describe table %%s: %%w\", inst.tableName(), rerr)")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\tnames, ok := rec.Column(0).(*array.String)")
	p("\t\tif !ok {")
	p("\t\t\terr = eh.Errorf(\"describe table %%s: name column is %%s, not a string\", inst.tableName(), rec.Column(0).DataType())")
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
	p("\t\terr = eh.Errorf(\"schema drift on %%s: table has %%d columns, the generated schema expects %%d — regenerated code against an old table (or vice versa); migrate or regenerate\", inst.tableName(), len(live), len(want))")
	p("\t\treturn")
	p("\t}")
	p("\tfor i, f := range want {")
	p("\t\tif live[i] != f.Name {")
	p("\t\t\terr = eh.Errorf(\"schema drift on %%s: column %%d is %%q, the generated schema expects %%q — the decode is positional; migrate or regenerate\", inst.tableName(), i, live[i], f.Name)")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
}

// emitBuilderEndSection renders the callback the deferred buffer closes
// sections through. It covers the union of the components' sections, since the
// buffer names a section by string and the DML names it by method.
func (inst emitter) emitBuilderEndSection(sb *strings.Builder, comps []storeComponent) {
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }
	seen := map[string]struct{}{}
	sections := make([]string, 0, 4)
	for _, c := range comps {
		for _, g := range c.groups {
			if _, has := seen[g.Section]; has {
				continue
			}
			seen[g.Section] = struct{}{}
			sections = append(sections, g.Section)
		}
	}
	p("// endSection closes one section's frame. The buffer calls it once per")
	p("// section, after every component that contributed to it has written.")
	p("func (inst *%s) endSection(section string) error {", inst.builderType())
	if len(sections) == 0 {
		p("\t_ = section")
		p("\treturn nil")
		p("}")
		p("")
		return
	}
	p("\tswitch section {")
	for _, sec := range sections {
		p("\tcase %q:", sec)
		p("\t\tinst.store.dml.GetSection%s().EndSection()", mappingplan.UpperFirst(sec))
	}
	p("\t}")
	p("\treturn nil")
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
	p("\t// ent mirrors the typed Add* calls so Commit can write the entity")
	p("\t// through to attached cache views; raw marks commits that touched")
	p("\t// the DML directly — those cannot be materialized faithfully and")
	p("\t// invalidate the key instead.")
	p("\tent %s", inst.entityType())
	p("\traw bool")
	p("\t// buf holds each component's section contributions until Commit, so")
	p("\t// components sharing a section share its frame. It also holds the")
	p("\t// frame invariants: one contribution per kind, and typed Adds")
	p("\t// exclusive with Raw (ADR-0183 D4).")
	p("\tbuf dmlruntime.DeferredSectionBuffer")
	p("\t// pushed counts the ambient memberships Begin pushed via the stampers;")
	p("\t// Commit/Rollback pop exactly that many (ADR-0112 M1).")
	p("\tpushed int")
	p("}")
	p("")
	inst.emitBuilderEndSection(sb, comps)
	hasPT := len(inst.model.passthrough) > 0
	p("// Begin opens one entity with the envelope roles as typed arguments")
	extras := make([]string, 0, 2)
	if inst.model.stateView {
		extras = append(extras, "a live lifecycle")
	}
	if hasPT {
		extras = append(extras, "the pass-through envelope")
	}
	beginSuffix := ""
	switch len(extras) {
	case 1:
		beginSuffix = " and " + extras[0]
	case 2:
		beginSuffix = ", " + extras[0] + " and " + extras[1]
	}
	p("// (Key, Order)%s.", beginSuffix)
	sig := fmt.Sprintf("func (inst *%s) Begin(id %s, %s %s", inst.storeType(), inst.keyGoType, inst.orderArg(), inst.orderGoType)
	if hasPT {
		sig += fmt.Sprintf(", env %sEnvelope", inst.StoreName)
	}
	p("%s) *%s {", sig, inst.builderType())
	lit := fmt.Sprintf("%s{ID: id, %s: %s", inst.entityType(), inst.orderEntityField(), inst.orderArg())
	if inst.model.stateView {
		lit += ", Lifecycle: recordstore.LifecycleLive"
	}
	if hasPT {
		lit += fmt.Sprintf(", %sEnvelope: env", inst.StoreName)
	}
	lit += "}"
	inst.emitBeginFrame(p, "inst.dml", "recordstore.LifecycleLive", "env")
	p("\tb := &%s{store: inst, key: id, ent: %s}", inst.builderType(), lit)
	p("\tb.pushed = inst.applyStampers()")
	p("\treturn b")
	p("}")
	p("")
	for _, c := range comps {
		p("// Add%s contributes the %s component to the open entity.", c.Kind, c.Kind)
		p("//")
		p("// The attributes are buffered, not written: a section frame closes for")
		p("// good, so a component that closed its own sections would shut out the")
		p("// next component sharing one. Commit writes them, one frame per section")
		p("// in first-seen order (ADR-0183 D4).")
		p("//")
		p("// A second Add of this component, or an Add on an entity already using")
		p("// Raw(), is refused: both used to mark the row un-mirrorable and carry")
		p("// on, which made its read-back shape depend on a call the writer had")
		p("// probably made by accident.")
		p("func (inst *%s) Add%s(row %s) *%s {", inst.builderType(), c.Kind, c.Kind, inst.builderType())
		p("\tif err := inst.buf.StartKind(%q); err != nil {", c.Kind)
		p("\t\tinst.store.dml.AppendError(err)")
		p("\t\treturn inst")
		p("\t}")
		for _, g := range c.groups {
			method := mappingplan.UpperFirst(g.Section)
			p("\tinst.buf.Enqueue(%q, %q, func() error {", g.Section, c.Kind)
			p("\t\treturn %s(inst.store.dml.GetSection%s(), row)", inst.codecName(c.Kind, "EmitSection"+method), method)
			p("\t})")
		}
		p("\tinst.ent.%s = option.Some(row)", c.Kind)
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
	p("\tif err := inst.buf.MarkRaw(); err != nil {")
	p("\t\tinst.store.dml.AppendError(err)")
	p("\t}")
	p("\tinst.raw = true // direct DML writes cannot be mirrored into the entity")
	p("\treturn inst.store.dml")
	p("}")
	p("")
	p("// Commit finishes the open entity, buffers the row, and writes it")
	p("// through to attached cache views: the entity is populated and pinned")
	p("// until the store's Flush makes it durable — reads after writes hit")
	p("// immediately, and the caching version gate plus the pin make a raced")
	p("// refetch of the pre-write row bounce off. A commit that touched")
	p("// Raw() cannot be materialized faithfully and invalidates the key")
	p("// instead. A failed Commit rolls the frame back — the entity is")
	p("// discarded and the store stays usable.")
	p("func (inst *%s) Commit() (err error) {", inst.builderType())
	p("\tif ferr := inst.buf.Flush(inst.endSection); ferr != nil {")
	p("\t\tinst.store.dml.AppendError(ferr) // surfaced by CommitEntity below")
	p("\t}")
	p("\tinst.buf.Reset()")
	p("\terr = %s", inst.ctrlCall("inst.store.dml", "CommitEntity"))
	p("\tinst.store.dml.PopMembershipsHighCardRef(inst.pushed) // release Begin's ambient stamps (consumed by the closed attributes)")
	p("\tif err != nil {")
	p("\t\t_ = %s // no-op error when no frame is open", inst.ctrlCall("inst.store.dml", "RollbackEntity"))
	p("\t\treturn")
	p("\t}")
	p("\tinst.store.buffered++")
	p("\tinst.store.dirty[inst.key] = struct{}{}")
	p("\tif inst.raw {")
	p("\t\tinst.store.notifyWrite(inst.key, nil)")
	p("\t} else {")
	p("\t\tent := inst.ent")
	p("\t\tinst.store.notifyWrite(inst.key, &ent)")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// Rollback abandons the open entity frame without committing it;")
	p("// already-buffered rows and the store remain usable.")
	p("func (inst *%s) Rollback() (err error) {", inst.builderType())
	p("\tinst.buf.Reset() // the buffered contributions are abandoned with the frame")
	p("\tinst.store.dml.PopMembershipsHighCardRef(inst.pushed)")
	p("\treturn %s", inst.ctrlCall("inst.store.dml", "RollbackEntity"))
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
			err = eb.Build().Str("kind", c.Kind).Str("idField", idCol.GoField).Str("idFieldGoType", gt).Str("keyGoType", inst.keyGoType).Errorf("component plain id field's Go type does not match the Key column — Ingest<Kind> cannot be emitted")
			return
		}
		ord := inst.orderArg()
		p("// Ingest%s buffers one whole entity per row carrying only the", c.Kind)
		p("// %s component, all stamped with %s — rows ship on the next Flush,", c.Kind, ord)
		p("// like every write. Keys must be distinct within one call (rows")
		p("// share %s, so duplicates would tie on Order): a duplicate returns", ord)
		p("// recordstore.ErrDuplicateIngestKey. On any error the rows buffered")
		p("// so far remain buffered — Flush ships them, DiscardPending drops")
		p("// them.")
		p("func (inst *%s) Ingest%s(%s %s, rows []%s) (err error) {", inst.storeType(), c.Kind, ord, inst.orderGoType, c.Kind)
		p("\tseen := make(map[%s]struct{}, len(rows))", inst.keyGoType)
		p("\tfor i := range rows {")
		p("\t\tif _, dup := seen[rows[i].%s]; dup {", idCol.GoField)
		p("\t\t\terr = eh.Errorf(\"ingest %s row %%d: %%w: key %%v\", i, recordstore.ErrDuplicateIngestKey, rows[i].%s)", lowerFirst(c.Kind), idCol.GoField)
		p("\t\t\treturn")
		p("\t\t}")
		p("\t\tseen[rows[i].%s] = struct{}{}", idCol.GoField)
		beginArgs := fmt.Sprintf("rows[i].%s, %s", idCol.GoField, ord)
		if len(inst.model.passthrough) > 0 {
			beginArgs += fmt.Sprintf(", %sEnvelope{}", inst.StoreName)
		}
		p("\t\terr = inst.Begin(%s).Add%s(rows[i]).Commit()", beginArgs, c.Kind)
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
	p("\t// Ordered flush (ADR-0112 SD5): make the dimension facts this batch")
	p("\t// references durable before the payload insert, so a referencing row is")
	p("\t// never durable ahead of its descriptor. On failure nothing is")
	p("\t// transferred yet — the buffered rows stay and the next Flush retries.")
	p("\t//")
	p("\t// This runs BEFORE the nothing-to-do return: a Begin that stamped and")
	p("\t// then rolled back, or a caller flushing a store whose own rows all")
	p("\t// went elsewhere, still leaves dimension rows buffered in the stampers,")
	p("\t// and skipping them here would strand descriptors no later Flush of")
	p("\t// this store is obliged to ship. Flushing an empty stamper is free.")
	p("\tif !inst.cfg.BestEffortStampFlush {")
	p("\t\tfor _, s := range inst.stampers {")
	p("\t\t\tif _, ferr := s.Flush(ctx); ferr != nil {")
	p("\t\t\t\terr = eh.Errorf(\"ordered flush of stampers: %%w\", ferr)")
	p("\t\t\t\treturn")
	p("\t\t\t}")
	p("\t\t}")
	p("\t}")
	p("\tif inst.buffered == 0 && len(inst.pending) == 0 {")
	p("\t\treturn")
	p("\t}")
	p("\trecords, err := %s", inst.ctrlCall("inst.dml", "TransferRecords", "nil"))
	p("\tif err != nil {")
	p("\t\terr = eh.Errorf(\"transfer records: %%w\", err)")
	p("\t\treturn")
	p("\t}")
	p("\trecords = append(inst.pending, records...)")
	p("\tinst.pending = nil")
	p("\tif len(records) > 0 {")
	p("\t\terr = inst.exec.InsertArrow(ctx, inst.tableName(), records)")
	p("\t\tif err != nil {")
	p("\t\t\tinst.pending = records")
	p("\t\t\terr = eh.Errorf(\"insert into %%s: %%w\", inst.tableName(), err)")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t}")
	p("\tfor _, rec := range records {")
	p("\t\trec.Release()")
	p("\t}")
	p("\tn = inst.buffered")
	p("\tinst.buffered = 0")
	p("\tfor k := range inst.dirty {")
	p("\t\tinst.notifyFlush(k) // durable now — release the views' dirty-window pins")
	p("\t}")
	p("\tclear(inst.dirty) // flushed — ClickHouse now serves the written state")
	p("\treturn")
	p("}")
	p("")
	p("// DiscardPending drops every committed-but-unflushed row: records")
	p("// retained by a failed Flush, rows still in the DML builder, and an")
	p("// open (uncommitted) entity frame. It gives a failed Flush \"never")
	p("// happened\" semantics — ClickHouse state is the truth afterwards.")
	p("// Ambient stamps are cleared with the frame they were pushed for —")
	p("// including any pushed through Raw() — so an abandoned builder cannot")
	p("// leak its stamps onto later entities.")
	p("func (inst *%s) DiscardPending() {", inst.storeType())
	p("\t_ = %s // no-op error when no frame is open", inst.ctrlCall("inst.dml", "RollbackEntity"))
	p("\tinst.dml.ClearMembershipsHighCardRef() // an abandoned Begin's stamps must not outlive its frame")
	p("\tif records, err := %s; err == nil {", inst.ctrlCall("inst.dml", "TransferRecords", "nil"))
	p("\t\tfor _, rec := range records {")
	p("\t\t\trec.Release()")
	p("\t\t}")
	p("\t}")
	p("\tfor _, rec := range inst.pending {")
	p("\t\trec.Release()")
	p("\t}")
	p("\tinst.pending = nil")
	p("\tinst.buffered = 0")
	p("\tfor k := range inst.dirty {")
	p("\t\tinst.notifyWrite(k, nil) // the cached write never became durable — invalidate")
	p("\t}")
	p("\tclear(inst.dirty) // nothing local remains — ClickHouse is the truth")
	p("}")
	p("")
	p("// Close discards everything unflushed and releases the store's Arrow")
	p("// builder; the store must not be used afterwards. Required for a")
	p("// clean shutdown under tracking/checked allocators — the default Go")
	p("// allocator needs no Close.")
	p("func (inst *%s) Close() {", inst.storeType())
	p("\tinst.DiscardPending()")
	if inst.privateControl() {
		p("\t%s%sReleaseBuilder(inst.dml)", inst.lowQ(), inst.dmlType())
	} else {
		p("\tinst.dml.Builder().Release()")
	}
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
	p("\t// FreshnessTTL enables age-based staleness onset (ADR-0100's")
	p("\t// external-writer staleness story): entries older than this read")
	p("\t// as stale — strict reads miss and queue a refetch, accept-stale")
	p("\t// reads keep serving. Zero disables (staleness stays signal-only).")
	p("\tFreshnessTTL time.Duration")
	p("\t// NegativeTTL enables absent-key marking: keys a clean fetch did")
	p("\t// not return are treated as absent for this long — misses on them")
	p("\t// neither queue nor suspend work items, so replay loops over keys")
	p("\t// that do not exist terminate. Zero disables.")
	p("\tNegativeTTL time.Duration")
	p("}")
	p("")
	p("// %s is the batched read-through, write-through KV view over a", inst.cacheType())
	p("// %s (ADR-0100 SD5): misses queue under work items and flush", inst.storeType())
	p("// as one IN (…) lookup, and local writes populate the view at Commit —")
	p("// pinned until the store's Flush makes them durable — so reads after")
	p("// writes hit immediately. Admission is version-gated on the entity's")
	if inst.orderU64() {
		p("// Order value: a raced refetch of an older row bounces off. Only")
	} else {
		p("// Order timestamp: a raced refetch of an older row bounces off. Only")
	}
	p("// EXTERNAL writers can leave the view stale; they need a caller-")
	p("// provided signal: MarkStale / Invalidate / InvalidateAll (a freshness")
	p("// TTL option exists on the underlying cache). Raw() commits and")
	p("// discarded writes invalidate instead of populating. Like the store")
	p("// it wraps, a view is single-goroutine. W is the work-item type (use")
	p("// struct{} when the suspend/replay machinery is not needed).")
	p("type %s[W comparable] struct {", inst.cacheType())
	p("\tst *%s", inst.storeType())
	p("\tcfg %s", inst.cacheConfigType())
	p("\tcache *caching.ReadThroughCache[%s, *%s, W]", inst.keyGoType, inst.entityType())
	p("}")
	p("")
	p("// New%s attaches a read-through, write-through cache view to st,", inst.cacheType())
	p("// registering its write-through and flush hooks with the store. Views")
	p("// attach for the store's lifetime — there is no detach.")
	p("func New%s[W comparable](st *%s, cfg %s) (inst *%s[W]) {", inst.cacheType(), inst.storeType(), inst.cacheConfigType(), inst.cacheType())
	p("\tif cfg.Capacity <= 0 {")
	p("\t\tcfg.Capacity = 1024")
	p("\t}")
	p("\tinst = &%s[W]{st: st, cfg: cfg}", inst.cacheType())
	p("\tinst.rebuild()")
	p("\t// The hooks close over the view, not the cache instance, keeping")
	p("\t// them correct if the cache is ever swapped out.")
	p("\tst.onWrite = append(st.onWrite, func(k %s, ent *%s) {", inst.keyGoType, inst.entityType())
	p("\t\tif ent == nil {")
	p("\t\t\tinst.cache.Delete(k) // not materializable or discarded: invalidate")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\tinst.cache.AddItem(k, ent) // version-gated write-through")
	p("\t\tinst.cache.Pin(k)          // dirty-window latch, released by Flush")
	p("\t})")
	p("\tst.onFlush = append(st.onFlush, func(k %s) { inst.cache.Unpin(k) })", inst.keyGoType)
	p("\treturn")
	p("}")
	p("")
	p("func (inst *%s[W]) rebuild() {", inst.cacheType())
	p("\topts := []caching.CacheOption[%s, *%s, W]{", inst.keyGoType, inst.entityType())
	p("\t\t// Admission mirrors the table's newest-row-per-key semantics:")
	if inst.orderU64() {
		p("\t\t// the Order value is the entity's monotonic version. The int64")
		p("\t\t// reinterpretation preserves the per-key order as long as one")
		p("\t\t// key's values share their top bit — which a single writer")
		p("\t\t// stream per key (the SD2 contract) gives by construction.")
		p("\t\tcaching.WithVersioning[%s, *%s, W](func(e *%s) int64 { return int64(e.Ord) }),", inst.keyGoType, inst.entityType(), inst.entityType())
	} else {
		p("\t\t// the Order timestamp is the entity's monotonic version.")
		p("\t\tcaching.WithVersioning[%s, *%s, W](func(e *%s) int64 { return e.Ts.UnixNano() }),", inst.keyGoType, inst.entityType(), inst.entityType())
	}
	p("\t}")
	p("\tif inst.cfg.FreshnessTTL > 0 {")
	p("\t\topts = append(opts, caching.WithFreshnessTTL[%s, *%s, W](inst.cfg.FreshnessTTL))", inst.keyGoType, inst.entityType())
	p("\t}")
	p("\tif inst.cfg.NegativeTTL > 0 {")
	p("\t\topts = append(opts, caching.WithNegativeCaching[%s, *%s, W](inst.cfg.NegativeTTL))", inst.keyGoType, inst.entityType())
	p("\t}")
	p("\tinst.cache = caching.NewReadThroughCache[%s, *%s, W](inst.cfg.Capacity, &%sFetcher{st: inst.st}, inst.cfg.FetchCriteria, opts...)", inst.keyGoType, inst.entityType(), inst.TableName)
	p("}")
	p("")
	p("// Get retrieves an entity by Key through the cache; local writes are")
	p("// visible immediately (write-through). A miss queues the key for the")
	p("// next batch fetch (the caching suspend/replay contract). A miss can")
	p("// also mean the batched fetch errored (misses swallow fetch errors;")
	p("// the circuit breaker backs off) — GetFetch surfaces the error")
	p("// instead, and the store's Latest stays the authoritative check. The")
	p("// returned entity is shared with the cache: treat it as immutable.")
	p("func (inst *%s[W]) Get(key %s) (ent *%s, found bool) {", inst.cacheType(), inst.keyGoType, inst.entityType())
	p("\treturn inst.cache.Get(key)")
	p("}")
	p("")
	p("// GetFetch is the single-lookup read: the cached entity when present,")
	p("// otherwise one immediate batched point fetch — fetch errors surface")
	p("// instead of reading as misses, so found=false with err=nil is the")
	p("// authoritative absent. A key in the dirty write window that the cache")
	p("// could not answer is an error rather than a stale row (see below).")
	p("// Prefer Get plus the work-item protocol when batching lookups across a")
	p("// frame; the initial miss here also queues the key, so a later batch")
	p("// fetch may include it redundantly (harmless).")
	p("func (inst *%s[W]) GetFetch(ctx context.Context, key %s) (ent *%s, found bool, err error) {", inst.cacheType(), inst.keyGoType, inst.entityType())
	p("\tent, found = inst.cache.Get(key)")
	p("\tif found {")
	p("\t\treturn")
	p("\t}")
	p("\tif _, d := inst.st.dirty[key]; d {")
	p("\t\t// The key was written locally and is not yet flushed, so ClickHouse")
	p("\t\t// still serves the PRE-write row — and the cache miss above means")
	p("\t\t// there is no local answer either (a Raw() commit, or a discarded")
	p("\t\t// write, invalidates the entry rather than materializing it; an")
	p("\t\t// ordinary commit is pinned and would have hit). Returning the")
	p("\t\t// pre-write row would be stale, and found=false would assert the")
	p("\t\t// authoritative absent this method promises and cannot stand behind")
	p("\t\t// here — so say so instead. Flush first, or read through Get plus the")
	p("\t\t// work-item protocol, whose fetcher drops dirty keys as a MISS, which")
	p("\t\t// queues a refetch rather than claiming absence.")
	p("\t\terr = eh.Errorf(\"get-fetch: key is in the dirty write window — written locally and not yet flushed, and the write was not materializable into the cache; Flush before reading it back\")")
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
	p("\t\tinst.cache.AddItem(key, e)")
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
	p("// MarkStaleIfOlder is the version-carrying external-writer signal:")
	p("// it stales the cached entry only if its Order is below order, so a")
	p("// redundant signal for a version the view already holds is free —")
	p("// the natural sink for an invalidation stream carrying (key, Order).")
	if inst.orderU64() {
		p("func (inst *%s[W]) MarkStaleIfOlder(key %s, order uint64) {", inst.cacheType(), inst.keyGoType)
		p("\tinst.cache.MarkAsStaleIfOlder(key, int64(order))")
	} else {
		p("func (inst *%s[W]) MarkStaleIfOlder(key %s, order time.Time) {", inst.cacheType(), inst.keyGoType)
		p("\tinst.cache.MarkAsStaleIfOlder(key, order.UnixNano())")
	}
	p("}")
	p("")
	p("// Invalidate drops the key's cached entry (L1 and stash).")
	p("func (inst *%s[W]) Invalidate(key %s) {", inst.cacheType(), inst.keyGoType)
	p("\tinst.cache.Delete(key)")
	p("}")
	p("")
	p("// InvalidateAll drops every cached entry — the bulk external-writer")
	p("// signal (e.g. after an import). In-flight miss bookkeeping (queued")
	p("// keys, pending work items) and the dirty-window pins are dropped")
	p("// with it: call between frames, with no suspended work and no")
	p("// unflushed local writes (the fetcher's dirty-guard keeps pre-write")
	p("// rows out of the cleared cache until the next Flush, at the cost of")
	p("// misses on those keys).")
	p("func (inst *%s[W]) InvalidateAll() {", inst.cacheType())
	p("\tinst.cache.Clear()")
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
		p("\tif found && inst.st.isTombstone(ent) {")
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
		p("\tif found && inst.st.isTombstone(ent) {")
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
	p("\tsb.WriteString(inst.tableName())")
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
	p("// cached. With write-through the version gate already rejects the")
	p("// pre-write row while the newer entry is resident; this guard is the")
	p("// remaining defense for dirty keys the views could NOT materialize")
	p("// (Raw commits, discarded writes) and for InvalidateAll inside a")
	p("// dirty window, where the cache is cold and the gate has nothing to")
	p("// compare against.")
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
		p("// component, ordered by (Order, Key) — so entities sharing an Order")
		p("// still come out in a fixed sequence. Rows that tie on BOTH (the same")
		p("// key written twice at the same Order) are not ordered against each")
		p("// other by this clause; the table keeps newest-per-key, so which of")
		p("// them survives is the engine's choice, not the scan's.")
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
		p("\tsql := \"SELECT * FROM \" + inst.tableName() +")
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
	p("\tsql := \"SELECT * FROM \" + inst.tableName() +")
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
	if inst.orderU64() {
		p("// replays everything (0 is the replay-everything bound — a caller")
		p("// whose Order values start at 0 must start them at 1 instead);")
	} else {
		p("// replays everything (zero time.Time has no defined UnixNano;")
		p("// recordstore.SeqTs(0) is the equivalent explicit bound);")
	}
	p("// opts.To bounds the replay exclusively (\"state as of To\") and")
	p("// opts.Limit caps the row count. The sequence is single-use; ctx")
	p("// must stay valid until iteration completes; the query may execute")
	p("// at call time or lazily during iteration (buffered in v1 — a")
	p("// streaming executor changes nothing visible); an error ends the")
	p("// sequence as a final (nil, err) pair. Reads see only flushed rows.")
	if inst.orderU64() {
		p("func (inst *%s) Replay(ctx context.Context, key %s, fromOrder uint64, opts recordstore.ReplayOptsU64) iter.Seq2[*%s, error] {", inst.storeType(), inst.keyGoType, inst.entityType())
	} else {
		p("func (inst *%s) Replay(ctx context.Context, key %s, fromOrder time.Time, opts recordstore.ReplayOpts) iter.Seq2[*%s, error] {", inst.storeType(), inst.keyGoType, inst.entityType())
	}
	p("\tsql := \"SELECT * FROM \" + inst.tableName() +")
	p("\t\t\" WHERE \" + %sColKey + \" = \" + %sKeyLiteral(key)", inst.StoreName, inst.TableName)
	if inst.orderU64() {
		p("\tif fromOrder > 0 {")
		p("\t\tsql += \" AND \" + %sColOrder + \" >= \" + strconv.FormatUint(fromOrder, 10)", inst.StoreName)
		p("\t}")
		p("\tif opts.To > 0 {")
		p("\t\tsql += \" AND \" + %sColOrder + \" < \" + strconv.FormatUint(opts.To, 10)", inst.StoreName)
		p("\t}")
	} else {
		p("\tif !fromOrder.IsZero() {")
		p("\t\tsql += \" AND \" + %sColOrder + \" >= fromUnixTimestamp64Nano(\" + strconv.FormatInt(fromOrder.UnixNano(), 10) + \")\"", inst.StoreName)
		p("\t}")
		p("\tif !opts.To.IsZero() {")
		p("\t\tsql += \" AND \" + %sColOrder + \" < fromUnixTimestamp64Nano(\" + strconv.FormatInt(opts.To.UnixNano(), 10) + \")\"", inst.StoreName)
		p("\t}")
	}
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
	beginArgs := "id, " + inst.orderArg()
	if len(inst.model.passthrough) > 0 {
		beginArgs += ", " + inst.StoreName + "Envelope{}"
	}
	if inst.model.stateView {
		p("// Delete appends a tombstone row for id — the marker row the tombstone")
		p("// pair recognises. Under the default pair the row carries no components")
		p("// and the u8 Lifecycle column marks the deletion; a configured pair")
		p("// composes the marker through TombstoneWrite on an ordinary builder")
		p("// frame instead (whose Lifecycle column then reads LifecycleLive — the")
		p("// pair is the marker). Either way the deletion writes through to")
		p("// attached cache views like any commit — versioned, so GetLive reads")
		p("// the key as absent immediately.")
		p("func (inst *%s) Delete(id %s, %s %s) (err error) {", inst.storeType(), inst.keyGoType, inst.orderArg(), inst.orderGoType)
		p("\tif inst.cfg.TombstoneWrite != nil {")
		p("\t\tb := inst.Begin(%s)", beginArgs)
		p("\t\tinst.cfg.TombstoneWrite(b)")
		p("\t\treturn b.Commit()")
		p("\t}")
		if len(inst.model.passthrough) > 0 {
			p("\tvar env %sEnvelope // a tombstone carries no pass-through payload", inst.StoreName)
		}
		inst.emitBeginFrame(p, "inst.dml", "recordstore.LifecycleTombstone", "env")
		p("\terr = %s", inst.ctrlCall("inst.dml", "CommitEntity"))
		p("\tif err != nil {")
		p("\t\t_ = %s // discard the failed frame; the store stays usable", inst.ctrlCall("inst.dml", "RollbackEntity"))
		p("\t\treturn")
		p("\t}")
		p("\tinst.buffered++")
		p("\tinst.dirty[id] = struct{}{}")
		p("\tinst.notifyWrite(id, &%s{ID: id, %s: %s, Lifecycle: recordstore.LifecycleTombstone})", inst.entityType(), inst.orderEntityField(), inst.orderArg())
		p("\treturn")
		p("}")
	} else {
		p("// Delete appends the tombstone marker row for id: an entity frame at")
		p("// (key, Order) composed by the configured TombstoneWrite — the row the")
		p("// configured TombstoneDetect recognises (this store binds no u8")
		p("// Lifecycle role; the pair is the whole marker). The deletion writes")
		p("// through to attached cache views like any commit — versioned, so")
		p("// GetLive reads the key as absent immediately.")
		p("func (inst *%s) Delete(id %s, %s %s) (err error) {", inst.storeType(), inst.keyGoType, inst.orderArg(), inst.orderGoType)
		p("\tb := inst.Begin(%s)", beginArgs)
		p("\tinst.cfg.TombstoneWrite(b)")
		p("\treturn b.Commit()")
		p("}")
	}
	p("")
	p("// GetLive is Latest plus tombstone interpretation: newest row wins, a")
	p("// tombstone reads as absent — the state-view read (the cache view")
	p("// carries the cached twin). Interpretation runs Go-side through the")
	p("// tombstone pair's detect, on the one newest row this verb needs.")
	p("func (inst *%s) GetLive(ctx context.Context, key %s) (ent *%s, found bool, err error) {", inst.storeType(), inst.keyGoType, inst.entityType())
	p("\tent, found, err = inst.Latest(ctx, key)")
	p("\tif err != nil || !found {")
	p("\t\treturn")
	p("\t}")
	p("\tif inst.isTombstone(ent) {")
	p("\t\tent = nil")
	p("\t\tfound = false")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
}

// emitComponentSQL publishes the store's ADR-0066 artefacts as one
// componentsql.Set, so the SQL a component definition generates is reachable
// from outside the package that bakes it (ADR-0189 §SD1).
//
// Emitted only when the store carries components: a Set with no kinds is
// refused at registration, and the import would be unused.
func (inst emitter) emitComponentSQL(sb *strings.Builder, comps []storeComponent) {
	if len(comps) == 0 {
		return
	}
	p := func(format string, args ...any) { fmt.Fprintf(sb, format+"\n", args...) }

	p("// %sComponentSQL publishes this store's ADR-0066 read-back artefacts —", inst.StoreName)
	p("// the SQL its component definitions generate — for an authoring surface")
	p("// to expand (ADR-0189). A host registers it into a componentsql.Registry;")
	p("// nothing here self-registers.")
	p("//")
	p("// Filter is the same constant the Scan verbs use, so the store's own read")
	p("// path and the authoring surface cannot disagree about what a conforming")
	p("// row is. Projection must not be embedded without Filter — it locates an")
	p("// attribute by indexOf and returns the first match, so on a row carrying a")
	p("// membership twice it answers plausibly and wrongly (ADR-0066).")
	p("//")
	p("// The column references are UNQUALIFIED, so a consumer embedding them in a")
	p("// join must bind them to %sTableName itself (ADR-0189 SD6).", inst.StoreName)
	p("var %sComponentSQL = componentsql.Set{", inst.StoreName)
	p("\tStore: %q,", inst.StoreName)
	p("\tTable: %sTableName,", inst.StoreName)
	p("\tKinds: map[string]componentsql.Artefacts{")
	for _, c := range comps {
		p("\t\t%q: {", c.Kind)
		p("\t\t\tPresence:   %q,", c.presence)
		p("\t\t\tValidator:  %q,", c.validator)
		p("\t\t\tFilter:     %sScan%sFilter,", inst.TableName, c.Kind)
		p("\t\t\tProjection: %q,", c.projection)
		p("\t\t},")
	}
	p("\t},")
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
	// One read-access reader per plain item-type group present in the schema
	// (Key and Order always; Lifecycle and Routing when the schema carries
	// them, whether as roles or pass-through envelope columns).
	plainVars := make([]string, 0, len(inst.model.groups))
	for _, g := range inst.model.groups {
		v := plainReaderVar(g.itemType)
		p("\t%s := %sNew%sPlain%sAttributes()", v, inst.raQ(), ra, plainReaderRoleToken(g.itemType))
		plainVars = append(plainVars, v)
	}
	for _, m := range order {
		p("\t%s := %sNew%sTagged%s()", seen[m].varN, inst.raQ(), ra, m)
	}
	readerVars := append([]string{}, plainVars...)
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
	keyVar := plainReaderVar(inst.model.key.itemType)
	orderVar := plainReaderVar(inst.model.order.itemType)
	keyField := "Value" + inst.model.key.pascal
	orderField := "Value" + inst.model.order.pascal
	p("\tn := %s.%s.Len()", keyVar, keyField)
	if !inst.orderU64() {
		p("\ttsType, ok := %s.%s.DataType().(*arrow.TimestampType)", orderVar, orderField)
		p("\tif !ok {")
		p("\t\terr = eh.Errorf(\"order column is not a timestamp (got %%s)\", %s.%s.DataType())", orderVar, orderField)
		p("\t\treturn")
		p("\t}")
	}
	p("\tents = make([]*%s, 0, n)", inst.entityType())
	p("\tfor i := range n {")
	p("\t\tent := &%s{", inst.entityType())
	p("\t\t\tID: %s.%s.Value(i),", keyVar, keyField)
	if inst.orderU64() {
		p("\t\t\tOrd: %s.%s.Value(i),", orderVar, orderField)
	} else {
		p("\t\t\tTs: %s.%s.Value(i).ToTime(tsType.Unit).UTC(),", orderVar, orderField)
	}
	if inst.model.stateView {
		p("\t\t\tLifecycle: %s.Value%s.Value(i),", plainReaderVar(inst.model.lifecycle.itemType), inst.model.lifecycle.pascal)
	}
	p("\t\t}")
	// Pass-through envelope columns: promoted-field assignment from the
	// group reader's typed accessor (scalars direct, arrays collected).
	for _, c := range inst.model.passthrough {
		rv := plainReaderVar(c.itemType)
		if c.isArray {
			p("\t\tent.%s = slices.Collect(%s.GetAttrValue%s(raruntime.EntityIdx(i)))", c.pascal, rv, c.pascal)
		} else {
			p("\t\tent.%s = %s.GetAttrValue%s(raruntime.EntityIdx(i))", c.pascal, rv, c.pascal)
		}
	}
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
		p("\t\t\trow, ok, e := %s(%s)", inst.codecName(c.Kind, "ReadRow"), strings.Join(args, ", "))
		p("\t\t\tif e != nil {")
		p("\t\t\t\terr = eh.Errorf(\"read %s component: %%w\", e)", lowerFirst(c.Kind))
		p("\t\t\t\treturn")
		p("\t\t\t}")
		p("\t\t\tif ok {")
		if idCol := goplan.FindPlainCol(c.plan, "id"); idCol != nil {
			p("\t\t\t\trow.%s = ent.ID", idCol.GoField)
		}
		if tsCol := goplan.FindPlainCol(c.plan, "ts"); tsCol != nil {
			// Under the u64 Order regime a column named ts is either the
			// Order role itself (backfilled from Ord) or an ordinary z64
			// pass-through, whose promoted envelope field carries the read
			// value already ("Ts" is not reserved there).
			src := "Ts"
			if inst.orderU64() && inst.model.order.name == "ts" {
				src = "Ord"
			}
			p("\t\t\t\trow.%s = ent.%s", tsCol.GoField, src)
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
