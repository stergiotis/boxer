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
//   - Dropped the Benthos config-DSL surface: kruField* constants,
//     FranzReaderUnorderedConfigFields, NewFranzReaderUnorderedFromConfig.
//     Replaced with FranzReaderUnorderedOpts struct +
//     DefaultFranzReaderUnorderedOpts factory + NewFranzReaderUnordered
//     constructor. The upstream "Deprecated: use toggled" markers
//     applied to those config-DSL helpers, not to the FranzReaderUnordered
//     struct itself, so the new constructor carries no deprecation tag —
//     the toggled wrapper composes ordered + unordered rather than
//     superseding either.
//   - Dropped the service.Batcher integration. Upstream's
//     partitionTracker held a service.Batcher per partition and a
//     dedicated goroutine driving size-based / time-based batch flushes.
//     This port emits one *kgo.Record per [Batch] (single-record
//     batches), simplifying partitionTracker to a checkpointer + lock.
//     Applications that want size or time batching wrap Read in their
//     own batcher externally.
//   - Dropped the franzRecordToMsgFn indirection (which defaulted to
//     FranzRecordToMessageV0 / V1) and the multi-header config flag.
//     This port exposes *kgo.Record directly per ADR-0005 /
//     EXPLANATION.md, so the conversion is unnecessary.
//   - Dropped ConsumerLag wiring (kafka_lag metadata, lag-refresh
//     goroutine, redpanda_lag gauge); same scope decision as
//     franz_reader_ordered.go (Phase 8 may revisit).
//   - Dropped ConnectionTest; callers can use NewFranzClient + Close.
//   - Replaced service.MessageBatch / *service.Message with the
//     package-local Batch (kgo.Fetches synthesized as single-topic /
//     single-partition); reuses the internal batchWithAckFn from
//     franz_reader_ordered.go since both readers produce per-partition
//     batches.
//   - Replaced service.AckFunc with AckFn; same advisory-args
//     contract as the ordered reader.
//   - Replaced *service.Logger with *zerolog.Logger (nil-safe via
//     zerolog.Nop() default). Replaced the atomic.Value-typed
//     batchChan with atomic.Pointer[chan batchWithAckFn].
//   - Refactored to boxer coding standards: receiver `inst`, named
//     return values, eh.Errorf, sized integer fields, compile-time
//     interface assertion.
//
// Logic preserved verbatim from upstream: per-partition checkpointer
// gating committed offsets to the head of the contiguous-acked range,
// CheckpointLimit-based back-pressure (PauseFetchPartitions when a
// partition's pending count meets the limit, ResumeFetchPartitions
// when it drops below), rebalance handling via OnPartitionsRevoked /
// Lost (no Assigned in upstream's unordered path; carried forward).

package kafka

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Jeffail/checkpoint"
	"github.com/Jeffail/shutdown"
	"github.com/cenkalti/backoff/v4"
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/twmb/franz-go/pkg/kgo"
)

// FranzReaderUnorderedOpts configures a FranzReaderUnordered.
//
// Unlike [FranzReaderOrderedOpts], this reader allows up to
// CheckpointLimit records per partition to be in-flight simultaneously.
// The application is free to process them in parallel; the
// checkpointer ensures that committed offsets reflect only the
// contiguous head of the acked-record range, so Kafka's at-least-once
// guarantee survives crashes.
type FranzReaderUnorderedOpts struct {
	// ConsumerGroup, when non-empty, enables consumer-group mode.
	// Mutually exclusive with explicit topic-partition specs.
	ConsumerGroup string

	// CheckpointLimit is the maximum number of pending (delivered but
	// not yet acked) records per partition before fetches pause.
	// Higher values increase throughput at the cost of more potential
	// duplicates on crash recovery; lower values mitigate duplicates
	// but reduce parallel processing capacity.
	CheckpointLimit int32

	// CommitPeriod is the auto-commit interval for consumer-group
	// mode. Ignored when ConsumerGroup is empty.
	CommitPeriod time.Duration

	// Logger receives franz-go and reader-internal log events. May be
	// nil, in which case zerolog.Nop() is used.
	Logger *zerolog.Logger
}

