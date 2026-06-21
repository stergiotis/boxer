---
type: adr
status: withdrawn
date: 2026-04-24
withdrawn-date: 2026-06-05
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: withdrawn (2026-06-05) — retracted before review, never implemented.** The data-contract publication direction described below was not pursued; see the [Status](#status) section for the rationale. Retained as a record of the option, per the append-only ADR convention. Do not implement.

# ADR-0060: Adopt ODCS v3.1.0 as Leeway's Data-Contract Target

## Context

Leeway is boxer's columnar protocol for semi-structured data ([`../skills/leeway-beginner/SKILLS.md`](../skills/leeway-beginner/SKILLS.md), [`../skills/leeway-advanced/SKILLS.md`](../skills/leeway-advanced/SKILLS.md)). It shreds domain documents into type-specific sections with an orthogonal membership graph, carries rich semantic metadata via aspect bitmasks, and ships self-describing physical column names that let a consumer reconstruct the full `TableDesc` from column names alone. The schema surface is programmatic today: `TableDescDto` (CBOR-serializable) is authoritative; generators emit Arrow, ClickHouse, and Go artifacts.

What Leeway does *not* carry is an explicit **data contract** layer. In the current ecosystem a data contract is a declarative, versioned, governance-grade interface between producers and consumers, carrying schema, ownership, SLAs, quality expectations, and compatibility rules. Catalog vendors (Collibra, Atlan, DataHub, Unity Catalog, Purview), quality tools (Soda, Great Expectations, Monte Carlo), and governance pipelines all hook on standardised contract formats; an Arrow/ClickHouse table with only a `TableDescDto` is invisible to them.

Two facts reshape the decision space as of 2026-04:

1. **The Data Contract Specification at datacontract.com has been officially deprecated in favour of the Open Data Contract Standard (ODCS).** Data Contract CLI will support DCS through end-of-2026 as a migration window and only ODCS thereafter. The DCS/ODCS split that existed for most of 2024–25 has collapsed into a single target.
2. **ODCS v3.1.0 (Linux Foundation, Bitol project, Apache 2.0, PayPal origin) landed** with relationships, logical temporal types, executable SLAs, stricter JSON Schema validation, and preserved `customProperties`. Zero breaking changes from v3.0.x.

Leeway has three structural features that interact with any contract standard:

1. **Plain value columns** (entity id, timestamp, routing, lifecycle, transaction, opaque) — conventional columns by design.
2. **Tagged value sections** — sparse, type-indexed containers of values carrying uniform column-wise aspects ([`../skills/leeway-advanced/SKILLS.md`](../skills/leeway-advanced/SKILLS.md)).
3. **Tagged attributes** — individual tag paths within a section, with memberships (5 kinds), high-card parameters, multi-membership aliasing, co-occurrence, and per-attribute value constraints. Graph-shaped; no standard contract format expresses this directly.

The physical naming convention (`tv:bool:lmvcard:lmvcard:u64:4gw:0:0:0::` and friends) already encodes schema in Base62-serialised column names — so schema discovery does not require an external registry. The lossless streaming JSON form is carried by `JsonCardEmitter` at [`../../public/semistructured/leeway/card/leeway_card_json.go`](../../public/semistructured/leeway/card/leeway_card_json.go); it is byte-deterministic (sorted co-groups, ordered sections/columns/tags) and a strict superset of native JSON. Reconstructed-document JSON (original `{"hostname": …, "metrics": {"cpu": …}}` shape) is not derivable in general — multi-membership, co-sections, sets-vs-arrays, and `value-card`-carried ragged tensors exceed what a JSON tree can express without loss.

Forces the decision must respect:

- **Descriptive mode.** A contract annotates an existing `TableDescDto`; it does not drive a fresh schema from scratch. ODCS/DCS tooling supports both modes, but descriptive is what matches Leeway's current Go-driven authoring.
- **Ecosystem gravity.** Commercial credibility for Leeway depends on playing well with the governance stack customers already run. "New, better data model" without a standards-compatible contract story is a procurement dead-end.
- **Go-first codebase.** Leeway is Go; ODCS tooling is Python-first (`datacontract-cli`). A Go-native ODCS surface does not exist publicly.
- **Three-level isomorphism.** Plain values and tagged-section columns align mechanically with ODCS fields; the attribute graph does not. The contract must separate these levels cleanly.
- **Lossy-projection honesty.** Any "reconstructed JSON for vanilla consumers" path is lossy; the contract must not claim what it cannot deliver.

The question this ADR settles: should Leeway adopt a standardised data-contract format, which one, and how does the graph-shaped attribute layer map into it?

## Design space (QOC)

**Question.** Which data-contract format — if any — should Leeway adopt as its canonical contract-publication target, given Leeway's three-level structure, its Go-first codebase, and the 2026 consolidation of the standards landscape around ODCS?

**Options.**

- **O1 — ODCS v3.1.0** _(chosen)_. Adopt Open Data Contract Standard v3.1.0 as the envelope. Plain values and tagged-section columns map to ODCS `schema.properties`; attribute-graph semantics go under `customProperties.x-leeway`. Card JSON is declared as a wire format via `servers`.
- **O2 — Data Contract Specification (datacontract.com)**. Target DCS instead of ODCS. Same three-level split; same extension mechanism.
- **O3 — Confluent Data Contracts**. Adopt Confluent's Schema Registry + CEL-rules model. Kafka-native, rules-centric.
- **O4 — Bespoke Leeway contract format**. Design a Leeway-native contract vocabulary, publish as an open spec, build tooling from scratch.
- **O5 — No standardised contract layer**. Status quo: `TableDescDto` is the only schema artifact; contracts are implicit in generated code.

**Criteria.**

- **C1 — Ecosystem alignment.** Does the format read directly into existing catalog / governance / quality tooling without per-customer custom integration?
- **C2 — Coverage of Leeway semantics.** How much of Leeway's three-level structure fits natively, versus requiring extension?
- **C3 — Extension discipline.** Is there a sanctioned extension mechanism so Leeway-specific additions are not a fork?
- **C4 — Durability / governance.** What is the standards-body backing, trajectory, and non-deprecation assurance?
- **C5 — Tooling availability.** What open-source tooling exists today? Is Go a first-class citizen?
- **C6 — Authoring ergonomics.** Is the format reviewable by humans and legible to LLMs and generators?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 ODCS v3.1 | O2 DCS | O3 Confluent | O4 Bespoke | O5 None |
|----|--------------|--------|--------------|------------|---------|
| C1 | ++           | +      | + (Kafka only) | −−       | −−      |
| C2 | +            | +      | −− (row-only)  | ++       | 0       |
| C3 | ++           | ++     | +              | n/a      | 0       |
| C4 | ++ (LF/Bitol)| −− (deprecated EOY-2026) | − (vendor)  | −− | −− |
| C5 | + (Python-first; Go gap) | + (same tool) | + (Kafka-Go)  | 0  | 0 |
| C6 | ++           | ++     | +              | 0        | 0       |

O1 strictly dominates O2 (O2 is end-of-lifed; the tooling maps to both), dominates O5 on every axis, and dominates O4 on all axes except raw coverage (O4's "++" on C2 is paid for by "−−" on C1, C4, and C6). O3 is complementary, not a substitute — it operates at the Kafka-topic layer and can be co-emitted alongside ODCS without conflict.

## Decision

We adopt **ODCS v3.1.0** as Leeway's canonical data-contract envelope, in descriptive mode, with the following committed shape:

1. **Three-level mechanical mapping.** Plain value columns render as ODCS fields directly; tagged-section physical columns (`val`, `valcard`, `lmv`, `mvhp`, cardinality columns) render as ODCS field groups carrying section-level use-aspects and per-column value-aspects; the tagged attribute graph (per-tag membership kinds, cardinalities, parameter domains, multi-membership, co-occurrence) renders under `customProperties.x-leeway`.
2. **Logical JSON Schema in the ODCS `schema` block**, derived from `TableDescDto`, describing the attribute-document view a consumer holds. Draft 2020-12 (the dialect ODCS v3.1.0 aligns with). Attribute presence, parameter patterns, cardinality bounds, and co-occurrence are expressed through standard JSON Schema constructs (`required`, `patternProperties`, `dependentRequired`, `$defs`) wherever possible; only residual graph-shaped concerns fall through to `x-leeway`.
3. **Card JSON as a declared wire format.** The existing `JsonCardEmitter` output is the lossless serialization; it appears in the ODCS `servers` block as `format: leeway-card-json` where a JSON transport is wanted. No reconstructed-document emitter is built or promised; card JSON is Leeway's canonical JSON serialization, and any JSON projection of general Leeway data retains card-like properties by necessity.
4. **Aspect-to-ODCS vocabulary mapping as a frozen artifact.** A single translation table (`valueaspects.AspectE` → ODCS field annotation; `useaspects.AspectE` → ODCS field annotation) drives both the plain-value and tagged-section-column passes. Encoding aspects stay out of the contract (materialization choices, not interface concerns).
5. **One-way derivation.** ODCS envelope is generated from `TableDescDto` + a small annotation file (ownership, SLA, attribute-scoped classifications). Reverse direction (ODCS → `TableDescDto`) is out of scope: Leeway's section assignment, membership-kind choice, and multi-membership semantics exceed ODCS's expressive reach.
6. **Quality check emission as SQL over self-describing columns.** Tier-1 (per-batch vectorised) and Tier-2 (warehouse pushdown) checks are generated from attribute-level constraints and emitted into ODCS's `quality` blocks; `datacontract-cli` or any ODCS-aware runner executes them. Leeway's naming convention makes this possible without a Leeway-native runtime.
7. **Confluent Data Contracts as complementary emission.** When a Leeway dataset is transported via Kafka streaming groups, the same generator emits a Confluent Data Contract form for the Kafka topics alongside the ODCS envelope. The two describe different layers (dataset vs. topic) and do not conflict.
8. **Generator location.** Initial implementation lives in a downstream consumer's staging tree next to the card emitter at `public/semistructured/leeway/`; upstreaming to `boxer/public/semistructured/leeway/` is a tracked follow-on.

### Subsidiary design decisions

- **SD1 — Three levels, two mechanical, one extension.** Levels 1 (plain values) and 2 (tagged-section columns) map to ODCS without human input beyond the aspect-translation table. Level 3 (attribute graph) requires authored annotations and partially falls into `x-leeway`; the parts that express naturally in JSON Schema (`required`, `patternProperties`, `dependentRequired`) are emitted there, *not* in the extension, to maximise standard-tool coverage.

- **SD2 — Descriptive, not authoritative.** `TableDescDto` remains the source of truth; the ODCS contract is derived. This matches how Leeway schemas are built today (Go `TableManipulator` fluent API). An authoritative mode (ODCS → `TableDescDto`) would require committing to a subset of ODCS that fits Leeway's expressivity; the descriptive choice preserves Leeway's full range.

- **SD3 — Extension namespace is `customProperties.x-leeway`.** ODCS sanctions `customProperties` for extensions; `x-`-prefixed keys are the cross-spec convention for non-standard fields. The extension is documented inside a downstream consumer (initially) as a Bitol-style addendum; upstreaming as a recognised extension to Bitol is a longer-horizon aspiration but not a commitment here.

- **SD4 — Card JSON is the canonical lossless JSON serialization; no reconstructed-document emitter.** Reconstructed JSON cannot in general represent multi-membership, set-vs-array distinction, co-section topology, or ragged-tensor `value-card` structure without loss. Rather than ship a lossy emitter that consumers would silently depend on, we commit to card JSON as the JSON form Leeway exposes. Consumers who want "nice JSON" either read the opaque / data-mart plain columns via SQL or accept the card form.

- **SD5 — Two JSON Schemas, scoped clearly.** The ODCS `schema` block carries the **logical** JSON Schema (attribute-document view, the shape a query result naturally has); a separate **card-JSON JSON Schema** describes the wire form and is emitted as a companion artifact for CI/wire validation. Conflating the two would lie to standard tooling about the shape it will see.

- **SD6 — Aspect mapping is versioned and lives with Leeway.** The `Aspect → ODCS vocabulary` table is a first-class maintained artifact; when new aspects land or ODCS adds logical types the table updates. Encoding aspects (`AspectLightGeneralCompression`, `AspectDeltaEncoding`, etc.) stay outside the contract entirely; they are materialisation knobs and would turn compression changes into breaking contract changes.

- **SD7 — PII / classification is attribute-scoped, not column-scoped.** `/user/email` in the shared `string` section must be classified at the attribute level in `x-leeway` to avoid overclassifying the whole `string` section. Section-level use-aspects express column-wide semantics cleanly (e.g. `AspectHumanReadable` across a text section); attribute-scoped concerns (PII, per-tag retention) live only at Level 3.

- **SD8 — Version Leeway schema and ODCS contract independently.** The contract carries its own `version` per ODCS v3.1.0 conventions. `TableDescDto` has its own internal versioning cadence. Compatibility (when does a contract change, when does a schema change force a contract revision) is a follow-on ADR; this ADR commits to independent versioning only.

- **SD9 — Opaque / data-mart columns get the most mileage from the standard path.** Since opaque columns are explicitly conventional ([`../skills/leeway-advanced/SKILLS.md`](../skills/leeway-advanced/SKILLS.md) §2.1), they are the surface on which generic SQL/BI tooling operates. The ADR does not expand opaque-column policy, but flags that richer opaque projections of frequently-queried tagged attributes compound the value of ODCS alignment proportionally.

- **SD10 — Go tooling gap is real and tracked.** No first-class Go ODCS parser/validator exists publicly as of 2026-04. The initial implementation validates via the published ODCS v3.1.0 JSON Schema using a generic Go JSON-Schema library (`gojsonschema` / `jsonschema-go`); a typed Go ODCS model package is a follow-on and a candidate contribution to Bitol. A Python step in CI (via `datacontract-cli`) is acceptable in the interim.

- **SD11 — NDJSON mode for card JSON is a flagged follow-on.** The current `JsonCardEmitter` emits one top-level JSON array per batch. A line-delimited per-entity mode would make every streaming JSON validator trivially work; worth doing but not blocking.

- **SD12 — Card-JSON parser is a flagged follow-on.** Today's emitter is output-only. A parser (card JSON → `TableDescDto` + values) enables round-trip CI and a lossless archive format; not required for the ADR but named as a natural next artifact.

- **SD13 — Reversibility.** If ODCS v4 breaks compatibility materially, or if a future standard supersedes ODCS, migration is bounded: the envelope is regenerated from `TableDescDto` + the annotation file under the new standard. Because the contract is derived, not authored, the cost is in the generator, not in migrating authored contracts.

## Alternatives

Rejection rationale for the top-level options is in the QOC matrix; notes below capture detail not visible in the ratings.

- **O2 — Data Contract Specification (datacontract.com).** Officially deprecated in favour of ODCS v3.1.0, with Data Contract CLI support ending with the 2026 calendar year. Rejected purely on durability: targeting a year-old spec that its own maintainers are sunsetting would be self-inflicted migration work.

- **O3 — Confluent Data Contracts.** Kafka-scoped (Schema Registry + CEL rules), not a dataset-level governance contract. Rejected as a *replacement* for ODCS; accepted as a *complementary* emission for the Kafka layer per decision point 7. Teams with a Kafka-centric deployment will want both.

- **O4 — Bespoke Leeway contract format.** Gives full expressive freedom (no extension residue, native graph semantics, tight coupling to `TableDescDto`), but zero ecosystem payoff. No catalog, quality tool, or governance pipeline reads a bespoke format today; the investment is in building integrations Leeway does not otherwise need. Rejected because Leeway's commercial credibility depends on adopting, not inventing, the governance vocabulary.

- **O5 — No standardised contract layer.** Leaves Leeway legible only to Leeway-aware tooling. Rejected because the addressable market for a "better columnar semi-structured protocol" shrinks sharply when it cannot participate in standard governance pipelines. The cost of adoption (a generator) is small compared to the addressable market it unlocks.

- **JSON Schema alone (no ODCS envelope).** Considered as a minimalist alternative: emit a JSON Schema per Leeway table and stop. Rejected because JSON Schema has no ownership, SLA, classification, or quality-rule vocabulary; catalog and governance tools expect a contract envelope, not a bare schema. The ODCS envelope is the payoff.

## Consequences

### Positive

- **Ecosystem alignment by construction.** Every ODCS-aware catalog, quality tool, lineage system, and governance pipeline reads Leeway contracts on day one, without per-customer custom integration.
- **"ODCS-compliant by construction" is defensible.** Levels 1 and 2 derive mechanically; the extension is the minimum residue that Leeway's graph semantics force. A standards-minded reviewer can verify this directly against the published v3.1.0 JSON Schema.
- **Descriptive mode means zero existing-schema disruption.** No Leeway table needs to change; contracts are an additive layer derived from `TableDescDto`.
- **Self-describing column names pay off twice.** They already let Leeway-aware tools recover schema without a registry; they now let the contract generator emit quality SQL without handholding a Leeway runtime into the check executor.
- **Card JSON gains a strategic role.** Previously a debugging/streamreadaccess reference implementation; now a declared `servers` format and the lossless JSON serialization of record.
- **Clear extension boundary.** `customProperties.x-leeway` documents exactly what Leeway adds beyond ODCS. The extension is small because JSON Schema absorbs a meaningful chunk of attribute-graph semantics.
- **Graduated adoption path.** Customers can start with plain-value + tagged-section-column contracts (the fully ODCS-compatible slice) and grow into the `x-leeway` extension when needed. Procurement and governance review passes are easier because the envelope is standard.
- **Confluent contracts co-emit cleanly.** Kafka-native shops get the contract they expect for topic evolution without abandoning the dataset-level ODCS view.

### Negative

- **Go tooling gap.** The Python-first ODCS ecosystem means CI either runs a Python step via `datacontract-cli` or the team builds Go-native typed models + validators. Both have cost. Mitigation path exists (contribute a Go ODCS library to Bitol) but is itself an investment.
- **Card JSON is not consumer-friendly JSON.** SD4 accepts this. Teams expecting `{"hostname": …, "metrics": {"cpu": …}}` shape will find the card layout unfamiliar; the answer is "that's the lossless form; SQL on opaque/data-mart columns is the vanilla-consumer path."
- **Aspect mapping freeze is new ongoing work.** Every new aspect (in `valueaspects`, `useaspects`, or upstream ODCS logical types) requires a table update and a contract-generator revision. Expected cadence is low; still non-zero.
- **ODCS v3.1.0 is early-majority.** Minor-version churn is likely even with the "zero breaking changes from v3.0.x" commitment; pinned version output is mandatory for reproducibility.
- **"Relationships" (new in 3.1.0) has uneven downstream tooling support.** Catalog ingest for relationship edges is maturing unevenly across vendors; rich relationship usage may be ahead of what some consumers can render.
- **The generator and card emitter live in a downstream consumer's staging today.** Upstreaming to boxer is a commitment-gated follow-on; the strategic promise ("Leeway is ODCS-compliant") eventually requires boxer to carry the capability, not just a downstream consumer.

### Neutral

- **Quality-check runtime is delegated to the ODCS tooling.** `datacontract-cli` + its Soda/Great-Expectations backends execute the emitted SQL. Leeway does not ship a check runner of its own; this is right-sized given SD10.
- **The contract artifact is authored per table, not per section.** One `.odcs.yaml` file per Leeway table is the granularity; streaming groups show up as multiple `servers` entries under one contract, not as separate files.
- **Registry is not required.** The self-describing naming convention plus the derived ODCS envelope together remove the need for an external schema registry for Leeway's own operation. Customers who run a registry for other reasons can ingest Leeway contracts into it, but there is no dependency.
- **Opaque columns become commercially load-bearing.** ([`../skills/leeway-advanced/SKILLS.md`](../skills/leeway-advanced/SKILLS.md) §2.1 already treats them as the BI surface.) ODCS alignment elevates them from "nice for BI" to "strategic adoption lever"; this ADR does not change opaque-column policy but notes the shift.

### Derived practices

- **New Leeway tables ship alongside a generated ODCS contract.** Once the generator lands, a table without a generated contract is a CI gap, not a design choice.
- **PII / classification on Leeway attributes is annotated at Level 3.** Never at the physical column level, to avoid overclassifying entire sections.
- **Encoding-aspect changes never force contract revisions.** Compression and encoding knobs live outside the contract by SD6; changing them is a materialisation concern.
- **Breaking ODCS changes trigger a generator revision, not a repository-wide contract migration.** Contracts are derived; they regenerate.

## Open questions

Tracked as named follow-ons, not gates on this ADR:

1. **Aspect-to-ODCS vocabulary mapping table.** Its own follow-up ADR freezing the table; includes decisions on aspects with no ODCS analogue (`AspectSparse`, `AspectReflectedBinaryCode`, the emulated-membership aspects).
2. **Logical JSON Schema derivation rules.** How polymorphism across sections encodes (likely `oneOf` with shape bounds), how high-card parameters translate to `patternProperties`, how `value-card` bounds express as array constraints.
3. **Streaming-group → ODCS `servers` mapping.** One server per group, or one server with a group selector parameter.
4. **Opaque-column lineage.** Whether derivation from tagged attributes is declared in `x-leeway.derivedFrom` for drift detection.
5. **Version compatibility policy.** Formal rules for when a Leeway schema change forces an ODCS contract version bump (covered by SD8 "independent" but needs fleshing out).
6. **Go-native ODCS library.** Whether built inside boxer, contributed to Bitol, or deferred behind the `datacontract-cli` Python step.
7. **Upstreaming card emitter and generator to boxer.** Scheduling and scope; the strategic "Leeway is ODCS-compliant" pitch is bounded in credibility until this lands.
8. **NDJSON mode** (SD11) and **card-JSON parser** (SD12) as individually small but separately scoped deliverables.

## Status

**Withdrawn 2026-06-05** — retracted before human review; never implemented.

The data-contract publication direction described above was not pursued. The schema-derivation, read-back, and query-generation needs that motivated it are being addressed directly by the `marshallgen`, `marshallreflect`, `mappingplan`, and `dql` line of work; the external-governance-interop layer (the ODCS envelope, catalog/quality-tool emission) is out of scope for now. No code was written against this ADR — the generator it described never left the design stage.

This file is kept per the append-only ADR convention as a record of the option and why it was set aside. The cross-references that other ADRs and skills docs once carried to it have been removed, so this ADR now stands alone. Should a standards-based data-contract layer be revisited, this is the starting point.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX) | Withdrawn`.
ADRs are append-only; withdrawal is recorded, not deleted.

## References

- [Bitol — Linux Foundation AI & Data project hosting ODCS](https://bitol.io/)
- [ODCS GitHub repository (bitol-io/open-data-contract-standard)](https://github.com/bitol-io/open-data-contract-standard)
- [ODCS v3.1.0 specification](https://bitol-io.github.io/open-data-contract-standard/v3.1.0/)
- [ODCS v3.1.0 release announcement — "Stronger, Smarter, and Stricter"](https://bitol.io/bitol-announces-odcs-v3-1-0-stronger-smarter-and-stricter/)
- [ODCS v3.1.0 deep-dive on relationships and richer metadata](https://dataintelligenceplatform.substack.com/p/odcs-v310-is-here-relationships-richer)
- [ODCS v3.1.0 JSON Schema (authoritative validator artifact)](https://github.com/bitol-io/open-data-contract-standard/blob/main/schema/odcs-json-schema-v3.1.0.json)
- [Data Contract CLI (open-source; Python; covers ODCS + DCS)](http://cli.datacontract.com/)
- [Data Contract CLI on GitHub](https://github.com/datacontract/datacontract-cli)
- [Data Contract Specification (datacontract.com) — deprecated in favour of ODCS](https://datacontract-specification.com/)
- [PayPal's original data contract template (pre-Bitol ancestor of ODCS)](https://github.com/paypal/data-contract-template)
- [Confluent Data Contracts (Schema Registry)](https://docs.confluent.io/platform/current/schema-registry/fundamentals/data-contracts.html)
- [`../skills/leeway-beginner/SKILLS.md`](../skills/leeway-beginner/SKILLS.md) — Leeway overview
- [`../skills/leeway-advanced/SKILLS.md`](../skills/leeway-advanced/SKILLS.md) — Leeway structural semantics, membership types, aspects
- [`../skills/leeway-streamreadaccess/SKILLS.md`](../skills/leeway-streamreadaccess/SKILLS.md) — `SinkI` protocol, card-JSON emitter
- [`../skills/canonicaltypes/SKILL.md`](../skills/canonicaltypes/SKILL.md) — canonical type signatures
- [`../../public/semistructured/leeway/card/leeway_card_json.go`](../../public/semistructured/leeway/card/leeway_card_json.go) — current `JsonCardEmitter` location
