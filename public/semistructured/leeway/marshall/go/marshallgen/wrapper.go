package marshallgen

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// WrapperEmitterI lets a target-specific layer inject schema-coupled
// emit blocks around the generic core that EmitPlan always produces.
//
// Method call order, intermixed with EmitPlan's own core blocks:
//
//	writeHeader
//	writeImports                   ← w.Imports(plan) folded into the import block
//	w.KindVars(sb, plan)           ← package-level membership-id symbol decls
//	w.Init(sb, plan)               ← optional package init() body
//	w.BeforeCore(sb, plan)         ← ActiveHints, Pool, anything pre-Columns
//	writeColumnsStruct + Len/Append + Row
//	writeBuildEntities (+ derived interfaces)
//	writeFillFromArrow (+ derived interfaces)
//	w.AfterCore(sb, plan)          ← Marshal, Unmarshal, Codec, anything post-core
//
// The core emit always references `kindXxx` membership symbols by the
// names KindVar() returns; the wrapper picks storage (var-resolved-from-
// registry vs declaration-order const) and is free to elide either
// block. NoOpWrapper does that.
//
// Implementations live in caller packages — keelson's FactsWrapper
// (runtime/codec/factswrapper) for the full boxer.facts wrapper stack,
// NoOpWrapper here for the schema-agnostic anchor-style emit.
type WrapperEmitterI interface {
	// Imports returns lines (each one a fully-quoted Go import spec)
	// that should be folded into the generated file's import block in
	// addition to the universal imports the core emits.
	//
	// Example return for a facts target:
	//
	//	[]string{
	//		"\"bytes\"",
	//		"\"sync\"",
	//		"cbdml \"github.com/.../keelson/runtime/factsschema/dml_cbor\"",
	//	}
	//
	// NoOpWrapper returns nil.
	Imports(plan *mappingplan.Plan) []string

	// KindVars writes the package-level declarations for the kindXxx
	// membership-id symbols every per-field driver in the core
	// references. The set of names is determined by the plan
	// (mappingplan.TaggedField.KindVar() per unique LWMembership) — the wrapper
	// only chooses storage:
	//
	//   - Facts target: `var kindXxx uint64` per name, resolved in
	//     Init via `vdd.Memb<Name>.GetId().Value()`.
	//   - Anchor target: `const kindXxx uint64 = N` per name, derived
	//     from declaration order in the plan.
	KindVars(sb *strings.Builder, plan *mappingplan.Plan)

	// Init writes the package init() body. May be empty (NoOpWrapper).
	Init(sb *strings.Builder, plan *mappingplan.Plan)

	// BeforeCore writes any per-kind blocks that must precede the
	// Columns struct (e.g. ActiveSections / ActiveFields sync.OnceValue
	// declarations, sync.Pool of dml builders). Optional.
	BeforeCore(sb *strings.Builder, plan *mappingplan.Plan) error

	// AfterCore writes any per-kind blocks that follow the schema-
	// agnostic core (Marshal / Unmarshal / Codec methods, bus-codec
	// bridge registration helpers, schema-specific readers). Optional.
	AfterCore(sb *strings.Builder, plan *mappingplan.Plan) error
}

// NoOpWrapper emits the schema-agnostic core only: kindXxx as
// package-local consts assigned from declaration order, no init() body,
// no pre-/post-core blocks. Matches today's `--target=anchor`
// generator output.
type NoOpWrapper struct{}

var _ WrapperEmitterI = NoOpWrapper{}

// Imports contributes no extra imports — the core emit covers
// everything the schema-agnostic surface needs.
func (NoOpWrapper) Imports(_ *mappingplan.Plan) []string { return nil }

