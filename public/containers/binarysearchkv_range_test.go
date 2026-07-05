package containers

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func rangeOracle(kv *BinarySearchGrowingKV[string, int], lo string, hi string, useHi bool) (out []kvPair) {
	for k, v := range kv.IteratePairs() {
		if kv.cmpKey(k, lo) < 0 {
			continue
		}
		if useHi && kv.cmpKey(k, hi) >= 0 {
			continue
		}
		out = append(out, kvPair{K: k, V: v})
	}
	return
}

func collectSeq2(seq func(yield func(string, int) bool)) (out []kvPair) {
	for k, v := range seq {
		out = append(out, kvPair{K: k, V: v})
	}
	return
}

func TestIterateRange_Bounds(t *testing.T) {
	kv := NewBinarySearchGrowingKVOrdered[string, int](8)
	for i, k := range []string{"b", "d", "f", "h"} {
		kv.UpsertSingle(k, i)
	}

	cases := []struct {
		name   string
		lo, hi string
		want   []kvPair
	}{
		{"full_cover", "a", "z", []kvPair{{"b", 0}, {"d", 1}, {"f", 2}, {"h", 3}}},
		{"exact_bounds_hi_exclusive", "b", "h", []kvPair{{"b", 0}, {"d", 1}, {"f", 2}}},
		{"absent_bounds", "c", "g", []kvPair{{"d", 1}, {"f", 2}}},
		{"lo_after_last", "x", "z", nil},
		{"hi_before_first", "a", "b", nil},
		{"lo_equals_hi", "d", "d", nil},
		{"lo_greater_than_hi", "h", "b", nil},
		{"single_element", "d", "e", []kvPair{{"d", 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, collectSeq2(kv.IterateRange(tc.lo, tc.hi)))
			if tc.want != nil {
				require.Equal(t, rangeOracle(kv, tc.lo, tc.hi, true), tc.want, "oracle cross-check")
			}
		})
	}
}

func TestIterateFrom_Bounds(t *testing.T) {
	kv := NewBinarySearchGrowingKVOrdered[string, int](8)
	for i, k := range []string{"b", "d", "f"} {
		kv.UpsertSingle(k, i)
	}

	require.Equal(t, []kvPair{{"b", 0}, {"d", 1}, {"f", 2}}, collectSeq2(kv.IterateFrom("a")))
	require.Equal(t, []kvPair{{"d", 1}, {"f", 2}}, collectSeq2(kv.IterateFrom("c")))
	require.Equal(t, []kvPair{{"d", 1}, {"f", 2}}, collectSeq2(kv.IterateFrom("d")), "lo inclusive")
	require.Nil(t, collectSeq2(kv.IterateFrom("g")))
}

func TestIterateRange_EmptyAndNil(t *testing.T) {
	empty := NewBinarySearchGrowingKVOrdered[string, int](0)
	require.Nil(t, collectSeq2(empty.IterateRange("a", "z")))
	require.Nil(t, collectSeq2(empty.IterateFrom("a")))

	var nilKV *BinarySearchGrowingKV[string, int]
	require.Nil(t, collectSeq2(nilKV.IterateRange("a", "z")))
	require.Nil(t, collectSeq2(nilKV.IterateFrom("a")))
}

func TestIterateRange_EarlyTermination(t *testing.T) {
	kv := NewBinarySearchGrowingKVOrdered[string, int](8)
	for i, k := range []string{"a", "b", "c", "d"} {
		kv.UpsertSingle(k, i)
	}
	var seen []string
	for k := range kv.IterateRange("a", "z") {
		seen = append(seen, k)
		if len(seen) == 2 {
			break
		}
	}
	require.Equal(t, []string{"a", "b"}, seen)
}

func TestIterateRange_CustomComparator(t *testing.T) {
	// Case-insensitive comparator: bounds compare under the same
	// equivalence as the keys.
	kv := NewBinarySearchGrowingKV[string, int](8, ciCompare)
	kv.UpsertSingle("Bravo", 1)
	kv.UpsertSingle("delta", 2)
	kv.UpsertSingle("Foxtrot", 3)

	require.Equal(t, []kvPair{{"Bravo", 1}, {"delta", 2}}, collectSeq2(kv.IterateRange("AAA", "FOO")))
	require.Equal(t, []kvPair{{"delta", 2}, {"Foxtrot", 3}}, collectSeq2(kv.IterateFrom("CHARLIE")))
}

// TestIterateRange_FlushesAtRangeTime pins that range reads share the
// Iterate* flush semantics: deferred UpsertBatch state and the start
// position are resolved when ranging begins, not when the Seq is built.
func TestIterateRange_FlushesAtRangeTime(t *testing.T) {
	kv := NewBinarySearchGrowingKVOrdered[string, int](8)
	kv.UpsertSingle("m", 0)
	seq := kv.IterateRange("a", "z")
	from := kv.IterateFrom("a")
	kv.UpsertBatch("b", 1)
	kv.UpsertBatch("m", 9) // duplicate, newest value wins

	require.Equal(t, []kvPair{{"b", 1}, {"m", 9}}, collectSeq2(seq))
	require.Equal(t, []kvPair{{"b", 1}, {"m", 9}}, collectSeq2(from))
}

// TestIterateRange_RandomizedAgainstOracle cross-checks random ranges
// against a filter over IteratePairs.
func TestIterateRange_RandomizedAgainstOracle(t *testing.T) {
	rnd := rand.New(rand.NewPCG(0x5EED, 0xFEED))
	kv := NewBinarySearchGrowingKVOrdered[string, int](64)
	for i := range 48 {
		kv.UpsertBatch(fmt.Sprintf("k%03d", rnd.IntN(200)), i)
	}
	for range 200 {
		lo := fmt.Sprintf("k%03d", rnd.IntN(220))
		hi := fmt.Sprintf("k%03d", rnd.IntN(220))
		require.Equal(t, rangeOracle(kv, lo, hi, true), collectSeq2(kv.IterateRange(lo, hi)), "lo=%s hi=%s", lo, hi)
		require.Equal(t, rangeOracle(kv, lo, "", false), collectSeq2(kv.IterateFrom(lo)), "lo=%s", lo)
	}
}