// DefaultFranzReaderUnorderedOpts returns options matching the
// upstream Connect plugin defaults: 1024 checkpoint limit, 5s commit
// period.
func DefaultFranzReaderUnorderedOpts() (opts FranzReaderUnorderedOpts) {
	opts = FranzReaderUnorderedOpts{
		CheckpointLimit: 1024,
		CommitPeriod:    5 * time.Second,
	}
	return
}

// FranzReaderUnordered implements [ConsumerI] with parallel
// per-partition delivery. See [FranzReaderUnorderedOpts] for the
// at-least-once + parallel-processing tradeoff and configuration.
//
// Client is exposed (after Connect) so applications can issue
// admin-level requests through the same kgo.Client the reader uses
// internally — most notably feeding it to [NewConsumerLag] for
// lag-gauge observability without opening a second connection.
// Nil before Connect and after Close.
type FranzReaderUnordered struct {
	clientOpts func() (opts []kgo.Opt, err error)
	opts       FranzReaderUnorderedOpts

	batchChan atomic.Pointer[chan batchWithAckFn]
	Client    *kgo.Client
	log       *zerolog.Logger
	shutSig   *shutdown.Signaller
}

// NewFranzReaderUnordered constructs a FranzReaderUnordered. clientOpts
// is invoked at Connect time to materialise the kgo client options
// (typically the union of [FranzConnectionDetails.FranzOpts] and
// [FranzConsumerDetails.FranzOpts]).
func NewFranzReaderUnordered(opts FranzReaderUnorderedOpts, clientOpts func() (kgoOpts []kgo.Opt, err error)) (inst *FranzReaderUnordered, err error) {
	if opts.CheckpointLimit <= 0 {
		err = eh.Errorf("CheckpointLimit must be > 0")
		return
	}
	log := opts.Logger
	if log == nil {
		nop := zerolog.Nop()
		log = &nop
	}
	inst = &FranzReaderUnordered{
		clientOpts: clientOpts,
		opts:       opts,
		log:        log,
		shutSig:    shutdown.NewSignaller(),
	}
	return
}

func (inst *FranzReaderUnordered) loadBatchChan() (ch chan batchWithAckFn, ok bool) {
	ref := inst.batchChan.Load()
	if ref == nil {
		return
	}
	ch = *ref
	ok = true
	return
}

func (inst *FranzReaderUnordered) storeBatchChan(ch chan batchWithAckFn) {
	if ch == nil {
		inst.batchChan.Store(nil)
		return
	}
	inst.batchChan.Store(&ch)
}

//------------------------------------------------------------------------------
// partitionTracker: per-partition checkpointer + send-to-out-channel

type partitionTracker struct {
	checkpointerLock sync.Mutex
	checkpointer     *checkpoint.Uncapped[*kgo.Record]

	outBatchChan chan<- batchWithAckFn
	commitFn     func(r *kgo.Record)
	shutSig      *shutdown.Signaller
}

func newPartitionTracker(batchChan chan<- batchWithAckFn, commitFn func(r *kgo.Record)) (inst *partitionTracker) {
	inst = &partitionTracker{
		checkpointer: checkpoint.NewUncapped[*kgo.Record](),
		outBatchChan: batchChan,
		commitFn:     commitFn,
		shutSig:      shutdown.NewSignaller(),
	}
	return
}

// add tracks the record on the partition's checkpointer and sends a
// single-record batch to the out channel. Returns true when the
// pending count meets limit and the caller should pause fetches.
func (inst *partitionTracker) add(ctx context.Context, r *kgo.Record, limit int32) (pauseFetch bool) {
	inst.checkpointerLock.Lock()
	releaseFn := inst.checkpointer.Track(r, 1)
	inst.checkpointerLock.Unlock()

	bAck := batchWithAckFn{
		topic:     r.Topic,
		partition: r.Partition,
		records:   []*kgo.Record{r},
		onAck: func() {
			inst.checkpointerLock.Lock()
			releaseRecord := releaseFn()
			inst.checkpointerLock.Unlock()
			if releaseRecord != nil && *releaseRecord != nil {
				inst.commitFn(*releaseRecord)
			}
		},
	}

	select {
	case <-ctx.Done():
		// On shutdown the record drops; skipping the send means we
		// won't commit this offset, which is correct.
	case inst.outBatchChan <- bAck:
	}

	inst.checkpointerLock.Lock()
	pauseFetch = inst.checkpointer.Pending() >= int64(limit)
	inst.checkpointerLock.Unlock()
	return
}

