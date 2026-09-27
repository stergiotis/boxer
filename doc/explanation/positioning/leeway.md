---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> Where this page and an ADR disagree, the ADR is the record. The
> product-level statement is [positioning-statement](../positioning-statement.md);
> this page applies the same template to one subsystem. The set and its
> review checklist are in the [folder README](./README.md).

# leeway — positioning

Positions the data model and the generators around it: describe a schema
once, and the ClickHouse DDL, the ingestion path, the Go readers, the bus
and wire codecs and the SQL read functions are projections of that
description. Persisting records with it is positioned by
[recordstore](./recordstore.md); the identifier scheme its tags use by
[identity](./identity.md).

## Short form

For a SQL practitioner who reads semi-structured data in ClickHouse — values
under several tags at once, ragged arrays, notes attached to single values —
and wants a table layout plain SQL can read without a lookup, leeway is a
data-mapping engine: describe the schema once, and the ClickHouse tables,
the ingestion path, the Go readers and writers and a versioned set of SQL
functions are all generated from it.

Unlike hand-written DDL, SQL and codecs for every kind of record, or a
schema-less JSON column whose structure is implied rather than declared,
leeway keeps one description as the only thing to maintain. The cost is a
vocabulary that exists nowhere else, a more involved write path, and
generators that fail the way compilers do: everywhere at once.

## Full form

**For** a SQL practitioner who reads semi-structured data in ClickHouse —
values under several tags at once, ragged arrays, notes attached to single
values — and wants a table layout plain SQL can read without a lookup, with
the Go readers and writers generated from the same description rather than
kept beside it,

**leeway is** a schema-on-write[^sow] data-mapping engine: a machine-
readable model of the data (plain values, tagged values, sections and
memberships[^membership]) from which ClickHouse tables, Arrow ingestion, Go
readers, a wire format and a versioned set of SQL functions are generated,

**that** makes memory, wire and storage three projections of one
description, stores a value once under several tags, and names physical
columns so a table explains itself,

**unlike** hand-written DDL, SQL and codecs per kind of record, or a schema-
less JSON column whose structure is implied rather than declared,

**leeway** treats the description as the thing you edit and the generated
code as diffable output. The cost is a vocabulary found nowhere else, a more
involved write path, long column names, and generators that fail the way
compilers do: everywhere at once.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | multi-membership, ragged tensors, value-grain annotation | the comparison in [leeway-vs-snowflake](../../skills/leeway-advanced/references/leeway-vs-snowflake.md) cited by [ADR-0022](../../adr/0022-leeway-lwq-flwor-query-language.md); [ADR-0066](../../adr/0066-leeway-dql-clickhouse-readback-generator.md) Context |
| For | a SQL practitioner; a layout plain SQL reads without a registry | the audiences of [ADR-0171](../../adr/0171-leeway-sql-read-surface.md), [ADR-0181](../../adr/0181-leeway-dql-authoring-surface.md), [ADR-0189](../../adr/0189-component-sql-authoring-surface.md); [leeway-sql-read-surface](../leeway-sql-read-surface.md) |
| For | writers and readers generated from the same description | [why-boxer](../why-boxer.md) P2 and P3 |
| is a | model and staged pipeline | [leeway EXPLANATION](../../../public/semistructured/leeway/EXPLANATION.md); [ADR-0066](../../adr/0066-leeway-dql-clickhouse-readback-generator.md), [ADR-0101](../../adr/0101-leeway-marshall-mixed-shape-sections.md) |
| is a | Arrow ingestion, CBOR wire, SoA as the pivot | [ADR-0089](../../adr/0089-rowdml-serialization-clickhouse-native-ingestion.md), [ADR-0042](../../adr/0042-keelson-leeway-codec-soa-generator.md), [ADR-0210](../../adr/0210-leeway-canonical-wire-generator.md) |
| is a | a versioned SQL read surface and function pack | [ADR-0171](../../adr/0171-leeway-sql-read-surface.md), [ADR-0162](../../adr/0162-leeway-co-ragged-function-pack.md), [ADR-0181](../../adr/0181-leeway-dql-authoring-surface.md), [ADR-0189](../../adr/0189-component-sql-authoring-surface.md) |
| that | every shape is a projection | [why-boxer](../why-boxer.md) P3 |
| that | one value under several tags | [ADR-0066](../../adr/0066-leeway-dql-clickhouse-readback-generator.md) Context; [ADR-0103](../../adr/0103-leeway-marshall-dynamic-membership-tuples.md), [ADR-0109](../../adr/0109-leeway-marshall-multi-membership-ref-tuples.md) |
| that | self-describing column names | [ADR-0182](../../adr/0182-leeway-aspects-v2-codec-and-vocabulary.md), [ADR-0226](../../adr/0226-leeway-schema-decode-views.md); [leeway-column-names](../leeway-column-names.md) |
| that | read ladder with a round-trip oracle | [ADR-0066](../../adr/0066-leeway-dql-clickhouse-readback-generator.md) |
| that | one read contract across three read paths | [ADR-0146](../../adr/0146-leeway-marshall-component-read-contract.md) |
| that | content hash invariant under aspects and width | [ADR-0201](../../adr/0201-leeway-canonical-record-form.md) |
| unlike | hand-written DDL, SQL and codecs per kind | [ADR-0066](../../adr/0066-leeway-dql-clickhouse-readback-generator.md) Alternatives; [ADR-0042](../../adr/0042-keelson-leeway-codec-soa-generator.md) Alternatives |
| unlike | a schema-less JSON or variant column | the comparison in [leeway-vs-snowflake](../../skills/leeway-advanced/references/leeway-vs-snowflake.md) ("schema is implicit, not contractual"); [ADR-0089](../../adr/0089-rowdml-serialization-clickhouse-native-ingestion.md) on row codecs that discard self-description |
| leeway | the description is the editing surface | [why-boxer](../why-boxer.md) P2 |
| trade | vocabulary, write path, column names, generator defect surface | [why-boxer](../why-boxer.md) P2 and P3 costs; [leeway EXPLANATION](../../../public/semistructured/leeway/EXPLANATION.md) trade-offs |

