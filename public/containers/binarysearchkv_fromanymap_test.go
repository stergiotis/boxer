package containers

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewBinarySearchGrowingKVFromAnyMap_NilEmpty(t *testing.T) {
	require.Nil(t, NewBinarySearchGrowingKVFromAnyMap(nil))
	require.Nil(t, NewBinarySearchGrowingKVFromAnyMap(map[string]interface{}{}))
}

func TestNewBinarySearchGrowingKVFromAnyMap_FlatScalars(t *testing.T) {
	in := map[string]interface{}{
		"title": "Sample",
		"count": 42,
		"draft": true,
		"score": 3.14,
		"empty": nil,
	}
	kv := NewBinarySearchGrowingKVFromAnyMap(in)
	require.NotNil(t, kv)
	require.Equal(t, 5, kv.Len())

	keys := slices.Collect(kv.IterateKeys())
	require.Equal(t, []string{"count", "draft", "empty", "score", "title"}, keys)

	v, has := kv.Get("title")
	require.True(t, has)
	require.Equal(t, "Sample", v)

	v, has = kv.Get("count")
	require.True(t, has)
	require.Equal(t, 42, v)

	v, has = kv.Get("empty")
	require.True(t, has)
	require.Nil(t, v)
}

func TestNewBinarySearchGrowingKVFromAnyMap_DeterministicOrder(t *testing.T) {
	// Iterate the same input map many times via NewBinarySearchGrowingKVFromAnyMap
	// — every call must yield the same key sequence regardless of Go's
	// random map iteration order on the input.
	in := map[string]interface{}{
		"zeta": 1, "alpha": 2, "mu": 3, "beta": 4, "kappa": 5,
	}
	expected := []string{"alpha", "beta", "kappa", "mu", "zeta"}
	for i := 0; i < 50; i++ {
		kv := NewBinarySearchGrowingKVFromAnyMap(in)
		require.Equal(t, expected, slices.Collect(kv.IterateKeys()))
	}
}

func TestNewBinarySearchGrowingKVFromAnyMap_NestedMap(t *testing.T) {
	in := map[string]interface{}{
		"meta": map[string]interface{}{
			"author": "alice",
			"year":   2026,
		},
		"flat": "x",
	}
	kv := NewBinarySearchGrowingKVFromAnyMap(in)
	require.NotNil(t, kv)

	flat, has := kv.Get("flat")
	require.True(t, has)
	require.Equal(t, "x", flat)

	metaRaw, has := kv.Get("meta")
	require.True(t, has)
	meta, ok := metaRaw.(*BinarySearchGrowingKV[string, interface{}])
	require.True(t, ok, "nested map should be converted to a BinarySearchGrowingKV")
	require.Equal(t, []string{"author", "year"}, slices.Collect(meta.IterateKeys()))
	v, _ := meta.Get("author")
	require.Equal(t, "alice", v)
}

func TestNewBinarySearchGrowingKVFromAnyMap_NestedAnyKeyMap(t *testing.T) {
	// yaml.v2 produces map[interface{}]interface{} for nested maps.
	// The converter must normalise non-string keys via fmt.Sprintf.
	in := map[string]interface{}{
		"meta": map[interface{}]interface{}{
			"author": "bob",
			42:       "answer",
			true:     "yes",
		},
	}
	kv := NewBinarySearchGrowingKVFromAnyMap(in)
	require.NotNil(t, kv)
	metaRaw, _ := kv.Get("meta")
	meta, ok := metaRaw.(*BinarySearchGrowingKV[string, interface{}])
	require.True(t, ok)
	require.ElementsMatch(t, []string{"42", "author", "true"}, slices.Collect(meta.IterateKeys()))
	v, _ := meta.Get("42")
	require.Equal(t, "answer", v)
}

func TestNewBinarySearchGrowingKVFromAnyMap_NestedSlice(t *testing.T) {
	in := map[string]interface{}{
		"tags": []interface{}{"go", "yaml", "demo"},
		"items": []interface{}{
			map[string]interface{}{"name": "first", "qty": 3},
			map[string]interface{}{"name": "second", "qty": 5},
		},
	}
	kv := NewBinarySearchGrowingKVFromAnyMap(in)

	tagsRaw, _ := kv.Get("tags")
	tags, ok := tagsRaw.([]interface{})
	require.True(t, ok)
	require.Equal(t, []interface{}{"go", "yaml", "demo"}, tags)

	itemsRaw, _ := kv.Get("items")
	items, ok := itemsRaw.([]interface{})
	require.True(t, ok)
	require.Len(t, items, 2)
	first, ok := items[0].(*BinarySearchGrowingKV[string, interface{}])
	require.True(t, ok, "map values inside a slice should be recursively converted")
	require.Equal(t, []string{"name", "qty"}, slices.Collect(first.IterateKeys()))
}
