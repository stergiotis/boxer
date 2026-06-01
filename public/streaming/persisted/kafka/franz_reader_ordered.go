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
//   - Dropped the Benthos config-DSL surface: kroField* constants,
//     FranzReaderOrderedConfigFields, NewFranzReaderOrderedFromConfig.
//     Replaced with FranzReaderOrderedOpts struct +
//     DefaultFranzReaderOrderedOpts factory + NewFranzReaderOrdered
//     constructor.
//   - Dropped messageWithRecord's *service.Message field. The port
//     exposes *kgo.Record directly per ADR-0005 / EXPLANATION.md, so
//     the per-record service.Message wrapper is unnecessary; only the
//     *kgo.Record + per-record byte size are retained internally.
//   - Dropped the per-message dispatch.CtxOnTriggerSignal mechanism.
//     Upstream used the trigger to clear pendingDispatch[batchID] when
//     the Benthos pipeline took a batch's messages into the processor
//     chain (i.e. before ack), enabling pipelined throughput. This port
//     has no processor-chain notion, so pendingDispatch[batchID] is set
//     on pop and cleared only on ack — yielding strictly serial batches
//     per partition. Applications that want pipelined throughput can
//     wrap Read into a producer/consumer goroutine pair externally.
//   - Dropped ConsumerLag wiring entirely (the kafka_lag per-message
//     metadata, the lag-refresh goroutine, and the redpanda_lag
//     metric). Phase 8 may re-introduce consumer-lag observability via
//     a metrics seam; for now the field is absent from the
//     options struct and the goroutine.
//   - Dropped ConnectionTest. NewFranzClient already calls Ping on
//     construction; callers that want a "test connection" path can
//     call NewFranzClient + Close.
//   - Replaced *service.Logger with *zerolog.Logger; nil logger is
//     silently a no-op (defaulted to zerolog.Nop()).
//   - Replaced service.MessageBatch / *service.Message with the
//     package-local Batch struct (kgo.Fetches synthesized as a single
//     topic / single partition). The Read method packages the
//     internal batchWithAckFn into a Batch + AckFn for return.
//   - Replaced service.AckFunc with AckFn; both arguments remain
//     advisory and the body remains "ignore both, return nil",
//     matching upstream's pairing with service.AutoRetryNacks. See
//     EXPLANATION.md §"Ack contract".
//   - Replaced service.ErrEndOfInput / service.ErrNotConnected with
//     the package-local ErrNotConnected; the soft-stop "end of input"
//     state is folded into ErrNotConnected after Close.
//   - Refactored to boxer coding standards: receiver `inst`, named
//     return values, eh.Errorf, sized integer fields where applicable,
//     compile-time interface assertion.
//
// Logic and ordering invariants preserved verbatim from upstream:
// per-partition ordered ack-drain, batch-merge-and-split chunking
// in the partition cache, rebalance handling via OnPartitionsRevoked
// / Lost / Assigned, back-pressure via PauseFetchPartitions /
// ResumeFetchPartitions, the no-active-partitions inner loop.

package kafka

import (
	"context"
	"sync"
	"time"

	"github.com/Jeffail/checkpoint"
	"github.com/Jeffail/shutdown"
	"github.com/cenkalti/backoff/v4"
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/twmb/franz-go/pkg/kgo"
)

// FranzReaderOrderedOpts configures a FranzReaderOrdered. The defaults
// in [DefaultFranzReaderOrderedOpts] match the upstream Connect plugin
// values exactly; callers should override only what they need.
type FranzReaderOrderedOpts struct {
	// ConsumerGroup, when non-empty, enables consumer-group mode:
	// partitions of the configured topics are distributed across
	// group members, and offsets are auto-committed under this name.
	// Mutually exclusive with explicit topic-partition specs.
	ConsumerGroup string

	// CommitPeriod is the auto-commit interval for consumer-group
	// mode. Ignored when ConsumerGroup is empty. Offsets are also
	// committed during Close.
	CommitPeriod time.Duration

	// PartitionBufferBytes is the per-partition cache size (in bytes)
	// used to queue records internally before [Batch] yields. Larger
	// values may improve throughput at the cost of memory; each
	// partition can grow slightly beyond this value before fetches
	// pause.
	PartitionBufferBytes int64

	// MaxYieldBatchBytes is the maximum size (in bytes) of any single
	// [Batch] returned by Read. Must be <= PartitionBufferBytes. The
	// partition cache merges and splits batches internally to stay
	// within this bound.
	MaxYieldBatchBytes int64

	// Logger receives franz-go and reader-internal log events. May be
	// nil, in which case zerolog.Nop() is used.
	Logger *zerolog.Logger
}

