---
type: adr
status: proposed
date: 2026-08-12
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0183: leeway component layer — consumer simplification and invariants

## Context

The 2026-08 review of the component layer concluded: the model is strong —
overlap under fusion, presence without global vocabulary, one contract
across three read paths are genuinely solved — but accumulated sharp edges
push the cost of *consuming* the layer beyond what is reasonable. The full
analysis, edge inventory (I1–I4, W1–W5, R1–R7, F1–F3, A1–A2), driver
attribution (S1–S4), and benchmark scenario live in
[the consumer-complexity page](../adr-background-work/leeway-components-consumer-complexity.md);
this ADR decides. Every decision was evaluated against that page's
benchmark: **B1** data-centric (metadata lives in the data, as facts),
**B2** federated mesh (thousands of domain-owned components, one shared
facts table, no write-time coordination), **B3** late-formulating
components (a contract may postdate the data and may later widen).

A same-day seam survey (2026-08-12) grounded the costs and corrected two
of the page's assumptions:

- The reflect-path lookup seam is small: `marshallreflect.MapLookup` is a
  naked map type with no constructor and exactly two production
  construction sites; a registry-backed constructor is additive
  (~150 LOC). But the vdd registry stores spinal-case names while DTO
  memberships are lowerCamel — a naive snapshot misses every lookup
  silently — and `marshallreflect/doc.go` already claims the facts target
  "wraps `vdd.KeelsonHrNkRegistry`", which is false today (no keelson
  package imports marshallreflect).
- The write-path remedy as drafted did not reach its own benchmark
  scenario: the facts encoders (`chstore`, `queryrunfacts`,
  `capmapfacts`) never touch DTOs or plans — they call the generated DML
  by hand and each open-codes the sealed-section workaround (~62 lines,
  ~13 encoders). Meanwhile the typed-store lift's hidden scope was the
  entity bag: unbounded multi-contribution would force
  `option.Option[Kind]` to a slice, dragging the cache, `Archetype()`,
  decode, and ADR-0100 SD5.

ADR-0105 D3b's storegen resolver (registry ids baked into generated
artifacts) is confirmed unbuilt; it gates the one worked example the
landscape lacks — the mesh scenario itself.

## Decision

We adopt the following, attacking the four accidental drivers at their
sites rather than documenting their symptoms. Working names are marked;
final naming lands with the code.

### D1 — Registry-stable ids are the default; positional ids are marked closed-world (S1)

- A vdd-side snapshot helper (working name `MembershipIdSnapshot()`)
  materializes the linked registry as `map[string]uint64`, keyed by the
  **marshalling-side (lowerCamel) spelling**. Name-style conversion
  happens only at this seam; the helper errors on post-conversion
  collision, and a test over the full registry pins round-trip
  conversion (the converter is non-idempotent at digit boundaries).
- marshallreflect gains a snapshot-backed constructor for `MapLookup`
  (working name `NewRegistryLookup`), typed on a minimal local interface
  or the materialized map — not on the registry type. Hand-built
  `MapLookup` literals stay legal and become the *documented*
  closed-world escape hatch, as does `NoOpWrapper`'s declaration-order
  regime on the generated side.
- One snapshot value feeds both existing seams: `FixedIdsWrapper` on the
  generated path and the new constructor on the reflect path. The private
  `mapIdLookup` bridge in `recordstore/gen` is deleted;
  `marshallreflect.LookupI` and `readback.IdLookup` stay deliberate
  structural twins (collapsing them forces an import direction between
  leaf packages for a one-method interface).
- `marshallreflect/doc.go`'s registry claim is corrected to describe
  what this ADR builds, not asserted as existing.
- A snapshot is link-set-dependent (the registry populates by
  package-init side effects); the helper's doc states that the linked
  vocabulary package defines the snapshot's extent.

### D2 — The storegen slice unblocks the mesh example (S1, ADR-0105 D3b)

`FactsWrapper` gains the generation-time id-source methods
(`PlanMembershipIds`, `GloballyUniqueIds`), resolving `vdd.Memb*` ids —
used as an **id source only**: `runtime/factsschema/storegen` feeds
`recordstore/gen` through `FixedIdsWrapper`, so the checked-in codec
artifacts' bytes are untouched. Generation runs in the gen-test lane
(the `sharedsection` pattern); a CLI command is deferred until a second
consumer exists — no recordstore generator command exists at all today,
and the gen test is the repo's proven lane.

### D3 — The vocabulary is published as facts; reconciliation is a query (S1, B1)

