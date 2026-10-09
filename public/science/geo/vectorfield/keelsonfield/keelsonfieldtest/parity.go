// Package keelsonfieldtest holds a field family to the reduction statements
// it stands in for (ADR-0291 §SD5): one field read through sqlfield's six
// statements in clickhouse-local and through its family three ways —
// through the same engine, through the trivial evaluator, and through it
// after play's canonicalization — compared purpose by purpose.
package keelsonfieldtest

import (
	"bytes"
	"context"
	"math"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/trivialsql"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/keelsonfield"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/sqlfield"
)

// Storm is a 2° global field over three hourly steps: a swirl that drifts,
// masked over a band so that missing nodes are exercised.
func Storm(t *testing.T) keelsonfield.Field {
	t.Helper()
	land := func(lon, lat float64) bool { return lat > 20 && lat < 40 && lon > 0 && lon < 40 }
	loader := vectorfield.NewGlobalAnalyticLoader(2, vectorfield.Masked(vectorfield.Swirl(7), land))
	f := keelsonfield.Field{Name: "storm", Unit: "m/s", SpeedMax: 30}
	t0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for s := range 3 {
		g, err := loader.LoadStep(context.Background(), s)
		require.NoError(t, err)
		f.Steps = append(f.Steps, t0.Add(time.Duration(s)*time.Hour))
		f.Grids = append(f.Grids, g)
	}
	return f
}

func newEngine(t *testing.T, reg *introspect.Registry) *introspectengine.Engine {
	t.Helper()
	if _, err := chlocalpool.LookupBinary(); err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(30 * time.Second)
	svc, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 3, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})
	caller := bus.NewClient("test.keelsonfield", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	e, err := introspectengine.New(introspectengine.Config{Registry: reg, Bus: caller}, logger)
	require.NoError(t, err)
	return e
}

// engineQueryer runs a statement through clickhouse-local.
type engineQueryer struct{ e *introspectengine.Engine }

func (inst engineQueryer) Query(ctx context.Context, statement string, params map[string]string) (rec arrow.RecordBatch, err error) {
	body, _, err := inst.e.QueryParams(ctx, statement, "ArrowStream", params)
	if err != nil {
		return nil, err
	}
	rdr, err := ipc.NewReader(bytes.NewReader(body), ipc.WithAllocator(memory.NewGoAllocator()))
	if err != nil {
		return nil, err
	}
	defer rdr.Release()
	var batches []arrow.RecordBatch
	for rdr.Next() {
		b := rdr.RecordBatch()
		b.Retain()
		batches = append(batches, b)
	}
	if err = rdr.Err(); err != nil {
		return nil, err
	}
	tbl := array.NewTableFromRecords(rdr.Schema(), batches)
	defer tbl.Release()
	for _, b := range batches {
		b.Release()
	}
	cols := make([]arrow.Array, tbl.NumCols())
	for i := range cols {
		ch := tbl.Column(i).Data()
		if len(ch.Chunks()) == 0 {
			cols[i] = array.MakeArrayOfNull(memory.NewGoAllocator(), tbl.Schema().Field(i).Type, 0)
			continue
		}
		cols[i], err = array.Concatenate(ch.Chunks(), memory.NewGoAllocator())
		if err != nil {
			return nil, err
		}
	}
	rec = array.NewRecordBatch(tbl.Schema(), cols, tbl.NumRows())
	for _, c := range cols {
		c.Release()
	}
	return rec, nil
}

// trivialQueryer runs a statement with no ClickHouse at all; canonical first
// rewrites it as play's executor does before sending (ADR-0108), which quotes
// every identifier, the keelson call and its argument names included.
type trivialQueryer struct {
	reg       *introspect.Registry
	canonical bool
}

func (inst trivialQueryer) Query(_ context.Context, statement string, params map[string]string) (rec arrow.RecordBatch, err error) {
	if inst.canonical {
		statement, err = passes.CanonicalizeFull(100).Run(statement)
		if err != nil {
			return nil, err
		}
	}
	rec, _, err = trivialsql.Read(inst.reg, statement, params)
	return
}

// Options narrows a parity run.
type Options struct {
	// Steps are the steps whose windows are compared; nil compares every
	// step. The reference ships the whole relation to clickhouse-local per
	// statement, so a long field is compared on a few.
	Steps []int
	// Requests are the windows compared; nil takes a global, a regional, a
	// seam-crossing, a coarse and a finer-than-native one.
	Requests []vectorfield.Request
	// MinMissing is how many masked samples the reference windows must hold,
	// so that a field meant to exercise missing nodes does.
	MinMissing int
}

// DefaultRequests are the windows a parity run compares by default.
var DefaultRequests = []vectorfield.Request{
	{West: -180, East: 180, South: -90, North: 90, MaxCols: 64, MaxRows: 32},     // the globe, coarse
	{West: -20, East: 40, South: 30, North: 70, MaxCols: 200, MaxRows: 120},      // a region, native
	{West: 150, East: 250, South: -40, North: 40, MaxCols: 80, MaxRows: 60},      // across the seam
	{West: 10, East: 30, South: 15, North: 45, MaxCols: 12, MaxRows: 10},         // a region, coarse
	{West: -179, East: -170, South: -89, North: -80, MaxCols: 400, MaxRows: 400}, // a corner, finer than the grid
}

