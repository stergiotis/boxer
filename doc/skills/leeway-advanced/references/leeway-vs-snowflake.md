---
type: explanation
audience: developer evaluating Leeway
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Leeway vs Snowflake VARIANT vs ClickHouse JSON v2 — Explanation

This document compares three architectures for storing and querying large volumes of semi-structured data with high schema variety: **Snowflake** with the VARIANT type (managed-cloud auto-shred), **ClickHouse JSON v2** (self-hosted auto-shred), and **ClickHouse + Leeway** (self-hosted explicit modelling), where Leeway is the columnar encoding protocol described in [SKILLS.md](../SKILLS.md). It is written for a developer evaluating Leeway against the available alternatives for a real workload — typically one where data volume is large enough that storage layout matters, schema variety is broad enough that a fixed relational schema is unacceptable, and the team has both the capacity to invest in modelling and the appetite for self-hosted infrastructure. The document does not advocate one over the others; it lays out what each can express, what each costs to operate, and when each wins, so the reader can match capabilities to their workload.

## Background

The semi-structured data problem sits at the intersection of three pressures: **variety** (many shapes that evolve over time — new fields, type drift, sparse long-tail attributes), **volume** (data is large enough that storage layout, scan patterns, and per-query cost matter), and **queryability** (analysts must filter, aggregate, join, and project with reasonable latency, ideally without rewriting the data per query shape).

Three architectures dominate the market:

**Snowflake VARIANT** is a managed-cloud schema-on-read approach. JSON ingests as VARIANT; the storage layer transparently shreds frequently-accessed paths into sub-columns; queries access fields via `data:field.subfield::TYPE`. Schema evolution is essentially free — you add fields to incoming JSON without DDL changes. Snowflake is delivered as a managed cloud service.

**ClickHouse JSON v2** — GA in CH 24.x as the production replacement for the experimental `Object('json')` — is the open-source self-hosted peer of VARIANT. JSON ingests into a `JSON` column; the storage layer auto-extracts hot subcolumns with statistics; queries access fields via dot notation (`col.field.subfield`) or `JSONExtract*` functions. Schema evolution is automatic, bounded by the `max_dynamic_paths` and `max_dynamic_types` parameters.

**ClickHouse + Leeway** addresses the problem via explicit canonical-types modelling. Data decomposes into *tagged value sections* keyed by canonical type (one section per `int64`, `string`, `float64array`, etc.), with multi-membership tagging, semantic aspect annotations, and parameterised paths. The Leeway SDK generates ClickHouse DDL, ingestion bindings, Apache Arrow targets, and read-access APIs from a single canonical-types definition. The system is self-hosted.

These map to two independent axes:

|  | Schema-on-read auto-shred | Schema-on-write modelling |
|---|---|---|
| **Managed cloud** | Snowflake VARIANT | (no peer) |
| **Self-hosted columnar** | ClickHouse JSON v2 | ClickHouse + Leeway |

The vertical axis (managed vs self-hosted) is a deployment and economics choice; the horizontal axis (auto-shred vs modelling) is a design choice about who owns the shape-management abstraction. The rest of this document focuses on the horizontal axis — what auto-shred and modelling each can express — because that is the choice Leeway forces. Where VARIANT and CH JSON v2 differ from each other (operational, ecosystem, maturity), it shows up in §"Operational and cost dimensions."

## How it works

### Snowflake VARIANT pipeline

1. JSON arrives via Snowpipe / `COPY INTO` into a column typed `VARIANT`.
2. The storage layer transparently shreds frequently-accessed paths into shredded sub-columns; per-path statistics are maintained automatically.
3. Path queries (`data:metrics.cpu::FLOAT`) push down to shredded columns where possible and fall back to VARIANT scan otherwise.
4. Pruning and zone maps work on shredded paths. Search optimization service can be enabled to accelerate point lookups.
5. Schema evolution is implicit — new paths appear and become queryable without DDL.

### ClickHouse JSON v2 pipeline

