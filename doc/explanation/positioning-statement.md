---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative. Where this page and [why-boxer](./why-boxer.md) or an ADR
> disagree, those are the record.

# Positioning statement

What boxer is for, in one paragraph — and the longer form that paragraph is
cut from. The form is Geoffrey Moore's positioning template: *for* a target
customer *who* has a need, *the product* is a category *that* delivers a key
benefit; *unlike* the primary alternative, *the product* differs in one
primary way. The short form leads the README's [Why](../../README.md#why)
section. This page carries the full six-slot form, the premise or decision
each clause rests on, the alternative the statement positions against, and
the words it deliberately does not use.

## Short form

For a small but ambitious data team ready to bet on the compounding of a
composable stack it owns — and needing air-gapped operability and
auditability from source — boxer is a sovereign, data-centric
data-engineering toolkit and end-to-end app stack over ClickHouse (the query
engine and database), written in Go with a Rust-rendered UI.

Rather than assembling best-of-breed parts with fragmented models and logs,
boxer lets you generate tables, codecs, ingestion and readers directly from
small, problem-oriented languages, and turns a query into a table, chart,
map or board — or, in a markdown file, an app. Every query, error and app
state lands in the same data model, operable by people and agents alike —
trading ecosystem breadth and a stable API for a stack a small team can read
from source, end to end.

## Full form

**For** a small but ambitious data team that is ready to bet on the
compounding of a composable stack it owns for the long term, and needs
air-gapped operability and auditability from source,

**boxer is** a sovereign, data-centric data-engineering toolkit and
end-to-end app stack over ClickHouse (the query engine and database), written
in Go with a Rust-rendered UI,

**that** lets a few people carry the whole stack: small problem-oriented
languages generate the ClickHouse tables, Arrow codecs, ingestion path,
statically typed readers and per-kind SQL functions for extraction and
transformation; a SQL query becomes a table, chart, map or board on screen —
or, in a markdown file, an app; a new requirement costs a projection, not a
new data layer.

**Unlike** a stack assembled from best-of-breed parts, each with its own
model and its own logs,

**boxer** records everything it does — queries, errors, grants, launches, app
state — in the same data model, queryable and renderable by its own tools and
operable by people and by agents through the same machine-readable surfaces,
inside one memory-safe Go + Rust boundary with the engine beside it as a
separate self-hosted process — trading ecosystem breadth and a stable API for
a stack a small team can read from source, end to end.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | a small team ready to bet on the compounding of a composable stack it owns | [why-boxer](./why-boxer.md) "The bet, named" and "Who this is for" (shares P1, wants the compounding of P2–P5 rather than a parts bin); the R5RS lineage cited under P2 |
| For | air-gapped operability, auditability from source | P1; the airgapped bundle ([ADR-0095](../adr/0095-airgapped-build-bundle.md)); the license gate ([ADR-0004](../adr/0004-license-gate-cyclonedx.md)) |
| is a | sovereign, data-centric, over ClickHouse, Go with a Rust-rendered UI | "The bet, named" (sovereignty defined operationally); P3 (the data-centric wager); the standing commitments (ClickHouse as the query-execution engine and database; Go and Rust; egui over a framed FFI) |
| that | a few people carry the whole stack | P6, and the P2→P6 chain in "The bet, named" |
| that | problem-oriented languages generate tables, codecs, ingestion, readers, per-kind SQL functions | P2; the leeway generators ([ADR-0066](../adr/0066-leeway-dql-clickhouse-readback-generator.md), [ADR-0089](../adr/0089-rowdml-serialization-clickhouse-native-ingestion.md), [ADR-0100](../adr/0100-recordstore-generated-leeway-clickhouse-store.md), [ADR-0189](../adr/0189-component-sql-authoring-surface.md)) |
| that | a query becomes a table, chart, map or board; a markdown file becomes an app | the play query graph and its panels ([ADR-0097](../adr/0097-play-reactive-query-graph.md), [ADR-0122](../adr/0122-play-kanban-panel.md), [ADR-0129](../adr/0129-play-layered-graph-panel.md)); SQL-defined applets ([ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md), build-time in v1) |
| that | a requirement costs a projection, not a data layer | P3 (what it buys) |
| Unlike | best-of-breed parts, each with its own model and logs | why-boxer's opening and "Who this is for — and not"; P3's failure mode (a collection of independent engines) |
| boxer | records everything it does in the same data model | P4; errors ([ADR-0041](../adr/0041-rowmarshall-error-shredding.md)), query runs ([ADR-0115](../adr/0115-query-observability-data-plane-strategy.md)), grants and audit ([ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md)), launches ([ADR-0135](../adr/0135-app-launch-requests.md)), app state ([ADR-0148](../adr/0148-app-workingsets.md), including its data-centricity update) |
| boxer | operable by people and by agents through machine-readable surfaces | P7 (the agentic tier over P3's surfaces); the introspection tables ([ADR-0094](../adr/0094-keelson-introspection-tables.md)); the natural-language surfaces are proposed, not shipped ([ADR-0120](../adr/0120-play-natural-language-ask-panel.md), [ADR-0139](../adr/0139-semantic-layer-text2dsl.md)) |
| boxer | one memory-safe Go + Rust boundary, the engine beside it | the standing commitments in why-boxer |
| boxer | trading ecosystem breadth and a stable API | P1 and P6 costs; "What this costs you" |

## The alternative named

The "unlike" slot names what a team with the same requirements would
otherwise do. Three were considered:

- **Vendored best-of-breed composition** — mainstream parts pinned and
  audited in-tree, built offline, glued by hand. It is the primary
  alternative because it meets P1 and is cheaper; why-boxer concedes that
  vendoring is cheaper than rewriting. The statement therefore differentiates
  on compounding — parts that share one model and one record of what they do
  — not on sovereignty, which the alternative also delivers.
- **A single-language sovereign stack** (for example Rust-only, with the
  analytical engine linked in) — pulls even the engine inside the memory-safe
  boundary at the cost of a less boring host and a larger crate closure.
- **Buying accountability** from a vendor who carries product liability,
  self-hostable and source-escrowed — keeps liability offload but fails
  "outlive its vendors" without escrow. A team that would accept escrow is
  not the statement's customer.

## Words deliberately not used

- Quality adjectives — *elegant*, *robust*, *performant*, *bloat*,
  *ultimate*: the repository's writing rule is descriptive and humble.
- *Formally verifiable* — nothing formal exists in the tree; the honest word
  is machine-checked (fuzz and property tests, goldens, conformance suites,
  declared pass properties).
- *Fully columnar* — the data plane is columnar (structs of arrays in
  memory, Arrow over the wire, ClickHouse on disk); bus control messages are
  deliberately record-shaped
  ([ADR-0036](../adr/0036-runtime-buscodec.md)).
- *AI-assisted* as a customer trait — authoring assistance is the producer's
  development model (P6), not the consumer's need; what the statement claims
  is agentic *operation* through machine-readable surfaces (P7).
- *Sink* for ClickHouse — it is the query-execution engine and database.
- *Replaces* best-of-breed — for a team outside P1, why-boxer says the
  mainstream stack is the better answer; the statement names the
  alternative, it does not dismiss it.

## Further reading

- [why-boxer](./why-boxer.md) — the premises the statement compresses.
- [README](../../README.md) — the short form in place.