## Boundary

- **Proposed or withdrawn, and absent from the clauses:** the FLWOR query
  language ([ADR-0022](../../adr/0022-leeway-lwq-flwor-query-language.md),
  [ADR-0023](../../adr/0023-leeway-lwq-go-api.md), proposed, unbuilt); the
  CBOR RPC codec ([ADR-0010](../../adr/0010-leeway-cbor-rpc-codec.md),
  deferred); external data-contract standards
  ([ADR-0060](../../adr/0060-leeway-data-contracts-odcs.md), withdrawn) —
  "contract" in this page means the generated read and authoring
  contracts, not a published standard.
- **Engine neutrality is untested.** The read contract is back-end-neutral
  by design; the claim "has never been tested by moving anything"
  ([leeway-second-substrate trial](../../trials/leeway-second-substrate/README.md)).
  The category clause says ClickHouse.
- Stream read access is partial per the package explanation; materialized
  projections in the SQL function set are deferred.

## Further reading

- [why-boxer](../why-boxer.md) P2 and P3 — the premises this engine enacts.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [leeway-sql-read-surface](../leeway-sql-read-surface.md), [leeway-dql-contracts](../leeway-dql-contracts.md), [leeway-marshalling](../../howto/leeway-marshalling.md).
- Decisions: [ADR-0066](../../adr/0066-leeway-dql-clickhouse-readback-generator.md),
  [ADR-0089](../../adr/0089-rowdml-serialization-clickhouse-native-ingestion.md),
  [ADR-0101](../../adr/0101-leeway-marshall-mixed-shape-sections.md),
  [ADR-0146](../../adr/0146-leeway-marshall-component-read-contract.md),
  [ADR-0171](../../adr/0171-leeway-sql-read-surface.md),
  [ADR-0181](../../adr/0181-leeway-dql-authoring-surface.md),
  [ADR-0189](../../adr/0189-component-sql-authoring-surface.md),
  [ADR-0201](../../adr/0201-leeway-canonical-record-form.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/semistructured/leeway

[^sow]: Schema-on-write: the structure is declared before data is stored, so readers get typed columns instead of parsing at query time.
[^membership]: A membership is a tag a value carries; one value can carry several, which is what "multi-membership" means throughout the leeway docs.