1. JSON arrives via standard CH ingestion (HTTP, Kafka engine, native protocol, `clickhouse-client`) into a column typed `JSON` or `JSON(...)` with hints (`SKIP path`, `max_dynamic_paths`, `max_dynamic_types`, type pinning per path).
2. The storage layer auto-extracts hot subcolumns at write time; sparse paths land in a shared dynamic store. Per-subcolumn statistics and indexes are maintained.
3. Path queries (`col.metrics.cpu`) push down to subcolumns where the path has been promoted; sparse paths fall back to a dynamic-typed scan via `dynamicElement` / `dynamicType`.
4. Schema evolution is implicit — new paths appear in the type's metadata automatically, bounded by `max_dynamic_paths` (default 1024). Paths beyond the limit remain queryable via the shared dynamic store but lose subcolumn-level pruning.
5. The same ClickHouse cluster serves both JSON-typed and natively-typed columns; mixing approaches in one schema (some columns JSON, others Leeway sections, others stock CH types) is supported.

### ClickHouse + Leeway pipeline

1. Define canonical types and aspect specifications in code (Go SDK, fluid API).
2. Run Leeway codegen → produces ClickHouse DDL, typed Go ingestion API, typed Go read-access API, and Apache Arrow bindings.
3. Ingest data through the typed API; values land in canonical-type sections with explicit multi-membership encoding (see SKILLS.md §"Example: Mapping JSON to a equivalent Leeway representation").
4. Query via standard ClickHouse SQL on the generated tables, with full visibility of section layout and physical column naming convention.
5. Schema evolution: extend canonical types or memberships, regenerate, apply migration. Leeway's nominal schema comparison and physical-column naming convention let the schema be reconstructed from any subset that preserves section boundaries.

The architectural crux is **who owns the shape-management abstraction**. Snowflake hides it behind VARIANT and the auto-shredder; Leeway makes it explicit, code-governed, and reflected in physical column names.

## What each system can express

This section walks through capability classes, with worked examples for the structural differentiators. The single-attribute scalar case (`{"hostname": "server-alpha"}`) is trivial in all three systems and is omitted.

Throughout this section, **"auto-shred"** refers to the Snowflake VARIANT and CH JSON v2 class collectively, because their expressivity ceilings are identical — both flatten semi-structured data into a path/value tree and rely on auto-extracted subcolumns. Where they differ is operational, not expressive; those differences live in the next section.

### Schema variety and sparse fields

Auto-shred (VARIANT or JSON v2) handles arbitrary new fields with no DDL change — VARIANT without a built-in cap, JSON v2 bounded by `max_dynamic_paths` (paths beyond the limit remain queryable but lose subcolumn pruning). Queries that don't reference new fields ignore them. Leeway requires the value's canonical type to exist in the schema. Most field additions are membership additions (a new path landing in an already-defined section) and need no schema change. New canonical types (first time the data contains, say, a `float64array`) require regenerating the schema and migrating. **Auto-shred is more convenient for unstructured evolution; Leeway is preferable when type contracts matter** — downstream pipelines that fail if a field's type silently changes from `int64` to `string`.

### Multi-membership values

The single most structural differentiator. Consider an e-commerce price `19.99` that simultaneously serves as `/price/current`, `/stats/min`, and `/promo/flash_sale`.

**Auto-shred (VARIANT, JSON v2)** must denormalise (duplicate the value into three distinct paths in the document) or normalise (separate edge table joining product → relation → value). Denormalisation triples storage and update cost; normalisation requires a JOIN at read time. Neither VARIANT nor JSON v2 has a primitive for "one value, multiple memberships."

**Leeway** stores `19.99` once in the `float64` section with `membership-card: 3` and three entries in `low-card-memberships`. No duplication, no JOIN. From the [SKILLS.md "Multi-Membership" Case](../SKILLS.md):

```json
"float64": {
  "low-card-memberships": ["/price/current", "/stats/min", "/promo/flash_sale"],
  "values": [19.99],
  "value-card": [1],
  "membership-card": [3]
}
```

