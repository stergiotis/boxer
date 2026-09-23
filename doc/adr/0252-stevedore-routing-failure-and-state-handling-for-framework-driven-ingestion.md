---
type: adr
status: accepted
date: 2026-09-22
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-23
---

# ADR-0252: stevedore — routing, failure and state handling for ingestion processors driven by a streaming framework

## Context

The workload is a **1:n streaming pipeline**: a message names a file, the
file is fetched from wherever it lives — HTTP, a local path, SFTP, an object
store — and it leaves as many messages, to be landed
later as rows in a record store, each row carrying the identity of the
file it came from. The pipeline is **owned by a streaming framework**, not
by this repository. Benthos (Redpanda Connect) is the one in use; Vector
and Apache NiFi are the same shape and could replace it. Such a framework
owns inputs and outputs, batching, retries, dead-letter routing, metrics,
tracing, configuration and process supervision, and composes work from
processors. One kind of processor is an **external process**: a binary it
starts, keeps running, and drives over stdin and stdout with framed
messages — one framed request, one framed reply, an error status beside
the reply, a pool of processes for parallelism, a frame-size limit, and a
per-message timeout after which the process is killed and restarted.
Benthos calls it a subprocess processor; NiFi's stream-command processor
and Vector's exec transform are the same contract.

What a processor *does* to a payload — decode a body into records, flatten
a record into leeway leaves, map a domain — is the **application's**: it
changes per pipeline, it is where the domain lives, and it is what an
application team wants to own. What sits around it does not change per
pipeline and is where two teams would make different, subtly wrong
choices:

- **Routing.** How one request becomes *n* items when the contract allows
  one reply; how a body split by the framework into many messages is put
  back together; how each item names the file and the position it came
  from when message metadata does not cross the contract.
- **Failure.** Which errors are worth retrying and which are not, and how
  the framework is told the difference when its contract carries a status
  string; what happens on a panic, an oversize body, a slow handler, so
  that the framework sees a status rather than a killed process.
- **State.** What a processor may hold between messages (nothing), what
  identity survives a redelivery, when a landing commits its offset, and
  how rows written twice read once.

What the tree holds: a Kafka reader and writer derived from the same
framework family (ADR-0005); a chunk protocol under `public/storage/blob`
whose first, intermediate and last chunk vocabulary is exactly the shape
of a body split into messages, with no consumer; generated record stores
(ADR-0100, ADR-0105) whose flush is one retryable insert; the house rule
that structure on a wire is facts-CBOR of a vocabulary kind with a
generated codec (ADR-0135 §SD2); and the sysmetrics tee (ADR-0184), the
precedent for landing facts-shaped payloads without knowing the domain.

Three earlier drafts of this ADR are withdrawn in place: a batch ingestor
with a ledger and a watchbill job; a standalone Kafka-to-Kafka stage; and
processors that decoded and shredded inside this repository. The last
misplaced the content work; this draft keeps the host and hands the
content to the application. Kill-reasons are under Alternatives.

## Design space (QOC)

**Question.** In a framework-driven ingestion, what does this repository
own?

**Options.**

- **O1** — the pipeline: a standalone service that consumes, fetches,
  produces and acknowledges.
- **O2** — processors with their content: decoders and a shredder behind
  the contract.
- **O3** — the host: the contract, routing, failure and state handling,
  with the content supplied by the application as a handler and a sink.
  (chosen)
- **O4** — nothing: each application writes its own framing and landing.

**Criteria.**

- **C1** — code this repository owns and tests.
- **C2** — what two applications would otherwise get subtly wrong: the
  contract, failure classes, identity, commit order.
- **C3** — coupling of this repository to data formats and domains.
- **C4** — portability across frameworks.
- **C5** — one operational model for the people running pipelines.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | −  | ++ | ++ |
| C2 | +  | +  | ++ | −− |
| C3 | −  | −− | ++ | ++ |
| C4 | −  | +  | ++ | +  |
| C5 | −− | ++ | ++ | ++ |

O3 is chosen. O4 is what exists, and it produced the chunk package with no
consumer and three framing conventions in one product family. O2 put the
domain in a library. O1 is what the framework is for.

## Decision

