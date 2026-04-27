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
