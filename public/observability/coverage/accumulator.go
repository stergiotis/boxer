package coverage

import (
	"sync"

	"github.com/RoaringBitmap/roaring"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefaultRestateEvery is the default full re-statement period in ticks: at
// the sampler's default cadence this bounds how long a consumer that lost a
// delta stays stale.
const DefaultRestateEvery = 60

// AccumulatorOptions parameterize the fold engine.
type AccumulatorOptions struct {
	// RestateEvery emits a full re-statement every N folds; 0 selects
	// DefaultRestateEvery, 1 makes every update full. The first fold is
	// always full.
	RestateEvery uint64
}

// Accumulator folds successive counter snapshots of one build into the
// cumulative covered set and pre-aggregated updates (ADR-0169 §SD3). It is
// pure state — no runtime/coverage, no clock — so it is fully testable with
// synthetic snapshots; Sampler wires it to the live runtime.
//
// Coverage is treated as monotone: units only ever enter the covered set.
// A snapshot with fewer or zeroed counters (only possible after
// ClearCounters, which nothing in this design calls) folds as "no change".
//
// Fold has a single-writer contract (one sampler goroutine); the read
// accessors are safe to call concurrently from provider goroutines.
type Accumulator struct {
	mu           sync.Mutex
	meta         *covsnap.MetaProfile
	restateEvery uint64
	seq          uint64
	covered      *roaring.Bitmap
	status       covsnap.RunStatus
	pkgAgg       []covsnap.PkgSample
	funcCovered  []uint32 // covered-unit count per flat function index
	funcFlatBase []int    // flat-index base per package
}

func NewAccumulator(meta *covsnap.MetaProfile, opts AccumulatorOptions) (inst *Accumulator) {
	restateEvery := opts.RestateEvery
	if restateEvery == 0 {
		restateEvery = DefaultRestateEvery
	}
	inst = &Accumulator{
		meta:         meta,
		restateEvery: restateEvery,
		covered:      roaring.New(),
		pkgAgg:       make([]covsnap.PkgSample, len(meta.Pkgs)),
		funcFlatBase: make([]int, len(meta.Pkgs)),
	}
	totalFuncs := 0
	for i := range meta.Pkgs {
		inst.pkgAgg[i].PkgIdx = uint32(i)
		inst.funcFlatBase[i] = totalFuncs
		totalFuncs += len(meta.Pkgs[i].Funcs)
	}
	inst.funcCovered = make([]uint32, totalFuncs)
	inst.status = covsnap.RunStatus{
		TotalUnits: meta.TotalUnits,
		TotalStmts: meta.TotalStmts,
		TotalFuncs: uint32(totalFuncs),
	}
	return
}

// Fold absorbs one counter snapshot and returns the tick's update. The
// sampledAtUnixMs stamp is the caller's so the engine stays clock-free.
func (inst *Accumulator) Fold(snap *covsnap.CounterSnapshot, sampledAtUnixMs int64) (upd *covsnap.Update, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if snap.MetaHash != inst.meta.Hash {
		return nil, eb.Build().Hex("snapshotMetaHash", snap.MetaHash[:]).Hex("buildMetaHash", inst.meta.Hash[:]).Errorf("counter snapshot meta hash does not match the accumulator's build")
	}
	inst.seq++
	full := inst.seq == 1 || inst.seq%inst.restateEvery == 0

	delta := roaring.New()
	touchedPkg := make(map[uint32]struct{})
	var touchedFuncs []covsnap.FuncSample
	for _, fc := range snap.Funcs {
		_, fn, ok := inst.meta.LookupFunc(fc.PkgIdx, fc.FuncIdx)
		if !ok {
			return nil, eb.Build().Uint32("pkgIdx", fc.PkgIdx).Uint32("funcIdx", fc.FuncIdx).Errorf("counter snapshot references an unknown function")
		}
		if len(fc.Counters) != len(fn.Units) {
			return nil, eb.Build().Int("counters", len(fc.Counters)).Uint32("pkgIdx", fc.PkgIdx).Uint32("funcIdx", fc.FuncIdx).Int("units", len(fn.Units)).Errorf("counter snapshot has a different number of counters than the function has units")
		}
		nonzero := uint32(0)
		for _, c := range fc.Counters {
			if c > 0 {
				nonzero++
			}
		}
		flat := inst.funcFlatBase[fc.PkgIdx] + int(fc.FuncIdx)
		before := inst.funcCovered[flat]
		// Monotone counters mean the nonzero set only grows; an equal (or
		// regressed) count proves no new units without a per-unit walk.
		if nonzero <= before {
			continue
		}
		agg := &inst.pkgAgg[fc.PkgIdx]
		for i, c := range fc.Counters {
			if c == 0 {
				continue
			}
			gid := fn.UnitBase + uint32(i)
			if inst.covered.Contains(gid) {
				continue
			}
			inst.covered.Add(gid)
			delta.Add(gid)
			stmts := fn.Units[i].NxStmts
			agg.CoveredUnits++
			agg.CoveredStmts += stmts
			inst.status.CoveredUnits++
			inst.status.CoveredStmts += stmts
		}
		if before == 0 {
			agg.CoveredFuncs++
			inst.status.CoveredFuncs++
		}
		inst.funcCovered[flat] = nonzero
		touchedPkg[fc.PkgIdx] = struct{}{}
		touchedFuncs = append(touchedFuncs, covsnap.FuncSample{PkgIdx: fc.PkgIdx, FuncIdx: fc.FuncIdx, CoveredUnits: nonzero})
	}

	upd = &covsnap.Update{
		MetaHash:        inst.meta.Hash,
		Seq:             inst.seq,
		SampledAtUnixMs: sampledAtUnixMs,
		Full:            full,
		Status:          inst.status,
	}
	if full {
		upd.Units = inst.covered.Clone()
		for i := range inst.pkgAgg {
			if inst.pkgAgg[i].CoveredUnits > 0 {
				upd.Pkgs = append(upd.Pkgs, inst.pkgAgg[i])
			}
		}
		for pkgIdx := range inst.meta.Pkgs {
			base := inst.funcFlatBase[pkgIdx]
			for funcIdx := range inst.meta.Pkgs[pkgIdx].Funcs {
				if covered := inst.funcCovered[base+funcIdx]; covered > 0 {
					upd.Funcs = append(upd.Funcs, covsnap.FuncSample{PkgIdx: uint32(pkgIdx), FuncIdx: uint32(funcIdx), CoveredUnits: covered})
				}
			}
		}
	} else {
		upd.Units = delta
		for pkgIdx := range inst.pkgAgg {
			if _, touched := touchedPkg[uint32(pkgIdx)]; touched {
				upd.Pkgs = append(upd.Pkgs, inst.pkgAgg[pkgIdx])
			}
		}
		upd.Funcs = touchedFuncs
	}
	return
}

// Status returns the current absolute cumulative totals.
func (inst *Accumulator) Status() (status covsnap.RunStatus) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.status
}

// CoveredBitmap returns a clone of the cumulative covered set.
func (inst *Accumulator) CoveredBitmap() (covered *roaring.Bitmap) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.covered.Clone()
}

// Meta returns the build's lookup profile. The profile is immutable after
// decode; callers must not mutate it.
func (inst *Accumulator) Meta() (meta *covsnap.MetaProfile) {
	return inst.meta
}

// Seq returns the number of folds so far.
func (inst *Accumulator) Seq() (seq uint64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.seq
}