// KindVars emits one `const kindXxx uint64 = N` per unique membership,
// where N is the 1-based index in declaration order. Stable + self-
// contained: no external registry consulted; membership identity is
// local to the generated package.
func (NoOpWrapper) KindVars(sb *strings.Builder, plan *mappingplan.Plan) {
	sb.WriteString("// --- Package-local membership ids (schema-agnostic target). ---\n\n")
	sb.WriteString("const (\n")
	for i, f := range uniqueMemberships(plan) {
		fmt.Fprintf(sb, "\t%s uint64 = %d\n", f.KindVar(), i+1)
	}
	sb.WriteString(")\n\n")
}

// Init writes nothing — anchor-style consts are init-time-resolved by
// the language itself; there is no buscodec / runtime registry to wire
// up.
func (NoOpWrapper) Init(_ *strings.Builder, _ *mappingplan.Plan) {}

// BeforeCore writes nothing — no ActiveHints, no Pool, no per-kind
// driver-state caching at the schema-agnostic altitude.
func (NoOpWrapper) BeforeCore(_ *strings.Builder, _ *mappingplan.Plan) error { return nil }

// AfterCore writes nothing — Marshal / Unmarshal / Codec are the
// caller's responsibility against the BuildEntities / FillFromArrow
// helpers the core emits.
func (NoOpWrapper) AfterCore(_ *strings.Builder, _ *mappingplan.Plan) error { return nil }

// MembershipIdSourceI is implemented by wrappers that can state, at
// generation time, the membership → id assignment their emitted kindXxx
// symbols carry at run time. Downstream generators that bake ids into
// artefacts beside the codec (recordstore/gen: the Scan filter SQL
// literals and the <Store>MembershipIds cross-check map) require their
// wrapper to implement it — an id baked from any source other than the
// codec's own would match nothing on read, silently.
type MembershipIdSourceI interface {
	// PlanMembershipIds returns the id for each of the plan's unique
	// ref-channel memberships (the KindVars set). A membership the
	// source cannot resolve is an error — never a silent zero.
	PlanMembershipIds(plan *mappingplan.Plan) (map[string]uint64, error)
	// GloballyUniqueIds reports whether two distinct membership names can
	// ever share an id under this source. Declaration-order numbering
	// restarts at 1 per plan and returns false; a registry-backed
	// assignment returns true. Consumers use it to decide whether kinds
	// sharing a section could alias (ADR-0100 SD6 / ADR-0105 D2).
	GloballyUniqueIds() bool
}

var _ MembershipIdSourceI = NoOpWrapper{}

// PlanMembershipIds implements MembershipIdSourceI with the same
// declaration-order assignment KindVars emits. It cannot fail — every
// ref-channel membership gets the next index.
func (NoOpWrapper) PlanMembershipIds(plan *mappingplan.Plan) (map[string]uint64, error) {
	return MembershipIds(plan), nil
}

// GloballyUniqueIds is false: ids restart at 1 per plan, so two kinds'
// distinct memberships routinely carry the same id.
func (NoOpWrapper) GloballyUniqueIds() bool { return false }

// FixedIdsWrapper emits kindXxx as package-local consts carrying
// caller-assigned values instead of declaration order — the
// membership-id override of ADR-0105 D2. The caller resolves each
// membership name against its registry (e.g. vdd TaggedIds, stable by
// the vdd contract) at generation time and supplies the snapshot; the
// emitted codec and everything a downstream generator derives from
// PlanMembershipIds then agree with every other artefact generated from
// the same registry.
//
// The uniqueness claim is the caller's: GloballyUniqueIds is true, so
// consumers may relax section-sharing gates. recordstore/gen verifies
// injectivity over the memberships it actually uses and rejects a map
// that repeats an id there.
type FixedIdsWrapper struct {
	// Ids maps membership name → id. Every ref-channel membership of
	// every plan emitted under this wrapper must be present; a missing
	// name fails EmitPlan (via PlanMembershipIds) rather than emitting a
	// wrong id.
	Ids map[string]uint64
}

var _ WrapperEmitterI = FixedIdsWrapper{}
var _ MembershipIdSourceI = FixedIdsWrapper{}

