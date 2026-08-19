package containers

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewBinarySearchGrowingKVFromAnyMap_NilEmpty(t *testing.T) {
	require.Nil(t, NewBinarySearchGrowingKVFromAnyMap(nil))
	require.Nil(t, NewBinarySearchGrowingKVFromAnyMap(map[string]any{}))
}

func TestNewBinarySearchGrowingKVFromAnyMap_FlatScalars(t *testing.T) {
	in := map[string]any{
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
	in := map[string]any{
		"zeta": 1, "alpha": 2, "mu": 3, "beta": 4, "kappa": 5,
	}
	expected := []string{"alpha", "beta", "kappa", "mu", "zeta"}
	for range 50 {
		kv := NewBinarySearchGrowingKVFromAnyMap(in)
		require.Equal(t, expected, slices.Collect(kv.IterateKeys()))
	}
}

func TestNewBinarySearchGrowingKVFromAnyMap_NestedMap(t *testing.T) {
	in := map[string]any{
		"meta": map[string]any{
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
	meta, ok := metaRaw.(*BinarySearchGrowingKV[string, any])
	require.True(t, ok, "nested map should be converted to a BinarySearchGrowingKV")
	require.Equal(t, []string{"author", "year"}, slices.Collect(meta.IterateKeys()))
	v, _ := meta.Get("author")
	require.Equal(t, "alice", v)
}

func TestNewBinarySearchGrowingKVFromAnyMap_NestedAnyKeyMap(t *testing.T) {
	// yaml.v2 produces map[interface{}]interface{} for nested maps.
	// The converter must normalise non-string keys via fmt.Sprintf.
	in := map[string]any{
		"meta": map[any]any{
			"author": "bob",
			42:       "answer",
			true:     "yes",
		},
	}
	kv := NewBinarySearchGrowingKVFromAnyMap(in)
	require.NotNil(t, kv)
	metaRaw, _ := kv.Get("meta")
	meta, ok := metaRaw.(*BinarySearchGrowingKV[string, any])
	require.True(t, ok)
	require.ElementsMatch(t, []string{"42", "author", "true"}, slices.Collect(meta.IterateKeys()))
	v, _ := meta.Get("42")
	require.Equal(t, "answer", v)
}

// Distinct any-typed keys that stringify identically collapse to one
// entry; which value survives is documented as unspecified, so only the
// length is pinned here.
func TestNewBinarySearchGrowingKVFromAnyMap_StringifyCollision(t *testing.T) {
	in := map[string]any{
		"meta": map[any]any{42: "int-key", "42": "string-key"},
	}
	kv := NewBinarySearchGrowingKVFromAnyMap(in)
	metaRaw, has := kv.Get("meta")
	require.True(t, has)
	meta, ok := metaRaw.(*BinarySearchGrowingKV[string, any])
	require.True(t, ok)
	require.Equal(t, 1, meta.Len(), "42 and \"42\" collide on the stringified key")
	v, has := meta.Get("42")
	require.True(t, has)
	require.Contains(t, []any{"int-key", "string-key"}, v)
}

func TestNewBinarySearchGrowingKVFromAnyMap_NestedSlice(t *testing.T) {
	in := map[string]any{
		"tags": []any{"go", "yaml", "demo"},
		"items": []any{
			map[string]any{"name": "first", "qty": 3},
			map[string]any{"name": "second", "qty": 5},
		},
	}
	kv := NewBinarySearchGrowingKVFromAnyMap(in)

	tagsRaw, _ := kv.Get("tags")
	tags, ok := tagsRaw.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"go", "yaml", "demo"}, tags)

	itemsRaw, _ := kv.Get("items")
	items, ok := itemsRaw.([]any)
	require.True(t, ok)
	require.Len(t, items, 2)
	first, ok := items[0].(*BinarySearchGrowingKV[string, any])
	require.True(t, ok, "map values inside a slice should be recursively converted")
	require.Equal(t, []string{"name", "qty"}, slices.Collect(first.IterateKeys()))
}