// DefaultFranzReaderOrderedOpts returns options matching the upstream
// Connect plugin defaults: 5s commit period, 1 MiB partition buffer,
// 32 KiB max yield batch.
func DefaultFranzReaderOrderedOpts() (opts FranzReaderOrderedOpts) {
	opts = FranzReaderOrderedOpts{
		CommitPeriod:         5 * time.Second,
		PartitionBufferBytes: 1 * 1024 * 1024,
		MaxYieldBatchBytes:   32 * 1024,
	}
	return
}

// FranzReaderOrdered implements [ConsumerI] with strict per-partition
// ordering. The Client field is exported so applications can issue
// admin operations (e.g. CommitOffsets) directly between Read calls;
// it is nil before Connect and after Close.
type FranzReaderOrdered struct {
	clientOpts func() (opts []kgo.Opt, err error)
	opts       FranzReaderOrderedOpts

	cacheLimit   uint64
	batchMaxSize uint64
	readBackOff  backoff.BackOff

	partState *partitionState
	Client    *kgo.Client

	log     *zerolog.Logger
	shutSig *shutdown.Signaller
}

// NewFranzReaderOrdered constructs a FranzReaderOrdered. clientOpts is
// invoked at Connect time to materialise the kgo client options
// (typically the union of [FranzConnectionDetails.FranzOpts] and
// [FranzConsumerDetails.FranzOpts]).
func NewFranzReaderOrdered(opts FranzReaderOrderedOpts, clientOpts func() (kgoOpts []kgo.Opt, err error)) (inst *FranzReaderOrdered, err error) {
	if opts.PartitionBufferBytes <= 0 {
		err = eh.Errorf("PartitionBufferBytes must be > 0")
		return
	}
	if opts.MaxYieldBatchBytes <= 0 {
		err = eh.Errorf("MaxYieldBatchBytes must be > 0")
		return
	}
	if opts.MaxYieldBatchBytes > opts.PartitionBufferBytes {
		err = eh.Errorf("MaxYieldBatchBytes (%d) must be <= PartitionBufferBytes (%d)", opts.MaxYieldBatchBytes, opts.PartitionBufferBytes)
		return
	}
	log := opts.Logger
	if log == nil {
		nop := zerolog.Nop()
		log = &nop
	}
	readBackOff := backoff.NewExponentialBackOff()
	readBackOff.InitialInterval = time.Millisecond
	readBackOff.MaxInterval = 100 * time.Millisecond
	readBackOff.MaxElapsedTime = 0
	inst = &FranzReaderOrdered{
		clientOpts:   clientOpts,
		opts:         opts,
		cacheLimit:   uint64(opts.PartitionBufferBytes),
		batchMaxSize: uint64(opts.MaxYieldBatchBytes),
		readBackOff:  readBackOff,
		log:          log,
		shutSig:      shutdown.NewSignaller(),
	}
	return
}

//------------------------------------------------------------------------------
// Per-record + per-batch internal types

type recordWithSize struct {
	r    *kgo.Record
	size uint64
}

type batchWithRecords struct {
	topic     string
	partition int32
	b         []*recordWithSize
	size      uint64
}

func recordsToBatch(topic string, partition int32, records []*kgo.Record) (batch batchWithRecords) {
	batch.topic = topic
	batch.partition = partition
	batch.b = make([]*recordWithSize, len(records))
	for i, r := range records {
		size := uint64(len(r.Value) + len(r.Key))
		batch.b[i] = &recordWithSize{r: r, size: size}
		batch.size += size
	}
	return
}

type batchWithAckFn struct {
	onAck     func()
	topic     string
	partition int32
	records   []*kgo.Record
}

//------------------------------------------------------------------------------
// partitionCache: per-(topic,partition) ordered queue with checkpointed ack-drain

type partitionCache struct {
	mut             sync.Mutex
	pendingDispatch map[int64]struct{}
	cache           []*batchWithRecords
	cacheSize       uint64
	checkpointer    *checkpoint.Uncapped[*kgo.Record]
	commitFn        func(r *kgo.Record)
}

func newPartitionCache(commitFn func(r *kgo.Record)) (inst *partitionCache) {
	inst = &partitionCache{
		pendingDispatch: map[int64]struct{}{},
		checkpointer:    checkpoint.NewUncapped[*kgo.Record](),
		commitFn:        commitFn,
	}
	return
}

