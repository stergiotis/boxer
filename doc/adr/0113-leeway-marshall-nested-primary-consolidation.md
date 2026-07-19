---
type: adr
status: proposed
date: 2026-07-19
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0113: leeway marshall — nested becomes the primary escalation surface; the flat escalation grammar freezes, its removal deferred

## Context

The marshall stack has two authoring front-ends (codegen `marshallgen`, runtime
`marshallreflect`, [ADR-0074](0074-leeway-marshall-package-layout.md)) over one
shared `mappingplan.Plan`. On the flat `lw:` grammar four *escalation* mechanisms
accreted, each added when a section outgrew a one-line tag: `:column` sub-columns
([ADR-0101](0101-leeway-marshall-mixed-shape-sections.md)), `@membership` dynamic
tuples ([ADR-0103](0103-leeway-marshall-dynamic-membership-tuples.md),
[ADR-0109](0109-leeway-marshall-multi-membership-ref-tuples.md)), channel flags,
and carrier siblings. A second spelling — the **nested attribute-struct model**
(role by field type, cardinality by field multiplicity) — shipped 2026-07-07..09
against the draft [nested how-to](../howto/leeway-marshalling-nested.md) without
an ADR; this document is also its retroactive design record.

A **2026-07-10 census** framed the question: every in-tree production DTO uses
only the **simple subset** (header roles plus `m,section` one-liners); the
escalation mechanisms have **exactly one** production consumer — an external
ontology adopter (downstream repository); `,explode`, `,highCardVerbatim`, and the
parametrized channels have **no** consumer anywhere, demos included. The
maintenance cost is multiplicative: the byte-identity invariant must hold across
gen × reflect × (flat | nested) × hand-DML.

*Census precision (2026-07-19, cross-repo check):* the adopter's tree holds one
**exploratory, reflect-only probe test** pairing `,explode` with a mixed carrier
(an edge-as-membership sketch, predating the census); no production usage. D1
stands — at enactment the probe migrates to hand-DML (D5's peer path for shapes
past the DTO model) or retires. It is also the first concrete trace of the
adopter-side carrier interest the D5 parking anticipates.

A **2026-07-19 review** added three findings that narrow the decision:

- **Read is grammar-neutral.** The batch read codec (`Unmarshal` / `FillFromArrow`)
  drives the shared Plan/DML/RA and is byte-identical whether the DTO was authored
  flat or nested. Deprecating flat neither protects nor endangers the read path —
  the one capability with no cheap hand-rolled substitute.
- **Hand-DML is a write peer to nested.** The generated name-bound
  `Add(<Section>Attr{…})` value-struct already gives name-safe writes with no DTO
  grammar, so nested's compile-time-safety edge over the alternatives is narrower
  than first assessed.
- **Nested's safety is worthless to a generator,** and the authoring policy points
  rich models at generation. So whether escalation DTOs are *generated* or
  *hand-authored* decides whether nested or a frozen flat grammar is the better
  target — a question the 2026-07-10 alternatives flagged as "real" and overrode.

