//go:build llm_generated_opus47

// Copyright 2024 Redpanda Data, Inc.
// Copyright 2026 Panos Stergiotis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Modifications applied during port (see ./NOTICE):
//   - Dropped the entire Benthos config-DSL surface: kfwField*
//     constants for both producer-limit and per-record fields,
//     FranzProducerLimitsFields, FranzProducerFields,
//     FranzProducerLimitsOptsFromConfig, FranzProducerOptsFromConfig,
//     FranzWriterConfigFields, FranzWriterConfigLints,
//     NewFranzWriterFromConfig.
//   - Dropped the per-record interpolation surface (Topic / Key /
//     Partition / Timestamp / MetaFilter as service.InterpolatedString /
//     service.MetadataFilter). The pebble2impl producer takes
//     *kgo.Record values directly per ADR-0015 / EXPLANATION.md;
//     applications populate Topic, Key, Value, Headers, Partition,
//     and Timestamp on each record themselves.
//   - Dropped the franzWriterHooks indirection (accessClientFn /
//     yieldClientFn / FranzSharedClientUseFn) and the
//     franz_shared_client.go registry pattern. The hooks existed so
//     Benthos input and output components — instantiated independently
//     from YAML — could share a *kgo.Client through a name-keyed
//     registry on *service.Resources. In this port the application
//     constructs the *kgo.Client (typically via NewFranzClient) and
//     passes it to NewFranzWriter directly; ownership stays with the
//     caller and Close does not tear it down.
//   - Dropped MessageBatchToFranzRecords, DecorateRecord, and the
//     SkipRecord sentinel (all customisation hooks for the
//     migrator-style record-rewriting path). Upstream's
//     dispatch.TriggerSignal calls go away with them.
//   - Dropped ConnectionTest. Callers invoke Ping themselves through
//     the *kgo.Client they constructed.
//   - Dropped the manual Produce + sync.WaitGroup fan-out in
//     batchWriter.writeBatch in favour of kgo.Client.ProduceSync,
//     which is functionally identical (waits for all produces; joins
//     errors) and roughly half the code.
//   - Refactored to boxer coding standards: receiver `inst`, named
//     return values, eh.Errorf wrapping, sized integer fields where
//     the upstream used uint64 (kgo's underlying field types are
//     int32 for batch/write byte caps), compile-time interface
//     assertion.

package kafka

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/twmb/franz-go/pkg/kgo"
)

// FranzProducerOpts captures the producer-side knobs that map to
// kgo.Opt values. All zero values are safe to use; the helpers in
// [DefaultFranzProducerOpts] match the upstream Connect plugin
// defaults.
type FranzProducerOpts struct {
	// Timeout is the per-produce-request timeout. Maps to
	// kgo.ProduceRequestTimeout.
	Timeout time.Duration

	// MaxMessageBytes bounds a single produced record batch (in bytes).
	// Maps to kgo.ProducerBatchMaxBytes. The Kafka broker's
	// max.message.bytes (or kafka_batch_max_bytes on Redpanda) must
	// be at least this value, otherwise the broker returns
	// MESSAGE_TOO_LARGE.
	MaxMessageBytes int32

	// BrokerWriteMaxBytes bounds the bytes written to a broker
	// connection in a single write. Maps to kgo.BrokerMaxWriteBytes.
	// The broker's socket.request.max.bytes must be at least this
	// value.
	BrokerWriteMaxBytes int32

	// Partitioner selects the partitioner. nil leaves the franz-go
	// default (StickyKeyPartitioner with murmur2 hashing). Use
	// kgo.RoundRobinPartitioner(), kgo.LeastBackupPartitioner(),
	// or kgo.ManualPartitioner() to override; the latter requires
	// every produced *kgo.Record to set Partition explicitly.
	Partitioner kgo.Partitioner

	// CompressionPrefs lists compression codecs in priority order;
	// the first one supported by the broker is used. nil leaves
	// kgo's default (snappy if supported, else uncompressed). Use
	// kgo.{Snappy,Lz4,Gzip,Zstd,No}Compression() to populate.
	CompressionPrefs []kgo.CompressionCodec

	// DisableIdempotent flips off the idempotent producer (default:
	// idempotent enabled, exactly-once-per-partition semantics on
	// retries). Set to true only when the cluster does not grant
	// the producer CLUSTER:IDEMPOTENT_WRITE — disabling means
	// duplicates may occur on producer retries.
	DisableIdempotent bool

	// AllowAutoTopicCreation requests broker-side auto-creation of
	// topics referenced in metadata fetches. Maps to
	// kgo.AllowAutoTopicCreation.
	AllowAutoTopicCreation bool
}

