package cborarrow

import (
	"bytes"
	"math"
	"testing"

	cbor "github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
)

func convertRow(t *testing.T, row map[string]any) (err error) {
	t.Helper()
	in, err := cbor.Marshal([]map[string]any{row})
	require.NoError(t, err)
	var out bytes.Buffer
	return Convert(bytes.NewReader(in), &out)
}

// A wire count above MaxInt must be refused, not narrowed to a negative
// int that passes the bounds check and indexes out of range.
func TestConvert_ScalarHugeLrcardRejected(t *testing.T) {
	err := convertRow(t, map[string]any{
		"symbol.lr":     []uint64{1, 2},
		"symbol.lrcard": []uint64{math.MaxUint64, 1},
		"symbol.value":  []string{},
	})
	require.Error(t, err)
}

func TestConvert_RangeHugeLrcardRejected(t *testing.T) {
	err := convertRow(t, map[string]any{
		"u32Range.lr":        []uint64{1, 2},
		"u32Range.lrcard":    []uint64{math.MaxUint64, 1},
		"u32Range.beginIncl": []uint32{},
		"u32Range.endExcl":   []uint32{},
	})
	require.Error(t, err)
}

// Counts whose int sum wraps to within the value length must be refused.
func TestConvert_ArrayWrappingCountsRejected(t *testing.T) {
	err := convertRow(t, map[string]any{
		"u32Array.lr":     []uint64{1, 2},
		"u32Array.lrcard": []uint64{1, 1},
		"u32Array.len":    []uint64{5, math.MaxUint64 - 4},
		"u32Array.value":  []uint32{},
	})
	require.Error(t, err)
}

func TestConvert_ScalarWellFormedAccepted(t *testing.T) {
	err := convertRow(t, map[string]any{
		"symbol.lr":     []uint64{1, 2},
		"symbol.lrcard": []uint64{1, 2},
		"symbol.value":  []string{"a", "b", "c"},
	})
	require.NoError(t, err)
}
