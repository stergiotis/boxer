---
type: explanation
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-10
---

> Analysis record behind
> [ADR-0182](../adr/0182-leeway-aspects-v2-codec-and-vocabulary.md): the
> measured case for replacing the base62(u64) aspect segments and for the
> timeless vocabulary pass. The decisions live in the ADR; this document
> keeps the measurements, the rejected options with their kill reasons, and
> the per-family rationale. Follows
> [the aspect-system review](./leeway-aspect-vocabularies-review.md).

# Leeway aspects v2 — segment encoding and vocabulary

## 1. Measured baseline

Method: extract quoted identifiers from the ten committed
`*ddl_clickhouse.out.sql` goldens (this repository and one external
adopter's), parse each with `HumanReadableNamingConvention.ParseColumn`,
take the three aspect segments per name, decode with the real coders.
807 leeway columns.

| segment kind | segments | non-empty | distinct sets | popcounts (n:count) | worst today |
| --- | --- | --- | --- | --- | --- |
| encoding hints | 807 | **804** | 14 | 1:230 2:389 3:84 4:101 | 3 chars |
| use aspects | 765 | 35 | 5 | 1:29 3:6 | **7 chars for one aspect** (`1d0DV72` = {Linking}) |
| value semantics | 807 | 151 | 13 | 1:52 2:20 3:31 4:48 | **10 chars for 3–4 aspects** (`dj3NSvbf4S`) |

Total aspect-segment cost, all three kinds:

| scheme | total chars | chars/column | worst segment |
| --- | --- | --- | --- |
| **current** — base62(u64 mask), empty = `"0"` | 4091 | 5.07 | 10 |
| current + empty = `""` | 2702 | 3.35 | 10 |
| **digit-list + empty = `""`** (chosen) | 2088 | 2.59 | 4 |
| group-pairs (5-bit chunks) | ~3228 | 4.00 | 6 |
| offset+mask | ~3104 | 3.85 | 11 |

Post-migration re-measurement (2026-08-10, same method, 812 columns):
**2094 chars, 2.58 chars/column, worst segment 4** — matching the
prediction within a percent.

Two facts dominate. **Empty sets are 34% of all aspect bytes** (1389 of
4091 chars are lone `"0"` markers — 730 use + 656 sem + 3 enc). And the
mask encoding prices a set by its **highest index**, not its content: one
aspect numbered 57 costs 10 chars, four aspects numbered 1–4 cost 1. Since
the enum is append-only, every future aspect lands at the top — the current
encoding taxes exactly the only evolution the wire format permits.

## 2. Representation proposals

- **R1 — empty segment is empty.** `""` becomes the valid empty set (the
  grammar already has empty positional segments). Kills 34% of aspect bytes
  and repairs the zero-value footgun: a default-constructed `AspectSet` is
  currently *invalid*, which is why call sites defensively write
  `EmptyAspectSet` everywhere.
- **R2 — digit-list (chosen, with R1 — ADR-0182 SD1).** A set is the ascending list
  of its aspect indices, one base62 char per index, shifted by one
  (index *i* → `alphabet[i+1]`), so the char `alphabet[0]` (`"0"`) never
  occurs in a v2 segment and any legacy `"0"` remains unambiguously the v1
  empty set. Index 60 = one char, {1,9,55,57} = 4 chars. Properties:
  - **Content-priced**: k aspects cost k chars, forever, regardless of
    numbering. Appending vocabulary no longer inflates existing sets.
  - **No 64 ceiling.** After the two reservations (`'0'` never occurs,
    top char is the escape) single chars cover indices 0–59; each escape
    prefix adds +60, chainable — defined now, needed only if a vocabulary
    outgrows 60. The `uint64` in-memory form can stay until the ceiling is
    actually approached; the wire stops being the thing that caps the enum.
  - **Canonical and locally checkable**: strictly ascending chars, one
    encoding per set, malformedness detectable per character. Unknown
    (too-new) aspects become *per-element* detectable instead of poisoning
    the whole set as the max-bit check does today.
  - **Absolute indices, not deltas.** Delta/gap coding was considered and
    rejected: within the single-char range the two are *identical in
    length* (every gap ≤ every index), and absolute digits decode
    per-character — `Contains` is a byte scan, an unknown or corrupted
    char damages one element instead of shifting every element after it,
    and one aspect is always the same char (greppable in raw DDL). Deltas
    only win past the single-char range on clustered sets, a regime the
    admission rule exists to prevent.
  - **Alphabet choice.** The legacy coder inherits `math/big`'s base-62
    digit order (`0-9a-zA-Z`), which is not ASCII-monotone. v2 uses the
    ASCII-sorted alphabet (`0-9A-Za-z`), so canonical form = strictly
    ascending bytes, checkable by memcmp (decided in ADR-0182 SD1).
  - **Human-decodable** against the enum table — one char, one aspect —
    which suits a naming convention that calls itself human-readable.
  - Cost honesty: on encoding hints (dense, low-numbered, on ~every
    column) digit-list is ~6% *worse* than the mask (1667 vs 1576 chars);
    it wins 2.6–3.5× on the sparse kinds and caps the worst case at 4 vs
    10. A per-kind hybrid would squeeze out the last 6% and is not worth
    two codecs.
- **R3 group-pairs / R4 offset+mask** — measured, beaten by R2 on this
  corpus (table above); kept for the record.
- **R5 — interning dictionary.** Only 14+5+13 distinct sets exist; a
  per-kind dictionary would encode any observed set in 1 char (~990 chars
  total). Rejected: the dictionary must live outside the name, so
  discovery stops being decidable from one name — the checking
  neighbourhood widens from a name to name-plus-registry, which is the
  slope this system deliberately stays off. The marginal ~1.1k chars over
  807 columns does not buy that.
- **R6 — move aspects out of names** (column comments / Arrow field
  metadata). Maximal name compaction, but `DiscoverTableFromColumnNames`
  is the self-description witness (ADR-0181 leans on it); names survive in
  result headers, logs and CSV where metadata does not. A premise change,
  not an encoding change — out of scope here.

## 3. Vocabulary pass — timeless, orthogonal, correct

Admission criterion adopted for the enums (the "no data fashion" rule,
ADR-0182 SD2):

> An aspect family is admissible when its meaning is anchored in
> mathematics, a long-lived open standard (Unicode, RFC, W3C), a practice
> that predates the current tooling generation, **or a format the engine
> itself commits to** (CBOR/JSON are leeway's own payload surface); and its
> domain is *closed* under that anchor (Stevens' four scales, Unicode's
> four normalization forms) or it is a genuinely independent boolean.
> Open-domain, technique- or product-shaped information — methodology tiers,
> library-specific transforms, architecture brands — is attribute-shaped
> data that belongs in canonical types, TableOptions or the catalog, not in
> flag slots.

Dispositions as decided (**bold = removed**; every removal had zero
writers):

**valueaspects (61 → 48 survivors, 59 with §3b additions):**

| family | disposition |
| --- | --- |
| scale-of-measurement ×4 | keep — Stevens, closed, adopter-written |
| **Feature\* ×8** | **remove** — scikit-learn-era feature-engineering vocabulary (`FeatureScalingRobust01` mirrors a library class); transformation lineage, not value semantics |
| **MachineLearningEmbedding** | **remove** — era-bound name for "vector value with an ML pedigree" |
| VectorValue | keep — mathematically timeless; with embeddings gone it is the home for vector-valued columns |
| **None** | **remove** — the empty set already says it; freeing bit 0 is what makes R2's `"0"`-reservation clean |
| lifespan ×5 | keep-with-definition — retention is timeless but five unanchored buckets invite writer disagreement; comments must state that boundaries are deployment-defined (validators cannot check them) |
| Json\*/Cbor\* ×8 | keep — engine-anchored per the criterion; the value-vs-encoding split is deliberate and documented |
| Url | keep — WHATWG-anchored, thirty years old |
| Id ×4 | keep — natural/surrogate/content-addressable are timeless; `DurableSuperNaturalKey`'s comment should cite its dimensional-modeling origin |
| TextUnicode ×8 | keep — Unicode-anchored, closed |
| Human/MachineReadable, Human/MachineGenerated | keep (writers exist); rename `AspectMachineGenerate` → `AspectMachineGenerated` — source-level only, wire-neutral, adopter migrates in lockstep |
| **BCD, ReflectedBinaryCode, TrinaryLogic** | **remove** — consumer-less retro value-encodings misfiled as semantics; timelessness is necessary, not sufficient; removal also keeps every index single-char |
| Graph ×3 | keep — graph theory |
| Anonymized | keep; *Pseudonymized*, a distinct legal-technical concept, enters via §3b Tier A |
| Mandatory/Optional | keep — semantic requiredness, adopter-written |
| EmulatedMembership ×4 | keep — EAV-transition path, adopter-written |

**useaspects (58 → 39 survivors, 47 with §3b additions):**

| family | disposition |
| --- | --- |
| **Indefinite** | **remove** — placeholder; empty set says it |
| governance nouns (Compliance, Risk, Privacy, Security, Authorization, Access, Audit, Quality, Policy, Ownership) | keep, with the unstated distinction written down: `Authorization` = grants and policy (who *may*); `Access` = access records (who *did*); `Audit` = examinations of controls (what was *checked*) |
| Provenance ×4 | keep — W3C PROV, closed |
| Lineage | keep, with the boundary defined: `Lineage` = artifact/column-level derivation topology between datasets; `Provenance*` = PROV-modeled record-level provenance |
| Classification / Taxonomy | keep both, defined: `Classification` = the section carries class labels *assigned to things*; `Taxonomy` = the section carries the classification *system itself* |
| Catalog, Unit, Spatial, Workflow, Linking, Testing, Device, Documentation, Collaboration, Interop, Evolution | keep — plain timeless nouns (fix `Evolution`'s string `"change-evolution"` → `"evolution"` while strings are still not user-facing) |
| Metrics, Log, Profile | keep — decades-old operational data kinds |
| **Observability** | **remove** — 2015+ umbrella over Metrics∪Log∪Profile(∪tracing); the trio covers it |
| **OrgUnit/Role/Process/Finance, BusinessAsset/Partner/Activity/Channel ×8** | **remove** — enterprise-architecture subject taxonomy; *what a section is about* is catalog data, not *what it is used for* — a category error on top of the fashion smell |
| **MiniDimension + SlowlyChangingDimension ×8** | **remove ×9, replaced** — one methodology's numbered technique catalogue goes; the timeless residue enters as the attribute-history family in §3b |
| QualityStaging/Core/Semantical | keep — the names are already methodology-neutral refinement stages; the *comments* get de-branded (define raw → cleansed → semantically-modeled on their own terms, fix the "medaillon" typo) so the definition no longer leans on an architecture brand |
| SectionMembershipsAllPrimary/Secondary | keep — consumer, ADR, checker: the exemplar |

**encodingaspects (24 → 23):** all keep (each maps to a real codec path or
native-type licence) except **None** (same reason as above).

## 3b. Additions

### Attribute-history family (useaspects, exclusive 1-of-3, replaces SCD ×9)

The timeless residue of the removed technique catalogue — what happens to a
prior value when a new one arrives:

| aspect | string | meaning | absorbs |
| --- | --- | --- | --- |
| `AspectHistoryRetained` | `history-retained` | changes append; prior values stay readable | SCD types 2, 4 |
| `AspectHistoryOverwritten` | `history-overwritten` | only the current value is kept | SCD type 1 |
| `AspectHistoryDual` | `history-dual` | a current view is maintained alongside retained history | SCD types 3, 5–7 |

With `AspectImmutable` admitted as a value aspect (Tier B below), the
family stays 1-of-3 and SCD type 0 maps to `Immutable` stamped on the
section's value columns — immutability is a property of the values, not a
history-treatment mode, and a fourth `HistoryImmutable` member would
duplicate it.

### Traffic Light Protocol family (useaspects, exclusive 1-of-5)

Dissemination marking per FIRST **TLP 2.0** (authoritative August 2022,
v1.0 deprecated; verified against <https://www.first.org/tlp/>, the
standard's maintainer — the CISA page describes the same label set):
`AspectTlpClear`, `AspectTlpGreen`, `AspectTlpAmber`,
`AspectTlpAmberStrict`, `AspectTlpRed` (`tlp-clear` … `tlp-red`), ordered
by increasing restriction. This is the principled successor to the
"confidentiality ×4" idea from old working notes. Orthogonal to the
existing governance nouns: TLP states *who may receive* the data,
`Privacy`/`Compliance` state what it is about. Version-honesty: TLP 2.0
renamed 1.0's WHITE to CLEAR — the family tracks a maintained standard, and
comments pin the version.

### What else was missing — Tiers A and B (both admitted)

**Tier A — demanded, or a consumer template already exists in-repo:**

| candidate | vocabulary | anchor | consumer evidence |
| --- | --- | --- | --- |
| `AspectIdReference` — value references another entity's key | value | referential integrity, Codd-era | the adopter's schema carries a literal `// TODO: Valueaspect for foreign key?`; schemaview relation edges and play join suggestions are natural readers. Completes the Id family, which today classifies only keys an entity *owns*, not references *out* |
| `AspectPseudonymized` — reversible de-identification, still personal data | value | data-protection law lineage (Convention 108, OECD guidelines — pre-tooling-generation) | pairs with the existing `Anonymized`; erasure/vault tooling and compliance filtering need the legal distinction |
| `AspectSecret` — credentials, keys, tokens | value | as old as passwords | the table widget's machine∧¬human hide rule is the exact template for a mask-if-secret rule; export/logging redaction follows |

**Tier B — standards-anchored; consumers follow:**

| candidate | vocabulary | anchor |
| --- | --- | --- |
| `AspectTransactionTime` / `AspectValidTime` | value | SQL:2011 system/application time; subsumes the streaming era's processing/event-time framing |
| `AspectImmutable` — never updated after first write | value | append-only storage; also SCD type 0's home if the history family stays 1-of-3 |
| `AspectSynthetic` — fixture/simulated data, not observations of the world | value | test fixtures and simulation, decades old; distinct from `MachineGenerated` (a machine-generated real timestamp is not synthetic); the sample-table generators are an obvious writer |
| `AspectSentinelMissing` — absence encoded in-band (−999, epoch zero) | value | the oldest data-quality hazard there is; a stats panel could warn |

### Epistemic origin family (valueaspects, exclusive 1-of-3)

How the value came to exist — the extensional/intensional distinction of
deductive databases, extended by the empirical third:

| aspect | string | meaning | anchor |
| --- | --- | --- | --- |
| `AspectMeasured` | `measured` | an observation of the world, captured by an instrument or observing process | measurement theory; W3C SOSA/SSN observations |
| `AspectAsserted` | `asserted` | declared by an agent as a claim or input (user entry, configuration, label) | extensional facts (EDB) in deductive databases |
| `AspectDerived` | `derived` | computed from other values | intensional/derived relations (IDB); PROV derivation |

Two guards, stated so the family cannot become the tarpit's first step:
**proximate origin wins** — the aspect describes the step that produced the
*stored* value, decidable per column, no chain-chasing (a corrected sensor
value stored post-computation is `derived`); and **no inference licence** —
`derived` asserts a mode, not the existence of a recoverable derivation;
derivation *edges* stay in provenance facts written by promotion, never in
the vocabulary. Orthogonal to Human/MachineGenerated, which names the agent
kind, not the mode: a user-typed name is HumanGenerated+Asserted, a sensor
reading MachineGenerated+Measured, a computed KPI MachineGenerated+Derived.

**Tier C — considered and not proposed (tarpit guard):**
cyclic/circular scales (circular statistics — wait for angular data);
censored/truncated observations (survival analysis — too specialized);
ISO 25012-style quality *dimensions* — those are measurements about data
and belong in the quality-probe program as facts, not in schema flags;
media/format typing beyond the engine-committed pair — open-domain,
attribute-shaped, belongs in canonical types or the catalog.

### Slot arithmetic

valueaspects: 61 −13 (Feature ×8, MLEmbedding, None, BCD/Gray/Trinary)
+11 (Tier A ×3, Tier B ×5, triad ×3) = **59**. useaspects: 58 −19
(Indefinite, Observability, org/business ×8, SCD+MiniDimension ×9) +8
(history ×3, TLP ×5) = **47**. encodingaspects: **23**. All indices inside
R2's single-char range of 60; the BCD/Gray/Trinary removal is what keeps
valueaspects under it.

## 4. Structure: declared families

The exclusivity knowledge currently lives in prose and one hand-coded
validator pair. Propose a small registry per package —
`Families = []Family{Name, Members, Exclusive}` — driving (a) a generic
per-set validator predicate (still local, still per-set: no relations, no
inference), (b) the enum's documentation of record, (c) later, DQL
authoring diagnostics. This is the checkable form of the review's family
tables.

## 4b. SQL-side consumers: aspect UDFs

The absolute-char property makes aspects decodable *in SQL*, directly from
physical names — `system.columns`, `DESCRIBE`, Arrow field names, anywhere
a name travels. This was impractical under base62(u64) (ClickHouse has no
base62 parser; decoding needs `arrayFold` gymnastics); under R2 it is
string arithmetic:

- `LW_ASPECT_SEG_SEM/ENC/USE(name)` — segment extraction:
  `splitByChar(':')` plus layout dispatch on part count (plain names have
  no use segment). **Generated** from the naming convention's position
  data, never hand-written — one source of truth with the Go parser.
- `LW_ASPECT_DECODE(seg)` — indices as `Array(UInt8)`: per char,
  `position(<alphabet>, c) − 2` (the −2 absorbs 1-based `position` and the
  `+1` shift). v0 may reject the escape char; `arrayFold` covers it when a
  vocabulary ever outgrows 60.
- `LW_ASPECT_HAS_SEM/ENC/USE(name, 'kebab-name')` — the payoff of
  absolute-not-delta: membership is one `position(seg, char) > 0`, with
  the name→char mapping a generated `transform` over the enum tables.
- `LW_ASPECT_NAMES_*(seg)` — decoded kebab names, the enum `String()`
  tables shipped server-side as `transform` arrays.

Install through the existing `LW_` UDF DDL mechanism (ADR-0162 lane;
`identsql.UdfDdlStatements` precedent); the ADR-0174 vocabulary panel's
server probe picks them up from `system.functions` unaided. The governance
payoff is a one-query, cluster-wide inventory with no Go in the loop:

```sql
SELECT database, table, name FROM system.columns
WHERE LW_ASPECT_HAS_USE(name, 'tlp-red')
   OR LW_ASPECT_HAS_SEM(name, 'secret')
```

This is also the missing *generic consumer*: the day the UDFs install,
every governance aspect (TLP, Secret, Pseudonymized, Anonymized, …)
becomes a queryable predicate, satisfying the admission rule wholesale
rather than aspect-by-aspect. Caveat: the UDFs assume a single-era
deployment — v1 and v2 non-empty segments are not reliably distinguishable
per segment, which is one more reason the migration re-creates old tables
rather than mixing eras.

## 5. Migration

The migration plan — one breaking window on the be47b3b4 playbook,
including the runtime facts-schema codegen lane (outside the gen-test
sweep) and the re-creation of old-era physical tables, the live runtime
facts database first among them — is owned by
[ADR-0182](../adr/0182-leeway-aspects-v2-codec-and-vocabulary.md)
(§Migration, milestones M0–M5), together with every decision this
document's analysis fed: R1+R2, the dispositions of §3, the additions of
§3b, the family registry, and the UDFs. The window closes when ADR-0181
ships the DQL surface.