// DefaultFranzProducerOpts returns producer options matching the
// upstream Connect plugin defaults: 10s request timeout, 1 MiB max
// message bytes, 100 MiB broker write max bytes, idempotent
// enabled, auto-topic-creation enabled.
func DefaultFranzProducerOpts() (opts FranzProducerOpts) {
	opts = FranzProducerOpts{
		Timeout:                10 * time.Second,
		MaxMessageBytes:        1 * 1024 * 1024,
		BrokerWriteMaxBytes:    100 * 1024 * 1024,
		AllowAutoTopicCreation: true,
	}
	return
}

// FranzOpts returns the kgo.Opt slice that configures a producer
// described by inst. The slice is freshly allocated; the caller may
// append further options before passing it to [NewFranzClient] or
// kgo.NewClient. Connection-level options from
// [FranzConnectionDetails.FranzOpts] are NOT included; combine the
// two when building the client option set.
func (inst *FranzProducerOpts) FranzOpts() (opts []kgo.Opt) {
	opts = []kgo.Opt{
		kgo.ProduceRequestTimeout(inst.Timeout),
		kgo.ProducerBatchMaxBytes(inst.MaxMessageBytes),
		kgo.BrokerMaxWriteBytes(inst.BrokerWriteMaxBytes),
	}
	if inst.Partitioner != nil {
		opts = append(opts, kgo.RecordPartitioner(inst.Partitioner))
	}
	if len(inst.CompressionPrefs) > 0 {
		opts = append(opts, kgo.ProducerBatchCompression(inst.CompressionPrefs...))
	}
	if inst.DisableIdempotent {
		opts = append(opts, kgo.DisableIdempotentWrite())
	}
	if inst.AllowAutoTopicCreation {
		opts = append(opts, kgo.AllowAutoTopicCreation())
	}
	return
}

//------------------------------------------------------------------------------

// FranzWriter implements [ProducerI] over a caller-supplied
// *kgo.Client. Ownership of the client stays with the caller:
// Close flushes pending produces but does not Close the client.
//
// This shape lets a single application share one client across a
// reader and a writer (or multiple writers) without the registry
// indirection used upstream. Construct the client once, hand it to
// every component that needs it, and Close the client when the
// application shuts down.
type FranzWriter struct {
	client *kgo.Client
	log    *zerolog.Logger
}

// NewFranzWriter wraps an existing *kgo.Client. logger may be nil;
// the writer itself logs nothing today, but a future implementation
// may surface produce-error context through it.
func NewFranzWriter(client *kgo.Client, logger *zerolog.Logger) (inst *FranzWriter, err error) {
	if client == nil {
		err = eh.Errorf("client must not be nil")
		return
	}
	if logger == nil {
		nop := zerolog.Nop()
		logger = &nop
	}
	inst = &FranzWriter{client: client, log: logger}
	return
}

// Connect verifies the client is reachable via Ping. The client may
// already be reachable from prior NewFranzClient construction; this
// call exists to satisfy the ProducerI lifecycle and is safe to
// invoke repeatedly.
func (inst *FranzWriter) Connect(ctx context.Context) (err error) {
	if inst.client == nil {
		err = ErrNotConnected
		return
	}
	err = inst.client.Ping(ctx)
	if err != nil {
		err = eh.Errorf("kafka writer ping: %w", err)
	}
	return
}

// Write produces the records synchronously: it returns only after
// every record has been acknowledged by the broker (or the context
// fires). Errors from individual produces are joined via
// errors.Join and returned as a single error; nil means every
// record was acknowledged.
//
// An empty records argument is a no-op returning nil. Records with
// no Topic or no Value are passed through to franz-go, which
// rejects them with a per-record error surfaced through the joined
// return.
func (inst *FranzWriter) Write(ctx context.Context, records ...*kgo.Record) (err error) {
	if inst.client == nil {
		err = ErrNotConnected
		return
	}
	if len(records) == 0 {
		return
	}
	results := inst.client.ProduceSync(ctx, records...)
	err = results.FirstErr()
	if err != nil {
		err = eh.Errorf("kafka write: %w", err)
	}
	return
}

// Close flushes pending produces. It does NOT close the underlying
// *kgo.Client — the caller owns the client and is responsible for
// its lifecycle. After Close returns, further calls to Write will
// observe the same client and may still succeed; the only effect
// of Close is to drain in-flight produces under ctx.
func (inst *FranzWriter) Close(ctx context.Context) (err error) {
	if inst.client == nil {
		return
	}
	err = inst.client.Flush(ctx)
	if err != nil {
		err = eh.Errorf("kafka writer flush: %w", err)
	}
	return
}

var _ ProducerI = (*FranzWriter)(nil)
