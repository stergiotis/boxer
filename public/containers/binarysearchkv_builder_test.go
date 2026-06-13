package containers

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBSKVBuilder_BasicBuild(t *testing.T) {
	b := NewBinarySearchGrowingKVBuilderOrdered[string, int](4)
	b.Stage("c", 3)
	b.Stage("a", 1)
	b.Stage("b", 2)

	require.Equal(t, 3, b.Len(), "staged count before freeze")

	kv := b.Freeze()
	require.Equal(t, 3, kv.Len())
	require.Equal(t, []string{"a", "b", "c"}, slices.Collect(kv.IterateKeys()))
	require.Equal(t, []int{1, 2, 3}, slices.Collect(kv.IterateValues()))
}

func TestBSKVBuilder_DuplicateKeys_NewestWins(t *testing.T) {
	b := NewBinarySearchGrowingKVBuilderOrdered[string, int](4)
	b.Stage("k", 1)
	b.Stage("k", 2)
	b.Stage("k", 3)
	require.Equal(t, 3, b.Len(), "raw stage count includes duplicates")

	kv := b.Freeze()
	require.Equal(t, 1, kv.Len(), "compaction collapsed duplicates")
	v, has := kv.Get("k")
	require.True(t, has)
	require.Equal(t, 3, v, "newest-wins: last Stage value survives")
}

func TestBSKVBuilder_StageSeq_FromMap(t *testing.T) {
	src := map[string]int{"z": 26, "a": 1, "m": 13}
	b := NewBinarySearchGrowingKVBuilderOrdered[string, int](len(src))
	b.StageSeq(maps.All(src))

	kv := b.Freeze()
	require.Equal(t, 3, kv.Len())
	require.Equal(t, []string{"a", "m", "z"}, slices.Collect(kv.IterateKeys()))
}

func TestBSKVBuilder_StageSeq_MixedWithStage(t *testing.T) {
	// Stage and StageSeq interoperate freely — both push onto the same
	// staging buffer.
	b := NewBinarySearchGrowingKVBuilderOrdered[string, int](6)
	b.Stage("a", 1)
	b.StageSeq(maps.All(map[string]int{"b": 2, "c": 3}))
	b.Stage("d", 4)

	kv := b.Freeze()
	require.Equal(t, 4, kv.Len())
	require.Equal(t, []string{"a", "b", "c", "d"}, slices.Collect(kv.IterateKeys()))
}

func TestBSKVBuilder_Empty(t *testing.T) {
	b := NewBinarySearchGrowingKVBuilderOrdered[string, int](0)
	kv := b.Freeze()
	require.True(t, kv.IsEmpty())
	require.Equal(t, 0, kv.Len())
}

func TestBSKVBuilder_PanicsOnStageAfterFreeze(t *testing.T) {
	b := NewBinarySearchGrowingKVBuilderOrdered[string, int](4)
	b.Stage("a", 1)
	_ = b.Freeze()
	require.PanicsWithValue(t,
		"BinarySearchGrowingKVBuilder: Stage after Freeze",
		func() { b.Stage("b", 2) },
	)
}

func TestBSKVBuilder_PanicsOnStageSeqAfterFreeze(t *testing.T) {
	b := NewBinarySearchGrowingKVBuilderOrdered[string, int](4)
	_ = b.Freeze()
	require.PanicsWithValue(t,
		"BinarySearchGrowingKVBuilder: StageSeq after Freeze",
		func() {
			b.StageSeq(maps.All(map[string]int{"x": 1}))
		},
	)
}

func TestBSKVBuilder_PanicsOnDoubleFreeze(t *testing.T) {
	b := NewBinarySearchGrowingKVBuilderOrdered[string, int](4)
	b.Stage("a", 1)
	_ = b.Freeze()
	require.PanicsWithValue(t,
		"BinarySearchGrowingKVBuilder: Freeze called twice",
		func() { _ = b.Freeze() },
	)
}

func TestBSKVBuilder_CustomComparator(t *testing.T) {
	// Reverse-order comparator to verify cmpKey is honoured through
	// the build → freeze → iterate pipeline.
	reverse := func(a, b string) int { return cmp.Compare(b, a) }
	bld := NewBinarySearchGrowingKVBuilder[string, int](4, reverse)
	bld.Stage("a", 1)
	bld.Stage("c", 3)
	bld.Stage("b", 2)

	kv := bld.Freeze()
	require.Equal(t, []string{"c", "b", "a"}, slices.Collect(kv.IterateKeys()))
}

func TestBSKVBuilder_PreservesOwnership(t *testing.T) {
	// The frozen container owns the builder's backing slices — verify
	// by mutating through the BSKV API and confirming the data is
	// independent of the builder.
	b := NewBinarySearchGrowingKVBuilderOrdered[string, int](4)
	b.Stage("a", 1)
	b.Stage("b", 2)
	kv := b.Freeze()

	require.True(t, kv.Delete("a"))
	require.Equal(t, 1, kv.Len())
	require.False(t, kv.Has("a"))
}

func TestBSKVBuilder_FrozenContainerSupportsAllReads(t *testing.T) {
	// Smoke-test that every BSKV read entry point works on a frozen
	// container with no further setup.
	b := NewBinarySearchGrowingKVBuilderOrdered[string, int](4)
	b.Stage("x", 10)
	b.Stage("y", 20)
	kv := b.Freeze()

	require.False(t, kv.IsEmpty())
	require.Equal(t, 2, kv.Len())
	require.True(t, kv.Has("x"))
	v, has := kv.Get("y")
	require.True(t, has)
	require.Equal(t, 20, v)
	require.Equal(t, 99, kv.GetDefault("missing", 99))

	var seen []string
	for k := range kv.IterateKeys() {
		seen = append(seen, k)
	}
	require.Equal(t, []string{"x", "y"}, seen)
}

// ExampleBinarySearchGrowingKVBuilder shows the canonical build → freeze
// → read flow.
func ExampleBinarySearchGrowingKVBuilder() {
	b := NewBinarySearchGrowingKVBuilderOrdered[string, int](4)
	b.Stage("charlie", 3)
	b.Stage("alpha", 1)
	b.Stage("bravo", 2)

	kv := b.Freeze()
	for k, v := range kv.IteratePairs() {
		fmt.Printf("%s=%d\n", k, v)
	}
	// Output:
	// alpha=1
	// bravo=2
	// charlie=3
}

// ExampleBinarySearchGrowingKVBuilder_stageSeq shows bulk-staging from
// an existing map (or any iter.Seq2 source).
func ExampleBinarySearchGrowingKVBuilder_stageSeq() {
	src := map[string]int{"z": 26, "a": 1, "m": 13}

	b := NewBinarySearchGrowingKVBuilderOrdered[string, int](len(src))
	b.StageSeq(maps.All(src))

	kv := b.Freeze()
	for k, v := range kv.IteratePairs() {
		fmt.Printf("%s=%d\n", k, v)
	}
	// Output:
	// a=1
	// m=13
	// z=26
}

// ExampleBinarySearchGrowingKVBuilder_newestWins shows that duplicate
// keys in the staging buffer collapse to the most recent value at
// Freeze time.
func ExampleBinarySearchGrowingKVBuilder_newestWins() {
	b := NewBinarySearchGrowingKVBuilderOrdered[string, string](4)
	b.Stage("config", "v1")
	b.Stage("config", "v2")
	b.Stage("config", "v3")

	kv := b.Freeze()
	v, _ := kv.Get("config")
	fmt.Println(v)
	// Output: v3
}