We will build **stevedore**, a library under `public/streaming`: a
**processor host** that speaks the framework's framed request-and-reply
contract, gives an application handler one request and turns its items
into one reply, classifies its errors into a status the framework can
route, and bounds its size, time and panics; a **lander host** that
consumes items from a topic, reassembles bodies the framework split,
hands each item to an application sink, and commits its offset only after
the sink's flush; and the **conventions** both share: an envelope kind on
the wire, identity derived from the request's origin, and a dead-letter
kind that lands as a row. Stevedore decodes nothing and shreds nothing.
An application links it into a binary of its own, the way a watchbill
handler is linked (ADR-0223 §SD5), and registers a handler and a sink.

A stevedore works the hatch and checks cargo against the manifest; what
the cargo is, is the shipper's business. The name goes into README § House
names on acceptance.

```text
framework pipeline (Benthos, or Vector, or NiFi)
  input ─▶ fetch ─▶ archive [header, body] ─▶ [app binary: stevedore.Host + app handler] ─▶ split ─▶ output
                                                request in ─▶ handler ─▶ items ─▶ one reply
                                                status: permanent:… | transient:… | empty
  errors: the framework's catch, retry and dead-letter routing, on the status

lander (app binary: stevedore.Lander + app sink)
  topic ─▶ envelope ─▶ reassemble (if split) ─▶ sink ─▶ flush ─▶ commit offset
                                                   permanent ─▶ dead-letter row
```

### Subsidiary design decisions

#### SD1 — Scope: the host, not the content

Stevedore owns the contract framing, the request and reply envelopes,
fan-out encoding, failure classification and bounding, identity,
reassembly, the lander's commit discipline and the dead-letter kind. The
application owns the handler (what a body becomes), the sink (what a row
is and which store it goes to), the fetch — whichever transport the file
lives behind, and its credentials — the pipeline configuration, topics and
the database. The host does not know where a body came from. Decoders and a leeway shredder
are application code, or a later leeway decision; the jsonbench ledger's
open row for a shredder stays open and is not this ADR's.

#### SD2 — The contract, stated once, and the two envelopes

A processor reads one length-prefixed frame from stdin and writes its
reply as frames on stdout: a status (empty on success), the payload, and
log lines as JSON with a level. Netstring and big-endian 32-bit length
prefixes are both supported, selected by flag, because frameworks differ;
nothing else is written to stdout. The framing is implemented here from
this statement, tested against fixture frames, and depends on no
framework's module.

Metadata does not cross the contract, so the **request** is the
framework's own binary archive of two parts — a small JSON header (the
origin, a hint, attributes the pipeline chooses to pass) and the body —
composed by the pipeline with its archive processor. The **reply** is the
same archive format holding *n* items, split by the pipeline's unarchive
processor into *n* messages. Each item is facts-CBOR of the envelope kind
`stevedoreItem`: the file reference, the item's ordinal, an optional
position (a line, a byte offset) the handler reports, and the
application's payload as bytes — itself facts-CBOR of the application's
own kind where it is structured, so the lander reads the envelope and the
sink reads the payload, each with its own generated codec.

```go
type HandlerI interface {
    // Handle turns one request into its items. A returned error is classified
    // (SD3); items already emitted before the error are discarded.
    Handle(ctx context.Context, req Request, emit func(Item) error) error
}
```

#### SD3 — Failure: classified where it arises, reported as a status

An error is **permanent** or **transient**, and the code that raises it
says which through two wrappers; an error nobody classified is transient.
The status text the host writes begins with the class, `permanent:` or
`transient:`, so a pipeline routes the two with a string match, which is
the only distinction the contract offers. The host also:

- **recovers a panic** in the handler into a permanent status, so one bad
  body does not cost the process and the messages queued behind it;
- **applies a deadline** below the framework's timeout, so a slow handler
  yields a transient status rather than a kill that looks like a hang;
- **refuses an oversize body** with a permanent status rather than
  building a reply the framework cannot hold, the bound being a flag set
  together with the framework's frame limit;
- **retries a transient error in place**, with exponential backoff and
  jitter up to a bounded attempt count, before reporting it — a fetch that
  hits a 503 is retried here, not by round-tripping the framework.