A **post-revision adversarial review (2026-07-19)** probed the family's stated
invariants. In this ADR's terms: byte-identity is test-backed; the census holds
(zero consumers for D1's sugar and the parametrized channels); the one-IR /
shared-builder / shared-layout spine should not be merged or split. Its one
structural finding: the two front-ends' accept sets are **not** identical today,
so the parity premise has confirmed cracks at the codegen edge (see
[Review fallout](#review-fallout-2026-07-19)). It endorsed D1 as the largest
available simplification and asked for a concrete trigger on D5's parked
carriers — both folded in below.

## Decision

**D1 — cut the zero-consumer flat sugar now.** Remove `,explode` (subsumed by the
nested `[]Attr`) and the `,highCardVerbatim` spelling (its channel survives at the
DML level) from the authoring grammar. Unconditional and independent of the rest.
The original ADR deferred this to a single switchover; with removal now deferred
(D3), user-less sugar is not held hostage to it. (The parametrized channels,
also user-less today, are the *carrier* family — parked in D5, not cut, because
their statements consumer is named.)

**D2 — nested is the primary escalation surface; the flat escalation grammar
freezes.** Anything past a one-line tag is authored nested. The flat grammar
remains the authoring surface for its **simple subset** — the nested model's
degenerate case, a closed list: the four header roles; `m,section` one-liners
(scalar / `Option` / bag) on the default, `highCardRef`, or verbatim channels;
`,unit`; `const=`; `,ct=`. The flat escalation spellings (`:column`,
`@membership`) are **frozen** — supported, no new features — **not removed**. The
two how-tos merge into one recipe (simple subset degenerate, nested escalation);
no per-front-end capability matrix. **D2 forces no migration and needs no parity
work.**

**D3 — removing the flat escalation grammar is deferred and contingent** (not a
scheduled switchover). `PlanBuilder` rejects the frozen flat escalation spellings
only once **both** hold:

- the sole escalation consumer has migrated to nested and shown it better in
  practice — its shapes (dynamic-membership verbatim tuples over mixed sections)
  already drive **both** front-ends, so the migration needs no parity work; and
- rich-model escalation is settled as **hand-authored** (nested wins → remove)
  rather than **generated** (flat is the better generation IR — plain tags, no
  marker import, compile-safety moot for generated code → keep flat frozen).

The still-deferred nested surfaces — the codegen value-marker bridge (known-hard;
a first attempt was reverted), `lw.Single` as a nested sub-column, One/Optional
dynamic-membership sections — are built **on consumer demand, not to enable
removal.** Spending the hard bridge to retire ≤1-consumer corners is rejected.

**D4 — ADR dispositions.** ADR-0103 and ADR-0109 → **accepted** (consumer exists,
gates green). ADR-0110 is folded into D5 and its file deleted; the number stays
retired.

**D5 — authoring policy and parked carriers.**

- Humans hand-write simple DTOs; rich models **generate** them (targeting the
  simple subset). Nested is the one escalation mechanism. Hand-written DML —
  name-safe via the generated `Add` value-struct — is a **first-class peer**, for
  the write path and for shapes past the DTO model, and stays in the byte-identity
  gate. Gaps close by generation or DML first, by grammar only with a named consumer.
- **Carriers** (per-value provenance = the membership *params* axis,
  [ADR-0072](0072-leeway-membership-carriage.md); the mixed and parametrized
  channels, [ADR-0008](0008-leeway-marshall-extensions.md)) stay **flat-grammar-only
  and single-attribute** for now. Nested spelling (reserved): an `lw.MixedRef` /
  `lw.MixedVerbatim` / `lw.RefParams` field that *is* the carrier — no value
  sibling — emitting one `AddMembership<Carrier>P` per attribute; params are
  wire-emitted even when empty (ADR-0008 SD8). Unparks when a consumer's
  per-value-provenance slice commits; until then barred inside tuples and as
  sub-columns. Parked is not permanent by inertia: the parametrized pair costs
  a carrier decode family in both back-ends while having no consumer anywhere,
  so if the named statements consumer is dropped from the roadmap — or when
  D3's contingencies resolve, whichever comes first — the parking is
  re-justified against a then-current commitment or the pair follows D1 out of
  the authoring grammar and the codecs (the ADR-0072 wire grid is untouched
  either way).

## Verification

- The byte-identity matrix stays load-bearing: `array.RecordEqual` + Arrow IPC
  equality + gen↔reflect cross-decode + nested-vs-flat equal-records, extended per
  nested surface as it lands.
- **Front-end parity gains a mechanical gate:** one shared DTO corpus through
  both `marshallgen.ParsePlan` and `marshallreflect.PlanFor`, asserting
  identical accept / reject decisions and, where both accept, equal plans.
  Parity was previously asserted by mirrored comments, several of which had
  drifted from the code; both 2026-07-19 acceptance defects live in exactly the
  gap this gate closes, and the membership-marker channel table is hand-mirrored
  in the two classifiers. Each fallout fix lands with its negative test.
- Every frozen flat escalation spelling keeps regenerating wire-stable.
- If D3 ever fires it adds negative tests: each removed spelling fails at
  `PlanFor` / `Validate` naming its nested (or DML) replacement.

## Review fallout (2026-07-19)

Marshall-family defects and drift found by the adversarial review — ordinary
fixes, none waiting on this ADR's acceptance. The two acceptance defects and
the parity gate landed (f202c473); the doc / comment drift and the two
hardening items remain open:

- **Codegen accepts `*S` nested-Optional and emits non-compiling code.** The
  emitter's Optional arms assume `option.Option[S]` (`Val` / `Has` SoA), so the
  written codec fails the next `go build` (confirmed end to end). Fix: drop the
  `*ast.StarExpr` branch in `nestedSectionCardinalityAst`; `*S` then falls
  through to `classifyType`'s pointer rejection, and the how-to's "codegen
  rejects `*S`" claim becomes true instead of false.
- **Codegen accepts unexported tagged top-level fields; reflect rejects them.**
  The tuple / nested element walkers check `ast.IsExported`; the top-level loop
  does not, and the reflect-side comment claims a parity that does not exist.
  Fix: add the check; correct the comment.
- **Doc / comment drift**, absorbed into D2's how-to merge: the nested how-to
  advertises `lw.U8Array`, which does not exist (add the marker + registry
  entry, or strike it); `marshallreflect.addNestedSectionField`'s comment says
  untagged sub-columns default to the lower-cased field name (the shared
  builder's default is `value`); the readback `Artefacts` comment claims all
  fragments reference the helper UDFs (presence and the non-const validator /
  filter are ClickHouse-built-ins-only — the property the single-statement
  executor contract relies on).
- **Smaller hardening:** `Validate` leaves scalar-section `BeginAttribute` arity
  unchecked, so a mis-wired DML panics mid-Marshal instead of failing
  validation; `ReadRowSupported` admits const-only kinds whose component then
  reads back as permanently absent (reject, like plain-only kinds).

Store-layer findings from the same review travel with their own tracks
([ADR-0100](0100-recordstore-generated-leeway-clickhouse-store.md) /
[ADR-0105](0105-keelson-adopts-generated-record-stores.md) /
[ADR-0112](0112-dimensionstore-interned-facts-additive-memberships.md)), not here.

## Alternatives

- **Proceed with the original scheduled switchover** (remove the flat escalation
  grammar at a P1–P3 parity gate). *Deferred, not rejected:* with one controlled
  consumer and a grammar-neutral read path, removal is cheap to postpone, and the
  hard value-marker bridge should not be spent before the generated-vs-hand-authored
  question (D3) is settled.
- **Freeze nested as a parked experiment** (keep flat primary). *Rejected* — but
  its strongest argument (generation needs no authoring ergonomics; rich models
  head toward generation) is now load-bearing **inside** D3's contingency rather
  than overridden.
- **Remove the nested front-end.** *Rejected:* reverts verified work, and the lane
  commits interleave wire-level network-type changes that must stay.
- **Replace the read codec with a generated plan-level read cursor** (write = DML,
  read = a generated cursor over the section / attribute / sub-column / membership
  structure), retiring **both** grammars. *Recorded for a future ADR:* symmetric
  with the DML `Add`, schema as sole source of truth, natural tuple reads (which
  `ReadRow`, [ADR-0100](0100-recordstore-generated-leeway-clickhouse-store.md) /
  [ADR-0105](0105-keelson-adopts-generated-record-stores.md), refuses). Not adopted
  here: it trades the DSL for a read-cursor generator and gives up one-call
  cross-section entity materialization — negligible for a single-section consumer,
  real once a domain object spans many sections. D5 keeps DML a peer so this path
  stays open.

## Consequences

- The interim two-paradigm cost is bounded and mostly paid down by **D1 + D2**
  (dead sugar gone, docs converged) **without** the migration or removal work.
- The permanent simple subset is untouched by construction — production DTOs never
  migrate.
- **Risk:** the frozen flat escalation grammar lingers indefinitely. Accepted —
  frozen, its maintenance cost is low, and D3 keeps a clean removal path if the
  contingencies resolve.

## Open questions

- **Will rich-model escalation DTOs be generated or hand-authored?** — settles D3.
- Do `,unit` / `,ct=` remain permanent simple-subset sugar or eventually deprecate?
  (lean: stay.)
- Does a generated plan-level read cursor supersede the codec read path? (its own ADR.)

## Status

Proposed (2026-07-10; **revised 2026-07-19**). Origin: a consolidation design
dialogue (2026-07-10) after a complexity review of the marshall stack; also the
retroactive record of the nested front-end shipped 2026-07-07..09. The 2026-07-19
revision splits the original switchover into an unconditional cleanup (D1), an
immediate freeze (D2), and a deferred, contingent removal (D3), on the
read-neutrality / hand-DML-parity / generation findings above. A same-day
post-revision adversarial review verified byte-identity and the census,
endorsed D1, and contributed the parity gate, the D5 trigger, and the fallout
list. No decision code accompanies this ADR — the fallout items proceed as
ordinary defect fixes; on acceptance the one concrete action is D1's sugar
removal, the rest is policy.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0042](0042-keelson-leeway-codec-soa-generator.md) — the original codegen paradigm
- [ADR-0072](0072-leeway-membership-carriage.md) — the membership channel grid (wire model; untouched)
- [ADR-0074](0074-leeway-marshall-package-layout.md) — the front-end / plan-model split
- [ADR-0008](0008-leeway-marshall-extensions.md) — carrier channels / SD8 presence
- [ADR-0101](0101-leeway-marshall-mixed-shape-sections.md), [ADR-0103](0103-leeway-marshall-dynamic-membership-tuples.md), [ADR-0109](0109-leeway-marshall-multi-membership-ref-tuples.md) — the escalation arc (its carrier extension, formerly ADR-0110, folded into D5)
- [ADR-0100](0100-recordstore-generated-leeway-clickhouse-store.md), [ADR-0105](0105-keelson-adopts-generated-record-stores.md) — store `ReadRow` (excludes tuples — the read-cursor alternative's opening)
- [doc/howto/leeway-marshalling.md](../howto/leeway-marshalling.md), [doc/howto/leeway-marshalling-nested.md](../howto/leeway-marshalling-nested.md) — the two recipes D2 converges
