package providers

import (
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stretchr/testify/require"
)

type fakeCoverageSource struct {
	meta    *covsnap.MetaProfile
	covered *roaring.Bitmap
	status  covsnap.RunStatus
	seq     uint64
}

func (f *fakeCoverageSource) Meta() *covsnap.MetaProfile        { return f.meta }
func (f *fakeCoverageSource) Status() covsnap.RunStatus         { return f.status }
func (f *fakeCoverageSource) CoveredBitmap() *roaring.Bitmap    { return f.covered.Clone() }
func (f *fakeCoverageSource) Seq() uint64                       { return f.seq }

// covTestMeta builds a two-package profile with the global unit index
// assigned the way DecodeMeta assigns it: pkg "m/a" funcs A g[0,3) with
// stmts (2,1,1) and B g[3,5) with (3,2); pkg "m/b" func C g[5,9).
func covTestMeta() *covsnap.MetaProfile {
	prof := &covsnap.MetaProfile{
		Hash:        [16]byte{0xab},
		Mode:        covsnap.ModeAtomic,
		Granularity: covsnap.GranularityPerBlock,
		Pkgs: []covsnap.PkgMeta{
			{Path: "m/a", Name: "a", ModulePath: "m", Funcs: []covsnap.FuncMeta{
				{Name: "A", SrcFile: "a.go", Units: []covsnap.UnitMeta{{NxStmts: 2}, {NxStmts: 1}, {NxStmts: 1}}},
				{Name: "B", SrcFile: "a.go", Units: []covsnap.UnitMeta{{NxStmts: 3}, {NxStmts: 2}}},
			}},
			{Path: "m/b", Name: "b", ModulePath: "m", Funcs: []covsnap.FuncMeta{
				{Name: "C", SrcFile: "b.go", Units: []covsnap.UnitMeta{{NxStmts: 1}, {NxStmts: 1}, {NxStmts: 1}, {NxStmts: 1}}},
			}},
		},
	}
	next := uint32(0)
	for i := range prof.Pkgs {
		pkg := &prof.Pkgs[i]
		pkg.UnitBase = next
		for j := range pkg.Funcs {
			fn := &pkg.Funcs[j]
			fn.UnitBase = next
			for _, u := range fn.Units {
				fn.NumStmts += u.NxStmts
			}
			next += uint32(len(fn.Units))
			prof.TotalStmts += fn.NumStmts
			pkg.NumStmts += fn.NumStmts
		}
		pkg.NumUnits = next - pkg.UnitBase
	}
	prof.TotalUnits = next
	return prof
}

func TestRegisterCoverageNames(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, RegisterCoverage(r, nil))
	require.Equal(t, []string{"coverage_funcs", "coverage_pkgs", "coverage_status"}, sortedNames(r))
}

func sortedNames(r *introspect.Registry) (names []string) {
	names = r.Names()
	return
}

// A nil source (uninstrumented build) yields empty tables, never absent
// ones — the table names must not depend on the build lane.
func TestCoverageTablesEmptyWithoutSource(t *testing.T) {
	for _, p := range []introspect.Provider{
		coverageStatusProvider{src: nil},
		coveragePkgsProvider{src: nil},
		coverageFuncsProvider{src: nil},
	} {
		require.NotNil(t, p.Schema(), p.Name())
		require.Equal(t, introspect.FreshnessLive, p.Freshness())
		batch, err := p.Snapshot(introspect.AllColumns())
		require.NoError(t, err, p.Name())
		require.EqualValues(t, 0, batch.NumRows(), p.Name())
		batch.Release()
	}
}

func TestCoverageTablesOverFakeSource(t *testing.T) {
	meta := covTestMeta()
	// Covered: A units g0,g2 (stmts 2+1), C unit g8 (stmts 1).
	src := &fakeCoverageSource{
		meta:    meta,
		covered: roaring.BitmapOf(0, 2, 8),
		status: covsnap.RunStatus{
			CoveredUnits: 3, TotalUnits: 9,
			CoveredStmts: 4, TotalStmts: 13,
			CoveredFuncs: 2, TotalFuncs: 3,
		},
		seq: 17,
	}

	status, err := coverageStatusProvider{src: src}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer status.Release()
	require.EqualValues(t, 1, status.NumRows())
	require.Equal(t, "ab000000000000000000000000000000", status.Column(0).ValueStr(0))
	require.Equal(t, "atomic", status.Column(1).ValueStr(0))
	require.Equal(t, "perblock", status.Column(2).ValueStr(0))
	require.Equal(t, "17", status.Column(3).ValueStr(0))
	require.Equal(t, "3", status.Column(4).ValueStr(0))  // covered_units
	require.Equal(t, "9", status.Column(5).ValueStr(0))  // total_units
	require.Equal(t, "13", status.Column(7).ValueStr(0)) // total_stmts

	pkgs, err := coveragePkgsProvider{src: src}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer pkgs.Release()
	require.EqualValues(t, 2, pkgs.NumRows())
	require.Equal(t, "m/a", pkgs.Column(0).ValueStr(0))
	require.Equal(t, "2", pkgs.Column(1).ValueStr(0)) // m/a covered_units
	require.Equal(t, "5", pkgs.Column(2).ValueStr(0)) // m/a total_units
	require.Equal(t, "3", pkgs.Column(3).ValueStr(0)) // m/a covered_stmts
	require.Equal(t, "m/b", pkgs.Column(0).ValueStr(1))
	require.Equal(t, "1", pkgs.Column(1).ValueStr(1)) // m/b covered_units
	require.Equal(t, "1", pkgs.Column(5).ValueStr(1)) // m/b covered_funcs

	funcs, err := coverageFuncsProvider{src: src}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer funcs.Release()
	require.EqualValues(t, 3, funcs.NumRows())
	// Row 0 = A (covered 2 of 3), row 1 = B (uncovered zero row), row 2 = C.
	require.Equal(t, "A", funcs.Column(1).ValueStr(0))
	require.Equal(t, "2", funcs.Column(4).ValueStr(0))
	require.Equal(t, "B", funcs.Column(1).ValueStr(1))
	require.Equal(t, "0", funcs.Column(4).ValueStr(1))
	require.Equal(t, "C", funcs.Column(1).ValueStr(2))
	require.Equal(t, "1", funcs.Column(4).ValueStr(2))
	require.Equal(t, "4", funcs.Column(5).ValueStr(2)) // C total_units
}