func (inst *partitionTracker) pauseFetch(limit int32) (pauseFetch bool) {
	inst.checkpointerLock.Lock()
	pauseFetch = inst.checkpointer.Pending() >= int64(limit)
	inst.checkpointerLock.Unlock()
	return
}

func (inst *partitionTracker) close(ctx context.Context) (err error) {
	inst.shutSig.TriggerSoftStop()
	inst.shutSig.TriggerHasStopped()
	select {
	case <-ctx.Done():
		err = ctx.Err()
	default:
	}
	return
}

//------------------------------------------------------------------------------
// checkpointTracker: topic → partition → partitionTracker

type checkpointTracker struct {
	mut       sync.Mutex
	topics    map[string]map[int32]*partitionTracker
	batchChan chan<- batchWithAckFn
	commitFn  func(r *kgo.Record)
}

func newCheckpointTracker(batchChan chan<- batchWithAckFn, commitFn func(r *kgo.Record)) (inst *checkpointTracker) {
	inst = &checkpointTracker{
		topics:    map[string]map[int32]*partitionTracker{},
		batchChan: batchChan,
		commitFn:  commitFn,
	}
	return
}

func (inst *checkpointTracker) close() {
	inst.mut.Lock()
	defer inst.mut.Unlock()

	for _, partitions := range inst.topics {
		for _, tracker := range partitions {
			_ = tracker.close(context.Background())
		}
	}
}

func (inst *checkpointTracker) addRecord(ctx context.Context, r *kgo.Record, limit int32) (pauseFetch bool) {
	inst.mut.Lock()
	topicTracker := inst.topics[r.Topic]
	if topicTracker == nil {
		topicTracker = map[int32]*partitionTracker{}
		inst.topics[r.Topic] = topicTracker
	}
	partTracker := topicTracker[r.Partition]
	if partTracker == nil {
		partTracker = newPartitionTracker(inst.batchChan, inst.commitFn)
		topicTracker[r.Partition] = partTracker
	}
	inst.mut.Unlock()
	pauseFetch = partTracker.add(ctx, r, limit)
	return
}