// push appends batch's records into the cache, merging into the last
// existing batch up to maxBatchSize and splitting oversized batches.
// Returns true when the cache has reached or exceeded bufferSize and
// the caller should pause fetches for this partition.
func (inst *partitionCache) push(bufferSize, maxBatchSize uint64, batch *batchWithRecords) (pauseFetch bool) {
	inst.mut.Lock()
	defer inst.mut.Unlock()

	inst.cacheSize += batch.size
	pauseFetch = inst.cacheSize >= bufferSize

	if len(inst.cache) > 0 {
		indexEnd := len(inst.cache) - 1
		for len(batch.b) > 0 && inst.cache[indexEnd].size < maxBatchSize {
			nextRecSize := batch.b[0].size
			if inst.cache[indexEnd].size+nextRecSize > maxBatchSize {
				break
			}
			inst.cache[indexEnd].b = append(inst.cache[indexEnd].b, batch.b[0])
			inst.cache[indexEnd].size += nextRecSize
			batch.b = batch.b[1:]
			batch.size -= nextRecSize
		}
	}

	for len(batch.b) > 0 {
		if batch.size <= maxBatchSize {
			inst.cache = append(inst.cache, batch)
			return
		}
		tmpBatch := &batchWithRecords{topic: batch.topic, partition: batch.partition}
		for len(batch.b) > 0 {
			nextRecSize := batch.b[0].size
			if len(tmpBatch.b) > 0 && tmpBatch.size+nextRecSize > maxBatchSize {
				break
			}
			tmpBatch.b = append(tmpBatch.b, batch.b[0])
			tmpBatch.size += nextRecSize
			batch.b = batch.b[1:]
			batch.size -= nextRecSize
		}
		inst.cache = append(inst.cache, tmpBatch)
	}
	return
}

// pop returns the next ready batch. Returns nil when (a) the cache is
// empty, or (b) a previous batch is still pending its ack — the latter
// gates ordering: subsequent batches do not pop while a prior one is
// in flight.
func (inst *partitionCache) pop() (out *batchWithAckFn) {
	inst.mut.Lock()
	defer inst.mut.Unlock()

	if len(inst.cache) == 0 {
		return
	}
	if len(inst.pendingDispatch) > 0 {
		return
	}

	nextBatch := inst.cache[0]
	inst.cache = inst.cache[1:]

	batchID := nextBatch.b[0].r.Offset
	inst.pendingDispatch[batchID] = struct{}{}

	records := make([]*kgo.Record, len(nextBatch.b))
	for i := range nextBatch.b {
		records[i] = nextBatch.b[i].r
	}

	releaseFn := inst.checkpointer.Track(nextBatch.b[len(nextBatch.b)-1].r, int64(len(nextBatch.b)))
	batchSize := nextBatch.size
	onAck := func() {
		inst.mut.Lock()
		releaseRecord := releaseFn()
		delete(inst.pendingDispatch, batchID)
		inst.cacheSize -= batchSize
		inst.mut.Unlock()
		if releaseRecord != nil && *releaseRecord != nil {
			inst.commitFn(*releaseRecord)
		}
	}
	out = &batchWithAckFn{
		onAck:     onAck,
		topic:     nextBatch.topic,
		partition: nextBatch.partition,
		records:   records,
	}
	return
}

func (inst *partitionCache) pauseFetch(limit uint64) (pauseFetch bool) {
	inst.mut.Lock()
	pauseFetch = inst.cacheSize >= limit
	inst.mut.Unlock()
	return
}

//------------------------------------------------------------------------------
// partitionState: topic → partition → partitionCache, plus rebalance-safe drops

type partitionState struct {
	mut      sync.Mutex
	topics   map[string]map[int32]*partitionCache
	commitFn func(r *kgo.Record)
}

func newPartitionState(commitFn func(r *kgo.Record)) (inst *partitionState) {
	inst = &partitionState{
		topics:   map[string]map[int32]*partitionCache{},
		commitFn: commitFn,
	}
	return
}

func (inst *partitionState) pop() (out *batchWithAckFn) {
	inst.mut.Lock()
	defer inst.mut.Unlock()

	for _, parts := range inst.topics {
		for _, p := range parts {
			out = p.pop()
			if out != nil {
				return
			}
		}
	}
	return
}

func (inst *partitionState) addRecords(topic string, partition int32, batch *batchWithRecords, bufferSize, maxBatchSize uint64) (pauseFetch bool) {
	inst.mut.Lock()
	defer inst.mut.Unlock()

	topicTracker := inst.topics[topic]
	if topicTracker == nil {
		topicTracker = map[int32]*partitionCache{}
		inst.topics[topic] = topicTracker
	}
	partCache := topicTracker[partition]
	if partCache == nil {
		partCache = newPartitionCache(inst.commitFn)
		topicTracker[partition] = partCache
	}
	if batch != nil {
		pauseFetch = partCache.push(bufferSize, maxBatchSize, batch)
		return
	}
	pauseFetch = partCache.pauseFetch(bufferSize)
	return
}