For datasets where multi-role values are common — catalog data, knowledge graphs, multi-aspect identity — this is structurally cheaper in Leeway. For datasets without such values, this capability is unused but costs nothing.

### Polymorphic fields

A heterogeneous array such as `events: [100, "error", 101, [1.1, 1.2]]` mixes types within a single logical column.

**Auto-shred**: VARIANT stores the array intact and tracks element types; querying "all integer events" requires `WHERE TYPEOF(value) = 'INTEGER'`, which doesn't prune at the storage layer — the engine scans the full array. JSON v2 behaves equivalently with `dynamicType` filtering: type information is preserved per value but per-type pruning is absent.

**Leeway**: each element type lives in its own canonical-type section. Indices 0 and 2 land in the `int64` section; index 1 in `symbol`; index 3 in `float64array`. Querying "all integer events" scans only the `int64` section. Original order is reconstructable via `high-card-parameters` when needed.

For analytics queries that filter or aggregate by type, Leeway's per-type scan is dramatically faster — and the cost difference grows with array length and type sparsity.

### Ragged tensors and variable-width arrays

Workloads with vector-per-row data of varying length — embeddings, layer weights, time-series windows — fit awkwardly in standard columnar formats.

**Auto-shred**: VARIANT or JSON v2 arrays; iteration is a scan with no native tensor operations. Both can store the data; neither offers efficient ragged-tensor access patterns.

**Leeway**: `value-card` chunks a flat value stream into logical units of varying size. The `float64array` section stores `[0.1, 0.5, 0.9, 1.0, 0.0, 0.0, 1.0]` as one column with `value-card: [3, 2, 2]`. Per-element predicates run vectorised; logical-unit boundaries are an O(1) lookup.

