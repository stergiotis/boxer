package containers

import (
	"fmt"
)

// NewBinarySearchGrowingKVFromAnyMap converts a dynamically-typed map
// (typically produced by YAML or JSON decoding) into a key-sorted
// BinarySearchGrowingKV.
//
// The conversion is recursive: nested map[string]any and
// map[any]any values are themselves converted into nested
// BinarySearchGrowingKV[string, any] values; []any sequences are
// walked and any maps inside them are converted too. Other values
// (string, int, bool, nil, …) pass through unchanged.
//
// Returns nil if m is nil or empty so callers can early-out on
// `if kv == nil`. The bulk path uses [BinarySearchGrowingKV.UpsertBatch]
// + a single [BinarySearchGrowingKV.ensureSorted] on first read, which
// is O(N log N) instead of UpsertSingle's O(N²).
//
// yaml.v2 sometimes produces map[any]any for nested maps; non-string
// keys are stringified with fmt.Sprintf("%v", k) to match the
// renderer behaviour in
// boxer/public/semistructured/markdown/obsidian/frontmatter.go.
func NewBinarySearchGrowingKVFromAnyMap(m map[string]interface{}) (kv *BinarySearchGrowingKV[string, interface{}]) {
	if len(m) == 0 {
		return
	}
	kv = NewBinarySearchGrowingKVOrdered[string, interface{}](len(m))
	for k, v := range m {
		kv.UpsertBatch(k, convertAnyMapValue(v))
	}
	return
}

// convertAnyMapValue is the recursive sibling of
// NewBinarySearchGrowingKVFromAnyMap. It walks one decoded value and
// returns the same value with maps replaced by their KV equivalent.
// Cycles cannot occur in YAML/JSON-decoded data, so no cycle guard is
// needed.
func convertAnyMapValue(v interface{}) (out interface{}) {
	switch t := v.(type) {
	case map[string]interface{}:
		out = NewBinarySearchGrowingKVFromAnyMap(t)
	case map[interface{}]interface{}:
		normalized := make(map[string]interface{}, len(t))
		for mk, mv := range t {
			normalized[fmt.Sprintf("%v", mk)] = mv
		}
		out = NewBinarySearchGrowingKVFromAnyMap(normalized)
	case []interface{}:
		converted := make([]interface{}, len(t))
		for i, item := range t {
			converted[i] = convertAnyMapValue(item)
		}
		out = converted
	default:
		out = v
	}
	return
}