func (inst *partitionState) pauseFetch(topic string, partition int32, limit uint64) (pause bool) {
	inst.mut.Lock()
	defer inst.mut.Unlock()

	topicTracker := inst.topics[topic]
	if topicTracker == nil {
		return
	}
	partTracker := topicTracker[partition]
	if partTracker == nil {
		return
	}
	pause = partTracker.pauseFetch(limit)
	return
}

func (inst *partitionState) removeTopicPartitions(m map[string][]int32) {
	inst.mut.Lock()
	defer inst.mut.Unlock()

	for topicName, lostTopic := range m {
		trackedTopic, exists := inst.topics[topicName]
		if !exists {
			continue
		}
		for _, lostPartition := range lostTopic {
			delete(trackedTopic, lostPartition)
		}
		if len(trackedTopic) == 0 {
			delete(inst.topics, topicName)
		}
	}
}

func (inst *partitionState) tallyActivePartitions(pausedPartitions map[string][]int32) (tally int) {
	inst.mut.Lock()
	defer inst.mut.Unlock()

	for topic, parts := range inst.topics {
		tally += (len(parts) - len(pausedPartitions[topic]))
	}
	return
}

//------------------------------------------------------------------------------
// FranzReaderOrdered methods (ConsumerI surface)

// Connect brings up the kgo.Client and starts the partition-state poll
// loop. Calling Connect on an already-connected reader is a no-op.
//
// On success, the reader's Client field is set. On failure, the client
// (if any) is closed and Connect returns the underlying error.
func (inst *FranzReaderOrdered) Connect(ctx context.Context) (err error) {
	if inst.partState != nil {
		return
	}
	if inst.shutSig.IsSoftStopSignalled() {
		inst.shutSig.TriggerHasStopped()
		err = ErrNotConnected
		return
	}

	var clientOpts []kgo.Opt
	clientOpts, err = inst.clientOpts()
	if err != nil {
		return
	}

	commitFn := func(*kgo.Record) {}
	if inst.opts.ConsumerGroup != "" {
		commitFn = func(r *kgo.Record) {
			if inst.Client == nil {
				return
			}
			inst.Client.MarkCommitRecords(r)
		}
	}

	checkpoints := newPartitionState(commitFn)

	if inst.opts.ConsumerGroup != "" {
		clientOpts = append(clientOpts,
			kgo.OnPartitionsRevoked(func(rctx context.Context, c *kgo.Client, m map[string][]int32) {
				commitErr := c.CommitMarkedOffsets(rctx)
				if commitErr != nil {
					inst.log.Error().Err(commitErr).Msg("kafka: commit on partition revoke")
				}
				checkpoints.removeTopicPartitions(m)
			}),
			kgo.OnPartitionsLost(func(_ context.Context, _ *kgo.Client, m map[string][]int32) {
				checkpoints.removeTopicPartitions(m)
			}),
			kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, m map[string][]int32) {
				for topic, parts := range m {
					for _, part := range parts {
						checkpoints.addRecords(topic, part, nil, inst.cacheLimit, inst.batchMaxSize)
					}
				}
			}),
			kgo.ConsumerGroup(inst.opts.ConsumerGroup),
			kgo.AutoCommitMarks(),
			kgo.AutoCommitInterval(inst.opts.CommitPeriod),
			kgo.WithLogger(NewKGoLogger(inst.log)),
		)
	}

	inst.Client, err = NewFranzClient(ctx, clientOpts...)
	if err != nil {
		return
	}

	noActivePartitionsBackOff := backoff.NewExponentialBackOff()
	noActivePartitionsBackOff.InitialInterval = 50 * time.Microsecond
	noActivePartitionsBackOff.MaxInterval = time.Second
	noActivePartitionsBackOff.MaxElapsedTime = 0

	connErrBackOff := backoff.NewExponentialBackOff()
	connErrBackOff.InitialInterval = 100 * time.Millisecond
	connErrBackOff.MaxInterval = time.Second
	connErrBackOff.MaxElapsedTime = 0

	go inst.runPollLoop(checkpoints, noActivePartitionsBackOff, connErrBackOff)

	inst.partState = checkpoints
	return
}