For ML and embedded-vector workloads, this is a Leeway advantage that auto-shred cannot replicate without lossy reshape into either a fixed-width column (loses ragged data) or a separate index column (rebuilds Leeway's `value-card` by hand).

### Annotation overlays at value grain

A common organisational pattern: layering PII tags, ML labels, governance flags, or quality scores onto existing data without rewriting it. Multiple teams want to bolt on metadata without touching the primary data shape.

**Auto-shred**: Snowflake's tag library applies tags to *columns*; masking policies govern row access. CH JSON v2 has no value-grain tagging primitive — column-level metadata exists but does not align to per-value memberships. In both, per-value tagging requires bolt-on columns or a separate join table, coupling annotations to primary data.

**Leeway**: co-sections share row count with a primary section but may have only memberships, no values. The PII team or governance team adds a co-section without touching the primary; vertical subsetting drops or keeps the secondary co-group whole. From SKILLS.md:

```go
manip.TaggedValueSection("null__labels").
    CoOf("null").
    AddSectionMembership(common.MembershipSpecLowCardVerbatim).
    AddSectionUseAspect(useaspects.AspectSectionMembershipsAllSecondary)
```

For organisations where multiple teams want to layer metadata onto shared data without coordinating writes, Leeway has a clean primitive that VARIANT does not match.

### Semantic and encoding aspects

Leeway exposes ~60 *value aspects* (scale-of-measurement: nominal/ordinal/interval/ratio; feature scaling: min-max/standard/robust; Unicode normalisation forms NFC/NFD/NFKC/NFKD; ID classifications: natural/surrogate/super-natural/content-addressable; lifespan tiers; graph topology vertex/edge/hyperedge; machine-vs-human-generated; encryption/compression flags) and ~24 *encoding aspects* (delta encoding, slowly-changing-float, intra/inter-record low cardinality, compression weight classes, sparse). Tooling reads these to make decisions automatically: an `AspectFeatureScalingMinMax01` column flows directly into an ML pipeline; an `AspectScaleOfMeasurementOrdinal` constrains which statistical operations apply; `AspectIdContentAddressableKey` informs deduplication.

Auto-shred storage (Snowflake VARIANT, CH JSON v2) provides column-level data types but no structured aspect system describing measurement scale, scaling intent, normalisation form, or ID classification. Snowflake's tag library and CH's column comments / metadata can model some governance flags but neither matches the ~60-aspect Leeway value-aspect surface, and neither propagates aspect information into the storage layer for tooling pickup. Equivalent metadata can be modelled in an external catalog (Atlan, DataHub) but is not first-class to either auto-shred storage and does not propagate into automated downstream tooling without integration work.

For organisations building automated data tooling — feature engineering, statistical correctness checks, governance pipelines — on top of the data, Leeway's aspect system is a force multiplier. For organisations that don't build such tooling, the aspect system is overhead.

### Streaming groups and row-based transport

**Auto-shred**: Snowflake streams via Snowpipe Streaming or classic Snowpipe; CH JSON v2 streams via Kafka engine, native protocol, or HTTP ingestion. In both, payloads are JSON-shaped with no semantic structure beyond path layout — the row-based transport boundary is decided by the application, not the storage layer.

**Leeway**: streaming groups declare which sections must travel together for row-based transport (Kafka, gRPC), preserving section co-alignment on the columnar side. Subsetting at section boundaries is an explicit, lossless operation. For event-streaming architectures that span row-based transport and columnar storage, Leeway has an explicit primitive; the auto-shred approaches delegate the boundary to the application.

## Operational and cost dimensions

This section is where the three architectures genuinely diverge. The capability section above mostly collapses VARIANT and JSON v2 into "auto-shred" because their expressivity is identical; here, the differences between managed-cloud auto-shred (Snowflake), self-hosted auto-shred (CH JSON v2), and self-hosted modelled (CH + Leeway) become load-bearing.

### Time to first useful query

Snowflake: hours. Create account, load JSON, query. **CH JSON v2: ~1–2 days** — deploy CH cluster, define a table with a `JSON` column, ingest, query. CH + Leeway: days to weeks — define canonical types, generate DDL, set up CH cluster, ingest via the typed API, query. **Snowflake wins for cloud-first ramp; CH JSON v2 is the fastest self-hosted starting point; Leeway is the slowest to first query but the only one that produces a code-governed schema.**

### Total cost of ownership at sustained TB-PB workloads

Snowflake cost = warehouse-hours + storage. A Large warehouse running continuously is on the order of $260k/year for compute alone at on-demand rates; storage is modest; search optimization service is extra; cost scales linearly with usage.

CH JSON v2 cost = hardware (capex/cloud) + storage + ops headcount. A right-sized CH cluster is roughly $50–150k/year amortised plus ops engineering (~$200k+/year FTE). No additional Leeway-specific dev cost. **This is the cheapest of the three on raw infra**, since you avoid both Snowflake's per-query billing and Leeway's modelling investment.

CH + Leeway cost = same hardware as JSON v2 + Leeway engineering investment (modelling, codegen maintenance, schema evolution discipline). The structural compression of Leeway (no value duplication for multi-membership values; per-type scans for polymorphic data) compounds at scale and offsets the engineering cost when the data has the right shape.

**Bottom line on TCO: CH JSON v2 is cheapest pure-infra; Leeway adds engineering cost that pays off in expressivity and downstream-tooling automation; Snowflake is most expensive at sustained high volume.** The crossover where Snowflake wins on TCO is at low-to-moderate workloads where engineer time is the binding constraint.

### Concurrency

Snowflake: excellent. Multi-warehouse isolation lets thousands of concurrent users coexist without interference. CH JSON v2 and CH + Leeway both inherit ClickHouse's manual concurrency story (replicas, load balancing). High concurrency is achievable on CH but operationally heavier than Snowflake. **Snowflake wins for highly-concurrent multi-tenant workloads, regardless of which CH approach you pick on the self-hosted side.**

### Lock-in and portability

Snowflake: substantial lock-in. Queries don't trivially port to OSS engines; data egress costs are real; cloud-only deployment. CH JSON v2: minimal lock-in to CH, but the auto-shredded subcolumn layout is implementation-dependent — exporting to other engines means re-flattening to JSON or Parquet. CH + Leeway: minimal lock-in. CH is open source and runs anywhere; Leeway-encoded data is portable as Parquet (via the Arrow target) or as raw CH MergeTree dumps; schemas are code, version-controlled, and reproducible.

**Both CH approaches win clearly over Snowflake on portability. Leeway has a slight edge over CH JSON v2 because explicit schemas survive engine migrations more cleanly than implementation-dependent subcolumn layouts.**

### Schema evolution

Snowflake VARIANT: trivial — new fields appear, queries opt in via path access, nothing to migrate. CH JSON v2: trivial within `max_dynamic_paths`; beyond the limit, paths still work but lose subcolumn benefits. CH + Leeway: structured — new memberships in existing sections are free; new canonical types require codegen + migration. **Snowflake wins for unstructured evolution. Leeway's structured evolution is preferable when type contracts matter** (downstream pipelines that fail if a field's type silently changes).