Dead letters are the framework's on the processor side. On the lander side
a permanent error becomes a row of the kind `stevedoreDeadLetter`: the
envelope's reference and ordinal, the error text, the message bytes. That
row is the one thing stevedore writes to a store itself.

#### SD4 — State: none in a processor, identity from the origin, reassembly by reference

A processor holds nothing between messages; a pool of them is
interchangeable. What survives a redelivery is identity: the **file
reference** is a tagged id under stevedore's claimed tag whose body hashes
the request's origin, or the request bytes when the header carries none,
so the same request yields the same reference and the same ordinals every
time. Content is never identity, because the framework may split a body
into messages before any processor sees it whole.

When the pipeline does split a body — its scanner into lines or chunks, or
its archive of a large fetch — the lander **reassembles by reference**
using the chunk vocabulary already in `public/storage/blob`: first,
intermediate, last, or first-and-last, with the index and the size so far
carried in the envelope's position. The reassembly is a state machine per
reference, bounded by a size and by an age; a file whose last chunk does
not arrive within the age is dead-lettered as incomplete and its chunks
released. A pipeline that does not split declares so, and the lander
skips the machine.

#### SD5 — The lander host: commit after flush, rows idempotent by key

`stevedore.Lander` consumes a topic through the Kafka reader under
`public/streaming/persisted/kafka`, decodes the envelope, reassembles
where declared, and hands each item to the application's sink. The host
owns batching — a flush every *n* items or every interval — and the
order: sink flush, then offset commit. A transient error from the sink
retries the flush, which a generated store already makes safe by keeping
the transferred rows pending; a permanent one writes a dead-letter row and
moves on. Delivery is therefore at-least-once, and the contract the sink
must keep is stated in its interface: a row's natural key is derived from
the reference and the ordinal, so a row written twice reads once when the
reader collapses on it, and the how-to says so.

```go
type SinkI interface {
    // Land writes one item. Its rows must be keyed by ref and ordinal so a
    // redelivered item is a rewrite, not a duplicate.
    Land(ctx context.Context, item Item) error
    // Flush makes everything landed since the last flush durable. The host
    // commits its offset only after Flush returns nil.
    Flush(ctx context.Context) error
}
```

