package main

import (
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
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

// TagValueClaim is this trial vocabulary's tag value, claimed from the
// width-32 class (ADR-0183 D0).
//
// The old value was 2, chosen to keep the trial's ids away from the runtime's
// — which is exactly the value the runtime vocabulary had also chosen, so
// every id here equalled a runtime id. Nothing broke because the two share no
// table; the claim now refuses the duplicate outright rather than leaving it
// to luck.
var TagValueClaim = tagmint.MustClaim("jsonbench", 2178313, MaxExpectedMemberships)

// MaxExpectedMemberships is what this vocabulary tells the mint it will need.
const MaxExpectedMemberships = 1 << 10

var NkRegistry = registry.MustNewNaturalKeyRegistry[*contract.VcsManagedContract](
	TagValueClaim, 8, NamingStyle, Contract,
)

var (
	// MembKindBlueskyEvent tags the row as one Jetstream event.
	MembKindBlueskyEvent = NkRegistry.MustBegin("blueskyKindEvent", 0).End()

	// MembJsonPath carries the low-cardinality half of a shredded value's
	// address — the JSON path with array positions elided to "_", e.g.
	// "/commit/record/langs/_". Rides as a MixedLowCardRef parameter.
	MembJsonPath = NkRegistry.MustBegin("blueskyJsonPath", 1).End()

	// MembJsonParams carries the high-cardinality half — the array indices
	// that "_" stands in for, comma-joined in path order. Only attached when
	// the path actually contains a "_".
	MembJsonParams = NkRegistry.MustBegin("blueskyJsonParams", 2).End()
)

// The trial's own results, as facts. The protocol asks for this explicitly
// (§6 Reporting): domain numbers land as facts and are read back through an
// applet, so the benchmark dogfoods the reporting layer it is measuring.
//
// Two kinds, because the two result shapes have different grains: one fact
// per (run, arm, query, try) for a timing, one per (run, arm, metric) for a
// size. Both carry run and arm so a page can pivot on either.
var (
	MembKindBenchTiming = NkRegistry.MustBegin("jsonbenchKindTiming", 3).End()
	MembKindBenchSize   = NkRegistry.MustBegin("jsonbenchKindSize", 4).End()

	MembBenchRun   = NkRegistry.MustBegin("jsonbenchRun", 5).End()
	MembBenchArm   = NkRegistry.MustBegin("jsonbenchArm", 6).End()
	MembBenchQuery = NkRegistry.MustBegin("jsonbenchQuery", 7).End()
	MembBenchTry   = NkRegistry.MustBegin("jsonbenchTry", 8).End()

	MembBenchSeconds     = NkRegistry.MustBegin("jsonbenchSeconds", 9).End()
	MembBenchMemoryBytes = NkRegistry.MustBegin("jsonbenchMemoryBytes", 10).End()

	MembBenchMetric      = NkRegistry.MustBegin("jsonbenchMetric", 11).End()
	MembBenchMetricValue = NkRegistry.MustBegin("jsonbenchMetricValue", 12).End()
)
