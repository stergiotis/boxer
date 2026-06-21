---
type: adr
status: proposed
date: 2026-05-08
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0022: leeway lwq — FLWOR-style query language for Leeway-stored data

## Context

A downstream consumer (not in this repo) uses ClickHouse + Leeway for semi-structured data (per ADR-0018 and the Leeway protocol at `boxer/public/semistructured/leeway/`). Leeway decomposes documents into typed canonical-type sections with multi-membership tagging, parameterised paths, semantic aspects, and co-section overlays. The structural advantages and capability comparison against Snowflake VARIANT and ClickHouse JSON v2 are documented in [`doc/skills/leeway-advanced/references/leeway-vs-snowflake.md`](../skills/leeway-advanced/references/leeway-vs-snowflake.md).

The query-time consequence of Leeway's storage decision is that idiomatic queries against Leeway data require:

- multi-section JOINs on `entity_id` plus `high_card_parameters` to recover per-attribute associations;
- `has(low_card_memberships, '...')` predicates in place of path operators;
- `groupArray(toJSONString(tuple(...)))` patterns to reconstruct nested tree shapes;
- UNION ALL across sections for polymorphic-type or aspect-driven queries;
- explicit JOINs to co-sections for value-grain annotations.

Leeway's Go SDK read-access codegen hides this in typed Go accessors, but several real consumer paths cannot use those accessors:

1. **BI tools and dashboards** consume CH SQL directly and face the verbose hand-written form.
2. **External API consumers** of dashboard or service payloads need tree-shaped (JSON) output, which currently requires manual `groupArray` + `tuple` + `toJSONString` assembly.
3. **Snowflake-VARIANT migrants** (a documented audience per the Snowflake comparison) lose the path-syntax ergonomics they were used to.
4. **Aspect-driven queries** (vector search via `AspectMachineLearningEmbedding`, governance via `AspectAnonymized`, ML feature pipelines) have no idiomatic syntactic expression in CH SQL.
5. **Multi-membership** and **co-section overlay** queries — capabilities unique to Leeway — have no concise syntax even for Leeway experts.

A previously-discussed path-lowering nanopass closes the basic gap by letting queries write `lvar_path('/items/_/qty')` and having it rewritten to section JOINs at compile time. That covers flat relational queries with path access, but does not give a natural form for tree-shaped construction (nested object/array output), aspect-driven section selection, multi-membership predicates, or co-section overlays. The widening question is whether to push further into a query language designed for the Leeway substrate, or to keep the nanopass narrow and accept the ergonomic ceiling.

## Design space (QOC)

**Question.** How do we provide an ergonomic query interface for Leeway-stored data that exposes the protocol's structural capabilities (multi-membership, polymorphic-by-type sections, ragged tensors, co-section overlays, semantic aspects, tree-shaped construction)?

**Options.**

- **O1** — Status quo: hand-written CH SQL on Leeway-generated tables, with the existing typed Go read-access codegen serving in-process consumers.
- **O2** — Path-lowering nanopass only: extend the `lvar_path` / `lvar_flatten` pseudo-function family to cover JSON construction, multi-membership predicates, aspect-driven dispatch, and co-section joins, while staying inside CH grammar.
- **O3** — `lwq`: a FLWOR-style query language with its own grammar, AST, and lowerer to CH SQL AST. Reuses the boxer CH DSL infrastructure (nanopass framework, AST, ToSQL emitter) and the Leeway schema env.

**Criteria.**

- **C1** — Ergonomics for tree-shaped output (nested JSON construction, the defining FLWOR strength).
- **C2** — Reuse of existing boxer infrastructure (CH AST, nanopass, Leeway env, codegen).
- **C3** — Surface area and ongoing maintenance burden.
- **C4** — Snowflake-VARIANT migration ergonomics.
- **C5** — First-class visibility for Leeway-unique constructs (multi-membership, aspects, co-sections).
- **C6** — Compatibility with existing CH workflows and BI tooling.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (status quo) | O2 (nanopass-only) | O3 (`lwq`) |
|----|-----------------|---------------------|------------|
| C1 | −−              | −                   | ++         |
| C2 | ++              | ++                  | +          |
| C3 | ++              | +                   | −          |
| C4 | −               | +                   | ++         |
| C5 | −               | +                   | ++         |
| C6 | ++              | ++                  | −          |

O2 dominates O1 on every criterion and covers a meaningful slice of the user-visible work at a fraction of the cost. O3 is the only option that handles tree-shaped construction natively (C1) and the only one with strong support for Leeway-unique constructs as first-class syntax (C5). O3 trades surface area (C3) and direct BI-tool authoring compatibility (C6) for those gains.

