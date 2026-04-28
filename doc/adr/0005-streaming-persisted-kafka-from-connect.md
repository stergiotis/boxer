---
type: adr
status: accepted
date: 2026-04-27
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-28
---

# ADR-0005: Port Redpanda Connect's franz-go Kafka Input/Output as a Boxer-Style Derivative

> **Migration note.** Originally drafted as pebble2impl ADR-0015 on
> 2026-04-27. Renumbered and migrated into boxer on 2026-04-28 when the
> [`public/streaming/persisted/kafka/`](../../public/streaming/persisted/kafka)
> package moved here so it can be reused across boxer-derived projects.
> The decision content below is unchanged from the original pebble2impl
> framing; pebble2impl is now a downstream consumer.

## Context

A production-grade Kafka client surface is needed downstream of boxer (originally for pebble2impl). Re-implementing the Kafka consumer/producer state machine from scratch against [`github.com/twmb/franz-go`][franz-go] is non-trivial — rebalance handling, ordered ack-drain semantics, transaction isolation, and idempotent-producer write paths each have known traps that [Redpanda Connect][rpcn]'s `internal/impl/kafka/` package has already solved in production.

That package is Apache-2.0 licensed (header sample verified at the pinned SHA across `input_kafka_franz.go`, `input_sarama_kafka.go`, `input_redpanda.go`, `output_kafka_franz.go`, `franz_writer.go`, `franz_reader_unordered.go`). Adopting it directly couples us to [`github.com/redpanda-data/benthos/v4/public/service`][benthos-service] — a stream-processing framework that imposes its own message, ack, and resource abstractions. The framework dependency is the actual cost; the franz-go interaction logic is the value.

The Apache-2.0 obligations on a derivative work distribution are §4.a (provide license), §4.b (mark modified files), §4.c (retain attribution), §4.d (carry NOTICE). All four are satisfiable; the design question is *which derivation strategy* minimises ongoing engineering cost while honouring those obligations.

## Design space (QOC)

**Question.** How should boxer obtain a franz-go-based Kafka input and output that satisfies boxer coding standards while honouring upstream's Apache-2.0 license?

**Options.**

- **O1** — *Verbatim copy with Apache headers retained.* `cp -r` the Connect kafka files; keep upstream headers, add a modification stub, depend on the Benthos `service` package as a module import or vendor it.
- **O2** — *Derivative under Apache-2.0, refactored to boxer style, Benthos service framework dropped.* Re-shape the consumer/producer loops against package-local interfaces (`ConsumerI`, `ProducerI`, `BatchI`, `AckFnI`); keep Apache attribution per §4.b–d.
- **O3** — *Clean-room reimplementation against franz-go directly.* Read only public docs (Kafka protocol spec, kgo README, RPK reference); forbid agent or human access to Connect source during writing.
- **O4** — *Adopt Benthos as a module dependency.* Pull `github.com/redpanda-data/benthos/v4` into `go.mod`; consume Kafka via its plugin registry; live with the framework abstraction.

**Criteria.**

- **C1 — Engineering fit with boxer CODINGSTANDARDS.** Receiver naming, named returns, error builders, sized integers — the patterns enforced for new code.
- **C2 — Legal complexity / ongoing license obligations.** Per-file notice maintenance, NOTICE drift, transitive attribution.
- **C3 — Initial port effort.** Engineering hours to a first working consumer/producer including tests.
- **C4 — Dependency footprint.** Transitive modules pulled in, blast radius of upstream changes.
- **C5 — Maintenance / upstream-drift cost.** Ongoing cost of tracking upstream improvements (rebalance fixes, new SASL mechanisms, etc.).

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | +  | −  |
| C2 | +  | +  | ++ | +  |
| C3 | ++ | −  | −− | ++ |
| C4 | −  | +  | ++ | −− |
| C5 | +  | +  | −  | +  |

**O1's fundamental gap.** boxer's CODINGSTANDARDS demands receiver name `inst`, interface suffix `I`, named return values, no `if err := f();` pattern, errors built via `eh.Errorf` / `eb.Build()...Errorf`, sized integer fields on structs, and compile-time interface checks. A verbatim copy violates every one of these on every file. The "scope of enforcement: new code and meaningful rewrites" carve-out doesn't help — porting a file *is* a meaningful rewrite. C1 collapses to `−−`.

