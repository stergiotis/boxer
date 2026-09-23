---
type: how-to
audience: developer putting an ingestion processor under a streaming framework, or landing its items
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified against a framework
> binary; do not cite as authoritative.

# How to run an ingestion processor under a streaming framework

The task-oriented walk: write a handler, run it as the external process a
framework drives, split its reply into messages, and land the items with a
sink. Why it is shaped this way is
[ADR-0252](../adr/0252-stevedore-routing-failure-and-state-handling-for-framework-driven-ingestion.md).
The framework — Benthos (Redpanda Connect) here, Vector or NiFi the same
way — owns the pipeline: inputs, outputs, batching, retries, dead-letter
routing, metrics. Stevedore owns what sits around your code: the framing,
the fan-out, the failure classes, the identity, the reassembly, the commit
order. Your code is a handler and a sink.

## 1. Write a handler

A handler turns one request into items. It gets the body, whatever header
the pipeline composed, and a context that ends at the host's deadline;
it emits as many items as it likes and returns nil, or an error.

```go
func splitLines(ctx context.Context, req stevedore.Request, emit func(stevedore.Emit) error) error {
    if !utf8.Valid(req.Body) {
        return stevedore.Permanentf("body is not text")     // no retry will cure it
    }
    for i, line := range bytes.Split(req.Body, []byte("\n")) {
        if err := emit(stevedore.Emit{Payload: line, Line: uint64(i + 1)}); err != nil {
            return err
        }
    }
    return nil
}
```

Say which errors are permanent with `stevedore.Permanent`; everything else
is transient and the host retries it in place before reporting it. A panic
is recovered into a permanent failure. Items emitted before an error are
discarded, so failing late is safe.

What the handler decodes, and what a payload is — a line, a JSON document,
facts-CBOR of your own kind — is yours. Set `Emit.PayloadKind` when the
bytes are a kind, so a lander can dispatch on it.

## 2. Run it as the framework's process

Link the host into a binary of your own — a subcommand under your
application's CLI — and hand it the handler:

```go
cfg := host.Config{
    Codec:    wire.CodecLengthPrefixedUint32BE,
    Reply:    host.ReplyStdout,
    MaxFrame: 16 << 20,
    Deadline: 20 * time.Second,
    LogLevel: zerolog.InfoLevel,
}
return host.RunStdio(ctx, cfg, stevedore.HandlerFunc(splitLines))
```

Three settings are set **together with the framework's**, because the
framework kills what exceeds its own:

| Host | Framework (Benthos `subprocess`) | Why |
|---|---|---|
| `MaxFrame` | `max_buffer` | a reply past the framework's buffer is a killed process, not a status |
| `Deadline` | the processor's timeout, where it has one | a slow handler yields a transient status rather than a restart |
| `Codec` | `codec_send` and `codec_recv` | the same on both sides; `lines` cannot carry a newline in a payload |

`Reply` matches how the framework reads a failure. Under `stdout`, the
upstream shape, the reply is on stdout and a failure is one line on
stderr, so the host writes logs to `LogOutput` and nothing else to stderr.
Under `three-frame` the status, the payload and the log lines are three
frames on stdout.

The example is `boxer stevedoredemo process`, and
[its pipeline configuration](../../public/app/commands/stevedoredemo/stevedoredemo_pipeline.yaml)
runs it on lines from stdin:

```yaml
- subprocess:
    name: boxer
    args: [stevedoredemo, process, --bare-body, --codec=length_prefixed_uint32_be]
    codec_send: length_prefixed_uint32_be
    codec_recv: length_prefixed_uint32_be
    max_buffer: 16777216
- unarchive:
    format: binary
```

## 3. Give the request a header, or send it bare

The host reads a request as the framework's binary archive of two parts, a
JSON header and the body, or of the body alone. With `--bare-body` (the
`BareBody` setting) the frame *is* the body and nothing is composed.

The header is where the pipeline says what it knows:

```json
{"origin": "requests/3/1041", "hint": "jsonl", "attributes": {"tenant": "x"}}
```

