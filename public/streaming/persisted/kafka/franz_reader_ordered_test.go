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
//   - Dropped TestPartitionCacheOrdering. Upstream's test exercised
//     the dispatch.TriggerSignal mechanism — workers in the middle
//     of a synthetic processing pipeline fire the trigger in a
//     "tangled" order to verify the partition cache still emits
//     batches in offset order. This port removed the per-message
//     dispatch trigger (see franz_reader_ordered.go's modification
//     block); without the trigger there is nothing to tangle, so
//     the test no longer applies.
//   - TestPartitionCacheBatching is preserved verbatim except for
//     the swap from *service.Message values to raw *kgo.Record
//     bytes (records carry Value directly; no AsBytes() wrapper).

package kafka

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// makeOrderedBatch builds a *batchWithRecords with `count` records,
// starting at startOffset, each with the given recSize, on
// (topic, partition). Helper for the ordered ack-drain tests below.
func makeOrderedBatch(topic string, partition int32, startOffset int64, count int, recSize uint64) (b *batchWithRecords) {
	b = &batchWithRecords{topic: topic, partition: partition}
	for i := range count {
		rec := &kgo.Record{
			Topic:     topic,
			Partition: partition,
			Offset:    startOffset + int64(i),
		}
		b.b = append(b.b, &recordWithSize{r: rec, size: recSize})
		b.size += recSize
	}
	return
}

func TestPartitionCacheBatching(t *testing.T) {
	pCache := newPartitionCache(func(*kgo.Record) {})
	bufSize, batchSize := uint64(1_000_000), uint64(10)

	var i int64
	testBatchIn := func(msgs ...string) (b *batchWithRecords) {
		b = &batchWithRecords{topic: "t", partition: 0}
		for _, m := range msgs {
			b.b = append(b.b, &recordWithSize{
				r:    &kgo.Record{Value: []byte(m), Offset: i},
				size: uint64(len(m)),
			})
			b.size += uint64(len(m))
			i++
		}
		return
	}

	popOutStrs := func(pCache *partitionCache) (outStrs []string) {
		tmp := pCache.pop()
		if tmp == nil {
			return
		}
		tmp.onAck()
		for _, r := range tmp.records {
			outStrs = append(outStrs, string(r.Value))
		}
		return
	}

	// Big batches are broken down.
	assert.False(t, pCache.push(bufSize, batchSize, testBatchIn(
		"aaaa",
		"bbbb",
		"cccc",
		"dd",
		"ee",
		"ffff",
	)))

	assert.Equal(t, []string{"aaaa", "bbbb"}, popOutStrs(pCache))
	assert.Equal(t, []string{"cccc", "dd", "ee"}, popOutStrs(pCache))
	assert.Equal(t, []string{"ffff"}, popOutStrs(pCache))
	assert.Equal(t, []string(nil), popOutStrs(pCache))

	// Small batches get records appended onto the prior batch.
	assert.False(t, pCache.push(bufSize, batchSize, testBatchIn(
		"aaaa",
		"bbbb",
	)))
	assert.False(t, pCache.push(bufSize, batchSize, testBatchIn(
		"cc",
		"dddd",
		"eeee",
		"ffff",
	)))
	assert.False(t, pCache.push(bufSize, batchSize, testBatchIn(
		"gg",
		"hh",
	)))
	assert.False(t, pCache.push(bufSize, batchSize, testBatchIn(
		"iiiiiiii",
	)))

	assert.Equal(t, []string{"aaaa", "bbbb", "cc"}, popOutStrs(pCache))
	assert.Equal(t, []string{"dddd", "eeee"}, popOutStrs(pCache))
	assert.Equal(t, []string{"ffff", "gg", "hh"}, popOutStrs(pCache))
	assert.Equal(t, []string{"iiiiiiii"}, popOutStrs(pCache))
	assert.Equal(t, []string(nil), popOutStrs(pCache))

	// Sanity: i was used to seed offsets; require it's been advanced.
	require.NotZero(t, i)
}

// TestPartitionCache_OrderedAck_GatesNextPop is the load-bearing
// ordering invariant: while a popped batch is in-flight (its onAck
// has not yet fired), pop() MUST return nil even if subsequent
// batches are queued behind it. EXPLANATION.md §"Strict in-order,
// exactly-once-call".
func TestPartitionCache_OrderedAck_GatesNextPop(t *testing.T) {
	pc := newPartitionCache(func(*kgo.Record) {})

	// Push two distinct batches. maxBatchSize=3 forces no-merge.
	pause1 := pc.push(1<<20, 3, makeOrderedBatch("t", 0, 0, 3, 1))
	pause2 := pc.push(1<<20, 3, makeOrderedBatch("t", 0, 3, 3, 1))
	assert.False(t, pause1)
	assert.False(t, pause2)

	// First pop returns batch 1.
	b1 := pc.pop()
	require.NotNil(t, b1)
	require.Len(t, b1.records, 3)
	assert.Equal(t, int64(0), b1.records[0].Offset)

	// Second pop is GATED — returns nil while b1 is in-flight even
	// though batch 2 is queued.
	require.Nil(t, pc.pop(), "pop must wait for prior batch's ack")

	// Ack b1 → batch 2 becomes poppable.
	b1.onAck()
	b2 := pc.pop()
	require.NotNil(t, b2)
	assert.Equal(t, int64(3), b2.records[0].Offset)
	b2.onAck()

	// Cache empty.
	assert.Nil(t, pc.pop())
}

