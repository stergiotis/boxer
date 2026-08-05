// coverage — live continuous-coverage tables over the in-process sampler
// (ADR-0169 §SD5). The stored history (§SD6) is deferred; these tables
// answer "what has this process executed so far" from the sampler's own
// state, with no ClickHouse and no bus round-trip.

package providers

import (
	"encoding/hex"

	"github.com/RoaringBitmap/roaring"
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
)

// CoverageSourceI is the providers' view of the live sampler
// (*coverage.Sampler satisfies it). The accessors are individually
// consistent, not mutually snapshot-atomic — between two calls another
// sample may fold. Coverage is monotone, so the worst case is a table one
// tick fresher in one column than another, which a Live table tolerates.
type CoverageSourceI interface {
	Meta() (meta *covsnap.MetaProfile)
	Status() (status covsnap.RunStatus)
	CoveredBitmap() (covered *roaring.Bitmap)
	Seq() (seq uint64)
}

// RegisterCoverage registers the three coverage tables into r. It takes
// the interface, never the concrete sampler, so this package stays free of
// runtime/coverage imports; a host on an uninstrumented build passes nil
// and gets empty tables rather than absent ones — the keelson('windows')
// precedent, so the set of table names does not depend on the build lane.
func RegisterCoverage(r *introspect.Registry, src CoverageSourceI) (err error) {
	err = r.Register(coverageStatusProvider{src: src})
	if err != nil {
		return
	}
	err = r.Register(coveragePkgsProvider{src: src})
	if err != nil {
		return
	}
	return r.Register(coverageFuncsProvider{src: src})
}

// coverageStatusProvider exposes one row of absolute cumulative totals —
// the tier-1 heartbeat as a table.
type coverageStatusProvider struct{ src CoverageSourceI }

