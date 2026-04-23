//go:build llm_generated_opus47

package h3

import (
	"iter"
	"unsafe"
)

// AllLatLngs pairs up parallel SoA lat/lng outputs as (cell, LatLng) views.
// cells, latsDeg and lngsDeg must be of equal length; shorter slices stop
// the iteration early. The LatLng view is constructed on the stack per
// yield; no allocations occur.
func AllLatLngs(cells []uint64, latsDeg []float64, lngsDeg []float64) iter.Seq2[uint64, LatLng] {
	return func(yield func(uint64, LatLng) bool) {
		n := len(cells)
		if len(latsDeg) < n {
			n = len(latsDeg)
		}
		if len(lngsDeg) < n {
			n = len(lngsDeg)
		}
		for i := 0; i < n; i++ {
			if !yield(cells[i], LatLng{LatDeg: latsDeg[i], LngDeg: lngsDeg[i]}) {
				return
			}
		}
	}
}

// AllCSRRowsU64 iterates a CSR-shaped uint64 payload (e.g., children,
// gridDisk) as (rowIdx, rowSlice) pairs. The row slice is a view into
// values — do not retain it past the yield.
func AllCSRRowsU64(values []uint64, offsets []int32) iter.Seq2[int, []uint64] {
	return func(yield func(int, []uint64) bool) {
		if len(offsets) < 2 {
			return
		}
		for i := 0; i < len(offsets)-1; i++ {
			start := int(offsets[i])
			end := int(offsets[i+1])
			if start < 0 || end < start || end > len(values) {
				return
			}
			if !yield(i, values[start:end]) {
				return
			}
		}
	}
}

// AllCSRRowsLatLng iterates a CSR-shaped pair of parallel lat/lng slices
// (e.g., cell boundaries) as (rowIdx, (latRow, lngRow)) triplets. Each row
// is a zero-copy view — do not retain it past the yield.
func AllCSRRowsLatLng(latsDeg []float64, lngsDeg []float64, offsets []int32) iter.Seq2[int, [2][]float64] {
	return func(yield func(int, [2][]float64) bool) {
		if len(offsets) < 2 {
			return
		}
		for i := 0; i < len(offsets)-1; i++ {
			start := int(offsets[i])
			end := int(offsets[i+1])
			if start < 0 || end < start || end > len(latsDeg) || end > len(lngsDeg) {
				return
			}
			if !yield(i, [2][]float64{latsDeg[start:end], lngsDeg[start:end]}) {
				return
			}
		}
	}
}

// AllCSRRowsString iterates a CSR-shaped []byte payload (e.g., cell
// strings) as (rowIdx, string) pairs. The string is a zero-copy view into
// buf — do not retain it past the yield.
func AllCSRRowsString(buf []byte, offsets []int32) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		if len(offsets) < 2 {
			return
		}
		for i := 0; i < len(offsets)-1; i++ {
			start := int(offsets[i])
			end := int(offsets[i+1])
			if start < 0 || end < start || end > len(buf) {
				return
			}
			// Zero-copy view. Safe because buf outlives the yield call
			// (the caller retains the []byte).
			slice := buf[start:end]
			s := unsafe.String(unsafe.SliceData(slice), len(slice))
			if !yield(i, s) {
				return
			}
		}
	}
}