// TestPartitionCache_CommitFnFiresWithLastRecord verifies the
// at-least-once-batch invariant: when a batch is acked, commitFn is
// called exactly once with the LAST record of the batch (the topmost
// offset). franz-go's MarkCommitRecords interprets this as
// "everything up to and including this record is committed".
func TestPartitionCache_CommitFnFiresWithLastRecord(t *testing.T) {
	var commits []int64
	pc := newPartitionCache(func(r *kgo.Record) { commits = append(commits, r.Offset) })

	pc.push(1<<20, 1<<20, makeOrderedBatch("t", 0, 10, 5, 1)) // offsets 10..14

	b := pc.pop()
	require.NotNil(t, b)
	require.Len(t, b.records, 5)

	// commitFn must NOT have fired before ack.
	require.Empty(t, commits)

	b.onAck()

	// commitFn fires exactly once with the last (topmost) offset.
	require.Len(t, commits, 1)
	assert.Equal(t, int64(14), commits[0])
}

// TestPartitionCache_BackPressureCrossesLimit verifies that pauseFetch
// flips on as cacheSize crosses the buffer limit, and flips back off
// after the in-flight batch is acked (which decrements cacheSize).
//
// Note: cacheSize stays high BETWEEN pop and ack — pop removes the
// batch from the queue but the records still occupy the back-pressure
// budget until ack confirms processing. This is intentional.
func TestPartitionCache_BackPressureCrossesLimit(t *testing.T) {
	pc := newPartitionCache(func(*kgo.Record) {})

	// 12 records of size 1 → cacheSize=12, limit=10 → paused.
	paused := pc.push(10, 1<<20, makeOrderedBatch("t", 0, 0, 12, 1))
	assert.True(t, paused, "push that crosses bufferSize must report pauseFetch=true")
	assert.True(t, pc.pauseFetch(10))

	b := pc.pop()
	require.NotNil(t, b)

	// Between pop and ack, cacheSize is unchanged → still paused.
	assert.True(t, pc.pauseFetch(10), "pop alone must not relieve back-pressure")

	b.onAck()

	// After ack, cacheSize=0 → unpaused.
	assert.False(t, pc.pauseFetch(10))
}

// TestPartitionState_PopAcrossPartitions verifies the in-order ack
// gate is per-partition, not global: distinct partitions can have
// in-flight batches simultaneously.
func TestPartitionState_PopAcrossPartitions(t *testing.T) {
	ps := newPartitionState(func(*kgo.Record) {})

	ps.addRecords("t", 0, makeOrderedBatch("t", 0, 0, 3, 1), 1<<20, 1<<20)
	ps.addRecords("t", 1, makeOrderedBatch("t", 1, 100, 3, 1), 1<<20, 1<<20)

	// Pop drains one partition's batch.
	b1 := ps.pop()
	require.NotNil(t, b1)

	// Pop again BEFORE acking b1 — must succeed, returning the OTHER
	// partition (since the gate is per-partitionCache, not global).
	b2 := ps.pop()
	require.NotNil(t, b2)
	assert.NotEqual(t, b1.partition, b2.partition, "pop should round-robin across partitions while one is in-flight")

	// Both partitions in-flight → next pop returns nil.
	assert.Nil(t, ps.pop())

	// Ack one → the freed partition is poppable but is also empty
	// (we only pushed one batch per partition), so still nil.
	b1.onAck()
	assert.Nil(t, ps.pop())
	b2.onAck()
}

// TestPartitionState_RemoveTopicPartitions verifies the rebalance
// path: when a partition is revoked, its tracker is dropped. Topics
// with no remaining partitions are removed entirely so the topic map
// doesn't grow unbounded across rebalances.
func TestPartitionState_RemoveTopicPartitions(t *testing.T) {
	ps := newPartitionState(func(*kgo.Record) {})

	// Three partitions across two topics.
	ps.addRecords("t1", 0, makeOrderedBatch("t1", 0, 0, 1, 1), 1<<20, 1<<20)
	ps.addRecords("t1", 1, makeOrderedBatch("t1", 1, 0, 1, 1), 1<<20, 1<<20)
	ps.addRecords("t2", 0, makeOrderedBatch("t2", 0, 0, 1, 1), 1<<20, 1<<20)

	// Revoke both t1 partitions.
	ps.removeTopicPartitions(map[string][]int32{"t1": {0, 1}})

	ps.mut.Lock()
	_, t1Still := ps.topics["t1"]
	_, t2Still := ps.topics["t2"]
	t2Parts := len(ps.topics["t2"])
	ps.mut.Unlock()

	assert.False(t, t1Still, "t1 must be removed entirely (no partitions left)")
	assert.True(t, t2Still, "t2 must still be present")
	assert.Equal(t, 1, t2Parts, "t2 must still have its partition 0")
}

// TestPartitionCache_AckAfterPartitionRemoved exercises the corner
// where a batch was popped, the partition was then revoked (its
// partitionCache dropped from the topic map), and the application
// finally calls onAck. The closure still holds the partitionCache
// pointer, so the ack still updates that cache's state and calls
// commitFn — but the broker has already reassigned the partition,
// so the commit is a no-op at the broker (or kgo logs a warning).
// We just verify it doesn't crash and commitFn is still invoked.
func TestPartitionCache_AckAfterPartitionRemoved(t *testing.T) {
	var commits []int64
	ps := newPartitionState(func(r *kgo.Record) { commits = append(commits, r.Offset) })

	ps.addRecords("t", 0, makeOrderedBatch("t", 0, 7, 3, 1), 1<<20, 1<<20)
	b := ps.pop()
	require.NotNil(t, b)

	// Revoke the partition while b is in-flight.
	ps.removeTopicPartitions(map[string][]int32{"t": {0}})

	// onAck must not crash; commitFn still fires (the broker side
	// no longer cares, but our internal state must stay clean).
	require.NotPanics(t, b.onAck)
	require.Len(t, commits, 1)
	assert.Equal(t, int64(9), commits[0])
}
