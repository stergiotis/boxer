package sqlapplet

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
)

// coverageDefsBySlug parses the embedded coverage book and indexes it by slug.
func coverageDefsBySlug(t *testing.T) map[string]*AppletDef {
	t.Helper()
	defs, errs := ParseBook("coverage", help.MustSub(bookcoverageFS, "bookcoverage"))
	require.Empty(t, errs)
	require.Len(t, defs, 3)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	require.Len(t, bySlug, 3)
	return bySlug
}

// TestCoverageBookCorpus is the ADR-0132 §SD6 gate over the coverage book.
func TestCoverageBookCorpus(t *testing.T) {
	bySlug := coverageDefsBySlug(t)
	for slug, d := range bySlug {
		assert.Equal(t, EndpointIntrospection, d.Endpoint, slug)
		assert.Equal(t, analysis.QuerySecurityRead, d.Class, "%s: keelson('…') classifies as a local read", slug)
		assert.NotEmpty(t, d.Icon, slug)
		// Every knob is prelude-bound: the tables are live, so all three
		// applets draw whole on mount.
		assert.False(t, d.HasUnboundSlots, "%s: every knob is prelude-bound", slug)
	}

	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["cov-overview"].Tabs)
	assert.Equal(t, []TabSel{{ID: "treemap"}, {ID: "table"}}, bySlug["cov-map"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}, {ID: "detail"}}, bySlug["cov-uncovered"].Tabs)

	// The map declares the ADR-0166 nodes contract — a directory that is
	// also a package must be able to carry its own rectangle.
	mapSQL := bySlug["cov-map"].SQL
	for _, col := range []string{" AS id", " AS parent", " AS value", " AS label", " AS color"} {
		assert.Contains(t, mapSQL, col, "cov-map: the nodes contract needs%s", col)
	}
	assert.NotContains(t, mapSQL, " AS stack", "cov-map: the folded arm would win over the nodes arm")

	// The colour channel is a RATIO on a declared scale, not a bracket: without
	// the endpoints the panel surveys the result and the ramp stretches to
	// whatever this repository happens to span (ADR-0166 §SD2).
	for _, col := range []string{" AS color_min", " AS color_max", " AS color_unit"} {
		assert.Contains(t, mapSQL, col, "cov-map: a percentage ramp must declare%s", col)
	}
}

// TestMintCoverageBook mints the book beside its siblings, guarding slug
// collisions across all six embedded corpora.
func TestMintCoverageBook(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{
		{id: "sqlapplet", fsys: help.MustSub(bookFS, "book"), topics: []app.TopicT{app.TopicRuntime}},
		{id: "topology", fsys: help.MustSub(booktopoFS, "booktopo"), topics: []app.TopicT{app.TopicTopology}},
		{id: "godep", fsys: help.MustSub(bookgodepFS, "bookgodep"), topics: []app.TopicT{app.TopicCode}},
		{id: "pprof", fsys: help.MustSub(bookpprofFS, "bookpprof"), topics: []app.TopicT{app.TopicObservability}},
		{id: "capmap", fsys: help.MustSub(bookcapmapFS, "bookcapmap"), topics: []app.TopicT{app.TopicCode}},
		{id: "coverage", fsys: help.MustSub(bookcoverageFS, "bookcoverage"), topics: []app.TopicT{app.TopicObservability}},
	})
	require.Empty(t, errs)
	assert.Equal(t, 28, minted)
	m, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + "cov-map"))
	require.True(t, ok)
	assert.Equal(t, "Coverage map", m.Display)
	assert.Equal(t, []app.TopicT{app.TopicObservability}, m.Topics)
}

