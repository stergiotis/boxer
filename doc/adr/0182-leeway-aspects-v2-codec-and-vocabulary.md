---
type: adr
status: accepted
date: 2026-08-10
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-10
---

# ADR-0182: leeway aspects v2 — segment codec and timeless vocabulary

## Context

Leeway annotates schemas with three closed aspect vocabularies
(`valueaspects`, `useaspects`, `encodingaspects`), embedded as base62(u64
bitmask) segments in every physical column name and recovered by
`DiscoverTableFromColumnNames`. Two reviews
([vocabularies](../adr-background-work/leeway-aspect-vocabularies-review.md),
[v2 analysis](../adr-background-work/leeway-aspects-v2-encoding-and-vocabulary.md))
established: the numbering is a durable wire format with a hard 64 ceiling;
the mask encoding prices a set by its highest index (10 chars for 3–4
aspects) and 34% of all aspect bytes are lone `"0"` empty markers, measured
over 807 columns in the committed DDL goldens; and the vocabulary carried
era-bound "data fashion" families with zero readers. The Categorial removal
(be47b3b4) exercised the full renumber/regenerate/adopter-lockstep path.
The window for a breaking pass closes when
[ADR-0181](./0181-leeway-dql-authoring-surface.md) makes aspect names
user-facing DQL vocabulary.

## Decision

### SD1 — Segment codec v2: empty is empty, sets are digit-lists

