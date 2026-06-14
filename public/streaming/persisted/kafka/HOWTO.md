---
type: how-to
audience: engineer with a specific task
status: stable
reviewed-by: "@stergiotis"
reviewed-date: 2026-04-27
---

# How to use the streaming/persisted/kafka package

Recipes for the common consumer and producer flows. The package exposes the franz-go-derived plumbing through three reader implementations and a producer; this guide shows the minimum code each requires. For *why* the API is shaped the way it is — the ack contract, the iterator vs slice tradeoff, the concrete-vs-interface decision — see [`EXPLANATION.md`](EXPLANATION.md). For the upstream-derivation decision (Apache-2.0, Benthos service framework dropped), see [`ADR-0005`](../../../../doc/adr/0005-streaming-persisted-kafka-from-connect.md).

## When to use this recipe

This file exists because every realistic Kafka recipe needs a running broker. An `example_test.go` cannot satisfy a real `// Output:` block without one, so per [boxer's documentation standard](https://github.com/stergiotis/boxer/blob/main/doc/DOCUMENTATION_STANDARD.md) §1 ("How-To Guides"), the recipes live here in Markdown.

## Prerequisites

- Go 1.26+ with the build tags from `./tags`; invoke tooling as `go ... -tags "$(cat tags | tr -d $'\n')"`.
- A reachable Kafka or Redpanda broker. For tests, the package ships a testcontainers-based [integration_test.go](integration_test.go) — see "Run integration tests" below.
- Familiarity with [`github.com/twmb/franz-go/pkg/kgo`](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo): records (`kgo.Record`), client construction, consumer groups, and the partitioner choices.

## Recipe 1: produce records

```go
import (
    "context"

    "github.com/twmb/franz-go/pkg/kgo"
    "github.com/stergiotis/boxer/public/streaming/persisted/kafka"
)

func produce(ctx context.Context, brokers []string, records []*kgo.Record) (err error) {
    connDetails := kafka.DefaultFranzConnectionDetails()
    connDetails.SeedBrokers = brokers
    connDetails.ClientID = "my-producer"

    producerOpts := kafka.DefaultFranzProducerOpts()
    // producerOpts.Partitioner = kgo.RoundRobinPartitioner() // optional

    kgoOpts := append([]kgo.Opt{}, connDetails.FranzOpts()...)
    kgoOpts = append(kgoOpts, producerOpts.FranzOpts()...)

    client, err := kafka.NewFranzClient(ctx, kgoOpts...)
    if err != nil {
        return
    }
    defer client.Close()

    writer, err := kafka.NewFranzWriter(client, nil)
    if err != nil {
        return
    }
    if err = writer.Connect(ctx); err != nil {
        return
    }
    if err = writer.Write(ctx, records...); err != nil {
        return
    }
    err = writer.Close(ctx) // flush in-flight produces
    return
}
```

The application owns the `*kgo.Client`. `writer.Close` flushes pending produces but does not close the client; close it yourself when the application shuts down.

## Recipe 2: consume records (ordered)

```go
func consume(ctx context.Context, brokers []string, topic, group string) (err error) {
    connDetails := kafka.DefaultFranzConnectionDetails()
    connDetails.SeedBrokers = brokers
    connDetails.ClientID = "my-consumer"

    consDetails := kafka.DefaultFranzConsumerDetails()
    consDetails.Topics = []string{topic}

    readerOpts := kafka.DefaultFranzReaderOrderedOpts()
    readerOpts.ConsumerGroup = group

    clientOptsFn := func() (opts []kgo.Opt, err error) {
        opts = append(opts, connDetails.FranzOpts()...)
        opts = append(opts, consDetails.FranzOpts()...)
        return
    }

    reader, err := kafka.NewFranzReaderOrdered(readerOpts, clientOptsFn)
    if err != nil {
        return
    }
    if err = reader.Connect(ctx); err != nil {
        return
    }
    defer reader.Close(context.Background())

    for {
        batch, err := reader.Read(ctx)
        if err != nil {
            return err
        }
        for r := range batch.Records.RecordsAll() {
            // process r.Topic / r.Partition / r.Offset / r.Key / r.Value / r.Headers
            _ = r
        }
        if err = batch.Ack(ctx, nil); err != nil {
            return err
        }
    }
}
```

The ordered reader guarantees per-partition order of `RecordsAll()` and gates subsequent batch reads on the prior batch's `Ack`. See [`EXPLANATION.md` §"Ack contract"](EXPLANATION.md#ack-contract) for the strict in-order, exactly-once-call invariant.

## Recipe 3: switch ordered ↔ unordered at runtime

`FranzReaderToggled` lets a single configuration knob pick between strictly-ordered (default) and parallel-per-partition (unordered) modes:

```go
toggledOpts := kafka.DefaultFranzReaderToggledOpts()
toggledOpts.Unordered = unorderedFlag // true for parallel, false for ordered
if toggledOpts.Unordered {
    toggledOpts.UnorderedOpts.ConsumerGroup = group
} else {
    toggledOpts.OrderedOpts.ConsumerGroup = group
}

reader, err := kafka.NewFranzReaderToggled(toggledOpts, clientOptsFn)
```

The returned value is a `ConsumerI`; the rest of the loop is identical to Recipe 2.

## Recipe 4: track consumer-group lag

[`ConsumerLag`](consumer_lag.go) polls `kadm.Lag(group)` on a fixed interval and exposes per-(topic, partition) lag through both an in-memory cache (queryable via `Load`) and a user-supplied `LagSinkFn` callback (typically wired to a Prometheus / OTel / zerolog sink).

```go
import "github.com/rs/zerolog"

reader, err := kafka.NewFranzReaderOrdered(readerOpts, clientOptsFn)
if err != nil { /* ... */ }
if err = reader.Connect(ctx); err != nil { /* ... */ }
defer reader.Close(context.Background())

lag := kafka.NewConsumerLag(
    reader.Client,                 // reuse the reader's client (read-only admin requests)
    readerOpts.ConsumerGroup,
    5*time.Second,                 // refresh period
    &zerolog.Logger{},             // optional; nil → zerolog.Nop()
    func(topic string, partition int32, lag int64) {
        // Push to whatever metric backend the application uses:
        // promGauge.WithLabelValues(topic, fmt.Sprint(partition)).Set(float64(lag))
    },
)
lag.Start()
defer lag.Stop()

// Application can also poll the cache directly:
behind := lag.Load("orders", 0)
```

`ConsumerLag` does not touch the reader's poll loop; the `kadm.Client` it constructs internally only issues admin RPCs (broker offsets, committed offsets) which are read-only and don't conflict with consumption. Reusing `reader.Client` avoids opening a second TCP connection.

## Recipe 5: invoke the bundled `pebble kafka` CLI

The package ships a kcat-style CLI at [`cli/`](cli), exposed as a subcommand by downstream consumers (e.g. pebble2impl wires it into `./pebble.sh`). Three nested subcommands cover the common interactive flows.

```bash
# List metadata: brokers, topics, partition leader/replica/ISR.
./pebble.sh kafka list -b 127.0.0.1:9092

# Produce a few records (one per stdin line; -K splits key from value):
echo -e 'k1=v1\nk2=v2' | ./pebble.sh kafka produce -b 127.0.0.1:9092 -t demo -K '='

# Produce from netstring-framed stdin (binary-safe; bytes can contain
# newlines, NULs, commas, anything). Round-trips with --output-mode=netstring:
./pebble.sh kafka consume -b 127.0.0.1:9092 -t src -e --output-mode=netstring \
  | ./pebble.sh kafka produce -b 127.0.0.1:9092 -t dst --input-mode=netstring

# Consume the first 10 records and exit. Format string verbs:
#   %t topic, %p partition, %o offset, %k key, %s value, %T timestamp-ms;
#   \n \t \\ escapes; %% literal.
./pebble.sh kafka consume -b 127.0.0.1:9092 -t demo -c 10 -f '%t/%p:%o key=%k value=%s\n'

# Tail forever; Ctrl+C exits gracefully (commits offsets first when -G is set).
./pebble.sh kafka consume -b 127.0.0.1:9092 -t demo -G my-group

# Run until idle for 3s ("end of log" approximation, useful for scripts).
./pebble.sh kafka consume -b 127.0.0.1:9092 -t demo -e

# CBOR output: one self-delimiting CBOR map per record with full
# metadata (topic, partition, offset, timestamp_ms, key, value, headers).
# Pipe to xxd / cbor2json / your processing pipeline.
./pebble.sh kafka consume -b 127.0.0.1:9092 -t demo -c 100 --output-mode=cbor | xxd

# Netstring output: each record's value framed as `<len>:<bytes>,`.
# Stream-parseable; preserves binary content; the `,` terminator
# distinguishes empty values (`0:,`) from end-of-stream.
./pebble.sh kafka consume -b 127.0.0.1:9092 -t demo -c 100 --output-mode=netstring
```

Environment variables `PEBBLE_KAFKA_BROKERS` and `PEBBLE_KAFKA_CLIENT_ID` set per-flag defaults so scripts can `export PEBBLE_KAFKA_BROKERS=…` once and stay terse.

### SASL authentication

```bash
# PLAIN auth (env vars recommended for the password — keeps it out of shell history):
PEBBLE_KAFKA_SASL_PASSWORD=s3cret \
  ./pebble.sh kafka list -b broker:9093 \
    --sasl-mechanism=PLAIN --sasl-username=alice

# SCRAM-SHA-512:
./pebble.sh kafka list -b broker:9093 \
    --sasl-mechanism=SCRAM-SHA-512 \
    --sasl-username=alice --sasl-password=s3cret

# OAUTHBEARER (static token):
./pebble.sh kafka list -b broker:9093 \
    --sasl-mechanism=OAUTHBEARER --sasl-token="${BEARER_TOKEN}"
```

Supported mechanisms: `PLAIN`, `SCRAM-SHA-256`, `SCRAM-SHA-512`, `OAUTHBEARER`. Case-insensitive. Empty / `none` disables SASL.

### TLS

```bash
# TLS-only (system CA pool):
./pebble.sh kafka list -b broker:9093 --tls

# TLS with custom CA bundle:
./pebble.sh kafka list -b broker:9093 \
    --tls-ca-file=/etc/redpanda/ca.crt

# Insecure dev-cluster (self-signed certs):
./pebble.sh kafka list -b broker:9093 \
    --tls-ca-file=/etc/redpanda/ca.crt \
    --tls-skip-verify

# Mutual TLS (client cert + key):
./pebble.sh kafka list -b broker:9093 \
    --tls-ca-file=/etc/redpanda/ca.crt \
    --tls-cert-file=/etc/redpanda/client.crt \
    --tls-key-file=/etc/redpanda/client.key

# SASL_SSL (SASL on top of TLS) — common for managed clusters:
PEBBLE_KAFKA_SASL_PASSWORD=s3cret \
  ./pebble.sh kafka list -b broker:9093 \
    --tls --sasl-mechanism=SCRAM-SHA-512 --sasl-username=alice
```

Any `--tls-*` file flag implies `--tls`; you don't have to pass both. Min TLS version is 1.2.

For the IPv4/IPv6 `localhost` gotcha some Podman setups hit, prefer `127.0.0.1:PORT` over `localhost:PORT` when targeting a podman-rootless Kafka container.

## Recipe 6: run the integration tests against Podman

The package's [`integration_test.go`](integration_test.go) is gated behind the `integration` build tag and requires Docker or Podman to spin up a `redpandadata/redpanda` container.

### Podman (rootless)

```bash
# Make sure the user-level Podman socket is running:
systemctl --user start podman.socket

# Run the tests:
DOCKER_HOST=unix:///run/user/$UID/podman/podman.sock \
TESTCONTAINERS_RYUK_DISABLED=true \
  go test -tags "$(cat tags | tr -d $'\n'),integration" -count=1 -v -timeout 5m \
    ./public/streaming/persisted/kafka/...
```

`TESTCONTAINERS_RYUK_DISABLED=true` is required because rootless Podman cannot reliably co-host the Ryuk reaper container that testcontainers-go uses for resource cleanup; with Ryuk disabled, container teardown is handled by the test's `t.Cleanup` instead.

### Docker

```bash
go test -tags "$(cat tags | tr -d $'\n'),integration" -count=1 -v -timeout 5m \
  ./public/streaming/persisted/kafka/...
```

No env-var gymnastics needed.

### Expected runtime

About 11–12 seconds end-to-end on a warm machine: ~2s for the connectivity test (one container), and ~9s for the produce/consume roundtrip (one shared container, two subtests).

## Common pitfalls

- **Zero-value `FranzConnectionDetails`.** Constructing the struct literal with only `SeedBrokers` set leaves `RequestTimeoutOverhead` at zero, which kgo would reject (`kgo.RequestTimeoutOverhead` requires ≥100ms). [`FranzConnectionDetails.FranzOpts`](franz_client.go) suppresses zero-value duration options to fall back to kgo's defaults, but using [`DefaultFranzConnectionDetails`](franz_client.go) is the recommended starting point.
- **Forgetting `batch.Ack`.** A consumer that never acks stalls the partition; the consumer-group session times out and rebalance evicts the consumer. The package does not detect this — see [`EXPLANATION.md` §"Strict in-order, exactly-once-call"](EXPLANATION.md#strict-in-order-exactly-once-call).
- **Sharing `*kgo.Client` across reader and writer.** The producer accepts a caller-supplied client. The readers do not (yet); they construct their own client internally via the `clientOpts` factory. A consumer + producer pair therefore opens two `*kgo.Client` instances. A `NewFranzReaderFromClient` variant would close this gap; see [`EXPLANATION.md` §"Open today"](EXPLANATION.md#open-today).
- **Dropping the `kgo.Record.Value` for tombstones.** A nil `Value` is a valid Kafka tombstone (delete-marker). The producer and readers preserve nil; application code must not collapse `nil` and `[]byte{}` accidentally.
