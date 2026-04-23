//go:build llm_generated_opus47

package h3arrow

import (
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Sentinel errors.
var (
	// ErrEmptyOffsets is returned by the CSR adapters when the offsets
	// slice is empty (it must have at least one element — the terminating
	// offsets[N]).
	ErrEmptyOffsets = eh.New("h3arrow: CSR offsets must be non-empty (len(offsets) == N+1)")

	// ErrOffsetsOutOfRange is returned when offsets[N] exceeds len(values).
	ErrOffsetsOutOfRange = eh.New("h3arrow: CSR offsets[N] exceeds len(values)")
)

// bytesOfU64 returns a zero-copy []byte view over a []uint64 backing
// array. Assumes little-endian host (documented at package h3).
func bytesOfU64(vals []uint64) (out []byte) {
	if len(vals) == 0 {
		return
	}
	out = unsafe.Slice((*byte)(unsafe.Pointer(&vals[0])), len(vals)*8)
	return
}

// bytesOfF64 returns a zero-copy []byte view over a []float64 backing
// array.
func bytesOfF64(vals []float64) (out []byte) {
	if len(vals) == 0 {
		return
	}
	out = unsafe.Slice((*byte)(unsafe.Pointer(&vals[0])), len(vals)*8)
	return
}

// bytesOfI32 returns a zero-copy []byte view over a []int32 backing array.
func bytesOfI32(vals []int32) (out []byte) {
	if len(vals) == 0 {
		return
	}
	out = unsafe.Slice((*byte)(unsafe.Pointer(&vals[0])), len(vals)*4)
	return
}

// CellsAsArrowUint64 zero-copy-wraps an h3 cell slice as an arrow Uint64
// array. The caller must keep cells reachable until the returned array is
// Released.
func CellsAsArrowUint64(cells []uint64) (out *array.Uint64) {
	buf := memory.NewBufferBytes(bytesOfU64(cells))
	data := array.NewData(
		arrow.PrimitiveTypes.Uint64,
		len(cells),
		[]*memory.Buffer{nil, buf},
		nil,
		0, 0,
	)
	defer data.Release()
	out = array.NewUint64Data(data)
	return
}

// Float64sAsArrowFloat64 zero-copy-wraps a []float64 as an arrow Float64
// array. The caller must keep vals reachable until the returned array is
// Released.
func Float64sAsArrowFloat64(vals []float64) (out *array.Float64) {
	buf := memory.NewBufferBytes(bytesOfF64(vals))
	data := array.NewData(
		arrow.PrimitiveTypes.Float64,
		len(vals),
		[]*memory.Buffer{nil, buf},
		nil,
		0, 0,
	)
	defer data.Release()
	out = array.NewFloat64Data(data)
	return
}

// CSRAsArrowListUint64E zero-copy-wraps CSR (values, offsets) as an arrow
// List<Uint64> array with N = len(offsets)-1 rows. offsets must have
// offsets[0] == 0 and offsets[N] == len(values); these are the standard
// CSR invariants emitted by the h3 package's variable-arity bulk methods
// (e.g., [h3.Handle.CellsToChildrenE], [h3.Handle.GridDisksE]).
//
// Returns the wrapped array or nil on invariant violation. The caller must
// keep values and offsets reachable until the array is Released.
func CSRAsArrowListUint64E(values []uint64, offsets []int32) (out *array.List, err error) {
	err = validateCSR(offsets, len(values))
	if err != nil {
		return
	}
	n := len(offsets) - 1

	valuesBuf := memory.NewBufferBytes(bytesOfU64(values))
	valuesData := array.NewData(
		arrow.PrimitiveTypes.Uint64,
		len(values),
		[]*memory.Buffer{nil, valuesBuf},
		nil,
		0, 0,
	)
	defer valuesData.Release()

	offsetsBuf := memory.NewBufferBytes(bytesOfI32(offsets))
	listData := array.NewData(
		arrow.ListOf(arrow.PrimitiveTypes.Uint64),
		n,
		[]*memory.Buffer{nil, offsetsBuf},
		[]arrow.ArrayData{valuesData},
		0, 0,
	)
	defer listData.Release()
	out = array.NewListData(listData)
	return
}

// CSRAsArrowListFloat64E zero-copy-wraps CSR (values, offsets) as an arrow
// List<Float64> array with N = len(offsets)-1 rows. Used by consumers of
// [h3.Handle.CellsToBoundariesE] to hand lat and lng ring rows into arrow
// pipelines.
func CSRAsArrowListFloat64E(values []float64, offsets []int32) (out *array.List, err error) {
	err = validateCSR(offsets, len(values))
	if err != nil {
		return
	}
	n := len(offsets) - 1

	valuesBuf := memory.NewBufferBytes(bytesOfF64(values))
	valuesData := array.NewData(
		arrow.PrimitiveTypes.Float64,
		len(values),
		[]*memory.Buffer{nil, valuesBuf},
		nil,
		0, 0,
	)
	defer valuesData.Release()

	offsetsBuf := memory.NewBufferBytes(bytesOfI32(offsets))
	listData := array.NewData(
		arrow.ListOf(arrow.PrimitiveTypes.Float64),
		n,
		[]*memory.Buffer{nil, offsetsBuf},
		[]arrow.ArrayData{valuesData},
		0, 0,
	)
	defer listData.Release()
	out = array.NewListData(listData)
	return
}

func validateCSR(offsets []int32, valuesLen int) (err error) {
	if len(offsets) == 0 {
		err = ErrEmptyOffsets
		return
	}
	last := offsets[len(offsets)-1]
	if int(last) > valuesLen || last < 0 {
		err = ErrOffsetsOutOfRange
	}
	return
}