func (coverageStatusProvider) Name() string                         { return "coverage_status" }
func (coverageStatusProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (coverageStatusProvider) Schema() *arrow.Schema {
	return coverageStatusTable(nil).Schema()
}

type covStatusRow struct {
	metaHash    string
	mode        string
	granularity string
	samples     uint64
	status      covsnap.RunStatus
}

func (p coverageStatusProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []covStatusRow
	if p.src != nil {
		meta := p.src.Meta()
		rows = []covStatusRow{{
			metaHash:    hex.EncodeToString(meta.Hash[:]),
			mode:        meta.Mode.String(),
			granularity: meta.Granularity.String(),
			samples:     p.src.Seq(),
			status:      p.src.Status(),
		}}
	}
	return coverageStatusTable(rows).Build(proj, len(rows)), nil
}

func coverageStatusTable(rows []covStatusRow) *introspect.Table {
	return introspect.NewTable().
		String("meta_hash", func(i int) string { return rows[i].metaHash }).
		String("mode", func(i int) string { return rows[i].mode }).
		String("granularity", func(i int) string { return rows[i].granularity }).
		Uint64("samples", func(i int) uint64 { return rows[i].samples }).
		Uint64("covered_units", func(i int) uint64 { return uint64(rows[i].status.CoveredUnits) }).
		Uint64("total_units", func(i int) uint64 { return uint64(rows[i].status.TotalUnits) }).
		Uint64("covered_stmts", func(i int) uint64 { return uint64(rows[i].status.CoveredStmts) }).
		Uint64("total_stmts", func(i int) uint64 { return uint64(rows[i].status.TotalStmts) }).
		Uint64("covered_funcs", func(i int) uint64 { return uint64(rows[i].status.CoveredFuncs) }).
		Uint64("total_funcs", func(i int) uint64 { return uint64(rows[i].status.TotalFuncs) })
}

// coveragePkgsProvider exposes one row per instrumented package, covered or
// not — zero-covered rows keep "what is NOT exercised" a WHERE clause
// instead of an anti-join against the meta.
type coveragePkgsProvider struct{ src CoverageSourceI }

func (coveragePkgsProvider) Name() string                         { return "coverage_pkgs" }
func (coveragePkgsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (coveragePkgsProvider) Schema() *arrow.Schema {
	return coveragePkgsTable(nil).Schema()
}

type covPkgRow struct {
	path       string
	modulePath string
	covered    covsnap.PkgSample
	totalUnits uint32
	totalStmts uint32
	totalFuncs int
}

func (p coveragePkgsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []covPkgRow
	if p.src != nil {
		meta := p.src.Meta()
		aggPkgs, _ := covsnap.AggregateCovered(meta, p.src.CoveredBitmap())
		sparse := make(map[uint32]covsnap.PkgSample, len(aggPkgs))
		for _, s := range aggPkgs {
			sparse[s.PkgIdx] = s
		}
		rows = make([]covPkgRow, 0, len(meta.Pkgs))
		for i := range meta.Pkgs {
			pkg := &meta.Pkgs[i]
			rows = append(rows, covPkgRow{
				path:       pkg.Path,
				modulePath: pkg.ModulePath,
				covered:    sparse[uint32(i)],
				totalUnits: pkg.NumUnits,
				totalStmts: pkg.NumStmts,
				totalFuncs: len(pkg.Funcs),
			})
		}
	}
	return coveragePkgsTable(rows).Build(proj, len(rows)), nil
}

func coveragePkgsTable(rows []covPkgRow) *introspect.Table {
	return introspect.NewTable().
		String("pkg_path", func(i int) string { return rows[i].path }).
		// The module prefix a book trims to get repository-relative paths
		// (the coverage treemap's hierarchy is worthless with three
		// hostname levels on top).
		String("module_path", func(i int) string { return rows[i].modulePath }).
		Uint64("covered_units", func(i int) uint64 { return uint64(rows[i].covered.CoveredUnits) }).
		Uint64("total_units", func(i int) uint64 { return uint64(rows[i].totalUnits) }).
		Uint64("covered_stmts", func(i int) uint64 { return uint64(rows[i].covered.CoveredStmts) }).
		Uint64("total_stmts", func(i int) uint64 { return uint64(rows[i].totalStmts) }).
		Uint64("covered_funcs", func(i int) uint64 { return uint64(rows[i].covered.CoveredFuncs) }).
		Uint64("total_funcs", func(i int) uint64 { return uint64(rows[i].totalFuncs) })
}

// coverageFuncsProvider exposes one row per instrumented function — the
// per-line lens' index (~tens of thousands of rows on a full build; the
// `?cols=` hint and projections apply as usual).
type coverageFuncsProvider struct{ src CoverageSourceI }

func (coverageFuncsProvider) Name() string                         { return "coverage_funcs" }
func (coverageFuncsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (coverageFuncsProvider) Schema() *arrow.Schema {
	return coverageFuncsTable(nil).Schema()
}

type covFuncRow struct {
	pkgPath      string
	name         string
	srcFile      string
	lit          bool
	coveredUnits uint32
	totalUnits   int
	totalStmts   uint32
}

func (p coverageFuncsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []covFuncRow
	if p.src != nil {
		meta := p.src.Meta()
		_, aggFuncs := covsnap.AggregateCovered(meta, p.src.CoveredBitmap())
		sparse := make(map[[2]uint32]uint32, len(aggFuncs))
		for _, s := range aggFuncs {
			sparse[[2]uint32{s.PkgIdx, s.FuncIdx}] = s.CoveredUnits
		}
		for pi := range meta.Pkgs {
			pkg := &meta.Pkgs[pi]
			for fi := range pkg.Funcs {
				fn := &pkg.Funcs[fi]
				rows = append(rows, covFuncRow{
					pkgPath:      pkg.Path,
					name:         fn.Name,
					srcFile:      fn.SrcFile,
					lit:          fn.Lit,
					coveredUnits: sparse[[2]uint32{uint32(pi), uint32(fi)}],
					totalUnits:   len(fn.Units),
					totalStmts:   fn.NumStmts,
				})
			}
		}
	}
	return coverageFuncsTable(rows).Build(proj, len(rows)), nil
}

func coverageFuncsTable(rows []covFuncRow) *introspect.Table {
	return introspect.NewTable().
		String("pkg_path", func(i int) string { return rows[i].pkgPath }).
		String("func", func(i int) string { return rows[i].name }).
		String("src_file", func(i int) string { return rows[i].srcFile }).
		Bool("lit", func(i int) bool { return rows[i].lit }).
		Uint64("covered_units", func(i int) uint64 { return uint64(rows[i].coveredUnits) }).
		Uint64("total_units", func(i int) uint64 { return uint64(rows[i].totalUnits) }).
		Uint64("total_stmts", func(i int) uint64 { return uint64(rows[i].totalStmts) })
}