- A claim component (working name `vocabclaim`) lands in the facts
  vocabulary: `(claimant, role, membershipName, membershipId, contract)`
  with `role ∈ {registry, writer, reader}`. Processes publish at init —
  assignments are frozen per binary, so init-time is the honest cadence;
  the append-only table plus workingset semantics give the current view.
- Reconciliation needs no new mechanism class: registry skew (I2), a
  reader's resolved assignment vs the vocabulary of record, and
  `InspectLookup`'s "resolved but never carried" heuristic become SQL
  views over claim joins; the outer join (asserted vs used) is the
  interesting one.
- **Name governance (I4):** the vdd registry package remains the coinage
  chokepoint — names enter by PR to the vocabulary package, and the
  registry refuses duplicate coinage within a link set. The claims view
  audits deployed reality against it (two contracts claiming one name is
  a queryable fact). Domain namespacing is deferred until a second
  registry/contract exists.
- Kind names are vocabulary and publish the same way (a kind attribute is
  already a membership). Consuming systems *may* publish their declared
  signatures as `role: reader` claims — the dual join is impact analysis
  — but adoption is per-consumer, not mandated.
- The earlier DDL/table-comment assignment fingerprint is **killed**: it
  records vocabulary outside the data spine (fails B1) and its
  monotone-union comment is a write-time coordination point (fails B2).

### D4 — Write-path absorption: one buffering mechanism under all three spellings (S2)

- A schema-agnostic, reflect-free **deferred-section buffer** (working
  name `dml/runtime.DeferredSectionBuffer`) buffers per-section
  contributions as thunks in first-seen section order and flushes one
  frame per section at commit. It is the single absorption mechanism:
  the generated typed builders enqueue instead of emitting, the facts
  encoders replace their hand-rolled workarounds with it, and
  `RowComposer` converges onto it (its current buffer is the spec).
- **One contribution per kind per entity stays the invariant.** Double-Add
  of a kind is a loud, attributed error at the Add — replacing today's
  silent `raw = true` degradation — and the entity bag stays
  `option.Option[Kind]`, preserving cache write-through, `Archetype()`,
  decode, and ADR-0100 SD5 unchanged. Multiplicity *within* a kind is the
  container shapes' job, not the bag's.
- **`Raw()` and buffered Adds are mutually exclusive per entity frame**,
  refused loudly at the second spelling's first use. Raw remains the
  escape hatch for layouts outside any kind, on entities that use it
  exclusively.
- Flush order is first-seen section order across contributions; both
  front-ends adopt it and the parity corpus is updated once,
  deliberately. Co-section groups flush whole; the equal-count commit
  check becomes reachable cross-kind for the first time and enters the
  corpus. Ambient stamps evaluate at flush; with Raw exclusivity the
  stamp stack is frame-stable on the typed path, and the buffer's doc
  says so.
- Error attribution rides along: buffer entries carry kind and section,
  and the DML's bare second-visit error gains the section name.
- The disjoint-sections gate **stays** — it guards read-side aliasing
  under positional ids, not the write frame. What dissolves is the
  write-spelling trichotomy; what remains is typed-vs-Raw, stated once.

### D5 — Contract evolution is monotone, and narrowing is loud (facts 1–2, R7)

- The invariant, stated as a rule in the consumer entry (D7): a slot's
  admits-set only grows (required → optional → many); a widened
  definition must keep reading rows written under the narrower one; wide
  readers and narrow writers coexist indefinitely (B2 has no flag-day);
  narrowing is the breaking direction and must be loud.
- The pinned silent defect is fixed: a **unit-shaped read refuses a
  multi-element value** with an attributed error, on every read path —
  reflect decode, generated codec, and the CH readback validator —
  extending ADR-0146 D4's attribute-count discipline to the value-count
  rung. The as-found assertions in `arity_evolution_test.go` move to the
  new behaviour.
- Sanctioned widening ladder today: required → optional and
  unit → container (both pinned). Shape crossings (scalar → tuple) are
  unpinned and unsanctioned until the corpus's tuple rung pins them.

### D6 — The plan is the definition; the skins say so (S3)

- Docs reframe: a component is a `mappingplan.Plan`; the struct is one
  authoring syntax among three. The type-discipline statement
  (components structurally typed, memberships nominally typed) opens the
  reframed skill.
- `Plan`'s Go residue (`GoType()`, `KindVar()`, Add-method descriptors)
  is recorded as a named constraint any future non-Go plan source
  inherits; no code moves now.
- `DefaultClassifier` is renamed to what it is (working name
  `PathPrefixClassifier`) with a deprecated alias; `nil` (all-primary)
  remains the default. Generated-side role filtering (F2) is descoped
  until a consumer needs it, consistent with the ADR-0073 adoption gap.

