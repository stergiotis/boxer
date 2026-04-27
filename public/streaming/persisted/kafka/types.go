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
//   - Replaced service.MessageBatch / service.Message with the Batch
//     struct here, holding kgo.Fetches directly to avoid wrapping cost.
//   - Replaced service.AckFunc with AckFn; the (ctx, processErr) shape
//     is preserved verbatim from upstream so the franz-reader port can
//     keep its closure form, even though both arguments are advisory in
//     the upstream contract (see EXPLANATION.md §"Ack contract").
//   - Added ConsumerI / ProducerI as pebble2impl-local seams; upstream
//     used service.Input / service.Output for the same role.

package kafka

import (
	"context"
	"errors"

	"github.com/twmb/franz-go/pkg/kgo"
)

// ErrNotConnected is returned by ConsumerI.Read and ProducerI.Write when
// the underlying client has not yet been brought up via Connect, or when
// it has already been torn down via Close. Callers should treat this as
// a transient signal during construction and a terminal signal after
// teardown; the boundary between the two is the caller's responsibility.
//
// The sentinel mirrors service.ErrNotConnected from the upstream Benthos
// service framework so error-handling idioms ported alongside the
// reader/writer compile unchanged.
var ErrNotConnected = errors.New("kafka: client not connected")

// AckFn is the per-batch acknowledgement callback returned by
// [ConsumerI.Read]. The caller invokes it once after the batch has been
// fully processed; the underlying reader uses the call as a notification
// to advance committed offsets and release the batch's slot in any
// internal back-pressure cache.
//
// The (ctx, processErr) shape preserves the upstream Connect signature
// for closure compatibility with the ported reader. Today both arguments
// are advisory: the franz-go-derived reader ignores ctx and processErr,
// always returns nil, and propagates errors out-of-band through the
// reader's own Connect/Close lifecycle. Future implementations may
// honour either argument; callers MUST therefore pass a real ctx and a
// faithful processErr regardless of the present semantics. See
// EXPLANATION.md §"Ack contract" for the full discussion and the strict
// in-order constraint.
type AckFn func(ctx context.Context, processErr error) (err error)

// Batch is the unit returned by [ConsumerI.Read]: a non-empty
// [github.com/twmb/franz-go/pkg/kgo.Fetches] together with the
// acknowledgement callback that releases its slot in the reader's
// back-pressure window.
//
// Records is exposed as the kgo type directly rather than wrapped in an
// envelope interface. The package treats *kgo.Record as the message
// envelope; rationale is in EXPLANATION.md §"Why concrete types".
//
// Iteration follows kgo's vocabulary — callers may use
// Records.RecordsAll() for an iter.Seq[*kgo.Record], or
// Records.EachRecord / Records.EachPartition for the structured
// callback variants.
//
// Ack must be called exactly once per Batch. Callers that wish to
// abort processing without committing must still invoke Ack with the
// processing error so the reader can release back-pressure capacity;
// the reader will not advance committed offsets for an aborted batch
// when a future implementation honours processErr.
type Batch struct {
	Records kgo.Fetches
	Ack     AckFn
}

// ConsumerI is the seam through which application code consumes Kafka
// records via the franz-go-derived readers in this package. The three
// concrete implementations (ordered, unordered, toggled — landing in
// later phases) all satisfy this interface; callers should depend on
// the interface, not the concrete type, except where a strategy-specific
// option matters.
//
// Lifecycle: Connect must be called before Read; Read may return
// [ErrNotConnected] if the precondition is violated. Close is safe to
// call from any state and is idempotent. Read blocks until at least one
// record is available, the context is cancelled, or a terminal reader
// error occurs.
type ConsumerI interface {
	Connect(ctx context.Context) (err error)
	Read(ctx context.Context) (b Batch, err error)
	Close(ctx context.Context) (err error)
}

// ProducerI is the seam through which application code produces Kafka
// records via the franz-go-derived writer.
//
// Lifecycle mirrors [ConsumerI]: Connect → Write* → Close. Write is
// variadic over *kgo.Record so single-record Publish and batched
// produce share one entry point; the underlying franz-go client
// batches internally according to its kgo.ProducerBatchMaxBytes
// configuration regardless of how the caller groups arguments.
//
// Write returns nil only when every record in the call has been
// acknowledged by the broker (ProduceSync semantics). Idempotent and
// transactional modes are configured on the underlying client; the
// interface is unaware of them.
type ProducerI interface {
	Connect(ctx context.Context) (err error)
	Write(ctx context.Context, records ...*kgo.Record) (err error)
	Close(ctx context.Context) (err error)
}
