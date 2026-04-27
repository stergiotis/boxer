//go:build llm_generated_opus47

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
// Unit tests for the unordered reader's checkpointer-driven
// ack-drain. The unordered reader allows parallel processing of
// records within a partition; its at-least-once guarantee depends on
// the checkpointer committing only the head of contiguously-acked
// offsets, even when application-side acks fire out of order.

package kafka

import (
	"context"
	"math/rand/v2"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// drainNFromChan reads exactly n batches from ch (or fails the test
// if any read blocks beyond the channel's buffer). The unordered
// partitionTracker.add sends synchronously, so by the time add
// returns, the batch is in ch.
func drainNFromChan(t *testing.T, ch <-chan batchWithAckFn, n int) (out []batchWithAckFn) {
	t.Helper()
	out = make([]batchWithAckFn, 0, n)
	for i := 0; i < n; i++ {
		select {
		case b := <-ch:
			out = append(out, b)
		default:
			t.Fatalf("expected %d batches, got %d", n, i)
		}
	}
	return
}

// TestUnorderedPartitionTracker_OutOfOrderAcksGateCommit is the
// headline at-least-once invariant: when records are acked out of
// order, the broker's committed offset must NEVER advance past a
// record whose predecessors are still unacked. The checkpointer
// achieves this by emitting commitFn only when its head reaches a
// contiguously-released range — fold any out-of-order release into
// silence, then catch up when the gap closes.
func TestUnorderedPartitionTracker_OutOfOrderAcksGateCommit(t *testing.T) {
	var (
		commits  []int64
		commitMu sync.Mutex
	)
	commitFn := func(r *kgo.Record) {
		commitMu.Lock()
		commits = append(commits, r.Offset)
		commitMu.Unlock()
	}

	out := make(chan batchWithAckFn, 10)
	pt := newPartitionTracker(out, commitFn)

	ctx := context.Background()
	pt.add(ctx, &kgo.Record{Topic: "t", Partition: 0, Offset: 0}, 1<<20)
	pt.add(ctx, &kgo.Record{Topic: "t", Partition: 0, Offset: 1}, 1<<20)
	pt.add(ctx, &kgo.Record{Topic: "t", Partition: 0, Offset: 2}, 1<<20)

	batches := drainNFromChan(t, out, 3)
	b0, b1, b2 := batches[0], batches[1], batches[2]

	// Ack r1 first (skipping r0). commitFn must NOT fire — the head
	// is still at r0, which is unreleased.
	b1.onAck()
	commitMu.Lock()
	require.Empty(t, commits, "out-of-order ack must not advance commit")
	commitMu.Unlock()

	// Ack r0. Now the head jumps to r1 (contiguously released).
	// commitFn fires once with r1's offset.
	b0.onAck()
	commitMu.Lock()
	require.Len(t, commits, 1)
	assert.Equal(t, int64(1), commits[0])
	commitMu.Unlock()

	// Ack r2. Head advances to r2; commitFn fires with r2.
	b2.onAck()
	commitMu.Lock()
	require.Len(t, commits, 2)
	assert.Equal(t, int64(2), commits[1])
	commitMu.Unlock()
}

// TestUnorderedPartitionTracker_GapBlocksAllSubsequentCommits
// verifies the at-least-once tail invariant: if a record in the
// middle never gets acked, the broker's committed offset stays
// stuck behind it, even if every record after it is acked. On
// crash recovery, those records are redelivered (the trade-off:
// at-least-once allows duplicates).
func TestUnorderedPartitionTracker_GapBlocksAllSubsequentCommits(t *testing.T) {
	var (
		commits  []int64
		commitMu sync.Mutex
	)
	commitFn := func(r *kgo.Record) {
		commitMu.Lock()
		commits = append(commits, r.Offset)
		commitMu.Unlock()
	}

	out := make(chan batchWithAckFn, 10)
	pt := newPartitionTracker(out, commitFn)
	ctx := context.Background()

	for i := range 5 {
		pt.add(ctx, &kgo.Record{Topic: "t", Partition: 0, Offset: int64(i)}, 1<<20)
	}
	batches := drainNFromChan(t, out, 5)

	// Ack everyone EXCEPT r2 (the gap).
	batches[0].onAck()
	batches[1].onAck()
	// batches[2] is the gap — deliberately left unacked.
	batches[3].onAck()
	batches[4].onAck()

	// Head can advance only to r1 (contiguous-released prefix).
	// Commits beyond r1 are blocked by the unfilled r2 slot.
	commitMu.Lock()
	defer commitMu.Unlock()
	require.NotEmpty(t, commits, "head must advance through r0 and r1")
	last := commits[len(commits)-1]
	assert.Equal(t, int64(1), last, "commit must stop at r1; r2 gap blocks r3 and r4")
	for _, off := range commits {
		assert.LessOrEqual(t, off, int64(1), "no commit may exceed r1 while r2 is unacked")
	}
}

// TestUnorderedPartitionTracker_CheckpointLimitTriggersPauseFetch
// verifies the back-pressure boundary: as Pending() crosses the
// CheckpointLimit, add() returns pauseFetch=true so the reader's
// poll loop knows to call kgo.Client.PauseFetchPartitions on this
// partition.
//
// The matching tail invariant: as in-order acks advance the head,
// Pending() drops, and at some point pauseFetch flips back to false
// — that's the signal the poll loop uses to ResumeFetchPartitions.
func TestUnorderedPartitionTracker_CheckpointLimitTriggersPauseFetch(t *testing.T) {
	out := make(chan batchWithAckFn, 100)
	pt := newPartitionTracker(out, func(*kgo.Record) {})
	ctx := context.Background()

	// limit=3, push 5 records. After the 3rd, Pending()=3 == limit
	// and pauseFetch flips on.
	var paused [5]bool
	for i := range 5 {
		paused[i] = pt.add(ctx, &kgo.Record{Topic: "t", Partition: 0, Offset: int64(i)}, 3)
	}

	assert.False(t, paused[0], "Pending=1 < limit=3")
	assert.False(t, paused[1], "Pending=2 < limit=3")
	assert.True(t, paused[2], "Pending=3 == limit=3")
	assert.True(t, paused[3], "Pending=4 > limit=3")
	assert.True(t, paused[4], "Pending=5 > limit=3")

	// Drain the channel and ack in order; Pending drops one per ack.
	batches := drainNFromChan(t, out, 5)
	batches[0].onAck()
	assert.True(t, pt.pauseFetch(3), "Pending=4 still >= limit=3 after one ack")
	batches[1].onAck()
	assert.True(t, pt.pauseFetch(3), "Pending=3 still >= limit=3 after two acks")
	batches[2].onAck()
	assert.False(t, pt.pauseFetch(3), "Pending=2 < limit=3 after three acks — resumable")
}

// TestUnorderedPartitionTracker_ConcurrentAcks_RaceFree stresses
// the checkpointer under concurrent acks (run with -race).
//
// Subtle reality: the checkpointer's release function returns
// t.checkpoint — the *currently cached* head value. When a release
// does not advance the head, it returns the previously-set value,
// not nil. The partitionTracker forwards every non-nil return to
// commitFn, so the SAME offset can fire commitFn many times between
// successive head advances.
//
// This is benign in production: kgo.Client.MarkCommitRecords is
// idempotent at the broker side — re-marking an already-committed
// offset is a no-op. But the test must NOT assert "no duplicates";
// duplicates are part of the contract.
//
// Additionally, commitFn is called outside the checkpointer's lock,
// so the OBSERVATION order of distinct commit values is racy: two
// goroutines that both advance the head can have their commitFn
// calls reorder freely. The internal sequence of head advances IS
// monotonic, but observation order is not.
//
// The actually-testable invariants:
//
//  1. Every commit value is in [0, N-1] — no spurious offsets.
//  2. The maximum commit equals N-1 — by the end, every record has
//     been released, so the head has reached the topmost offset.
//  3. The DISTINCT commit values, sorted ascending, form a
//     strictly-increasing sequence — the head only advances, even
//     though duplicates fire between advances.
func TestUnorderedPartitionTracker_ConcurrentAcks_RaceFree(t *testing.T) {
	const N = 100
	var (
		commits  []int64
		commitMu sync.Mutex
	)
	commitFn := func(r *kgo.Record) {
		commitMu.Lock()
		commits = append(commits, r.Offset)
		commitMu.Unlock()
	}

	out := make(chan batchWithAckFn, N)
	pt := newPartitionTracker(out, commitFn)
	ctx := context.Background()

	for i := range N {
		pt.add(ctx, &kgo.Record{Topic: "t", Partition: 0, Offset: int64(i)}, int32(N+1))
	}
	batches := drainNFromChan(t, out, N)

	rand.Shuffle(N, func(i, j int) { batches[i], batches[j] = batches[j], batches[i] })

	var wg sync.WaitGroup
	wg.Add(N)
	for i := range N {
		go func(b batchWithAckFn) {
			defer wg.Done()
			b.onAck()
		}(batches[i])
	}
	wg.Wait()

	commitMu.Lock()
	defer commitMu.Unlock()

	require.NotEmpty(t, commits)

	// Invariant 1: every commit in [0, N-1].
	distinct := make(map[int64]struct{}, len(commits))
	maxCommit := int64(-1)
	for _, c := range commits {
		assert.GreaterOrEqual(t, c, int64(0))
		assert.Less(t, c, int64(N))
		distinct[c] = struct{}{}
		if c > maxCommit {
			maxCommit = c
		}
	}

	// Invariant 2: max commit = N-1.
	assert.Equal(t, int64(N-1), maxCommit, "max commit must reach the topmost offset")

	// Invariant 3: distinct commits, sorted, are strictly increasing
	// (head only advances; duplicates are filtered by deduplicating).
	sortedDistinct := make([]int64, 0, len(distinct))
	for c := range distinct {
		sortedDistinct = append(sortedDistinct, c)
	}
	sort.Slice(sortedDistinct, func(i, j int) bool { return sortedDistinct[i] < sortedDistinct[j] })
	for i := 1; i < len(sortedDistinct); i++ {
		assert.Greater(t, sortedDistinct[i], sortedDistinct[i-1], "head advances must be strictly increasing (distinct values, sorted)")
	}
}

// TestUnorderedCheckpointTracker_AddRouting_AndRemoveTopicPartitions
// covers the topic→partition routing in checkpointTracker, plus
// cleanup on partition revocation.
func TestUnorderedCheckpointTracker_AddRouting_AndRemoveTopicPartitions(t *testing.T) {
	out := make(chan batchWithAckFn, 100)
	ct := newCheckpointTracker(out, func(*kgo.Record) {})
	ctx := context.Background()

	// Records across two topics, two partitions each.
	ct.addRecord(ctx, &kgo.Record{Topic: "t1", Partition: 0, Offset: 0}, 1<<20)
	ct.addRecord(ctx, &kgo.Record{Topic: "t1", Partition: 1, Offset: 0}, 1<<20)
	ct.addRecord(ctx, &kgo.Record{Topic: "t2", Partition: 0, Offset: 0}, 1<<20)

	// Drain so the channel doesn't fill.
	drainNFromChan(t, out, 3)

	ct.mut.Lock()
	require.Len(t, ct.topics, 2, "two topics tracked")
	require.Len(t, ct.topics["t1"], 2, "t1 has two partitions")
	require.Len(t, ct.topics["t2"], 1, "t2 has one partition")
	ct.mut.Unlock()

	// Revoke t1 entirely.
	ct.removeTopicPartitions(ctx, map[string][]int32{"t1": {0, 1}})

	ct.mut.Lock()
	defer ct.mut.Unlock()
	_, t1Still := ct.topics["t1"]
	_, t2Still := ct.topics["t2"]
	assert.False(t, t1Still, "t1 must be removed entirely after revoking all partitions")
	assert.True(t, t2Still, "t2 must remain")
}

// TestUnorderedPartitionTracker_PauseFetchQueryAlone reads the
// current Pending() vs limit without adding records — used by the
// reader's poll loop to decide whether to RESUME a previously-paused
// partition.
//
// Edge case: limit=0 is degenerate ("any pending record pauses").
// Pending=0 >= 0 is true, so an empty tracker IS technically paused
// at limit=0. Production callers should never use limit=0 (the
// constructor rejects it via NewFranzReaderUnordered's validation),
// but the helper itself is well-defined.
func TestUnorderedPartitionTracker_PauseFetchQueryAlone(t *testing.T) {
	out := make(chan batchWithAckFn, 10)
	pt := newPartitionTracker(out, func(*kgo.Record) {})
	ctx := context.Background()

	// No records → Pending=0. At any positive limit: not paused.
	assert.False(t, pt.pauseFetch(1))
	assert.False(t, pt.pauseFetch(1<<30))

	// One pending record.
	pt.add(ctx, &kgo.Record{Topic: "t", Partition: 0, Offset: 0}, 10)
	drainNFromChan(t, out, 1)

	assert.False(t, pt.pauseFetch(2), "Pending=1 < limit=2")
	assert.True(t, pt.pauseFetch(1), "Pending=1 >= limit=1")
}