// Parity registers f as the family base and holds it to the reduction
// statements. It skips without a clickhouse binary.
func Parity(t *testing.T, base string, f keelsonfield.Field, opts Options) {
	t.Helper()
	reg := introspect.NewRegistry()
	require.NoError(t, keelsonfield.Register(reg, base, f))
	e := newEngine(t, reg)
	ctx := context.Background()

	ref, err := sqlfield.NewSource(ctx, engineQueryer{e}, sqlfield.Relation{From: "keelson('" + base + "')"}, sqlfield.Options{})
	require.NoError(t, err, "the reference: the reduction statements in clickhouse-local")
	viaEngine, err := sqlfield.NewSource(ctx, engineQueryer{e}, sqlfield.Relation{Family: base}, sqlfield.Options{})
	require.NoError(t, err, "the family through clickhouse-local")
	viaTrivial, err := sqlfield.NewSource(ctx, trivialQueryer{reg: reg}, sqlfield.Relation{Family: base}, sqlfield.Options{})
	require.NoError(t, err, "the family with no ClickHouse")
	viaCanonical, err := sqlfield.NewSource(ctx, trivialQueryer{reg: reg, canonical: true}, sqlfield.Relation{Family: base}, sqlfield.Options{})
	require.NoError(t, err, "the family with no ClickHouse, canonicalized as play sends it")

	want := ref.Describe()
	for name, src := range map[string]*sqlfield.Source{"engine": viaEngine, "trivial": viaTrivial, "canonical": viaCanonical} {
		got := src.Describe()
		assert.Equal(t, want.Steps, got.Steps, name)
		assert.Equal(t, []float64{want.West, want.East, want.South, want.North, want.DLon, want.DLat},
			[]float64{got.West, got.East, got.South, got.North, got.DLon, got.DLat}, name)
		assert.Equal(t, want.PeriodicLon, got.PeriodicLon, name)
		// ClickHouse's quantile samples above 8192 values; the palette's high
		// end agrees closely, not exactly.
		assert.InEpsilon(t, want.SpeedMax, got.SpeedMax, 0.05, name)
	}

	requests := opts.Requests
	if requests == nil {
		requests = DefaultRequests
	}
	steps := opts.Steps
	if steps == nil {
		for i := range want.Steps {
			steps = append(steps, i)
		}
	}
	finite, missing := 0, 0
	for _, req := range requests {
		for _, step := range steps {
			req.Step = step
			w, err := ref.Sample(ctx, req)
			require.NoError(t, err)
			for _, u := range w.U {
				if math.IsNaN(float64(u)) {
					missing++
				} else {
					finite++
				}
			}
			for name, src := range map[string]*sqlfield.Source{"engine": viaEngine, "trivial": viaTrivial, "canonical": viaCanonical} {
				g, err := src.Sample(ctx, req)
				require.NoError(t, err, name)
				assertSameWindow(t, w, g, name, req)
			}
		}
		ws, err := ref.Summarize(ctx, req)
		require.NoError(t, err)
		for name, src := range map[string]*sqlfield.Source{"engine": viaEngine, "trivial": viaTrivial, "canonical": viaCanonical} {
			gs, err := src.Summarize(ctx, req)
			require.NoError(t, err, name)
			require.Len(t, gs, len(ws), name)
			for i := range ws {
				assert.Equal(t, ws[i].Valid, gs[i].Valid, "%s step %d %+v", name, i, req)
				assertClose(t, ws[i].Mean, gs[i].Mean, "%s mean step %d %+v", name, i, req)
				assertClose(t, ws[i].Max, gs[i].Max, "%s max step %d %+v", name, i, req)
			}
		}
	}
	// The comparison is not vacuous: windows held values and masked nodes,
	// and the family sources read the family.
	assert.Greater(t, finite, 1000)
	assert.GreaterOrEqual(t, missing, opts.MinMissing)
	for name, src := range map[string]*sqlfield.Source{"engine": viaEngine, "trivial": viaTrivial, "canonical": viaCanonical} {
		served, _ := src.LastServed(sqlfield.PurposeWindow)
		assert.Contains(t, served.Statement, "keelson('"+base+"_window'", name)
	}
}

func assertSameWindow(t *testing.T, want, got vectorfield.Window, name string, req vectorfield.Request) {
	t.Helper()
	require.Equal(t, []any{want.Cols, want.Rows, want.Level, want.Step}, []any{got.Cols, got.Rows, got.Level, got.Step}, "%s %+v", name, req)
	assert.Equal(t, []float64{want.West, want.North, want.DLon, want.DLat}, []float64{got.West, got.North, got.DLon, got.DLat}, "%s %+v", name, req)
	for i := range want.U {
		assertClose(t, want.U[i], got.U[i], "%s U[%d] %+v", name, i, req)
		assertClose(t, want.V[i], got.V[i], "%s V[%d] %+v", name, i, req)
		assertClose(t, want.Speed[i], got.Speed[i], "%s Speed[%d] %+v", name, i, req)
	}
}

// assertClose holds two float32 values equal within summation order: both
// sum in float64 and round to float32, in different orders.
func assertClose(t *testing.T, want, got float32, msg string, args ...any) {
	t.Helper()
	if math.IsNaN(float64(want)) || math.IsNaN(float64(got)) {
		assert.True(t, math.IsNaN(float64(want)) && math.IsNaN(float64(got)), append([]any{msg + ": %v vs %v"}, append(args, want, got)...)...)
		return
	}
	assert.InDelta(t, want, got, 1e-5*max(1, math.Abs(float64(want))), append([]any{msg}, args...)...)
}
