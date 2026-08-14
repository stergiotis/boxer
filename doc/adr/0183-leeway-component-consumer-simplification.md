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
- The registry's own minting is registration-ordinal: a natural key's id
  is `tag.ComposeId(offset + Len())` at package-init time
  (`stopa/registry/lw_registry_nk.go`) — the positional regime S1
  indicts, one altitude up — and nothing pins it: `VcsManagedContract`
  validates only name shape and tag parity, and the vdd package carries
  no tests, so a mid-file insertion or a file rename silently renumbers
  every later registration while the wire keeps the old ids. The
  sibling tag-value registry already takes its value explicitly at the
  call site; the natural-key registry is the outlier.
- Cross-vocabulary disjointness in a shared table rides entirely on
  distinct **effective** tag values, and nothing checks them. The three
  cohabiting vocabularies sit at effective 1 (vdd — odd, despite
  runtime/vocab's comment stating an effective-even convention), 2
  (runtime), and 16 (capmap, a named base constant); the jsonbench app
  vocabulary independently picked 2, colliding with runtime — latent
  only because it shares no table. `VcsManagedContract` meanwhile
  checks parity of the *declared* value, which is 0 in all four
  packages. Three readings of "convention A" coexist; none is
  enforced. Beyond minting, the pipeline treats ids as opaque uint64s
  (no non-test use of `Split`/`SameTag`/`RemoveTag` anywhere in it);
  the only other tag-structure consumer is `identity/identsql`,
  ADR-0106 SD2's SQL half. The allocation is also inverted: vdd's ~140
  names sit on effective tag value 1 — the single two-bit prefix, whose
  62-bit body is the scheme's widest — while the actual wide-body
  constituency (the `identity/identgen` runtime generators:
  badger-sequenced, leased, in-memory) competes for the same scarce
  short prefixes; the tag-planning layer that would size them
  (`identity/fibonacci`, ADR-0106 SD4) exists and is unused by every
  vocabulary package.

ADR-0105 D3b's storegen resolver (registry ids baked into generated
artifacts) was unbuilt when this ADR was written, and gated the one
worked example the landscape lacks — the mesh scenario itself. It has
since been built for ADR-0184 (see D2), which both removes that gate and
supplies a second vocabulary to test the design against.

## Decision

We adopt the following, attacking the four accidental drivers at their
sites rather than documenting their symptoms. Working names are marked;
final naming lands with the code.

### D0 — Vocabulary ids are declared in source, never derived from initialization order (S1 at the mint)

Implicit ordinal minting in the natural-key registry is replaced by
explicit declaration: every registration in a VCS-managed registry
states its untagged ordinal beside its name — protobuf-field-number
style, and the parameter shape the sibling tag-value registry already
has —

```go
MembDroneStatus = Reg.MustBegin("droneStatus", 17).End()
```

with the registry composing the tag as before. Duplicate ordinals or
names are refused at init (loud in any test that links the package),
and `VcsManagedContract` refuses the implicit form outright, so
order-derived ids survive only in non-VCS-managed (test/ephemeral)
registries. The `(name, ordinal)` pair becomes statically analyzable —
greppable and reviewable in a diff exactly like a `const` block — and
initialization order stops mattering: file renames, var reordering,
and partial link sets can no longer change any value.
Link-set-dependence thereby shrinks to **extent** — a partial link set
yields fewer names, never different ids — and D2 already fails closed
on missing names.

Scope: all four VCS-managed registries — `keelson/vdd` (~140
registrations across 14 files), `keelson/runtime/vocab`,
`gov/capmapvocab`, and the jsonbench app vocabulary. Migration keeps
today's ordinals (dump each registry via `IterateAll()`, splice each
current ordinal into its registration); an explicit ordinal also
inherits a per-registry ceiling — the body budget below the registry's
tag — which `AddTag`'s guard already makes loud.

