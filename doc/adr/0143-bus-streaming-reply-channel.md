---
type: adr
status: proposed
date: 2026-07-25
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0143: A streaming reply channel for bus request/reply

## Context

The bus answers a request with one byte slice:
`Request(subject, payload) (reply []byte, err error)`. That shape decides
three things at once, and all three bite the same case.

A `clickhouse-local` worker spawned by the chlocal broker
([ADR-0028](./0028-chlocal-low-latency-sql-cap.md)) produces its result
progressively on stdout. To answer over the bus today the broker must drain
that output to completion into memory before replying. So:

- **Peak memory is the whole result.** A result larger than comfortable is
  not slow, it is impossible.
- **Nothing is consumable until everything arrives.** A caller that would
  happily render the first rows waits for the last.
- **A dead producer is indistinguishable from a slow one**, because the
  only signal is a reply that has not arrived yet.

ADR-0028 anticipated this and reserved the slot: its `Streaming bool`
request flag says "bypass the in-memory buffer". The service refuses it to
this day — `streaming not implemented in M2; set Streaming=false` — because
the flag needs a wire that does not exist. This ADR is about that wire.

Two constraints come from elsewhere and are not up for renegotiation here.
[ADR-0089](./0089-rowdml-serialization-clickhouse-native-ingestion.md) fixed
that the **result wire format stays separate from the ingestion wire
format**; a streaming reply must not become a second way to ingest.
[ADR-0142](./0142-runstream-result-frame-contract.md) fixed the semantics a
result stream must carry — data, advisory progress, and exactly one
terminal, with *absence of a terminal meaning incomplete*. This is the
transport that would carry those semantics, not a second opinion about
them.

[query-system-requirements](../explanation/query-system-requirements.md)
names this E8 and calls it "the only wire-level novelty in the whole
catalog", which is the reason it is gated behind its own decision rather
than landing as part of the extension-point sweep.

## Decision

> **This ADR is a proposal awaiting sign-off. Nothing here is implemented,
> and no code should be written against it until it is accepted.**

We would give bus request/reply an **ordered, chunked, backpressured reply
stream** with an explicit end-of-stream or error marker, and bounded,
caller-configured retention. Sketched:

- **Ordered and sequenced.** Every chunk carries a sequence number, so a
  consumer can tell a gap from a reorder from a duplicate rather than
  assuming a well-behaved transport. This is the same discipline
  [ADR-0142](./0142-runstream-result-frame-contract.md)'s collector already
  enforces in-process; the transport should not weaken it.
- **An explicit terminator.** Exactly one end-of-stream or error marker.
  Consistent with ADR-0142: a stream that simply stops is *incomplete*, not
  complete, and a consumer must not have to infer the difference from a
  timeout.
- **Backpressured.** A consumer that reads slowly must slow the producer,
  not accumulate an unbounded queue on its behalf. Without this the change
  moves the memory problem from the broker to the bus rather than solving
  it.
- **Bounded retention, configured by the caller.** A late joiner may see
  some recent history; how much is the caller's decision and it is finite.
  Unbounded replay would reintroduce, on the bus, exactly the "hold the
  whole result" property being removed from the broker.

Boxer would ship the mechanism only. Retention *policy*, replay windows,
and what a system does with a partially consumed stream stay with the
system, as with every other extension point in the catalog.

**A second consumer, and what it implies for the shape.** This ADR was
first written for one motivating case — a `clickhouse-local` worker whose
stdout must be drained whole before the broker can reply. A second is now
committed roadmap: an async ClickHouse cluster streaming results over Kafka
or NATS ([ADR-0144](./0144-query-engine-adapters.md), engine 3). It needs
the same four properties over a different transport.

Two independent consumers is a materially better case than one, and it
changes what "narrow" should mean here. The framing must be **transport-
agnostic** — carried equally by the in-process broker, NATS, and Kafka —
rather than shaped around the broker's reply path, which was the obvious
economy while there was one consumer. Concretely, that argues against
leaning on any bus-specific reply correlation, and for a framing whose
sequence, terminator and retention are properties of the payload rather
than of the transport carrying it.

It also raises a prerequisite this ADR does not own: on a **shared**
channel the run identity must be globally unique, and today's minted id is
unique per host only. ADR-0144 carries that fix; a streaming channel built
before it would correlate two hosts' streams onto one key.

**Out of scope for this ADR**, to keep it narrow: integrating the channel
into the introspection `/query` endpoint, into play, or into the cluster
adapter. Those are adopters, and the wire should be decided before anything
is built on it.

## Alternatives

- **Leave it buffered.** Honest and simple, and it is what ships today. The
  cost is a hard ceiling on result size and no progressive consumption; the
  question is whether that ceiling is one we accept permanently.
- **Chunk without backpressure** (fire-and-forget publishes on a per-request
  subject). Much simpler, and it moves the memory problem from the broker
  to the bus instead of removing it.
- **Hand back a file path or shared-memory handle.** Sidesteps the wire
  entirely and is plausible for a co-located broker, but it does not
  survive the transport becoming NATS, and it invents a second lifetime to
  manage.
- **Reuse the ingestion wire.** Explicitly ruled out by
  [ADR-0089](./0089-rowdml-serialization-clickhouse-native-ingestion.md);
  the two formats have different obligations and coupling them makes both
  harder to change.

## Consequences

### Positive

- A result larger than memory becomes possible rather than merely slow.
- Progressive consumption: a caller can render or process a prefix.
- ADR-0028's reserved `Streaming` flag can finally mean something.
- The frame contract gains a transport, so its guarantees reach past
  in-process consumers.

### Negative

- This is genuinely new wire surface, with everything that implies:
  versioning, a second failure mode per request, and a harder-to-reason
  concurrency story than one byte slice.
- Backpressure across a bus abstraction that must work both in-process and
  over NATS is the hard part, and the two transports do not offer it in the
  same way.
- Close discipline becomes the caller's problem, as ADR-0028 §SD9 already
  noted for the streaming flag. A leaked stream holds a worker.

### Neutral

- Consumers keep the option of the buffered path; streaming stays opt-in
  per request.
- Retention is bounded by construction, so a late joiner sees *some*
  history or none — never all of it.

## Status

**Proposed — awaiting sign-off before any implementation.** The
implementation plan this ADR belongs to gates the work on that sign-off
explicitly; the milestone is deliberately unstarted.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — the chlocal broker and the reserved `Streaming` request flag.
- [ADR-0089](./0089-rowdml-serialization-clickhouse-native-ingestion.md) — result wire stays separate from ingestion wire.
- [ADR-0142](./0142-runstream-result-frame-contract.md) — the frame semantics this transport would carry.
- [doc/explanation/query-system-requirements.md](../explanation/query-system-requirements.md) — E8, and why it is the catalog's only wire-level novelty.
