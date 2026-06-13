// Copyright 2025 Redpanda Data, Inc.
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
//   - Replaced *service.MetricGauge with a LagSinkFn callback. Callers
//     wire whatever metric backend they want (Prometheus, OTel, plain
//     logging); nil disables emission. The package does not commit to
//     a metrics interface yet — see EXPLANATION.md §"Open today".
//   - Replaced *service.Logger with *zerolog.Logger; nil-safe via
//     zerolog.Nop() default in the constructor.
//   - Replaced redpanda-data/connect/v4/internal/asyncroutine.Periodic
//     with stdlib time.Ticker + a context-cancelled goroutine. The
//     refresh-on-tick semantics are identical; the asyncroutine package
//     is internal and not re-usable.
//   - Refactored to boxer coding standards: receiver `inst`, named
//     return values, sized integer fields.
//   - Refresh logic, lag-cache key shape, max(lag,0) clamping, and the
//     kadm.Client usage preserved verbatim.

package kafka

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// LagSinkFn is the per-(topic, partition) callback the consumer-lag
// tracker invokes once per refresh tick for every partition the group
// is consuming. lag is the broker-reported gauge value (records),
// clamped at 0. nil is a valid LagSinkFn — pass it to
// [NewConsumerLag] to skip metric emission entirely while still
// populating the in-memory cache for [ConsumerLag.Load].
type LagSinkFn func(topic string, partition int32, lag int64)

// ConsumerLag tracks consumer-group lag asynchronously by polling
// kadm.Lag(consumerGroup) on a fixed interval. Each refresh updates an
// internal cache (queryable via [ConsumerLag.Load]) and, if a
// [LagSinkFn] was supplied at construction, fans out the per-partition
// lag through that callback.
//
// Safe lifecycle is single-goroutine: call Start once, Stop once.
// Concurrent Start/Stop is not detected.
type ConsumerLag struct {
	client        *kgo.Client
	consumerGroup string
	refreshPeriod time.Duration
	log           *zerolog.Logger
	sink          LagSinkFn
	topicLagCache sync.Map

	cancel context.CancelFunc
	done   chan struct{}
}

// NewConsumerLag constructs a ConsumerLag. The supplied client is used
// only as a transport for kadm admin requests (Lag); the request volume
// is one round-trip per refreshPeriod, so reusing a reader's
// [FranzReaderOrdered.Client] / [FranzReaderUnordered.Client] is
// idiomatic. Logger may be nil; sink may be nil.
func NewConsumerLag(client *kgo.Client, consumerGroup string, refreshPeriod time.Duration, logger *zerolog.Logger, sink LagSinkFn) (inst *ConsumerLag) {
	if logger == nil {
		nop := zerolog.Nop()
		logger = &nop
	}
	inst = &ConsumerLag{
		client:        client,
		consumerGroup: consumerGroup,
		refreshPeriod: refreshPeriod,
		log:           logger,
		sink:          sink,
	}
	return
}

// Start launches the refresh goroutine. Calling Start more than once
// without an intervening Stop leaks the prior goroutine; the package
// does not detect this.
func (inst *ConsumerLag) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	inst.cancel = cancel
	inst.done = make(chan struct{})
	go inst.run(ctx)
}

// Stop signals the refresh goroutine to exit and blocks until it
// returns. Safe to call multiple times.
func (inst *ConsumerLag) Stop() {
	if inst.cancel == nil {
		return
	}
	inst.cancel()
	<-inst.done
	inst.cancel = nil
}

// Load returns the most recent lag observed for the given
// (topic, partition). Returns 0 when no observation has been recorded
// yet — callers cannot distinguish "no data" from "actually caught up";
// pair Load with knowledge of whether Start has had time for at least
// one refresh tick.
func (inst *ConsumerLag) Load(topic string, partition int32) (lag int64) {
	val, ok := inst.topicLagCache.Load(cacheKey(topic, partition))
	if !ok {
		return
	}
	lag, _ = val.(int64)
	return
}

func (inst *ConsumerLag) run(ctx context.Context) {
	defer close(inst.done)
	t := time.NewTicker(inst.refreshPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			inst.refresh(ctx)
		}
	}
}

// refresh polls kadm.Lag once and updates both the cache and the sink.
// Errors are logged at debug level (matching upstream's posture: the
// lag gauge is best-effort observability, not a correctness primitive).
func (inst *ConsumerLag) refresh(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, inst.refreshPeriod)
	defer cancel()

	adm := kadm.NewClient(inst.client)
	lags, err := adm.Lag(ctx, inst.consumerGroup)
	if err != nil {
		inst.log.Debug().Err(err).Str("group", inst.consumerGroup).Msg("kafka: lag refresh failed")
		return
	}
	lags.Each(func(gl kadm.DescribedGroupLag) {
		for _, topicLag := range gl.Lag {
			for _, pl := range topicLag {
				lag := pl.Lag
				if lag < 0 {
					lag = 0
				}
				inst.topicLagCache.Store(cacheKey(pl.Topic, pl.Partition), lag)
				if inst.sink != nil {
					inst.sink(pl.Topic, pl.Partition, lag)
				}
			}
		}
	})
}

// cacheKey builds the cache lookup key. Format matches upstream:
// "topic_partition" (single underscore separator). The string concat
// allocates per call; refresh runs at the configured period (5s by
// default), so allocation cost is negligible. Callers using Load on a
// hot path should cache the result themselves.
func cacheKey(topic string, partition int32) (key string) {
	key = fmt.Sprintf("%s_%s", topic, strconv.Itoa(int(partition)))
	return
}