Three observations are decisive:

1. The path-lowering nanopass (O2) is **strictly subsumed** by `lwq`'s lowerer — the same path-resolution and section-JOIN machinery is needed in both. Building O2 first delays O3 only by the work that's reusable; it is not a wasted investment, but it also does not eliminate O3's main cost.
2. Tree-shaped construction (C1) is **not addressable** in an in-CH-grammar pseudo-function approach without devolving into nested-function-call patterns that become an ad-hoc query language of their own. If tree-shaped output matters, O3 is the cleaner answer.
3. BI-tool compatibility (C6) is a genuine concern, but mitigated because `lwq` compiles *to* CH SQL — a BI tool consumes the output of `lwq` even if it cannot author `lwq` directly. Saved-view-and-then-consume is the pragmatic bridge.

## Decision

We will build **`lwq`** (Leeway-Wide Query), a FLWOR-style query language for Leeway-stored data, compiled to ClickHouse SQL via the existing boxer CH DSL infrastructure. The package will live at `boxer/public/db/leeway/lwq/` as a peer of `boxer/public/db/clickhouse/dsl/`.

The path-lowering nanopass (O2) is preserved as a v0 deliverable embedded within `lwq`'s lowering pipeline — `lvar_path` / `lvar_flatten` pseudo-functions remain valid and useful for SQL-shaped consumers, while `lwq`'s grammar sits above them as the FLWOR-shaped front end.

Implementation is phased:

- **v0 (prototype)** — parser for the core FLWOR clauses (`for`, `where`, `return`), implicit scope nesting from path prefixes, flat relational output. Co-delivers the SQL-embedded `lvar_*` family.
- **v1 (useful)** — `let`, `order by`, inner FLWOR for aggregation, JSON construction emitter, multi-membership predicates (`with roles [...]`).
- **v2 (powerful)** — co-section joins, aspect-driven section selection (`with aspect ...`), polymorphic-path disambiguation, cross-grain aggregation correctness.
- **v3 (extensible)** — user-defined functions, multi-target backends (Apache Arrow, DuckDB), CBOR construction, recursive queries.

The decision recorded by this ADR is to start, scoped through v1. v2 and v3 are trajectory, not commitment; each transition requires its own go/no-go review against then-current usage signal.

Indicative scope (not a delivery commitment): on the order of a few thousand lines of net-new code through v1, growing further if v2/v3 are pursued, by analogy with the existing CH DSL infrastructure (~6k LOC). Wall-time depends on staffing and is intentionally omitted from this ADR — sizing belongs in the implementation plan, not the architectural decision.

The compilation pipeline is:

```
lwq source
  → ANTLR4 parser            → lwq AST
  → resolver (Leeway env)    → annotated lwq AST
  → scope analyser           → annotated lwq AST + scope tree
  → lowerer                  → CH SQL AST (existing boxer/public/db/clickhouse/dsl/ast)
  → construction emitter     → relation | JSON | CBOR shape
  → existing CH ToSQL        → CH SQL text
```

Stages 5–7 reuse existing boxer infrastructure unchanged. Net-new code is the parser, AST, resolver, scope analyser, lowerer, and construction emitter.

## Leeway speciality coverage

Beyond the syntactic ergonomics gap, `lwq` is justified by the set of Leeway capabilities that have no idiomatic expression in CH SQL — and in several cases no representation at all in alternative storage models (Snowflake VARIANT, CH JSON v2). The language deliberately surfaces these as first-class constructs.

**Multi-membership.** A single value can bear N memberships (the SKILLS.md example: `19.99` simultaneously playing `/price/current`, `/stats/min`, `/promo/flash_sale` with `membership-card: 3`). The language handles this in three orthogonal modes:

- *Filtering by role-set* — `for $v with roles ['/price/current', '/stats/min']` selects values playing all listed roles. Compiles to conjunction of `has(low_card_memberships, ...)` predicates.
- *Filtering by cardinality* — `where membership_card($v) >= 2` selects values playing multiple roles regardless of which. Compiles to a predicate on the section's `mvhp_card` column (or the equivalent under whichever MembershipSpec is in play).
- *Reading the role-set* — `$v.roles` returns the membership array as a sequence; usable in `return` constructors (`return { roles: $v.roles }`) and in further predicates.

The membership-role classifier from boxer ADR-0007 splits memberships into **primary** (defining the attribute) and **secondary** (annotating it). `lwq` honours this distinction: `with primary roles [...]` filters only on primary memberships, `with secondary roles [...]` only on secondary, and the unqualified `with roles [...]` operates on the union. Section uniformity hints (`AspectSectionMembershipsAllPrimary` and the `Secondary` peer) drive short-circuit dispatch in the lowerer so uniform-role sections skip the per-membership classifier call.

