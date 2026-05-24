//go:build llm_generated_opus47

package marshallgen

import (
	"fmt"
	"strings"
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
// Implementations live in caller packages — pebble's FactsWrapper for
// the full runtime.facts wrapper stack, NoOpWrapper here for the
// schema-agnostic anchor-style emit.
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
	Imports(plan *Plan) []string

	// KindVars writes the package-level declarations for the kindXxx
	// membership-id symbols every per-field driver in the core
	// references. The set of names is determined by the plan
	// (TaggedField.KindVar() per unique LWMembership) — the wrapper
	// only chooses storage:
	//
	//   - Facts target: `var kindXxx uint64` per name, resolved in
	//     Init via `vdd.Memb<Name>.GetId().Value()`.
	//   - Anchor target: `const kindXxx uint64 = N` per name, derived
	//     from declaration order in the plan.
	KindVars(sb *strings.Builder, plan *Plan)

	// Init writes the package init() body. May be empty (NoOpWrapper).
	Init(sb *strings.Builder, plan *Plan)

	// BeforeCore writes any per-kind blocks that must precede the
	// Columns struct (e.g. ActiveSections / ActiveFields sync.OnceValue
	// declarations, sync.Pool of dml builders). Optional.
	BeforeCore(sb *strings.Builder, plan *Plan) error

	// AfterCore writes any per-kind blocks that follow the schema-
	// agnostic core (Marshal / Unmarshal / Codec methods, bus-codec
	// bridge registration helpers, schema-specific readers). Optional.
	AfterCore(sb *strings.Builder, plan *Plan) error
}

// NoOpWrapper emits the schema-agnostic core only: kindXxx as
// package-local consts assigned from declaration order, no init() body,
// no pre-/post-core blocks. Matches today's `--target=anchor`
// generator output.
type NoOpWrapper struct{}

var _ WrapperEmitterI = NoOpWrapper{}

// Imports contributes no extra imports — the core emit covers
// everything the schema-agnostic surface needs.
func (NoOpWrapper) Imports(_ *Plan) []string { return nil }

// KindVars emits one `const kindXxx uint64 = N` per unique membership,
// where N is the 1-based index in declaration order. Stable + self-
// contained: no external registry consulted; membership identity is
// local to the generated package.
func (NoOpWrapper) KindVars(sb *strings.Builder, plan *Plan) {
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
func (NoOpWrapper) Init(_ *strings.Builder, _ *Plan) {}

// BeforeCore writes nothing — no ActiveHints, no Pool, no per-kind
// driver-state caching at the schema-agnostic altitude.
func (NoOpWrapper) BeforeCore(_ *strings.Builder, _ *Plan) error { return nil }

// AfterCore writes nothing — Marshal / Unmarshal / Codec are the
// caller's responsibility against the BuildEntities / FillFromArrow
// helpers the core emits.
func (NoOpWrapper) AfterCore(_ *strings.Builder, _ *Plan) error { return nil }

// uniqueMemberships returns plan.Fields filtered so each LWMembership
// appears at most once (first-seen wins), skipping Verbatim-flagged
// memberships (those don't get a kindXxx declaration — the literal
// []byte name is embedded directly at the call site). Multi-sub-column
// sections (u32Range with beginIncl + endExcl) have two fields sharing
// one membership; KindVars decl per membership, not per field.
func uniqueMemberships(plan *Plan) (out []TaggedField) {
	seen := map[string]bool{}
	for _, f := range plan.Fields {
		if f.Flags.Verbatim {
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
