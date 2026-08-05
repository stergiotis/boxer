package main

import (
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// The trial's leeway vocabulary, deliberately tiny.
//
// The canonical leeway JSON mapping (mapping.LoadJsonMapping) tags every
// shredded value with its *path*, carried verbatim as a low-cardinality
// string in the `lmv` column, with array indices split off into the `mvhp`
// parameter column. boxer.facts cannot do that: its tagged-value sections
// accept only LowCardRef / HighCardRef / MixedLowCardRefHighCardParameters
// (factsschema.go, `membershipSpec`), all of which identify a membership by a
// uint64 registered in a vcs-managed registry. An open JSON corpus has no
// closed path vocabulary to register.
//
// So the path itself moves into the parameter channel: one registered
// membership, `blueskyJsonPath`, whose high-cardinality parameter carries the
// path bytes. Array indices ride a second membership on the same value,
// `blueskyJsonParams` — facts' multi-membership support is what lets the two
// halves stay separate columns rather than being concatenated into one
// string, which is what the canonical scheme's lmv/mvhp split buys.
//
// That is the faithful translation of the scheme into this schema, and the
// cost of the detour — path strings stored per attribute instead of
// dictionary-encoded refs — is one of the things the trial measures.
var Contract = contract.NewVcsManagedContract()

const NamingStyle = naming.LowerSpinalCase

// TagValueRegistry is scoped to this trial so its ids cannot collide with the
// runtime's own vocab (public/keelson/runtime/vocab). Offset 2 for the same
// reason the runtime uses it: fibonacci-coded tags reserve 0 as invalid
// (ADR-0106 §SD8) and the vcs-managed convention keeps effective ids even.
var TagValueRegistry = registry.MustNewTagValueRegistry[*contract.VcsManagedContract](
	identifier.TagValue(2), NamingStyle, 4, Contract,
)

var MembersTagValue = TagValueRegistry.MustBegin("jsonbenchMembers", 0).End()

var NkRegistry = registry.MustNewNaturalKeyRegistry[*contract.VcsManagedContract](
	MembersTagValue.GetTagValue(), 8, NamingStyle, identifier.UntaggedId(0), Contract,
)

var (
	// MembKindBlueskyEvent tags the row as one Jetstream event.
	MembKindBlueskyEvent = NkRegistry.MustBegin("blueskyKindEvent").End()

	// MembJsonPath carries the low-cardinality half of a shredded value's
	// address — the JSON path with array positions elided to "_", e.g.
	// "/commit/record/langs/_". Rides as a MixedLowCardRef parameter.
	MembJsonPath = NkRegistry.MustBegin("blueskyJsonPath").End()

	// MembJsonParams carries the high-cardinality half — the array indices
	// that "_" stands in for, comma-joined in path order. Only attached when
	// the path actually contains a "_".
	MembJsonParams = NkRegistry.MustBegin("blueskyJsonParams").End()
)

// The trial's own results, as facts. The protocol asks for this explicitly
// (§6 Reporting): domain numbers land as facts and are read back through an
// applet, so the benchmark dogfoods the reporting layer it is measuring.
//
// Two kinds, because the two result shapes have different grains: one fact
// per (run, arm, query, try) for a timing, one per (run, arm, metric) for a
// size. Both carry run and arm so a page can pivot on either.
var (
	MembKindBenchTiming = NkRegistry.MustBegin("jsonbenchKindTiming").End()
	MembKindBenchSize   = NkRegistry.MustBegin("jsonbenchKindSize").End()

	MembBenchRun   = NkRegistry.MustBegin("jsonbenchRun").End()
	MembBenchArm   = NkRegistry.MustBegin("jsonbenchArm").End()
	MembBenchQuery = NkRegistry.MustBegin("jsonbenchQuery").End()
	MembBenchTry   = NkRegistry.MustBegin("jsonbenchTry").End()

	MembBenchSeconds     = NkRegistry.MustBegin("jsonbenchSeconds").End()
	MembBenchMemoryBytes = NkRegistry.MustBegin("jsonbenchMemoryBytes").End()

	MembBenchMetric      = NkRegistry.MustBegin("jsonbenchMetric").End()
	MembBenchMetricValue = NkRegistry.MustBegin("jsonbenchMetricValue").End()
)
