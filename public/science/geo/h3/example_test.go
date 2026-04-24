//go:build llm_generated_opus47

package h3_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/stergiotis/boxer/public/science/geo/h3"
)

// ExampleRuntime demonstrates the minimum flow: build a Runtime, acquire
// a Handle, call a bulk method, release the handle, close the runtime.
//
// No `// Output:` block: the example depends on the compiled h3.wasm
// artifact. When the repository ships a placeholder (pre-build), this
// example cannot assert a fixed output. The companion pure-Go examples
// below carry output assertions.
func ExampleRuntime() {
	ctx := context.Background()
	rt, err := h3.NewRuntime(ctx, h3.RuntimeConfig{PoolSize: 1})
	if err != nil {
		if errors.Is(err, h3.ErrExportNotFound) || errors.Is(err, h3.ErrNoWasmBytes) {
			fmt.Println("skip: wasm bridge not built")
			return
		}
		fmt.Println("error:", err)
		return
	}
	defer func() { _ = rt.Close() }()

	handle, err := rt.AcquireE(ctx)
	if err != nil {
		fmt.Println("acquire:", err)
		return
	}
	defer handle.Release()

	lats := []float64{37.7749}
	lngs := []float64{-122.4194}
	cells, status, err := handle.LatLngsToCellsE(ctx, h3.ResolutionR9, lats, lngs, nil, nil)
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Printf("status=%s cells=%d\n", status[0], len(cells))
}

// ExampleAllLatLngs demonstrates iterating parallel SoA outputs as
// (cell, LatLng) views without allocating per-element structs.
func ExampleAllLatLngs() {
	cells := []uint64{1, 2, 3}
	lats := []float64{0.0, 10.0, 20.0}
	lngs := []float64{0.0, 11.0, 22.0}
	for cell, ll := range h3.AllLatLngs(cells, lats, lngs) {
		fmt.Printf("cell=%d lat=%.1f lng=%.1f\n", cell, ll.LatDeg, ll.LngDeg)
	}
	// Output:
	// cell=1 lat=0.0 lng=0.0
	// cell=2 lat=10.0 lng=11.0
	// cell=3 lat=20.0 lng=22.0
}

// ExampleAllCSRRowsU64 demonstrates iterating a CSR-shaped payload
// (e.g., children of a cell) row-by-row.
func ExampleAllCSRRowsU64() {
	values := []uint64{100, 101, 102, 200, 300, 301}
	offsets := []int32{0, 3, 4, 6}
	for row, slice := range h3.AllCSRRowsU64(values, offsets) {
		fmt.Printf("row=%d values=%v\n", row, slice)
	}
	// Output:
	// row=0 values=[100 101 102]
	// row=1 values=[200]
	// row=2 values=[300 301]
}

// ExampleHandle_LatLngToCellE demonstrates the scalar wrapper for the
// common "one point → one cell" UI-glue case. Returns the cell index
// and per-element status directly, without the caller having to build
// a 1-element slice or index [0] on return.
func ExampleHandle_LatLngToCellE() {
	ctx := context.Background()
	rt, err := h3.NewRuntime(ctx, h3.RuntimeConfig{PoolSize: 1})
	if err != nil {
		if errors.Is(err, h3.ErrExportNotFound) || errors.Is(err, h3.ErrNoWasmBytes) {
			fmt.Println("skip: wasm bridge not built")
			return
		}
		fmt.Println("error:", err)
		return
	}
	defer func() { _ = rt.Close() }()

	handle, err := rt.AcquireE(ctx)
	if err != nil {
		fmt.Println("acquire:", err)
		return
	}
	defer handle.Release()

	cell, status, err := handle.LatLngToCellE(ctx, h3.ResolutionR9, 37.7749, -122.4194)
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Printf("status=%s cell=%d\n", status, cell)
}

// ExampleHandle_GridDiskE shows the scalar k-ring wrapper — returns a
// flat []uint64 for single-cell inputs, skipping the CSR offsets[] that
// the bulk form produces for N-cell batches.
func ExampleHandle_GridDiskE() {
	ctx := context.Background()
	rt, err := h3.NewRuntime(ctx, h3.RuntimeConfig{PoolSize: 1})
	if err != nil {
		if errors.Is(err, h3.ErrExportNotFound) || errors.Is(err, h3.ErrNoWasmBytes) {
			fmt.Println("skip: wasm bridge not built")
			return
		}
		fmt.Println("error:", err)
		return
	}
	defer func() { _ = rt.Close() }()

	handle, err := rt.AcquireE(ctx)
	if err != nil {
		fmt.Println("acquire:", err)
		return
	}
	defer handle.Release()

	cell, _, err := handle.LatLngToCellE(ctx, h3.ResolutionR7, 51.0992, 17.0366)
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	ring, _, err := handle.GridDiskE(ctx, 2, cell)
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Printf("k=2 ring has %d cells\n", len(ring))
}

// ExampleFirstFailure demonstrates the status-triage helper. Typical use:
// short-circuit the bulk op on any non-Ok element.
func ExampleFirstFailure() {
	statuses := []h3.StatusE{h3.StatusOk, h3.StatusOk, h3.StatusInvalidCell, h3.StatusOk}
	idx, code, ok := h3.FirstFailure(statuses)
	if !ok {
		fmt.Println("all ok")
		return
	}
	fmt.Printf("first failure at index %d: %s\n", idx, code)
	// Output: first failure at index 2: invalid_cell
}