- **Alphabet:** the ASCII-sorted base62 alphabet `0-9A-Za-z` (not
  `math/big`'s `0-9a-zA-Z`), so alphabet order equals byte order.
- **Empty set:** the empty string. A default-constructed `AspectSet` is
  valid; `EmptyAspectSet` becomes `""`.
- **Non-empty set:** one char per aspect, ascending — index *i* encodes as
  `alphabet[i+1]`. `'0'` (position 0) never occurs in a v2 segment.
  `'z'` (position 61) is the escape: each occurrence adds +60 to the
  following digit, chainable. Single chars therefore cover indices 0–59.
- **Canonical form:** strictly ascending decoded indices; within the
  single-char range this is strictly ascending bytes. Exactly one encoding
  per set; validators reject non-canonical segments.
- **Membership without decoding:** `Contains(a)` is a byte search for
  `alphabet[a+1]` — also expressible in SQL as one `position()` call.
- **Validity is per element:** an unknown (too-new) index invalidates that
  element, not the whole set; readers surface known aspects plus an
  unknown-count instead of today's silent whole-set poisoning.
- **Era tolerance:** a bare `"0"` decodes as the empty set (the v1 empty
  marker, unambiguous since `'0'` is reserved). Any other v1 segment is
  foreign; mixed-era deployments are unsupported (see Consequences).

### SD2 — Vocabulary: timeless, family-grouped, renumbered

Admission criterion (recorded in each package's doc comment): anchored in
mathematics, a long-lived open standard, a practice predating the current
tooling generation, or a format the engine itself commits to; domain closed
under the anchor, or a genuinely independent boolean; open-domain
technique/tier/brand information belongs in canonical types, TableOptions
or the catalog.

- **Removed** (all zero-reader): `valueaspects` — `None`, `Feature` ×8,
  `MachineLearningEmbedding`, `BinaryCodedDecimal`, `ReflectedBinaryCode`,
  `TrinaryLogic` (13); `useaspects` — `Indefinite`, `Observability`,
  Org/Business ×8, `MiniDimension` + `SlowlyChangingDimension` ×8 (19);
  `encodingaspects` — `None` (1).
- **Added, `valueaspects`** (11): `IdReference` (foreign reference — the
  adopter's literal TODO), `Pseudonymized`, `Secret`; `TransactionTime`,
  `ValidTime` (SQL:2011), `Immutable`, `Synthetic`, `SentinelMissing`; and
  the epistemic origin family `Measured` / `Asserted` / `Derived`
  (exclusive 1-of-3; proximate origin wins; no inference licence —
  derivation edges stay in promotion-written facts).
- **Added, `useaspects`** (8): attribute-history `HistoryRetained` /
  `HistoryOverwritten` / `HistoryDual` (exclusive; SCD types map 2,4 / 1 /
  3,5–7; type 0 = value-level `Immutable`); Traffic Light Protocol
  `TlpClear` / `TlpGreen` / `TlpAmber` / `TlpAmberStrict` / `TlpRed`
  (exclusive; FIRST TLP 2.0, version pinned in comments).
- **Kept with definitions written into comments:** quality tiers
  Staging/Core/Semantical defined as refinement stages on their own terms
  (de-branded); `Authorization` (who may) vs `Access` (who did) vs `Audit`
  (what was checked); `Classification` (labels assigned to things) vs
  `Taxonomy` (the classification system itself); `Lineage`
  (artifact/column-level derivation topology) vs `Provenance*`
  (PROV-modeled record level); lifespan ×5 with boundaries stated as
  deployment-defined and advisory; `VectorValue` stays.
- **Renames** (source-level only): `AspectMachineGenerate` →
  `AspectMachineGenerated`; `Evolution`'s string `"change-evolution"` →
  `"evolution"`.
- **Numbering:** family-grouped, since everything renumbers anyway —
  segment chars then cluster by family. Final sizes: `valueaspects` 59,
  `useaspects` 47, `encodingaspects` 23 — all inside the single-char range.

### SD3 — Family registry and generic exclusivity checking

Each package exports `Families = []Family{Name, Members, Exclusive}`. A
generic per-set predicate replaces the hand-coded uniformity-pair check in
`TableValidator` and covers every exclusive family (scales, epistemic,
history, TLP, lifespan, compression levels, …). Checks stay per-set and
local; the registry is documentation of record and, later, the source for
DQL authoring diagnostics. No relations between aspects, no inference.

### SD4 — Aspect UDFs

A generated `LW_ASPECT_*` family (segment extraction per kind, decode to
indices, name lookup, `LW_ASPECT_HAS_*` membership via `position()`),
emitted from the naming convention's position data and the enum tables,
installed through the ADR-0162 UDF lane and visible to the ADR-0174
vocabulary panel probe. Makes every aspect a queryable predicate over
`system.columns` — the generic consumer that satisfies the admission rule
for the governance families wholesale. v0 rejects the escape char.

## Surfaces — Tier 1

Breaking: `AspectE` values (renumber), `AspectSet` string semantics
(`EmptyAspectSet` `"0"` → `""`, digit-list segments), removed/renamed Go
constants, every physical column name carrying a non-empty aspect segment,
all generated artifacts embedding them (goldens, DDL/DML outputs, the
runtime facts-schema codegen outputs), and the external adopter's schema
sources. Unchanged: the three-vocabulary split, attachment points
(use → tagged sections only), the naming convention's segment positions,
`Contains`/`IterateAspects`/merger APIs (signatures).

## Alternatives

- **Delta/gap coding** — identical length inside the single-char range,
  loses per-char independence (membership needs a prefix sum; one bad char
  shifts all later elements). Rejected.
- **5-bit group pairs / offset+mask** — measured worse on the corpus
  (~3228 / ~3104 chars vs 2088). Rejected.
- **Interning dictionary** (1 char per observed set) — smallest, but
  discovery stops being decidable from one name. Rejected on the
  self-description premise.
- **Aspects out of names** (column comments / Arrow metadata) — names
  survive in result headers, logs and CSV where metadata does not; a
  premise change, not an encoding change. Rejected here.
- **Deprecate-in-place instead of removal** — keeps fashion families
  occupying wire slots forever; with zero readers and a pre-DQL window,
  removal is strictly cheaper. Rejected.
- **Keep the u64 mask, only garden the vocabulary** — consolidation lowers
  indices but the append-tax and 64 ceiling remain. Rejected.

## Consequences

- Aspect bytes in physical names drop ~49% on the measured corpus
  (5.07 → 2.59 chars/column; worst segment 10 → 4), and the 64-slot
  ceiling stops being wire-bound.
- The zero-value footgun and the dual empty encoding disappear; validity
  becomes per-element; canonicality is byte-checkable.
- One-time cost: full regeneration, adopter lockstep, and re-creation of
  old-era physical tables — including the live runtime facts database, the
  first real instance. Mixed-era deployments are unsupported; `"0"`-as-empty
  is the only cross-era tolerance.
- The vocabulary shrinks 144 → 129 while gaining three principled families;
  every exclusive family becomes mechanically checkable; aspects become
  queryable in SQL without Go in the loop.
- ADR-0181's future `sem:`/`enc:`/`use:` tokens inherit a cleaned,
  defined, registry-backed name set.

## Migration — Tier 1

Preconditions: the in-flight runtime facts-schema codegen rework must land
first (its outputs embed segments and its regen command is a migration
step); no DQL surface work consumes aspect names meanwhile; shared-worktree
discipline (explicit-path commits) throughout.

Then one window, ordered, each step gated on green:

1. Codec v2 in the three encoder packages (M0), behind its own tests.
2. Vocabulary edit + renumber + family registry + validator generalization
   (M1); canonical-enum and exclusivity tests green.
3. Regeneration sweep (M2): write-style gen-tests, `rewriteGold` flip for
   the e2e golden, the runtime facts-schema codegen command, full default
   suite; re-measure the corpus and record the numbers in the background
   doc.
4. Adopter lockstep (M3): constant swaps (including the rename), regen,
   its clickhouse-local readback round-trip as the end-to-end gate; paired
   commits, explanations in both messages, as for be47b3b4.
5. UDFs (M4): generate, install statements, integration-lane test against
   clickhouse-local, vocabulary-panel visibility.
6. Live-data re-derivation (M5): re-create old-era tables from regenerated
   DDL; re-ingest is a data-owner decision recorded at execution time.

## Verification plan — Tier 1

- Codec: round-trip property tests against a reference u64-set model;
  canonicality (reject non-ascending, reject `'0'`, escape round-trip);
  `""`/`"0"` empty semantics; per-element unknown handling.
- Vocabulary: existing canonical-enum tests (dense contiguity, `String()`
  injectivity and stylable-name validity) adapted; a generic
  family-exclusivity test driven by the registry.
- System: full default suite plus regenerated goldens; the corpus
  measurement re-run (expected ≈2.1k chars, worst 4); the adopter's
  clickhouse-local round-trip; a UDF integration test decoding
  `system.columns` of a freshly created table and agreeing with the Go
  decoder.

## Status

Accepted 2026-08-10.

- **M0 — segment codec v2 in the three aspect packages.** ✓
- **M1 — vocabulary pass, family registry, validator generalization.**
- **M2 — regeneration sweep and corpus re-measurement.**
- **M3 — external-adopter lockstep migration.**
- **M4 — `LW_ASPECT_*` UDFs, installed and probed.**
- **M5 — old-era physical-table re-derivation, docs and skills touch-ups.**

## References

- Background: [aspect-system review](../adr-background-work/leeway-aspect-vocabularies-review.md);
  [v2 measurement and proposals](../adr-background-work/leeway-aspects-v2-encoding-and-vocabulary.md).
- Precedent: Categorial removal, commit be47b3b4.
- FIRST, *Traffic Light Protocol 2.0* — <https://www.first.org/tlp/>.
- Related ADRs: [0162](./0162-leeway-co-ragged-function-pack.md) (UDF lane),
  [0174](./0174-play-sql-vocabulary-panel.md) (vocabulary panel),
  [0181](./0181-leeway-dql-authoring-surface.md) (DQL surface, the window).
