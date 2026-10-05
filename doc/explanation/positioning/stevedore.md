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

# stevedore — positioning

Positions the host around an ingestion processor that a streaming framework
drives as an external process: the framed contract, the failure classes,
identity, and landing. The Kafka reader and writer beside it are a clause
here. The table shape dead letters and landed rows go into is positioned by
[recordstore](./recordstore.md).

## Short form

For an application team that owns an ingestion handler and runs it under a
streaming framework, and needs the framing, failure handling, identity and
landing around that handler to be the same in every pipeline, stevedore is
the host for framework-driven processors[^processor]: one library that
speaks the framework's process contract, classifies failures, derives
identity from the request's origin, and lands items in ClickHouse after the
flush.

Unlike each processor carrying its own framing, retry and landing code, or a
standalone service that re-implements what the framework already provides,
stevedore keeps the code that is the same in every pipeline in one place,
testable without a broker. The cost is at-least-once delivery, so every sink
must key by reference and part, and a body bounded by one reply.

## Full form

**For** an application team that owns an ingestion handler and runs it under
a streaming framework, and needs routing, failure handling, identity and
landing around that handler to be the same in every pipeline and to survive
a crash mid-batch,

**stevedore is** the host for framework-driven processors[^processor]: a
library speaking the framework's external-process contract, a lander that
commits to ClickHouse after the flush, dead letters as rows in the facts
table[^facts], and a Kafka reader and writer ported without the framework
they came from,

**that** puts the code that is the same in every pipeline in one tested
library: failure is a class the host acts on, a redelivered request yields a
byte-identical reply, and landing is idempotent by reference, part and
ordinal,

**unlike** each processor carrying its own framing, retry and landing code,
or a standalone consumer service that re-implements the consume,
acknowledge, back-off and dead-letter machinery the framework already has,

**stevedore** depends on no framework's code and runs the same handler under
a framework, in process, or in a test. The cost is at-least-once delivery, a
body bounded by one reply, and a failure class that is a convention the
framework does not enforce.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | an application team owning the handler under a framework | [ADR-0252](../../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md) Context ("what an application team wants to own"); the how-to's audience ([stevedore-processors](../../howto/stevedore-processors.md)) |
| is a | the framework's external-process contract, owned as a library | [ADR-0252](../../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md) Decision |
| is a | a lander that commits after the flush; dead letters as facts rows | [ADR-0252 §SD3, §SD5](../../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md) |
| is a | an in-process runner (`drive`) | [ADR-0252](../../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md), the `drive` Update |
| is a | a Kafka reader and writer ported without their framework | [ADR-0005](../../adr/0005-streaming-persisted-kafka-from-connect.md) |
| that | failure as a class prefix the host acts on | [ADR-0252 §SD3](../../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md) |
| that | identity from the request's origin; byte-identical replies on redelivery | [ADR-0252 §SD4](../../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md) and its Update |
| that | at-least-once, idempotent by reference, part, ordinal | [ADR-0252 §SD5](../../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md); the lander does not reassemble (Update) |
| unlike | per-processor framing and landing code | [ADR-0252](../../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md) Alternatives O4 ("a chunk protocol without a consumer and framing conventions that differ per binary") |
| unlike | a standalone consumer service | [ADR-0252](../../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md) Alternatives O1 (re-implements what the framework provides; a second operational model) |
| stevedore | depends on no framework's module | [ADR-0252](../../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md) (importing a framework's helpers "contradicts portability"); [ADR-0005](../../adr/0005-streaming-persisted-kafka-from-connect.md) Alternatives O4 (the framework as a dependency) |
| trade | at-least-once; a body bounded by one reply; the class is a convention | [ADR-0252](../../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md) Consequences |

## Boundary

- The frameworks themselves are hosts, not foils: stevedore runs *under*
  them. Nothing in the record rejects a connector framework as such.
- Deferred, and absent from the clauses: a fetcher service for bodies the
  framework cannot hold in one reply; decoders and a leeway shredder inside
  the library; landing bytes into [lading](./lading.md) through a per-file
  writer; multi-frame replies.
- **Proposed, not verified:** the end-to-end run under a real framework
  binary and the crash-mid-batch integration test are in the accepted plan
  and unrun ([stevedore-processors](../../howto/stevedore-processors.md) says
  so). The "survives a crash mid-batch" clause in the For slot rests on the
  design and its unit tests, not on that test.

## Further reading

- [why-boxer](../why-boxer.md) — the premises the clauses compress.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [stevedore-processors](../../howto/stevedore-processors.md) — running a processor and landing its items.
- Decisions: [ADR-0252](../../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md),
  [ADR-0005](../../adr/0005-streaming-persisted-kafka-from-connect.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/streaming/stevedore

[^processor]: The program a streaming framework starts to transform each message; stevedore is the part of that program that is not the transformation.
[^facts]: `boxer.facts`: the one ClickHouse table shape every durable record lands in, so any record can be joined with any other in SQL.