func (inst *checkpointTracker) pauseFetch(topic string, partition int32, limit int32) (pause bool) {
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

func (inst *checkpointTracker) removeTopicPartitions(ctx context.Context, m map[string][]int32) {
	inst.mut.Lock()
	defer inst.mut.Unlock()

	for topicName, lostTopic := range m {
		trackedTopic, exists := inst.topics[topicName]
		if !exists {
			continue
		}
		for _, lostPartition := range lostTopic {
			trackedPartition, ok := trackedTopic[lostPartition]
			if ok {
				_ = trackedPartition.close(ctx)
			}
			delete(trackedTopic, lostPartition)
		}
		if len(trackedTopic) == 0 {
			delete(inst.topics, topicName)
		}
	}
}

//------------------------------------------------------------------------------
// FranzReaderUnordered methods (ConsumerI surface)

// Connect brings up the kgo.Client and starts the unordered poll loop.
func (inst *FranzReaderUnordered) Connect(ctx context.Context) (err error) {
	if _, ok := inst.loadBatchChan(); ok {
		return
	}
	if inst.shutSig.IsSoftStopSignalled() {
		inst.shutSig.TriggerHasStopped()
		err = ErrNotConnected
		return
	}

	batchChan := make(chan batchWithAckFn)

	commitFn := func(*kgo.Record) {}
	if inst.opts.ConsumerGroup != "" {
		commitFn = func(r *kgo.Record) {
			if inst.Client == nil {
				return
			}
			inst.Client.MarkCommitRecords(r)
		}
	}
	checkpoints := newCheckpointTracker(batchChan, commitFn)

	var clientOpts []kgo.Opt
	clientOpts, err = inst.clientOpts()
	if err != nil {
		return
	}

	if inst.opts.ConsumerGroup != "" {
		clientOpts = append(clientOpts,
			kgo.OnPartitionsRevoked(func(rctx context.Context, c *kgo.Client, m map[string][]int32) {
				commitErr := c.CommitMarkedOffsets(rctx)
				if commitErr != nil {
					inst.log.Error().Err(commitErr).Msg("kafka: commit on partition revoke")
				}
				checkpoints.removeTopicPartitions(rctx, m)
			}),
			kgo.OnPartitionsLost(func(rctx context.Context, _ *kgo.Client, m map[string][]int32) {
				checkpoints.removeTopicPartitions(rctx, m)
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

	connErrBackOff := backoff.NewExponentialBackOff()
	connErrBackOff.InitialInterval = 100 * time.Millisecond
	connErrBackOff.MaxInterval = time.Second
	connErrBackOff.MaxElapsedTime = 0

	go inst.runPollLoop(inst.Client, batchChan, checkpoints, connErrBackOff)

	inst.storeBatchChan(batchChan)
	return
}

// runPollLoop is the long-lived consumer loop. It polls fetches,
// routes records into per-partition trackers (which fan out to
// batchChan), and drives back-pressure pause/resume.
func (inst *FranzReaderUnordered) runPollLoop(cl *kgo.Client, batchChan chan batchWithAckFn, checkpoints *checkpointTracker, connErrBackOff backoff.BackOff) {
	defer func() {
		cl.Close()
		inst.Client = nil
		checkpoints.close()
		inst.storeBatchChan(nil)
		close(batchChan)
		if inst.shutSig.IsSoftStopSignalled() {
			inst.shutSig.TriggerHasStopped()
		}
	}()

	closeCtx, done := inst.shutSig.SoftStopCtx(context.Background())
	defer done()

	for {
		// Stall-prevention timeout: see the matching comment in
		// franz_reader_ordered.go's runPollLoop.
		stallCtx, pollDone := context.WithTimeout(closeCtx, time.Second)
		fetches := cl.PollFetches(stallCtx)
		pollDone()

		if handleFetchErrors(closeCtx, fetches, inst.log, connErrBackOff) {
			return
		}

		if closeCtx.Err() != nil {
			return
		}

		pauseTopicPartitions := map[string][]int32{}
		iter := fetches.RecordIter()
		for !iter.Done() {
			record := iter.Next()
			if checkpoints.addRecord(closeCtx, record, inst.opts.CheckpointLimit) {
				pauseTopicPartitions[record.Topic] = append(pauseTopicPartitions[record.Topic], record.Partition)
			}
		}

		resumeTopicPartitions := map[string][]int32{}
		for pausedTopic, pausedPartitions := range cl.PauseFetchPartitions(pauseTopicPartitions) {
			for _, pausedPartition := range pausedPartitions {
				if !checkpoints.pauseFetch(pausedTopic, pausedPartition, inst.opts.CheckpointLimit) {
					resumeTopicPartitions[pausedTopic] = append(resumeTopicPartitions[pausedTopic], pausedPartition)
				}
			}
		}
		if len(resumeTopicPartitions) > 0 {
			cl.ResumeFetchPartitions(resumeTopicPartitions)
		}
	}
}

// Read returns the next available single-record batch. Returns
// [ErrNotConnected] when called before Connect or after Close.
//
// As with the ordered reader, the returned [Batch.Records] is a
// synthesized single-topic / single-partition kgo.Fetches; the
// underlying batch always contains exactly one record because this
// port does not bundle a Batcher (see ./NOTICE).
func (inst *FranzReaderUnordered) Read(ctx context.Context) (b Batch, err error) {
	ch, ok := inst.loadBatchChan()
	if !ok {
		err = ErrNotConnected
		return
	}
	var mAck batchWithAckFn
	var open bool
	select {
	case mAck, open = <-ch:
		if !open {
			err = ErrNotConnected
			return
		}
	case <-ctx.Done():
		err = ctx.Err()
		return
	}
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

// Close triggers a soft stop and waits for the poll loop to drain.
func (inst *FranzReaderUnordered) Close(ctx context.Context) (err error) {
	go func() {
		inst.shutSig.TriggerSoftStop()
		if _, ok := inst.loadBatchChan(); !ok {
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

var _ ConsumerI = (*FranzReaderUnordered)(nil)
