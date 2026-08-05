package coverage

import (
	"sync"
	"testing"

	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stretchr/testify/require"
)

func synthUnits(stmts ...uint32) (units []covsnap.UnitMeta) {
	units = make([]covsnap.UnitMeta, len(stmts))
	for i, s := range stmts {
		units[i] = covsnap.UnitMeta{StLine: uint32(i + 1), EnLine: uint32(i + 1), NxStmts: s}
	}
	return
}

// finalizeSynthMeta assigns the global unit index and totals the same way
// DecodeMeta does, so synthetic profiles obey the covsnap invariants.
func finalizeSynthMeta(prof *covsnap.MetaProfile) *covsnap.MetaProfile {
	next := uint32(0)
	stmts := uint32(0)
	for i := range prof.Pkgs {
		pkg := &prof.Pkgs[i]
		pkg.UnitBase = next
		pkg.NumStmts = 0
		for j := range pkg.Funcs {
			fn := &pkg.Funcs[j]
			fn.UnitBase = next
			fn.NumStmts = 0
			for _, u := range fn.Units {
				fn.NumStmts += u.NxStmts
			}
			next += uint32(len(fn.Units))
			stmts += fn.NumStmts
			pkg.NumStmts += fn.NumStmts
		}
		pkg.NumUnits = next - pkg.UnitBase
	}
	prof.TotalUnits = next
	prof.TotalStmts = stmts
	return prof
}

// Two packages, three functions: A has units g[0,3) with stmts (2,1,1),
// B g[3,5) with (3,2), C g[5,9) with (1,1,1,1). 9 units, 13 statements.
func synthMeta() *covsnap.MetaProfile {
	return finalizeSynthMeta(&covsnap.MetaProfile{
		Hash:        [16]byte{0xaa, 0xbb},
		Mode:        covsnap.ModeAtomic,
		Granularity: covsnap.GranularityPerBlock,
		Pkgs: []covsnap.PkgMeta{
			{Path: "m/a", Name: "a", ModulePath: "m", Funcs: []covsnap.FuncMeta{
				{Name: "A", SrcFile: "a.go", Units: synthUnits(2, 1, 1)},
				{Name: "B", SrcFile: "a.go", Units: synthUnits(3, 2)},
			}},
			{Path: "m/b", Name: "b", ModulePath: "m", Funcs: []covsnap.FuncMeta{
				{Name: "C", SrcFile: "b.go", Units: synthUnits(1, 1, 1, 1)},
			}},
		},
	})
}

func synthSnap(meta *covsnap.MetaProfile, funcs ...covsnap.FuncCounters) *covsnap.CounterSnapshot {
	return &covsnap.CounterSnapshot{MetaHash: meta.Hash, Funcs: funcs}
}

func funcSampleSet(t *testing.T, samples []covsnap.FuncSample) map[[2]uint32]uint32 {
	t.Helper()
	set := make(map[[2]uint32]uint32, len(samples))
	for _, fs := range samples {
		key := [2]uint32{fs.PkgIdx, fs.FuncIdx}
		require.NotContains(t, set, key, "duplicate func sample")
		set[key] = fs.CoveredUnits
	}
	return set
}