// coverageGateSource is the fixture the engine gate serves the coverage
// tables from: module "m" with a two-level path tree, one fully-entered
// package, one untouched package, and one partially covered package —
// the shapes the three buffers reason about.
//
// Meta (global unit index): m/app/alpha A1 g[0,3) stmts (2,1,1) and
// A2 g[3,5) stmts (3,2); m/app/beta B1 g[5,7) stmts (1,1);
// m/lib L1 g[7,11) stmts (1,1,1,1). Covered set {0,1,2,3,7,8}:
// alpha 7/9 stmts, beta 0/2, lib 2/4 — 9/15 overall (60%).
func coverageGateSource() *fakeCoverageGateSource {
	prof := &covsnap.MetaProfile{
		Hash:        [16]byte{0xcc},
		Mode:        covsnap.ModeAtomic,
		Granularity: covsnap.GranularityPerBlock,
		Pkgs: []covsnap.PkgMeta{
			{Path: "m/app/alpha", Name: "alpha", ModulePath: "m", Funcs: []covsnap.FuncMeta{
				{Name: "A1", SrcFile: "alpha.go", Units: []covsnap.UnitMeta{{NxStmts: 2}, {NxStmts: 1}, {NxStmts: 1}}},
				{Name: "A2", SrcFile: "alpha.go", Units: []covsnap.UnitMeta{{NxStmts: 3}, {NxStmts: 2}}},
			}},
			{Path: "m/app/beta", Name: "beta", ModulePath: "m", Funcs: []covsnap.FuncMeta{
				{Name: "B1", SrcFile: "beta.go", Units: []covsnap.UnitMeta{{NxStmts: 1}, {NxStmts: 1}}},
			}},
			{Path: "m/lib", Name: "lib", ModulePath: "m", Funcs: []covsnap.FuncMeta{
				{Name: "L1", SrcFile: "lib.go", Units: []covsnap.UnitMeta{{NxStmts: 1}, {NxStmts: 1}, {NxStmts: 1}, {NxStmts: 1}}},
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
	return &fakeCoverageGateSource{
		meta:    prof,
		covered: roaring.BitmapOf(0, 1, 2, 3, 7, 8),
		status: covsnap.RunStatus{
			CoveredUnits: 6, TotalUnits: 11,
			CoveredStmts: 9, TotalStmts: 15,
			CoveredFuncs: 3, TotalFuncs: 4,
		},
		seq: 5,
	}
}

type fakeCoverageGateSource struct {
	meta    *covsnap.MetaProfile
	covered *roaring.Bitmap
	status  covsnap.RunStatus
	seq     uint64
}

func (f *fakeCoverageGateSource) Meta() *covsnap.MetaProfile     { return f.meta }
func (f *fakeCoverageGateSource) Status() covsnap.RunStatus      { return f.status }
func (f *fakeCoverageGateSource) CoveredBitmap() *roaring.Bitmap { return f.covered.Clone() }
func (f *fakeCoverageGateSource) Seq() uint64                    { return f.seq }

// TestCoverageBookQueriesExecute runs every buffer verbatim — SET prelude
// included — through the introspect engine over fixture coverage tables. A
// buffer that parses, classifies and mints can still name a column that
// does not exist.
func TestCoverageBookQueriesExecute(t *testing.T) {
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}

	logger := zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.WarnLevel)
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(120 * time.Second)
	svc, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 3, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})

	reg := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(reg))
	require.NoError(t, providers.RegisterCoverage(reg, coverageGateSource()))

	caller := bus.NewClient("test.bookcoverage.engine", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	e, err := introspectengine.New(introspectengine.Config{Registry: reg, Bus: caller}, logger)
	require.NoError(t, err)

	query := func(sql string) (rows []string, err error) {
		body, _, qErr := e.Query(context.Background(), sql, "TabSeparated")
		if qErr != nil {
			return nil, qErr
		}
		out := strings.TrimSpace(string(body))
		if out == "" {
			return nil, nil
		}
		return strings.Split(out, "\n"), nil
	}

	bySlug := coverageDefsBySlug(t)
	for slug, d := range bySlug {
		rows, qErr := query(d.SQL)
		require.NoError(t, qErr, "%s: buffer failed", slug)
		assert.NotEmpty(t, rows, "%s: the sink returned no rows", slug)
	}

	// The overview reports the fixture: 60% statement coverage, two of three
	// packages touched.
	rows, err := query(bySlug["cov-overview"].SQL)
	require.NoError(t, err)
	joined := strings.Join(rows, "\n")
	assert.Contains(t, joined, "covermode\tatomic")
	assert.Contains(t, joined, "statement coverage %\t60")
	assert.Contains(t, joined, "statements\t9 / 15")
	assert.Contains(t, joined, "packages touched\t2 / 3")

	// The map: the module prefix is trimmed, prefixes become directory
	// nodes, and exactly one root remains.
	mapSQL := bySlug["cov-map"].SQL
	rows, err = query(mapSQL)
	require.NoError(t, err)
	assert.Len(t, rows, 5, "root + app + two leaves under it + lib")

	rows, err = query(overSink(mapSQL, "SELECT id FROM nodes WHERE parent = ''"))
	require.NoError(t, err)
	assert.Equal(t, []string{"m"}, rows, "exactly one root, the module")

	rows, err = query(overSink(mapSQL, "SELECT parent, pct FROM nodes WHERE id = 'app'"))
	require.NoError(t, err)
	assert.Equal(t, []string{"m\t63.6"}, rows, "a directory node parents to the module and colours by its subtree (7/11)")

	rows, err = query(overSink(mapSQL, "SELECT parent, pct FROM nodes WHERE id = 'app/beta'"))
	require.NoError(t, err)
	assert.Equal(t, []string{"app\t0"}, rows, "an untouched package sits at the bottom of the ramp")

	// The scale is the query's, not the result's: pinned to 0–100 on every row,
	// so the panel does not survey this fixture's 0–63.6 and paint 63.6 as
	// fully covered. `color` carries the ratio and `pct` repeats it under the
	// name the rest of the book uses.
	rows, err = query(mapSQL)
	require.NoError(t, err)
	for _, row := range rows {
		f := strings.Split(row, "\t")
		require.Len(t, f, 12, "cov-map: id,parent,label,value,unit,color,color_min,color_max,color_unit,+3")
		assert.Equal(t, []string{"0", "100", "%"}, f[6:9], "every row declares the same 0–100%% scale")
		assert.Equal(t, f[11], f[5], "`color` is `pct` bound to the colour channel")
	}

	// size_by = 'uncovered' turns the area into the gap.
	unc := strings.Replace(mapSQL, "SET param_size_by = 'stmts';", "SET param_size_by = 'uncovered';", 1)
	rows, err = query(overSink(unc, "SELECT toInt64(value) FROM nodes WHERE id = 'app/alpha'"))
	require.NoError(t, err)
	assert.Equal(t, []string{"2"}, rows, "alpha has 9 statements, 7 covered")

	// The uncovered browser: default population is never-entered functions.
	rows, err = query(bySlug["cov-uncovered"].SQL)
	require.NoError(t, err)
	require.Len(t, rows, 1, "one never-entered function in the fixture")
	assert.Contains(t, rows[0], "m/app/beta\tB1")

	partial := strings.Replace(bySlug["cov-uncovered"].SQL, "SET param_show = 'uncovered';", "SET param_show = 'partial';", 1)
	rows, err = query(partial)
	require.NoError(t, err)
	assert.Len(t, rows, 2, "A2 (1 of 2 units) and L1 (2 of 4 units)")

	all := strings.Replace(bySlug["cov-uncovered"].SQL, "SET param_show = 'uncovered';", "SET param_show = 'all';", 1)
	rows, err = query(all)
	require.NoError(t, err)
	assert.Len(t, rows, 4, "every function in the fixture")
}
