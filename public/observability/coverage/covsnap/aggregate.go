package covsnap

import (
	"github.com/RoaringBitmap/roaring"
)

// AggregateCovered recomputes the per-package and per-function rollups from
// a covered set — the from-scratch counterpart of the sampler's incremental
// maintenance (the two are cross-checked in tests). Consumers holding a
// meta profile and an accumulated bitmap (a re-statement, or a provider
// over live state) use it instead of re-deriving the range walk.
//
// The walk is a single ascending pass over the bitmap with a two-pointer
// advance through the meta's contiguous function ranges: O(covered+funcs).
// Bits outside the profile's unit range are ignored. Only covered
// functions/packages produce entries.
func AggregateCovered(meta *MetaProfile, covered *roaring.Bitmap) (pkgs []PkgSample, funcs []FuncSample) {
	if covered == nil || covered.IsEmpty() {
		return
	}
	pkgIdx := 0
	funcIdx := 0
	var cur *FuncSample
	var curPkg *PkgSample
	var curStmts uint32
	flushFunc := func() {
		if cur != nil {
			funcs = append(funcs, *cur)
			curPkg.CoveredUnits += cur.CoveredUnits
			curPkg.CoveredStmts += curStmts
			curPkg.CoveredFuncs++
			cur = nil
			curStmts = 0
		}
	}
	flushPkg := func() {
		flushFunc()
		if curPkg != nil {
			pkgs = append(pkgs, *curPkg)
			curPkg = nil
		}
	}
	it := covered.Iterator()
	for it.HasNext() {
		gid := it.Next()
		// Advance to the package owning gid.
		for pkgIdx < len(meta.Pkgs) && gid >= meta.Pkgs[pkgIdx].UnitBase+meta.Pkgs[pkgIdx].NumUnits {
			flushPkg()
			pkgIdx++
			funcIdx = 0
		}
		if pkgIdx >= len(meta.Pkgs) {
			break // bit beyond the profile — foreign, ignore
		}
		pkg := &meta.Pkgs[pkgIdx]
		// Advance to the function owning gid.
		for funcIdx < len(pkg.Funcs) && gid >= pkg.Funcs[funcIdx].UnitBase+uint32(len(pkg.Funcs[funcIdx].Units)) {
			flushFunc()
			funcIdx++
		}
		if funcIdx >= len(pkg.Funcs) {
			continue // gap inside the package range — foreign, ignore
		}
		fn := &pkg.Funcs[funcIdx]
		if gid < fn.UnitBase {
			continue
		}
		if curPkg == nil {
			curPkg = &PkgSample{PkgIdx: uint32(pkgIdx)}
		}
		if cur == nil {
			cur = &FuncSample{PkgIdx: uint32(pkgIdx), FuncIdx: uint32(funcIdx)}
		}
		cur.CoveredUnits++
		curStmts += fn.Units[gid-fn.UnitBase].NxStmts
	}
	flushPkg()
	return
}