### Built-in features

Snowflake provides time travel, secure data sharing, granular RBAC, search optimization, masking policies, replication, and a mature feature surface. The two ClickHouse options share what CH itself provides — RBAC, row-level policies, replication, projections, materialised views. Time travel and secure data sharing are not built-in to CH; cross-team data sharing relies on CH's privilege model. Many features are buildable but not turnkey. **Snowflake wins on enterprise-governance feature surface; the JSON v2 vs Leeway choice does not affect this dimension.**

### Tooling and BI integration

Snowflake has first-class connectors across the BI ecosystem (Tableau, Looker, Hex, dbt). Both CH options share the CH ecosystem — growing but smaller; most BI tools have CH connectors of varying maturity. JSON v2 columns may need slightly more massaging in BI tools that expect strongly-typed columns; Leeway's generated DDL produces typed CH columns that BI tools see naturally. **Snowflake wins on BI/tooling integration today; among CH options, Leeway is marginally friendlier to typed BI consumers.**

## Decision criteria

A consolidated table for matching priorities to architecture:

| If your priority is | Pick |
|---|---|
| Time-to-first-query, small team, cloud-first | Snowflake VARIANT |
| Time-to-first-query, self-hosted preferred, modelling capacity scarce | **CH JSON v2** |
| Concurrency at thousands-of-users scale | Snowflake |
| Built-in time travel, secure data sharing, regulated-workload features | Snowflake |
| BI tool ecosystem maturity | Snowflake |
| Truly schemaless data you don't model, cloud OK | Snowflake VARIANT |
| Truly schemaless data you don't model, self-hosted preferred | **CH JSON v2** |
| Multi-membership values (graph-edge-like attributes) | CH + Leeway |
| Polymorphic columns with per-type analytics | CH + Leeway |
| Ragged tensors, variable-width vectors | CH + Leeway |
| Value-grain annotation overlays (PII, ML labels, governance) | CH + Leeway |
| Automated tooling driven by semantic aspects | CH + Leeway |
| Streaming-architecture awareness (explicit row-transport boundaries) | CH + Leeway |
| Lowest TCO at moderate variety, no expressivity needs | **CH JSON v2** |
| Lowest TCO at sustained high-volume workloads with structurally rich data | CH + Leeway |
| Portability and lock-in avoidance | Either CH option (slight edge to Leeway) |
| Code-governed schema with type contracts | CH + Leeway |

Rules of thumb:

1. **Auto-shred (Snowflake VARIANT or CH JSON v2) wins** when your data is fundamentally unmodelled key-value pairs and modelling capacity is scarce. Pick Snowflake if cloud-first and operational simplicity dominates; pick JSON v2 if self-hosted, cost-sensitive, or already running ClickHouse.
2. **Leeway wins** when your data has structure beyond "JSON with named fields" — multi-role values, polymorphic types, ragged dimensions, semantic aspects worth modelling — and the engineering investment is justified by volume × query-complexity.
3. **The CH JSON v2 → CH + Leeway path is a natural progression**: start with JSON v2 for rapid iteration on a single CH cluster; migrate hot or structurally-rich sections to Leeway when expressivity ceilings start to bite. Mixing JSON-typed and Leeway-typed columns in the same CH schema is supported and routine.