**O3's gap.** A clean-room reimplementation produces output free of Apache obligations (C2 = `++`), but the franz-go consumer loop has subtle contract details — per-partition checkpointing under rebalance, ack-drain ordering with `client.PauseFetchPartitions`, `AbortBufferedRecords` interaction with the producer's idempotent sequence — that take production-incident cycles to discover. Recreating those without reading existing solutions multiplies discovery cost; C3 is `−−` and C5 is `−` because divergence accumulates.

**O4's gap.** Importing Benthos brings ~150 MB of transitive plugins (SQL, Snowflake, MQTT, Pulsar, et al.) we do not use, plus its configuration-DSL surface (`service.ConfigField`) which doesn't compose with whatever pipeline shape downstream consumers evolve. C4 is `−−`.

## Decision

We will adopt **O2 — derivative under Apache-2.0, refactored to boxer style, Benthos service framework dropped**. The port lives at [`public/streaming/persisted/kafka/`](../../public/streaming/persisted/kafka), pinned to upstream commit [`50aa034a668cc7d03d6acdcf63791fc36906a21c`][upstream-pin] (2026-04-24).

Scope: input + output, franz-go (`kgo`) variant only. Out of scope: sarama variant, schema registry input/output, Redpanda-specific input/output/cache wrappers, AWS MSK IAM helpers, the [`enterprise/`][upstream-enterprise] RCL-licensed subdirectory.

License compliance: per-file Apache-2.0 header carrying the upstream copyright + a boxer modification copyright; per-file modification notice per §4.b; package-level [NOTICE][pkg-notice] per §4.d listing upstream provenance + third-party deps; aggregate [`doc/legal/third_party.md`][third-party] tracking transitive attributions.

Local interface boundary (replaces `service.MessageBatch`, `service.AckFunc`, `service.Resources`):

```go
type RecordEnvelopeI interface {
    Topic() (s string)
    Partition() (p int32)
    Offset() (o int64)
    Key() (b []byte)
    Value() (b []byte)
    Timestamp() (t time.Time)
    Header(name string) (val []byte, ok bool)
}

type AckFnI func(ctx context.Context, processErr error) (err error)

type BatchI interface {
    Records() (seq iter.Seq[RecordEnvelopeI])
    Ack() (fn AckFnI)
}

type ConsumerI interface {
    Connect(ctx context.Context) (err error)
    Read(ctx context.Context) (b BatchI, err error)
    Close(ctx context.Context) (err error)
}
```

Names are placeholders; the Phase 1 implementation reconciles them against existing boxer streaming abstractions (none today) and against boxer's library-style sub-package conventions.

## Alternatives

- **O1 — Verbatim copy.** Rejected: incompatible with boxer CODINGSTANDARDS at every receiver, return-value, and error-handling callsite; per-file diff against upstream rapidly becomes uninterpretable.
- **O3 — Clean-room reimplementation.** Rejected: the discovery cost of rebalance / ack-drain / idempotent-producer edge cases dominates; agent-assisted clean-room is also hard to defend honestly when the source is one click away.
- **O4 — Adopt Benthos as a module dep.** Rejected: ~150 MB of unused transitive plugins, framework abstractions misaligned with the embedded-library shape downstream consumers need, configuration via `service.ConfigField` is inappropriate for an in-process consumer.
- **Vendor `Jeffail/checkpoint` + `Jeffail/shutdown` instead of taking module deps.** Considered; both are MIT and small. Rejected because they are well-maintained external libraries with no benefit from vendoring; they go into `go.mod` like any other dep.

## Consequences

### Positive

- We inherit production-tested franz-go interaction patterns: rebalance handling, partition-ordered checkpointing, idempotent-producer write paths, SASL mechanism wiring, transaction isolation, header encoding.
- All ported files conform to boxer CODINGSTANDARDS, so the package composes idiomatically with the rest of boxer.
- Apache-2.0 obligations are static and small: one license file, one NOTICE per package, one modification line per file. No copyleft.
- The [`enterprise/`][upstream-enterprise] subdir and its RCL-licensed contents stay strictly outside the dependency graph — license review is one-shot, not ongoing.
- Re-applying upstream improvements is mechanical: read the upstream diff at the new pinned SHA, translate the boxer-style refactor onto the changed files, bump the SHA in NOTICE.