The rule extends one level up, and gains an authority: **tag values
are claimed, not constructed**. A dedicated package,
**`identity/tagmint`**, is the only source of the unforgeable token
the registry constructors accept (`tagmint.MustClaim(name, value)`) —
no claim, no registry, so the fence is the type
system, not convention. The value stays in the consumer, one literal
at the claim site (never assembled from an offset in one place and a
relative zero in another); the provider checks **uniqueness**
(duplicate value or name refused at init) and **fit** — the claimed
value's width class must suit the family's declared cardinality, via
the tag-planning layer built for exactly this
(`identity/fibonacci`, ADR-0106 SD4: `SelectFittingTagValueRange`,
`WidthClass`). The per-package `TagValueRegistry` instances dissolve
into `tagmint`, and "convention A"'s parity check retires
with them. Claim-time uniqueness is per link set, like every
init-populated registry; the goldens make it total — the D1 union
test reads the committed golden files, so no link set can hide a
collision.

**Vocabulary tags move to the width-32 class.** Today's allocation is
inverted (see Context): the smallest families hold the widest bodies.
All four vocabulary packages re-key to claimed values from the class
of tag values whose fibonacci code spends exactly 32 bits
(`WidthClass(32)` in the plan layer) — a uniform 32/32 tag/body split
for every vocabulary. That buys: 32 body bits (~4·10⁹ ids) per
vocabulary; a large pool of claimable tag values in the class (the
plan layer's `TagValueCount` states it exactly); one constant split
width for `identsql` views and `SameTag` mask compares; and symmetry
with D8, whose reserved runtime tag is this same class's maximum.
Exact legible values are pinned at M1 with the goldens. The move
frees the short prefixes for high-cardinality families (`identgen`)
and kills the jsonbench/runtime duplicate en passant. This re-keys every composed
membership id — a **breaking data change**, taken deliberately in the
pre-first-consumer window (the ADR-0182 precedent: run the breaking
pass while the window is open). The code side is *nearly* free: the
facts codecs and `factswrapper` resolve ids at package init, so a
rebuild carries them. Generation-baked ids are no longer hypothetical,
though — D2 landed first, so `sysmfacts` already bakes `sysmvocab`'s ids
into its emitted Scan filters and cross-check map. Those regenerate from
a gen test, so the cost is a regeneration step in the flag-day rather
than a hand edit, but the re-key now has to name it. Existing facts data
is regenerated or migrated with the re-key; once claims are live, D3
flags any straggler binary.

Naming rides the same decision: the `leeway/stopa` umbrella
(registries, contracts, natural-key encoding) is renamed
**`leeway/namemint`** — the name mint, where a name is struck into
its timeless id. "stopa" carried no documented meaning; the new pair
makes the two-level minting story readable from the import graph — a
vocabulary package claims its tag from `tagmint`, then registers
names into a `namemint` registry that composes the ids. ("namemint"
over "symbolmint": the repo's term for the atom is *name* — natural
keys, `naming.StylableName`, I4 name governance — and one atom should
carry one term. "idmint" was rejected for colliding with
`identity/identgen`'s runtime-generation role.) Both package docs
state what their names mean. The rename is mechanical (20 non-test
importers) and ships in M1's flag-day.

### D1 — Registry-stable ids are the default; positional ids are marked closed-world (S1)

- The snapshot helper is **built**, and not where this section first put
  it. [`storegen.MembershipIds`](../../public/keelson/runtime/factsschema/storegen/storegen.go)
  takes any natural-key registry through a one-method local interface
  (`IterateAll()`) and materializes it as `map[string]uint64`, keyed by
  the **marshalling-side (lowerCamel) spelling**. Name-style conversion
  happens only at that seam; it refuses a post-conversion collision and a
  zero id (tag value zero is reserved invalid, ADR-0106 §SD8), and a test
  over the full vdd vocabulary pins round-trip conversion — the converter
  is non-idempotent at digit boundaries, which is what the pin is for.

  Consumer-side and registry-parameterized rather than vdd-side, and that
  is the correction rather than an implementation detail: a helper living
  in vdd would have had to be copied for the second vocabulary.
  ADR-0184's `sysmvocab` registry consumes it unchanged, which is the
  property the vdd-side shape could not have had.
- marshallreflect gains a snapshot-backed constructor for `MapLookup`
  (working name `NewRegistryLookup`), typed on a minimal local interface
  or the materialized map — not on the registry type. Hand-built
  `MapLookup` literals stay legal and become the *documented*
  closed-world escape hatch, as does `NoOpWrapper`'s declaration-order
  regime on the generated side. **Not built** — the storegen slice needed
  only the generated path.
- One snapshot value feeds both seams. The generated path is done:
  `storegen` hands the map to `FixedIdsWrapper`. The reflect path waits
  on the constructor above, and with it the deletion of the private
  `mapIdLookup` bridge in `recordstore/gen`.
  `marshallreflect.LookupI` and `readback.IdLookup` stay deliberate
  structural twins (collapsing them forces an import direction between
  leaf packages for a one-method interface).
- `marshallreflect/doc.go`'s registry claim is corrected to describe
  what this ADR builds, not asserted as existing.
- Under D0 a snapshot is link-set-dependent in extent only (the
  registry still populates by package-init side effects, so the linked
  vocabulary package defines which names appear — the helper's doc says
  so); no link set can change a value.
- Each VCS-managed registry gains a committed **assignment golden** — a
  test pinning every `(name, id)` pair, where the pinned id is the
  **composed tagged id**. D0's init-time refusal catches duplicates and
  reordering; the golden catches the remaining silent case, editing an
  existing ordinal; and one further test unions all goldens and asserts
  global disjointness — pinned ids carry their tags, so a tag-value
  collision between two vocabularies (the jsonbench/runtime duplicate
  in the Context) surfaces there as an id collision. Together they make
  the append-only discipline mechanical rather than aspirational.

### D2 — The storegen slice unblocks the mesh example (S1, ADR-0105 D3b)

**Built** (ADR-0105 D2, ADR-0184 M1), through a smaller seam than this
section first specified. `FactsWrapper` gains nothing:
[`runtime/factsschema/storegen`](../../public/keelson/runtime/factsschema/storegen/storegen.go)
resolves ids itself via `MembershipIds` (D1) and hands them to
`recordstore/gen` as a `marshallgen.FixedIdsWrapper`. The checked-in
codec artifacts' bytes are untouched either way.

Putting `PlanMembershipIds`/`GloballyUniqueIds` on `FactsWrapper` would
have been the wrong shape twice over, which building the alternative made
clear. `FactsWrapper` is the facts *codec's* wrapper and resolves
`vdd.Memb*`, so a store generated through it would have been bound to vdd
— where a store binds whichever vocabulary its components are written
against, and `boxer.facts` carries several kept apart by tag value
(ADR-0184 §SD4 mints at base 32). It would also have put id resolution on
a type whose job is emitting codec source. Nothing needs the methods: the
generator already accepts an id source, and a snapshot is a plain map.

Generation runs in the gen-test lane (the `sharedsection` pattern); a CLI
command is deferred until a second consumer exists — no recordstore
generator command exists at all today, and the gen test is the repo's
proven lane.

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
  interesting one. Views that partition by tag (per-vocabulary lanes,
  D8's runtime lane) use `identity/identsql` — ADR-0106 SD2's SQL
  half, already consumed by the play vocabulary panel.
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
- Division of labor across D0/D1/D3: the goldens are the **preventive**
  control, in VCS, before anything ships; the claims are the
  **detective** control, in data, catching what ships anyway (stale
  binaries, skipped tests, forks). Neither mints. The code is the sole
  id authority — the store never allocates — and reconciliation
  surfaces line items whose remedy is operational (rebuild, redeploy,
  retire an assignment), never transactional.

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
  domain wrote earlier — no longer gated, since D2 is built.
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
- Runtime- and roundtrip-minted ids get a **reserved tag, decided
  here**: one designated large-value tag (working name
  `RuntimeMintTagValue`), claimed through D0's authority like every
  other tag value and pinned by a test beside the assignment goldens. Purpose: ids assigned
  outside VCS — memberships coined at runtime (names unknown at build
  time, e.g. ingested foreign vocabularies) and roundtrip/passthrough
  cases where data-carried names need local ids that survive
  write→read→write without a VCS registration. The fence is the name,
  not parity: `VcsManagedContract` refuses the reserved value (against
  the *effective*, offset-adjusted tag), no generated artifact bakes
  ids under it, and D3 claims publish such ids like any others while
  the reconciliation views partition by tag, so the runtime lane
  audits separately from the VCS vocabulary. Prefix-freedom (ADR-0106)
  already makes distinct tags collision-free, so the reservation
  carries meaning, not safety — and "convention A"'s even/odd parity
  rule is recorded as a pre-0106 legacy heuristic superseded by this
  named fence. Proposed value: the largest tag value whose fibonacci
  code still leaves 32 body bits — the maximum of the same width-32
  class D0 assigns to vocabularies, a legible 32/32 split, ~4·10⁹
  runtime ids — computed and pinned at M1. The allocator itself
  (who assigns bodies; how independent writers stay collision-free)
  remains deferred to its own ADR.

### Milestones

- **M0 — value-arity refusal on every read path.** D5's fix; the pinned
  assertions move.
- **M1 — id-source hardening.** D0: claim authority, explicit ordinals
  (kept at today's values), the vocabulary-tag re-key — one flag-day,
  now including a regeneration pass for the stores D2 already baked —
  and the assignment goldens; D1: the reflect-path constructor +
  closed-world marking + `doc.go` correction. The snapshot helper the
  rest of D1 rests on landed early, with M2.
- **M2 — storegen slice.** ✓ D2: `runtime/factsschema/storegen` +
  `MembershipIds` + gen test, built ahead of M1 because ADR-0184 needed
  it. `FactsWrapper` was left alone — see D2 for why that turned out to
  be the point rather than a shortcut.
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
| `stopa/registry` natural-key API (renamed `namemint/registry`) | explicit-ordinal registration; `VcsManagedContract` refuses implicit minting (D0) | four vocabulary packages rewrite registrations; ~20 importers update the path; assignment goldens |
| `identity/identifier` tag space | named runtime-mint tag reserved; `VcsManagedContract` refuses it (D8) | reservation pin test; D3 views partition by tag |
| `identity/tagmint` (new) | claiming API; token required by the `namemint` registries; parity check and per-package `TagValueRegistry` instances retired (D0) | four vocabulary packages re-key to width-32 claims; facts data regenerated/migrated |
| `marshallreflect` exported API | constructor + minimal resolver interface added (D1); unit-read refusal (D5) | `doc.go` correction; arity tests |
| vdd vocabulary package | claim kind + publication (D3) | reconciliation views |
| `runtime/factsschema/storegen` (new) ✓ | registry → id-snapshot helper and the facts-bound store generator (D1, D2); no existing surface changed — `FactsWrapper` and the `marshallgen` wrapper contract are untouched | gen tests in the consuming package; naming round-trip pin |
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
- **`iota` in a single const block (for D0).** Rejected: it fixes the
  cross-file fragility but keeps positional derivation — a mid-block
  insertion still silently renumbers everything below; explicit
  ordinals are equally legible and strictly more robust.
- **Content-derived ids (name hash).** Rejected: order-independent but
  opaque to humans, needs collision management, and renumbers the
  entire existing wire history.
- **A vocabulary manifest + codegen.** Deferred: the same properties as
  D0 with more machinery; revisit only if vocabulary scale outgrows
  hand-maintained Go source.
- **Parity-partitioned tag space (odd tags for non-VCS minting).**
  Rejected: "convention A" predates ADR-0106's fibonacci tags — under
  prefix-free codes parity carries no structural meaning, and a
  meaning-bearing reservation must be named, not inferred from a
  numeric property.
- **Goldens without a claiming authority.** Rejected: goldens detect a
  collision after the fact; waterproof means a vocabulary registry
  cannot exist without a checked claim — prevention at the type level.
- **Keeping vocabularies on their small tag values.** Rejected: it
  spends the scheme's shortest prefixes (widest bodies) on its
  smallest families while `identgen`'s high-cardinality generators
  need them, and the jsonbench/runtime duplicate shows the hand-picked
  regime does not even stay disjoint.
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
- Every composed membership id changes once (the vocabulary-tag
  re-key) — a deliberate break taken while the window is open; facts
  data regenerates or migrates with it.
- API-2 decided now means implementation may surface refinements; they
  land as dated updates rather than a fresh design round.

### Neutral

- Claim rows add a small, bounded write to every process init.
- Positional ids remain fully supported — reclassified, not removed.

## Migration — Tier 1

- **Breaks.** Implicit registration on a VCS-managed registry: refused
  at init (D0). The vocabulary-tag re-key changes every composed
  membership id: facts data written under the old tags predates the
  new assignment (D0). The `leeway/stopa` import path becomes
  `leeway/namemint` (D0). Double-Add of one kind: silent `raw = true` fallback
  becomes an error. Mixing `Raw()` with typed Adds in one entity frame:
  refused. Multi-element values under unit-shaped slots: read error on
  all paths (previously silent zero-fill). `DefaultClassifier`:
  deprecated alias.
- **Path.** The four vocabulary packages keep today's ordinals (dump
  via `IterateAll()`, splice each into its registration) and claim
  narrow-body tag values; ordinal explication, the re-key, and the
  goldens ship as one flag-day in M1. `Memb*` references are unchanged
  and every codec resolves at init, so a rebuild carries the code
  side; facts tables are regenerated (introspection-derived facts
  recompute) or migrated in the same window. Callers composing kinds via `Raw()` move to typed Adds
  once M6 lands (the reason Raw was needed disappears); pure-raw
  entities are untouched. No known writer produces
  multi-element-under-unit rows; any that surfaces reads the D5 error
  and fixes its plan arity.
- **Regeneration.** `recordstore` stores and DML artifacts regenerate
  (48 `*.out.go`); factsschema regen lane runs for the claim kind; no
  FFI boundary is involved. The tag re-key changes resolved values but
  no checked-in artifact: codecs and `factswrapper` resolve at init, so
  the `*.out.go` files stay byte-stable and a rebuild suffices.
- **Old shape.** Raw-fallback and zero-fill behaviours are removed
  outright; the offset+relative tag-value split and convention A's
  parity rule are retired outright; positional ids are kept
  indefinitely as the marked closed-world regime.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the per-registry assignment goldens,
  init-time duplicate refusal, and claim-authority refusal tests
  (duplicate value or name; band misfit) (D0), the naming round-trip pin
  over the full registry and the converging-name and zero-id refusals
  (D1, in `storegen`), `arity_evolution_test.go` with moved assertions (D5),
  `sharedsection` round-trip and the parity corpus (D4), the storegen
  gen test (D2), the failure-mode corpus (D7). CH-backed behaviour:
  the clickhouse-local readback suite (D5's validator rung) and the
  `//go:build integration` lane for D3's reconciliation views.
- **What would fail.** An edited ordinal fails the assignment golden; a
  duplicate fails init in every test linking the package; a VCS-managed
  registration landing on the reserved runtime tag fails contract
  validation, and moving the reserved value fails its pin; a registry
  constructed without a claimed tag value fails to compile; a snapshot
  whose keys miss the DTO spelling fails
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