### D7 — One consumer entry, executable current-state, prose net-negative (S4)

- **U1:** the `leeway-components` skill becomes the consumer entry,
  reordered **mesh-first** — registry-backed ids over the shared facts
  table lead; the schema-agnostic closed world is the special case.
  `leeway-advanced` and the marshalling how-to shed duplicated component
  matter; EXPLANATION files become orientation pointers. Net line count
  across the landscape goes down.
- The **failure-mode corpus** (X-class) is adopted: one small test or
  worked example per surviving edge — I1, I2-diagnosis, R1, R2, R6
  retain-discipline, the tuple rung — with the **centerpiece** the
  missing main-scenario example: registry-resolved ids over a shared
  table, one domain formulating a component late over rows another
  domain wrote earlier (gated on D2).
- Hygiene the review caught: the marshalling how-to is re-reviewed and
  restamped; `EXPLANATION.md` gets a pass; ADR-0146's Context collision
  table is marked a historical (pre-D4) measurement via dated update; a
  new doclint rule flags a `status: stable` document edited after its
  `reviewed-date` (rule id assigned at implementation).
- This ADR's References carry the load-bearing-ADR map — which of the
  thirteen component-layer ADRs a consumer actually needs.

### D8 — Recorded descopes and deferrals

- **R2** (empty container unrepresentable) and **W3** (RowComposer error
  deferral) are inherent — wire fact and buffering cost; each gets one
  statement in the D7 home, nothing else.
- The educational app (plan-explorer chaining `mappingplanview` and
  `componentview`) stays deferred; it consumes the failure-mode corpus
  when built and gates nothing.
- Deferred, with their kill-reasons above: storegen CLI (D2), domain
  namespacing (D3), generated-side roles (D6).

### Milestones

- **M0 — value-arity refusal on every read path.** D5's fix; the pinned
  assertions move.
- **M1 — registry-backed lookup seam.** D1: snapshot helper +
  constructor + closed-world marking + `doc.go` correction.
- **M2 — storegen slice.** D2: `FactsWrapper` id-source methods;
  `runtime/factsschema/storegen` + gen test.
- **M3 — failure-mode corpus.** D7's X-class, centerpiece included.
- **M4 — doc unification.** D6 reframing + D7's U1, hygiene items, and
  the doclint rule.
- **M5 — vocabulary as facts v1.** D3: claim kind, init-time
  publication, reconciliation views.
- **M6 — write-path absorption.** D4: buffer, typed builders, facts
  encoders, RowComposer convergence, parity flush order.

M5 and M6 are independent of each other and may swap.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `marshallreflect` exported API | constructor + minimal resolver interface added (D1); unit-read refusal (D5) | `doc.go` correction; arity tests |
| vdd vocabulary package | snapshot helper added (D1); claim kind + publication (D3) | naming round-trip pin; reconciliation views |
| `marshallgen` wrapper contract | `FactsWrapper` gains id-source methods (D2) | `runtime/factsschema/storegen` + gen test |
| `recordstore/gen` emitted builder | Add verbs buffer; double-Add and Raw-mixing refuse loudly (D4) | 6 stores / 48 `*.out.go` regenerate; `sharedsection` tests |
| `dml/runtime` | deferred-section buffer added; second-visit error gains section name (D4) | facts encoders (`chstore`, `queryrunfacts`, `capmapfacts`); DML regeneration |
| generated codec read path + CH readback validator | unit-read refusal (D5) | readback suite |
| front-end parity contract | flush order becomes first-seen section order (D4) | parity corpus, updated once |
| facts vocabulary (registry members) | one new claim kind (D3) | factsschema regen lane |
| `membershiprole` API | classifier rename with deprecated alias (D6) | call sites, mechanical |
| doc landscape + doclint rule set | U1 consolidation; stale-stamp rule (D7) | skill, how-to, EXPLANATIONs, ADR-0146 dated update |

## Alternatives

- **DDL/table-comment assignment fingerprint.** Killed: vocabulary
  outside the data spine (B1), write-time coordination point (B2).
- **Split API-3 — buffer-and-facts only, defer the typed-store lift.**
  Rejected in dialogue: leaves the consumer surface a trichotomy
  indefinitely; the one-per-kind scoping (D4) removes the hidden scope
  that motivated the split.
- **Slice entity bags (unbounded multi-contribution per kind).**
  Rejected: multiplicity within a kind is the container shapes' job;
  slice bags drag SD5, the cache, and `Archetype()` for a shape the
  model does not need.