**Co-value.** Sections that share a parameter scope are co — their attributes belong together at the corresponding scope grain. `lwq` exposes co-value semantics in two complementary forms:

- *Implicit* — scope-nested `for` clauses (`for $i in $order/items/_`) automatically join across all sections that contain paths under the extended scope. The lowerer emits `high_card_parameters` equality predicates between sibling sections without the user writing them. This is the dominant idiom and what most users will write.
- *Explicit* — `join $label in cosection($v, 'string__pii_labels')` joins a primary section with a named co-section, sharing parameter scope and `entity_id`. Used for annotation overlays and multi-representation patterns. The explicit form is required when the co-section name is not prefix-derivable from the primary path.

**Annotation-only co-sections.** Co-sections with memberships but no value columns. The language treats them as value-grain tag overlays: `with secondary roles` over a `cosection` join produces filtering on per-value annotations without touching the primary section's value column. This is the syntactic form of the "secondary co-section for annotation overlays" pattern documented in the [Leeway skill](../skills/leeway-advanced/SKILLS.md).

**Multi-representation co-sections.** A geographic dataset can carry `(latitude, longitude)` in one co-section and an `h3` index in another, sharing memberships. `lwq`'s explicit `cosection(...)` form addresses each by name; aspect-driven selection (`with aspect AspectGeographicH3`) lets queries pick the representation matching the analytical need without hard-coding section names.

**Aliasing in canonical JSON construction.** Per ADR-0007, the canonical JSON layout for Leeway data is attribute-centric: when one value plays multiple primary roles, the JSON output collapses into one attribute object with an `aliases` field rather than emitting the value once per role. The construction emitter respects this — `output canonical-json` produces aliasing-aware output (one object, `aliases` array); `output json` produces straightforward role-keyed output (one entry per role). The choice is per-query.

**Membership type taxonomy.** Sections vary in MembershipSpec (`HighCardRef`, `LowCardVerbatim`, `MixedLowCardVerbatimHighCardParameters`, and the others enumerated in SKILLS.md); the actual columns present in each section depend on the spec. The lowerer dispatches per-spec when generating filter predicates and projections. This is not user-visible — `lwq` queries are spec-agnostic at the source level — but it shapes a meaningful chunk of the lowerer's section-handling code.

**Aspect-driven section selection.** Value aspects (the ~60 aspects enumerated in the Leeway skill — `AspectMachineLearningEmbedding`, `AspectFeatureScalingMinMax01`, `AspectIdContentAddressableKey`, `AspectAnonymized`, etc.) tag sections with semantic intent. `with aspect ...` filters bindings to sections carrying the aspect, enabling polymorphic dispatch: vector search across all embedding-bearing sections, governance queries across all anonymised-aspect-bearing sections, ML feature selection across all scaling-aspect-bearing sections. This is `lwq`'s mechanism for exposing Leeway's distinctive semantic-aspect surface to query authors without committing them to specific section names.

**Streaming groups.** The Leeway protocol declares streaming groups for row-based transport (Kafka, gRPC). The construction emitter respects these declarations: when a deferred output target (CBOR, v3) serializes within a streaming group, co-sections in the group are kept together and section-boundary subsetting remains lossless. v0/v1 do not address this directly but the design space leaves room.

The point of enumerating these is to record what specifically would be lost under O1 (status quo) or O2 (nanopass-only): each capability above either has no syntactic form in CH SQL or requires hand-rolled patterns that re-discover Leeway protocol details query-by-query. `lwq`'s value is not just terseness — it is the surfacing of capabilities the substrate has but the existing query layer hides.

## Alternatives

- **Status quo (O1).** Rejected: leaves five real consumer paths underserved (BI tools, external tree-shaped API consumers, Snowflake migrants, aspect-driven analytics, multi-membership and co-section users). The pain compounds as Leeway adoption widens.
- **Path-lowering nanopass alone (O2).** Rejected as a *terminal* solution but adopted as a v0 milestone within O3. By itself it cannot express tree-shaped construction without devolving into a private nested-function-call dialect that re-invents query-language ergonomics badly.
- **Adopt an existing XQuery engine (e.g. BaseX, Saxon) over a Leeway-to-XML export.** Rejected: round-trip cost is prohibitive (Leeway → XML → XQuery → results), the storage layout's structural advantages disappear in the export, and XQuery has no idiom for multi-membership or aspect-driven dispatch.
- **Snowflake-style SQL extensions inside the CH grammar.** Rejected: collides with the canonical-form discipline of the CH DSL's Grammar2, and CH's own roadmap (the JSON v2 type covered in the comparison doc) is the engine-side answer to that surface.
- **DataFusion SQL dialect as the query language.** Rejected: DataFusion has its own dialect; choosing it forces a backend commitment that conflicts with the "CH first, Arrow/DuckDB later" multi-target goal.
- **GraphQL as the query layer.** Rejected: different paradigm, does not compose with SQL pipelines, has no idiom for analytical aggregation.

