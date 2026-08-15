// Package meshdemo is the worked example for the scenario the component layer
// exists for, and did not have one of: **two domains sharing one facts table
// and nothing else**, where the second formulates its component *after* the
// first has already written rows.
//
// Every other pedagogical artifact in the tree is closed-world — the same code
// writes and reads, and the ids are a map somebody typed
// (`anchor/ecsdemo`), a declaration order (`recordstore/example`), or a
// caller-supplied snapshot (`recordstore/sharedsection`). Those are honest
// examples of a closed world. They are not this one, and this one is the main
// scenario (ADR-0183 D7, the X-class centerpiece).
//
// # The story this package runs
//
// The **fleet agent** (domain A) writes [FleetSample] rows to `boxer.facts`
// through a generated store. It knows a vocabulary and its own DTO; it does
// not know who reads.
//
// Later — after those rows exist — **capacity planning** (domain B) decides it
// cares about host load. It writes [HostLoad], a Go struct naming three
// memberships out of the same vocabulary, and reads the rows the agent already
// wrote. There is no generated artifact for HostLoad, no migration, no change
// to the agent, and no coordination: B resolves its ids from the registry at
// run time, builds a plan from the struct, and projects.
//
// Three properties carry the whole model, and each has a test:
//
//   - **The ids agree because the names do.** The agent's store baked ids at
//     generation time from the registry snapshot; B resolves the same names
//     through the same registry at run time. Nothing compares them at the
//     boundary — they agree because both went to the vocabulary
//     (`TestBakedAndRuntimeIdsAgree`).
//   - **A component is satisfied structurally, not declared.** HostLoad does
//     not claim the agent's `meshKindFleetSample` marker and does not need to:
//     the slots decide, the kind marker only accelerates. A row satisfies
//     HostLoad because it carries HostLoad's memberships
//     (`TestLateComponentDecodesRowsTheAgentWrote` in memory,
//     `TestLateComponentFindsTheAgentsRowsBySql` through the table, and
//     `TestLateComponentClaimsNoKindMarker` on why it may).
//   - **Absence is first class.** HostLoad names `meshDrainedAt`, which no
//     writer in this demo ever sets. Those rows are not broken: the slot reads
//     absent, which is a legal state rather than an error — asserted on every
//     row both read paths return.
//
// # Why the vocabulary is a real one
//
// This package claims a tag value and commits an assignment golden like every
// other vocabulary (ADR-0183 D0/D1). A demo vocabulary that skipped either
// would be demonstrating the wrong thing: the discipline *is* the mechanism
// that makes the two domains' ids agree without them ever meeting.
package meshdemo

import (
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// Contract is this vocabulary's leeway contract: version-controlled ids, each
// ordinal declared in source.
var Contract = contract.NewVcsManagedContract()

// NamingStyle matches the other vocabularies sharing `boxer.facts`, so a query
// joining demo rows to runtime facts reads the same way on both sides.
const NamingStyle = naming.LowerSpinalCase

// TagValueClaim is this vocabulary's tag value, the sixth of the width-32
// class (ADR-0183 D0). A demo claims like anything else: the claim is what
// keeps its ids disjoint from the runtime's, and the disjointness is what lets
// the demo's rows live in the same table as everyone's.
var TagValueClaim = tagmint.MustClaim("meshdemo", 2178314, MaxExpectedMemberships)

// MaxExpectedMemberships is what this vocabulary tells the mint it will need.
const MaxExpectedMemberships = 1 << 10

// NkRegistry holds the demo's membership names. Both domains resolve through
// it — the agent at generation time, capacity planning at run time — and that
// is the only thing they share.
var NkRegistry = registry.MustNewNaturalKeyRegistry(TagValueClaim, 16, NamingStyle, Contract)

// The vocabulary. Each states its ordinal; the ids are what rows carry.
//
// Note who writes what: the agent writes the first five. `meshDrainedAt` is
// named here and written by nobody, which is how a vocabulary usually looks —
// names outlive and outnumber the writers that use them at any one moment.
var (
	// MembKindFleetSample marks a row as one fleet sample. It is an
	// assertion the agent makes about its own rows, not an identity: a
	// reader that gates on it is trusting the writer to have declared
	// itself, which forfeits exactly the late-formulation property this
	// package demonstrates.
	MembKindFleetSample = NkRegistry.MustBegin("meshKindFleetSample", 0).End()

	// MembHost names the box a row is about.
	MembHost = NkRegistry.MustBegin("meshHost", 1).End()
	// MembRegion is where that box sits.
	MembRegion = NkRegistry.MustBegin("meshRegion", 2).End()

	// MembCpuPercent carries the reading.
	MembCpuPercent = NkRegistry.MustBegin("meshCpuPercent", 3).End()
	// MembUptimeSeconds is the age of the box that produced it.
	MembUptimeSeconds = NkRegistry.MustBegin("meshUptimeSeconds", 4).End()

	// MembDrainedAt is claimed by [HostLoad] and written by nobody in this
	// demo — the ordinary case of a slot that reads absent.
	MembDrainedAt = NkRegistry.MustBegin("meshDrainedAt", 5).End()
)