`origin` matters most: the **file reference** every item carries is a
tagged id whose body hashes it, so a redelivered request produces the same
reference and the same ordinals, and a lander's rows collapse on them. Put
the input's topic, partition and offset there, or a path, or a message id.
Without an origin the reference hashes the body.

A pipeline that splits a body before the processor — a scanner into
chunks, say — sets `split`, `part`, `parts` (once known) and `last` on the
header of each piece; the host copies them onto every item and a lander
reassembles by reference (§5).

## 4. Read the reply

One reply per request, always: an archive of *n* items, each facts-CBOR
of the `stevedoreItem` kind carrying the reference, the origin, the ordinal,
the line and offset the handler gave, the split fields, and your payload.
The framework's `unarchive` in its binary format turns it into *n*
messages; the fields ride inside each because metadata neither crosses the
contract nor survives the split.

A failure is a status, `permanent: …` or `transient: …`, and the message
is left unchanged and marked failed. Route on the prefix:

```yaml
- catch:
    - switch:
        - check: error().has_prefix("transient:")
          processors: [ { retry: {...} } ]
        - processors: [ { mapping: 'meta dead = "yes"' } ]
```

## 5. Land the items

A sink writes items as rows. Its one contract: key the row by the item's
reference and ordinal, so a redelivered item is a rewrite.

```go
type mySink struct{ store *myfacts.MyStore }

func (inst *mySink) Land(ctx context.Context, item stevedore.Item) error {
    row := myfacts.MyRow{Ref: item.Ref.Value(), Ordinal: item.Ordinal, /* your fields from item.Payload */}
    return inst.store.Begin(rowId(item), time.Now(), myfacts.MyEnvelope{NaturalKey: rowKey(item)}).AddMyRow(row).Commit()
}
func (inst *mySink) Flush(ctx context.Context) error { _, err := inst.store.Flush(ctx); return err }
```

The lander consumes a topic with the Kafka reader, decodes each envelope,
hands it to the sink, and commits the batch's offset only after `Flush`
returned nil. A permanent error from `Land` becomes a **dead-letter row**
of the `stevedoreDeadLetter` kind on `boxer.facts`, through a store the
application places by database; a transient one is retried, then stops the
lander with nothing acknowledged, so the restart redelivers.

```go
l := lander.New(lander.Config{Reassemble: true, ReassembleMaxAge: 10 * time.Minute},
    reader, &mySink{store}, &lander.StoreDeadLetters{Store: deadStore})
err := l.Run(ctx)
```

**Delivery is at-least-once on both sides.** After a crash between a flush
and its acknowledgement, rows are written twice and read once when the
reader collapses on the key; a count that does not collapse over-counts.

With `Reassemble` on, split items are held per reference until the last
part arrives, bounded by `ReassembleMaxBytes` across sets and
`ReassembleMaxAge` per set; a set that outlives the age is dead-lettered
as `incomplete` and released.

The example is `boxer stevedoredemo land`, which prints each item as a JSON
line and logs dead letters, or writes them to ClickHouse with
`--dead-letters=clickhouse` against the `CLICKHOUSE_*` variables.

## 6. What does not work, and why

- **A body larger than a reply.** The contract holds one reply whole on
  both sides, within one frame and one timeout. The host refuses a body
  above `MaxBody` with a permanent status rather than build a reply the
  framework will kill. Split it in the pipeline, or wait for the fetcher
  service ADR-0252 defers.
- **The `lines` codec with binary payloads.** A newline in a payload is a
  frame boundary under it; the host refuses to write one. Use a
  length-prefixed codec for anything that is not text.
- **Logs on stderr under the stdout reply.** The framework reads every
  stderr line as the pending message's failure. The host sends logs to
  `LogOutput` instead; a handler that writes to stderr itself breaks the
  contract.
- **An origin that changes between deliveries.** A reference hashes the
  origin, so an origin carrying a timestamp or a random id makes every
  redelivery a new file. Use what the input repeats: topic, partition,
  offset.