## Consequences

### Positive

- Tree-shaped output (nested JSON for APIs, dashboards, agent payloads) becomes a one-pass query instead of hand-written `groupArray(toJSONString(tuple(...)))` assembly.
- A direct migration ramp from Snowflake VARIANT: equivalent expressivity with FLWOR-shaped syntax that is more compact than VARIANT path syntax for nested construction.
- Aspect-driven queries (vector search, governance, ML feature engineering) gain a clean syntactic primitive — `for $v in section(*) with aspect ...` — with no analog in CH SQL or VARIANT.
- Multi-membership predicates (`with roles [...]`) become first-class, exposing a Leeway capability that VARIANT and JSON v2 structurally cannot represent.
- Co-section overlay joins gain explicit syntax (`join $label in cosection(...)`), making the value-grain annotation pattern teachable instead of folkloric.
- The compiler is a natural site for query-time optimisations (materialised-view selection, projection use, aspect-aware pruning) that would otherwise be hand-applied per query.
- Composability with future Leeway-aware passes: time-travel AS OF rewriting, aspect-driven cluster-key suggestion, CDC-stream materialisation can all share the schema env and run on the lowered CH AST.

### Negative

- Substantial maintenance commitment — indicative scope is a few thousand lines of net-new code through v1, more if v2/v3 are pursued. The exact figures are deferred to the implementation plan.
- New language for users to learn; FLWOR is uncommon outside XQuery shops, and the Leeway-specific extensions (`with roles`, `with aspect`, `cosection`) require Leeway-protocol fluency beyond the language itself.
- Tooling (LSP, syntax highlighting, error messages, formatter) is its own investment, not covered by this ADR.
- BI tools that author SQL directly cannot author `lwq` natively. Mitigation: `lwq` queries can be saved as CH views, which BI tools then consume; the authoring story becomes "developer writes `lwq`, dashboard reads view."
- Documentation surface expands: a query language wants reference docs, tutorials, a corpus of worked examples, and a VARIANT/SQL migration guide.
- Risk of feature creep into v3 territory (recursive functions, multi-target backends) before v1 is hardened. Phased delivery with explicit go/no-go gates is the mitigation.

### Neutral

- The package boundary places `lwq` in boxer, not in a downstream consumer. That consumer will be the first consumer but will not own the implementation — the same boxer-owns / consumer-consumes split as the membership-role classifier (boxer ADR-0007).
- The decision to start with CH as the only backend (Arrow and DuckDB deferred to v3) commits the lowerer to CH semantics. Generalising later requires lifting the lowerer to a target-agnostic IR — a reasonable but non-trivial refactor that should be planned before v2 if multi-backend looks likely.
- The `lvar_*` pseudo-function family remains valid post-`lwq` v0; users who prefer SQL-embedded form continue with them, while users who prefer FLWOR-shape adopt `lwq`. Maintaining both is a small additional surface but a meaningful ergonomic choice.

## Status

Proposed — awaiting review by Leeway and CH DSL maintainers and a downstream architecture review.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- [Leeway protocol skill](../skills/leeway-advanced/SKILLS.md) — canonical types, sections, memberships, aspects, co-sections.
- [Leeway vs Snowflake VARIANT vs CH JSON v2 comparison](../skills/leeway-advanced/references/leeway-vs-snowflake.md) — the syntactic and structural gap this ADR addresses.
- [ADR-0007](0007-leeway-membership-role-classifier.md) — membership-role classifier; the basis for `with roles` semantics.
- [ADR-0018](0018-leeway-card-json-canonical-format.md) — card-JSON canonical format; a representative tree-shaped construction target.
- Boxer CH DSL EXPLANATION ([`../../public/db/clickhouse/dsl/EXPLANATION.md`](../../public/db/clickhouse/dsl/EXPLANATION.md)) — the parsing, AST, and nanopass infrastructure the lowerer reuses.
- W3C XQuery 3.1 (FLWOR semantics): https://www.w3.org/TR/xquery-31/
- MacLean, Bellotti, Young, Moran, "Questions, Options and Criteria: Elements of Design Space Analysis," *Human-Computer Interaction*, 1991 — the QOC notation used above.