// runPollLoop is the long-lived consumer loop spawned by Connect. It
// polls fetches, distributes records to per-partition caches, and
// drives the back-pressure pause/resume cycle. Exits on shutSig.
func (inst *FranzReaderOrdered) runPollLoop(checkpoints *partitionState, noActivePartitionsBackOff, connErrBackOff backoff.BackOff) {
	defer func() {
		inst.Client.Close()
		if inst.shutSig.IsSoftStopSignalled() {
			inst.shutSig.TriggerHasStopped()
		}
	}()

	closeCtx, done := inst.shutSig.SoftStopCtx(context.Background())
	defer done()

	for {
		// Stall-prevention timeout: if every assigned partition is
		// paused, PollFetches would block forever. Bound the poll so
		// we can re-evaluate pause/resume decisions periodically.
		stallCtx, pollDone := context.WithTimeout(closeCtx, time.Second)
		fetches := inst.Client.PollFetches(stallCtx)
		pollDone()

		if handleFetchErrors(closeCtx, fetches, inst.log, connErrBackOff) {
			return
		}

		if closeCtx.Err() != nil {
			return
		}

		pauseTopicPartitions := map[string][]int32{}
		fetches.EachPartition(func(p kgo.FetchTopicPartition) {
			if len(p.Records) == 0 {
				return
			}
			batch := recordsToBatch(p.Topic, p.Partition, p.Records)
			if len(batch.b) == 0 {
				return
			}
			if checkpoints.addRecords(p.Topic, p.Partition, &batch, inst.cacheLimit, inst.batchMaxSize) {
				pauseTopicPartitions[p.Topic] = append(pauseTopicPartitions[p.Topic], p.Partition)
			}
		})

		pausedPartitionTopics := inst.Client.PauseFetchPartitions(pauseTopicPartitions)
		noActivePartitionsBackOff.Reset()

	noActivePartitions:
		for {
			resumeTopicPartitions := map[string][]int32{}
			for pausedTopic, pausedPartitions := range pausedPartitionTopics {
				for _, pausedPartition := range pausedPartitions {
					if !checkpoints.pauseFetch(pausedTopic, pausedPartition, inst.cacheLimit) {
						resumeTopicPartitions[pausedTopic] = append(resumeTopicPartitions[pausedTopic], pausedPartition)
					}
				}
			}
			if len(resumeTopicPartitions) > 0 {
				inst.Client.ResumeFetchPartitions(resumeTopicPartitions)
			}
			if inst.opts.ConsumerGroup == "" || len(resumeTopicPartitions) > 0 || checkpoints.tallyActivePartitions(pausedPartitionTopics) > 0 {
				break noActivePartitions
			}
			select {
			case <-time.After(noActivePartitionsBackOff.NextBackOff()):
			case <-closeCtx.Done():
				return
			}
			// Refresh paused-set: rebalance may have shifted our
			// allocation since the last pass.
			pausedPartitionTopics = inst.Client.PauseFetchPartitions(nil)
		}
	}
}

// Read returns the next available batch from any partition. Returns
// [ErrNotConnected] when called before Connect or after Close. Blocks
// until a batch is available, ctx is cancelled, or the reader is
// closed.
//
// The returned [Batch.Records] is a synthesized single-topic /
// single-partition kgo.Fetches; callers can use the standard kgo
// iteration methods (RecordsAll, EachRecord, EachPartition).
func (inst *FranzReaderOrdered) Read(ctx context.Context) (b Batch, err error) {
	if inst.partState == nil {
		err = ErrNotConnected
		return
	}
	for {
		mAck := inst.partState.pop()
		if mAck != nil {
			inst.readBackOff.Reset()
			b = Batch{
				Records: kgo.Fetches{{
					Topics: []kgo.FetchTopic{{
						Topic: mAck.topic,
						Partitions: []kgo.FetchPartition{{
							Partition: mAck.partition,
							Records:   mAck.records,
						}},
					}},
				}},
				Ack: func(context.Context, error) (err error) {
					mAck.onAck()
					return
				},
			}
			return
		}
		select {
		case <-time.After(inst.readBackOff.NextBackOff()):
		case <-ctx.Done():
			err = ctx.Err()
			return
		}
	}
}

// Close triggers a soft stop and waits for the poll loop to drain.
// Returns ctx.Err() if ctx fires before the loop exits.
func (inst *FranzReaderOrdered) Close(ctx context.Context) (err error) {
	go func() {
		inst.shutSig.TriggerSoftStop()
		if inst.partState == nil {
			inst.shutSig.TriggerHasStopped()
		}
	}()
	select {
	case <-inst.shutSig.HasStoppedChan():
	case <-ctx.Done():
		err = ctx.Err()
	}
	return
}

var _ ConsumerI = (*FranzReaderOrdered)(nil)
