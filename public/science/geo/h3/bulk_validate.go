//go:build llm_generated_opus47

package h3

import (
	"context"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// AreValidCellsE reports, for each input cell, whether it is a
// well-formed H3 index.
func (inst *Handle) AreValidCellsE(
	ctx context.Context,
	cells []uint64,
	validDst []bool,
) (valid []bool, err error) {
	n := len(cells)
	valid = slices.Grow(validDst[:0], n)[:n]
	if n == 0 {
		return
	}
	var cellsOff, validOff uint32
	cellsOff, err = inst.allocE(ctx, n*8)
	if err != nil {
		return
	}
	defer inst.freeNoE(ctx, cellsOff, n*8)
	validOff, err = inst.allocE(ctx, n)
	if err != nil {
		return
	}
	defer inst.freeNoE(ctx, validOff, n)

	err = inst.writeU64sE(cellsOff, cells)
	if err != nil {
		return
	}
	_, err = inst.fnAreValid.Call(
		ctx,
		uint64(cellsOff), uint64(uint32(n)),
		uint64(validOff),
	)
	if err != nil {
		err = eh.Errorf("h3_are_valid: %w", err)
		return
	}
	var raw []byte
	raw, err = inst.readBytesE(validOff, n)
	if err != nil {
		return
	}
	for i, b := range raw {
		valid[i] = b != 0
	}
	return
}

// GetResolutionsE returns the resolution of each input cell.
func (inst *Handle) GetResolutionsE(
	ctx context.Context,
	cells []uint64,
	resDst []ResolutionE,
	statusDst []StatusE,
) (res []ResolutionE, status []StatusE, err error) {
	n := len(cells)
	res = slices.Grow(resDst[:0], n)[:n]
	status = slices.Grow(statusDst[:0], n)[:n]
	if n == 0 {
		return
	}
	var cellsOff, resOff, statusOff uint32
	cellsOff, err = inst.allocE(ctx, n*8)
	if err != nil {
		return
	}
	defer inst.freeNoE(ctx, cellsOff, n*8)
	resOff, err = inst.allocE(ctx, n)
	if err != nil {
		return
	}
	defer inst.freeNoE(ctx, resOff, n)
	statusOff, err = inst.allocE(ctx, n)
	if err != nil {
		return
	}
	defer inst.freeNoE(ctx, statusOff, n)

	err = inst.writeU64sE(cellsOff, cells)
	if err != nil {
		return
	}
	_, err = inst.fnGetResolution.Call(
		ctx,
		uint64(cellsOff), uint64(uint32(n)),
		uint64(resOff), uint64(statusOff),
	)
	if err != nil {
		err = eh.Errorf("h3_get_resolution: %w", err)
		return
	}
	var raw []byte
	raw, err = inst.readBytesE(resOff, n)
	if err != nil {
		return
	}
	for i, b := range raw {
		res[i] = ResolutionE(b)
	}
	err = inst.readStatusE(statusOff, status)
	return
}