- **Defer all write-path work, sanction `Raw()` with guidance.**
  Rejected: the mesh's main composition path would stay the untrodden
  one, hand-rolled per encoder.
- **U2 (how-to as hub) / U3 (index page).** Rejected: the how-to's
  recipe form and stale stamp argue against growth; an index is the
  "new layer on top" anti-goal.
- **Per-API ADRs, or splitting the vocabulary design out.** Rejected in
  dialogue: one record decides; implementation refinements land as
  dated updates here.
- **A vocabulary service.** Not pursued: a live coordination point
  (B2); the facts substrate already is the shared medium (B1).

## Consequences

### Positive

- The silent class shrinks structurally: registry-stable ids by default
  (D1), skew queryable by anyone with SQL (D3), value-count narrowing
  loud (D5), double-Add loud (D4).
- One buffering mechanism replaces three write spellings' divergence and
  ~13 hand-rolled encoder workarounds.
- The mesh scenario gets its first end-to-end worked example, and the
  consumer entry leads with it.

### Negative

- 48 generated artifacts regenerate and 6 stores' emitted shape changes;
  the parity corpus's call-sequence identity is deliberately re-baselined
  once.
- Rows that previously decoded (multi-element under unit) now error —
  the point of D5, but any undiscovered writer of that shape breaks
  loudly at read.
- API-2 decided now means implementation may surface refinements; they
  land as dated updates rather than a fresh design round.

### Neutral

- Claim rows add a small, bounded write to every process init.
- Positional ids remain fully supported — reclassified, not removed.

## Migration — Tier 1

- **Breaks.** Double-Add of one kind: silent `raw = true` fallback
  becomes an error. Mixing `Raw()` with typed Adds in one entity frame:
  refused. Multi-element values under unit-shaped slots: read error on
  all paths (previously silent zero-fill). `DefaultClassifier`:
  deprecated alias.
- **Path.** Callers composing kinds via `Raw()` move to typed Adds once
  M6 lands (the reason Raw was needed disappears); pure-raw entities are
  untouched. No known writer produces multi-element-under-unit rows;
  any that surfaces reads the D5 error and fixes its plan arity.
- **Regeneration.** `recordstore` stores and DML artifacts regenerate
  (48 `*.out.go`); factsschema regen lane runs for the claim kind; no
  FFI boundary is involved.
- **Old shape.** Raw-fallback and zero-fill behaviours are removed
  outright; positional ids are kept indefinitely as the marked
  closed-world regime.

## Verification plan — Tier 1

- **Lane.** Default `go test`: naming round-trip pin over the full
  registry (D1), `arity_evolution_test.go` with moved assertions (D5),
  `sharedsection` round-trip and the parity corpus (D4), the storegen
  gen test (D2), the failure-mode corpus (D7). CH-backed behaviour:
  the clickhouse-local readback suite (D5's validator rung) and the
  `//go:build integration` lane for D3's reconciliation views.
- **What would fail.** A snapshot whose keys miss the DTO spelling fails
  the round-trip pin; a regression to silent zero-fill fails the moved
  arity assertions; buffered flush diverging between front-ends fails
  the parity corpus; a store generated without a registry-stable source
  for the facts table fails the gen test; doc drift of the stale-stamp
  class fails the new doclint rule.
- **Gap.** The mesh example simulates two domains as two packages in one
  repo — cross-deployment skew is exercised only as data (claim rows),
  not as genuinely separate binaries; acceptable because the claim
  substrate, not process separation, is the mechanism. D3's name
  governance is process (PR review), verified only by the audit view
  existing.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [Consumer-complexity analysis](../adr-background-work/leeway-components-consumer-complexity.md)
  — the edge inventory, drivers, benchmark, and costings this ADR decides
  over.
- Load-bearing ADRs for consumers of the layer:
  [ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md) (Scan/Filter, presence),
  [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md) (overlap, SD5/SD6),
  [ADR-0105](./0105-keelson-adopts-generated-record-stores.md) (id regimes, D2/D3b),
  [ADR-0146](./0146-leeway-marshall-component-read-contract.md) (read contract, D4–D6),
  [ADR-0073](./0073-leeway-membership-role.md) (roles),
  [ADR-0182](./0182-leeway-aspects-v2-codec-and-vocabulary.md) (timeless
  vocabulary precedent). The remaining component-layer ADRs are
  historical for consumers.
- Executable precedents: `marshallreflect_test/parity_corpus_test.go`,
  `recordstore/sharedsection/roundtrip_test.go`,
  `marshallreflect_test/arity_evolution_test.go`.