### Negative

- Per-file Apache headers and the package-level NOTICE must be maintained as files are added or removed. Drift is the failure mode (e.g., a new file forgets the modification line). Mitigation: a doclint-style header check or a CI grep before merge.
- We carry an ongoing translation tax: every upstream improvement we want must be re-shaped to the local interface before landing. For features we don't track upstream, this is a one-time cost; for features we do, it accumulates linearly with diff size.
- The `franz_shared_client.go` registry — useful when the same `kgo.Client` is reused across input + output in one process — must be ported alongside (Phase 1 inventory adjusted), since both directions are in scope. Without it, two clients are created per topic.
- ack semantics must be codified before Phase 5: the upstream contract is "fire-and-forget with internal checkpointing"; if the local `AckFnI` treats ack errors as terminal, message duplication on rebalance changes semantics. The Phase 5 plan must capture this in writing.

### Neutral

- The pinned upstream SHA means we are not on Connect's release cadence. Re-pinning is an explicit decision recorded in NOTICE + this ADR's References + a follow-up commit message.
- The `Jeffail/checkpoint` and `Jeffail/shutdown` libraries (MIT, Copyright 2024 Ashley Jeffs) become module dependencies. Their licenses are tracked via [`doc/legal/third_party.md`][third-party].
- A future port of Connect's *output* schema-registry / Avro / Protobuf codecs is unrelated and would warrant a separate ADR.
- The kafka package will not vendor a per-package `LICENSE` file. The repo-level [`licenses/Apache-2.0.txt`][apache-2.0-text] satisfies §4.a once for the whole repo.

## Status

Accepted on 2026-04-27 by @stergiotis (originally as pebble2impl ADR-0015); migrated into boxer as ADR-0005 on 2026-04-28.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- Upstream pin: [`redpanda-data/connect@50aa034a`][upstream-pin] (2026-04-24).
- Upstream license layout: [`redpanda-data/connect/licenses/`](https://github.com/redpanda-data/connect/tree/50aa034a668cc7d03d6acdcf63791fc36906a21c/licenses) — Apache-2.0 covers the regular plugins; RCL covers [`enterprise/`][upstream-enterprise].
- Local license text: [`licenses/Apache-2.0.txt`](../../licenses/Apache-2.0.txt).
- Package-level NOTICE: [`public/streaming/persisted/kafka/NOTICE`](../../public/streaming/persisted/kafka/NOTICE).
- Aggregate third-party attribution: [`doc/legal/third_party.md`](../legal/third_party.md).
- franz-go: [`github.com/twmb/franz-go`][franz-go] — BSD-3-Clause, Copyright 2020 Travis Bischel.
- Jeffail/checkpoint: [`github.com/Jeffail/checkpoint`](https://github.com/Jeffail/checkpoint) — MIT.
- Jeffail/shutdown: [`github.com/Jeffail/shutdown`](https://github.com/Jeffail/shutdown) — MIT.
- Apache License 2.0, Sections 4.a–4.d (redistribution): https://www.apache.org/licenses/LICENSE-2.0#redistribution.

[franz-go]: https://github.com/twmb/franz-go
[rpcn]: https://github.com/redpanda-data/connect
[benthos-service]: https://pkg.go.dev/github.com/redpanda-data/benthos/v4/public/service
[upstream-pin]: https://github.com/redpanda-data/connect/tree/50aa034a668cc7d03d6acdcf63791fc36906a21c/internal/impl/kafka
[upstream-enterprise]: https://github.com/redpanda-data/connect/tree/50aa034a668cc7d03d6acdcf63791fc36906a21c/internal/impl/kafka/enterprise
[pkg-notice]: ../../public/streaming/persisted/kafka/NOTICE
[third-party]: ../legal/third_party.md
[apache-2.0-text]: ../../licenses/Apache-2.0.txt