func TestAccumulatorFirstFoldIsFull(t *testing.T) {
	meta := synthMeta()
	acc := NewAccumulator(meta, AccumulatorOptions{})
	upd, err := acc.Fold(synthSnap(meta,
		covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 0, Counters: []uint32{1, 0, 1}},
		covsnap.FuncCounters{PkgIdx: 1, FuncIdx: 0, Counters: []uint32{0, 0, 0, 7}},
	), 42)
	require.NoError(t, err)
	require.Equal(t, uint64(1), upd.Seq)
	require.True(t, upd.Full)
	require.EqualValues(t, 42, upd.SampledAtUnixMs)
	require.Equal(t, meta.Hash, upd.MetaHash)
	require.EqualValues(t, 3, upd.Units.GetCardinality())
	require.True(t, upd.Units.Contains(0) && upd.Units.Contains(2) && upd.Units.Contains(8))
	require.Equal(t, covsnap.RunStatus{
		CoveredUnits: 3, TotalUnits: 9,
		CoveredStmts: 4, TotalStmts: 13,
		CoveredFuncs: 2, TotalFuncs: 3,
	}, upd.Status)
	require.Equal(t, []covsnap.PkgSample{
		{PkgIdx: 0, CoveredUnits: 2, CoveredStmts: 3, CoveredFuncs: 1},
		{PkgIdx: 1, CoveredUnits: 1, CoveredStmts: 1, CoveredFuncs: 1},
	}, upd.Pkgs)
	require.Equal(t, map[[2]uint32]uint32{{0, 0}: 2, {1, 0}: 1}, funcSampleSet(t, upd.Funcs))
}

func TestAccumulatorDeltaTicksCarryOnlyChanges(t *testing.T) {
	meta := synthMeta()
	acc := NewAccumulator(meta, AccumulatorOptions{})
	_, err := acc.Fold(synthSnap(meta,
		covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 0, Counters: []uint32{1, 0, 1}},
		covsnap.FuncCounters{PkgIdx: 1, FuncIdx: 0, Counters: []uint32{0, 0, 0, 1}},
	), 1)
	require.NoError(t, err)

	// A unchanged (fast path), B newly entered, C deepened by one unit.
	upd, err := acc.Fold(synthSnap(meta,
		covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 0, Counters: []uint32{5, 0, 9}},
		covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 1, Counters: []uint32{0, 2}},
		covsnap.FuncCounters{PkgIdx: 1, FuncIdx: 0, Counters: []uint32{1, 0, 0, 2}},
	), 2)
	require.NoError(t, err)
	require.False(t, upd.Full)
	require.EqualValues(t, 2, upd.Units.GetCardinality())
	require.True(t, upd.Units.Contains(4) && upd.Units.Contains(5))
	require.Equal(t, covsnap.RunStatus{
		CoveredUnits: 5, TotalUnits: 9,
		CoveredStmts: 7, TotalStmts: 13,
		CoveredFuncs: 3, TotalFuncs: 3,
	}, upd.Status)
	require.Equal(t, []covsnap.PkgSample{
		{PkgIdx: 0, CoveredUnits: 3, CoveredStmts: 5, CoveredFuncs: 2},
		{PkgIdx: 1, CoveredUnits: 2, CoveredStmts: 2, CoveredFuncs: 1},
	}, upd.Pkgs)
	require.Equal(t, map[[2]uint32]uint32{{0, 1}: 1, {1, 0}: 2}, funcSampleSet(t, upd.Funcs))

	// Same snapshot again: pure heartbeat.
	upd, err = acc.Fold(synthSnap(meta,
		covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 0, Counters: []uint32{5, 0, 9}},
		covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 1, Counters: []uint32{0, 2}},
		covsnap.FuncCounters{PkgIdx: 1, FuncIdx: 0, Counters: []uint32{1, 0, 0, 2}},
	), 3)
	require.NoError(t, err)
	require.False(t, upd.Full)
	require.EqualValues(t, 0, upd.Units.GetCardinality())
	require.Empty(t, upd.Pkgs)
	require.Empty(t, upd.Funcs)
	require.Equal(t, uint64(3), upd.Seq)
	require.EqualValues(t, 5, upd.Status.CoveredUnits)
}