// Imports contributes no extra imports, like NoOpWrapper.
func (FixedIdsWrapper) Imports(_ *mappingplan.Plan) []string { return nil }

// KindVars emits one `const kindXxx uint64 = <assigned>` per unique
// membership, in declaration order. EmitPlan validates coverage through
// PlanMembershipIds before emission; a name that is nonetheless missing
// here emits a deliberately non-compiling symbol naming the membership,
// never a silent zero.
func (inst FixedIdsWrapper) KindVars(sb *strings.Builder, plan *mappingplan.Plan) {
	sb.WriteString("// --- Caller-assigned membership ids (registry-stable target). ---\n\n")
	sb.WriteString("const (\n")
	for _, f := range uniqueMemberships(plan) {
		id, ok := inst.Ids[f.LWMembership]
		if !ok {
			fmt.Fprintf(sb, "\t%s uint64 = MISSING_MEMBERSHIP_ID_%s\n", f.KindVar(), f.LWMembership)
			continue
		}
		fmt.Fprintf(sb, "\t%s uint64 = %d\n", f.KindVar(), id)
	}
	sb.WriteString(")\n\n")
}

// Init writes nothing — the consts carry their values directly.
func (FixedIdsWrapper) Init(_ *strings.Builder, _ *mappingplan.Plan) {}

// BeforeCore writes nothing, like NoOpWrapper.
func (FixedIdsWrapper) BeforeCore(_ *strings.Builder, _ *mappingplan.Plan) error { return nil }

// AfterCore writes nothing, like NoOpWrapper.
func (FixedIdsWrapper) AfterCore(_ *strings.Builder, _ *mappingplan.Plan) error { return nil }

// PlanMembershipIds returns the caller-assigned id per unique
// ref-channel membership of the plan; a membership absent from Ids is an
// error naming it.
func (inst FixedIdsWrapper) PlanMembershipIds(plan *mappingplan.Plan) (map[string]uint64, error) {
	out := make(map[string]uint64)
	for _, f := range uniqueMemberships(plan) {
		id, ok := inst.Ids[f.LWMembership]
		if !ok {
			return nil, eb.Build().Str("membership", f.LWMembership).Errorf("fixed-ids wrapper: membership %q has no assigned id — every ref-channel membership needs an entry in Ids", f.LWMembership)
		}
		out[f.LWMembership] = id
	}
	return out, nil
}

// GloballyUniqueIds is true by the caller's contract: the supplied
// snapshot comes from a registry that never assigns one id to two names.
func (FixedIdsWrapper) GloballyUniqueIds() bool { return true }

// MembershipIds reports the package-local membership-id assignment the
// NoOpWrapper KindVars block emits: one id per unique ref-channel
// membership, 1-based, in declaration order. Exposed so downstream
// generators (e.g. recordstore/gen resolving read-back memberships for
// baked Scan SQL) reproduce exactly the ids the generated codec writes.
// Verbatim channels carry no id and are absent from the map.
func MembershipIds(plan *mappingplan.Plan) map[string]uint64 {
	out := make(map[string]uint64)
	for i, f := range uniqueMemberships(plan) {
		out[f.LWMembership] = uint64(i + 1)
	}
	return out
}

// uniqueMemberships returns plan.Fields filtered so each LWMembership
// appears at most once (first-seen wins), skipping channels that do
// not consult a registry (the literal []byte name is embedded at the
// call site, or the params-blob channels carry the wire payload
// directly). Multi-sub-column sections share one membership across
// two fields; KindVars declares per membership, not per field.
func uniqueMemberships(plan *mappingplan.Plan) (out []mappingplan.TaggedField) {
	seen := map[string]bool{}
	for _, f := range plan.Fields {
		if !f.Flags.Channel.NeedsKindVar() {
			continue
		}
		if seen[f.LWMembership] {
			continue
		}
		seen[f.LWMembership] = true
		out = append(out, f)
	}
	return
}