The dead-letter store is a facts-bound generated store the application
places by database (ADR-0223 §SD1's `Layout` pattern), verified and never
created (ADR-0184 §SD2).

#### SD6 — A library, an example, no command group

Stevedore ships as packages and one example application under `apps/`, on
the watchbilldemo pattern: a handler that splits a body into lines and a
sink that writes a trivial kind, driven end to end by a framework
configuration checked in beside it. No `boxer stevedore` command exists,
because there is nothing to run without an application's handler and
sink. No environment variable is added: the host is configured by flags
the framework passes as arguments, and a lander reads the `CLICKHOUSE_*`
registry like any headless boxer binary.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| a new natural-key vocabulary, tag claimed under `tagmint` | added: `stevedoreItem`, `stevedoreDeadLetter` and their memberships | the assignments golden, the repo-wide disjointness check |
| `boxer.facts` rows | added: the dead-letter kind | the how-to's queries, play's kind roster |
| `public/storage/blob/chunked` | gains its consumer: the reassembly counterpart of its writer, and a fixed-id generator for `Prepare` | its doc comment, which says the id is minted |
| `public/streaming/persisted/kafka` | unchanged; the lander is a caller | — |
| `apps/` | added: the example application and its pipeline configuration | the entry-points baseline |
| README § House names | added: `stevedore` | — |

## Alternatives

- **Processors with decoders and a shredder inside** (O2, the third draft).
  Rejected: what a payload becomes is the application's domain; a library
  that decides it couples this repository to every pipeline's formats.
- **A standalone service owning the pipeline** (O1, the second draft).
  Rejected: it re-implements consume, produce, acknowledge, backoff and
  dead letters that the framework provides in configuration, and it is a
  second operational model.
- **A batch ingestor with a ledger and a watchbill job** (the first draft).
  Rejected: its state substrate is one a stateless processor must not
  reach.
- **Nothing** (O4). Rejected: it is the status quo, and it produced a
  chunk protocol without a consumer and framing conventions that differ per
  binary.
- **Streaming bytes through a processor as chunks.** Rejected: the
  contract holds one reply whole on both sides. A pipeline splits a body
  with its own scanner and the lander reassembles (§SD4).
- **A fetcher service for bodies larger than a reply.** Deferred on
  evidence; its rule is written down: a body the framework cannot hold in
  one reply within its timeout.
- **Content-derived identity.** Rejected: a body may be split before it is
  whole anywhere.
- **Message headers for the reference.** Rejected: metadata does not cross
  the contract and does not survive a split.
- **Importing a framework's framing helpers.** Rejected: the contract is a
  few dozen lines, frameworks differ, and a module dependency on one
  contradicts portability.
- **NATS for the lander.** Rejected: core NATS is ephemeral by house
  decision (ADR-0090 §SD4).

## Consequences

### Positive

- This repository owns the parts that are invariant and subtle, and tests
  them without a broker: the contract, the classes, the identity, the
  reassembly, the commit order.
- An application writes a handler and a sink and inherits a correct
  processor and a correct landing.
- One operational model, and portable across frameworks by construction.
- The chunk protocol gains the consumer it was written for.

### Negative

- A body is bounded by one reply; large bodies are the pipeline's to split
  or the deferred fetcher's to stream.
- Fan-out costs an archive and a split per request, and the reference is
  repeated in every item.
- At-least-once end to end; every sink keys by reference and ordinal, and
  every reader collapses on it, or over-counts after a crash.
- Transient and permanent are a prefix convention on a status string,
  enforced by the host on its side and by nothing on the framework's.
- Reassembly holds a file's chunks in the lander's memory until the last
  arrives, bounded by the size and age limits the application sets.

### Neutral

- Stevedore lands one kind of its own, dead letters; every other row is
  the sink's.
- The framework's process pool is the parallelism; a handler is
  single-threaded per message.

## Migration — Tier 1

- **Breaks.** Nothing. The chunk package keeps its API and gains a caller.
- **Path.** None. The dead-letter store verifies the facts schema and
  creates nothing.
- **Regeneration.** `scripts/dev/generate.sh` — the vocabulary golden and
  the two kinds' codecs.
- **Old shape.** The three earlier drafts of this ADR are replaced in
  place; none was accepted.

## Verification plan — Tier 1

- **Lane.** Default `go test`: a **contract test** driving the host over
  pipes with fixture frames in both codecs and asserting the three reply
  frames, the status classes, a recovered panic, a deadline, and a refused
  oversize body; a **repeatability property** — the same request handled
  twice yields byte-identical replies; the reassembly machine over
  shuffled, duplicated and incomplete chunk sequences; the lander over the
  kafka package's doubles and the record-store example executor, asserting
  no offset commits before a flush. Integration lane: the lander against a
  local ClickHouse (the `chlocalpool` guard), asserting rows collapsed on
  natural key equal a clean run's after a crash mid-batch; and the example
  application under a framework binary obtained through `public/extbin`,
  end to end.
- **What would fail.** A second message from one request; anything but
  frames on stdout; a status without a class; a panic that exits the
  process; an offset committed before a flush; a redelivery that changes a
  reference or an ordinal; an incomplete file never dead-lettered.
- **Gap.** Throughput per message is asserted nowhere; a trial under
  `doc/trials` follows if a number is ever to be quoted.

## Milestones

- **M1 — the contract and the processor host.** Framing in both codecs,
  the envelopes, the classes, panic and deadline and size bounding, the
  contract test.
- **M2 — identity, the kinds and the lander host.** The vocabulary, the
  reference, the commit order, dead letters as rows.
- **M3 — reassembly**, on the chunk package, with the incomplete-file rule.
- **M4 — the example application and the how-to.** The end-to-end run
  under a framework binary lands with them.

Deferred, recorded so they are not rediscovered: the fetcher service and
its size rule; a leeway shredder and format decoders, as application code
or a leeway ADR; bytes into lading, with lading's per-file writer; a
processor emitting a physical row for a framework that posts to
ClickHouse itself; a multi-frame reply should a framework offer one.

## Status

Accepted 2026-09-23.

Resolved during the design dialogue: the request is the framework's two-part
archive composed by the pipeline (§SD2); the failure class is a prefix on
the status text (§SD3), since a status is the one frame every framework
routes on; the name is `stevedore`.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-09-23 — M1–M4 landed; the contract has two reply shapes, and three fields the design missed

What shipped: the framing (`wire`), the conventions (`stevedore`), the
processor host (`host`), the lander (`lander`), the vocabulary and the two
kinds, the reassembler as the chunk package's consumer, and the example as
the `stevedoredemo` command group with its pipeline configuration and the
how-to. Four refinements against the body as accepted:

- **§SD2 — two reply shapes, not one.** The upstream subprocess processor
  reads the payload from stdout and treats *any line on stderr* as the
  pending message's failure, leaving the message unchanged; the three-frame
  reply (status, payload, log lines on stdout) is a variant some frameworks
  use. The host supports both, selected by `Reply`; under the stdout shape
  logs go to a file or nowhere, because stderr is spoken for.
- **§SD2 — a bare body.** A pipeline that composes nothing sends the body
  as the frame with `BareBody`; the origin is then empty and the reference
  hashes the body.
- **§SD4 — an explicit split marker.** A first part of a split body carries
  index zero, no count and no last marker, which is what an unsplit item
  carries too; the header and the envelope gained `split`, and reassembly
  keys on it.
- **§SD6 — the example is a command group, not an app.** Entry points are
  subcommands of the main binary (CODINGSTANDARDS § Entry Points), and
  `apps/` holds windowed apps; the example lives under
  `public/app/commands/stevedoredemo` with its pipeline beside it.

Also recorded: the item envelope carries no timestamp, so a redelivered
request yields byte-identical replies, which the verification plan's
repeatability property pins; a reassembler remembers recently completed
ids so a late duplicate does not re-open a set; and the fetch is the
pipeline's or the handler's over whatever transport the file lives behind —
HTTP, a path, SFTP, an object store — which the body's Context now says.

### 2026-09-23 — the lander does not reassemble; a review's findings

An adversarial review of the four commits found that §SD4 as written
loses data. The lander acknowledged a batch after landing it, while the
parts of a split body it held sat in memory until the last part arrived; a
restart in between lost parts whose offsets were already committed, and
the set could never complete. Deferring the acknowledgement is not
available: the ordered reader holds the next read until the previous
batch is acknowledged, and a file's parts share a partition, so a set
spanning batches would deadlock. The at-least-once contract wins:

- **The lander lands a split item as it is**, one part per item, and a
  sink keys its rows by reference, part and ordinal. A sink that wants the
  body whole keeps the parts as durable rows and assembles them on read —
  the shape lading's block rows already have. The `Reassemble` options,
  the age sweep and the `incomplete` dead-letter class are gone.
- **The reassembler stays in the chunk package** as the protocol's
  consumer side, for a process that holds the whole set itself, with the
  review's corrections: a byte bound checked before a set is opened, held
  indexes judged once the total is known, and a whole set kept until the
  caller's `Done` so a failed handoff can retry.

The other findings, fixed with it: the lines codec is refused by the host,
since a reply is a binary archive; a length-prefixed frame is the zero
codec; a line frame is bounded by the configured bound rather than the
buffered reader's size; a payload ending in a carriage return is refused
under the lines codec because the reader strips one; a retry policy with
only its attempts set still backs off; and a proptable row that leaked in
from another session's tree is removed.

## References

- [ADR-0005](./0005-streaming-persisted-kafka-from-connect.md) — the Kafka
  reader the lander uses, and its derivation.
- [ADR-0135](./0135-app-launch-requests.md) §SD2 — structure on a wire is a
  vocabulary kind with a generated codec.
- [ADR-0184](./0184-sysmetrics-persistence-tee.md) — landing facts-shaped
  payloads without knowing the domain.
- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md),
  [ADR-0105](./0105-keelson-adopts-generated-record-stores.md) — generated
  stores, the retryable flush, the facts-bound lane.
- [ADR-0223](./0223-watchbill-durable-work-on-facts.md) — handlers linked
  into an application's binary, and the `Layout` pattern.
- [ADR-0198](./0198-fs-snapshot-store.md) — the lading store, and the
  per-file writer it lacks.
- [ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) §SD4 — core NATS only,
  no durability.
- [Facts-bound record stores](../explanation/facts-bound-record-stores.md) —
  what a facts-bound store can and cannot do.