func TestAccumulatorRestatesOnSchedule(t *testing.T) {
	meta := synthMeta()
	acc := NewAccumulator(meta, AccumulatorOptions{RestateEvery: 3})
	snap1 := synthSnap(meta, covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 0, Counters: []uint32{1, 0, 0}})
	snap2 := synthSnap(meta,
		covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 0, Counters: []uint32{1, 1, 0}},
		covsnap.FuncCounters{PkgIdx: 1, FuncIdx: 0, Counters: []uint32{0, 1, 0, 0}},
	)
	_, err := acc.Fold(snap1, 1)
	require.NoError(t, err)
	_, err = acc.Fold(snap2, 2)
	require.NoError(t, err)

	// Third fold carries no change, but the schedule forces a complete
	// re-statement: cumulative set, all covered packages and functions.
	upd, err := acc.Fold(snap2, 3)
	require.NoError(t, err)
	require.True(t, upd.Full)
	require.EqualValues(t, 3, upd.Units.GetCardinality())
	require.True(t, upd.Units.Contains(0) && upd.Units.Contains(1) && upd.Units.Contains(6))
	require.Equal(t, []covsnap.PkgSample{
		{PkgIdx: 0, CoveredUnits: 2, CoveredStmts: 3, CoveredFuncs: 1},
		{PkgIdx: 1, CoveredUnits: 1, CoveredStmts: 1, CoveredFuncs: 1},
	}, upd.Pkgs)
	require.Equal(t, map[[2]uint32]uint32{{0, 0}: 2, {1, 0}: 1}, funcSampleSet(t, upd.Funcs))
}

// Counters regress only after ClearCounters, which nothing here calls; the
// covered set is monotone by contract and a regressed snapshot folds as
// "no change".
func TestAccumulatorStaysMonotoneOnRegressedCounters(t *testing.T) {
	meta := synthMeta()
	acc := NewAccumulator(meta, AccumulatorOptions{})
	_, err := acc.Fold(synthSnap(meta, covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 0, Counters: []uint32{1, 1, 1}}), 1)
	require.NoError(t, err)
	upd, err := acc.Fold(synthSnap(meta, covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 0, Counters: []uint32{0, 0, 0}}), 2)
	require.NoError(t, err)
	require.EqualValues(t, 0, upd.Units.GetCardinality())
	require.Empty(t, upd.Funcs)
	require.EqualValues(t, 3, upd.Status.CoveredUnits)
	require.EqualValues(t, 3, acc.CoveredBitmap().GetCardinality())
}

func TestAccumulatorRejectsForeignSnapshots(t *testing.T) {
	meta := synthMeta()
	acc := NewAccumulator(meta, AccumulatorOptions{})

	foreign := synthSnap(meta)
	foreign.MetaHash = [16]byte{0xff}
	_, err := acc.Fold(foreign, 1)
	require.ErrorContains(t, err, "meta hash")

	_, err = acc.Fold(synthSnap(meta, covsnap.FuncCounters{PkgIdx: 9, FuncIdx: 0, Counters: []uint32{1}}), 2)
	require.ErrorContains(t, err, "unknown function")

	_, err = acc.Fold(synthSnap(meta, covsnap.FuncCounters{PkgIdx: 0, FuncIdx: 0, Counters: []uint32{1}}), 3)
	require.ErrorContains(t, err, "units")
}

// Fold is single-writer; Status/CoveredBitmap must be safe for concurrent
// readers — the race detector arbitrates.
func TestAccumulatorConcurrentReaders(t *testing.T) {
	meta := synthMeta()
	acc := NewAccumulator(meta, AccumulatorOptions{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range uint32(100) {
			c := i % 4
			_, err := acc.Fold(synthSnap(meta, covsnap.FuncCounters{PkgIdx: 1, FuncIdx: 0, Counters: []uint32{c, c, c, c}}), int64(i))
			if err != nil {
				panic(err)
			}
		}
	})
	for range 100 {
		_ = acc.Status()
		_ = acc.CoveredBitmap()
		_ = acc.Seq()
	}
	wg.Wait()
	require.EqualValues(t, 4, acc.Status().CoveredUnits)
}

// Test binaries cannot snapshot counters (meta finalization is deferred to
// an exit hook), so the live sampler must refuse construction here; its
// happy path runs in the integration lane inside a real instrumented probe.
func TestNewSamplerErrsInTestBinaries(t *testing.T) {
	_, err := NewSampler(SamplerOptions{})
	require.Error(t, err)
}