## Trade-offs

The honest costs of choosing each architecture.

### Choosing Leeway

- **Modelling discipline required.** Canonical types, sections, memberships, aspects all must be defined. The model becomes the contract; getting it wrong propagates downstream.
- **Engineering investment in the encoding layer.** The SDK does the heavy lifting, but using it well requires understanding the protocol. New team members face a learning curve auto-shred approaches do not impose.
- **Operational burden of self-hosted ClickHouse.** Cluster sizing, replication, backup, upgrade, monitoring — costs Snowflake users do not incur, though shared with the JSON v2 path.
- **Smaller ecosystem.** Fewer pre-built connectors, fewer "just works" integrations, smaller hiring pool of experienced engineers.
- **Schema evolution requires codegen.** Faster than ad-hoc DDL changes once the workflow is established, but slower than auto-shred's "just add a field."

### Choosing ClickHouse JSON v2

- **Schema is implicit, not contractual.** Auto-shredding decides what becomes a subcolumn; that decision is opaque and version-dependent. Downstream tooling cannot rely on stable column-level guarantees the way it can with Leeway-generated DDL.
- **Bounded by `max_dynamic_paths`.** Paths beyond the limit (default 1024) lose subcolumn benefits and degrade to dynamic-typed scans. High-cardinality path explosions force tuning.
- **Newer than VARIANT.** GA in CH 24.x; less battle-tested than Snowflake's VARIANT. Edge cases, performance regressions, and feature gaps are still being discovered. Tied to CH version cadence — major changes between minor releases happen.
- **No expressivity gain over VARIANT.** Multi-membership, polymorphic-by-type, ragged tensors, and value-grain annotations all hit the same auto-shred ceiling.
- **Inherits CH operational burden** without amortising it across the Leeway protocol — you self-host but get the auto-shred abstraction instead of the modelled one.

### Choosing Snowflake

- **Cost compounds at scale.** Warehouse-hours billing is convenient at low volume but punitive at sustained high throughput.
- **Vendor lock-in.** Queries don't port to OSS without rewriting; data egress costs deter switching; multi-cloud strategy is constrained.
- **Expressivity ceiling.** Multi-membership, polymorphic-by-type, ragged tensors, and value-grain annotations require lossy encoding in VARIANT.
- **Black-box performance.** When a query is slow, levers are clustering keys, search optimization, materialised views — all opaque relative to Leeway's explicit section layout or even CH JSON v2's tunable subcolumn hints.
- **No on-prem path.** Cloud-only deployment may be unacceptable for regulated workloads.

These trade-offs are structural, not implementation details. They follow from the schema-on-read versus schema-on-write split (Leeway vs auto-shred) crossed with the managed versus self-hosted split (Snowflake vs CH), and would persist under any reasonable re-implementation of any of the three.

## Further reading

- The Leeway protocol: [`doc/skills/leeway-advanced/SKILLS.md`](../SKILLS.md)
- Boxer Leeway package source — `boxer/public/semistructured/leeway/` (resolve via `bash scripts/boxer-path.sh`)
- Membership role classifier: boxer ADR-0007 (`$(boxer-path)/doc/adr/0007-leeway-membership-role-classifier.md`) and pebble2impl [ADR-0017](../../../adr/0017-leeway-membership-role-classifier.md)
- Card-JSON canonical format: pebble2impl [ADR-0018](../../../adr/0018-leeway-card-json-canonical-format.md)
- Snowflake VARIANT documentation: https://docs.snowflake.com/
- ClickHouse documentation: https://clickhouse.com/docs/ (see SQL reference → data types → JSON for the v2 type and its `max_dynamic_paths` / `max_dynamic_types` parameters)
