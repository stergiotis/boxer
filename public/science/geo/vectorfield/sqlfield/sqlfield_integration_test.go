//go:build integration

package sqlfield

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/vectorfieldtest"
)

// localQueryer runs each statement in a clickhouse-local process over one
// database directory.
type localQueryer struct {
	bin, path string
}

func (inst localQueryer) run(ctx context.Context, statement string, params map[string]string, format string) (out []byte, err error) {
	args := []string{"local", "--path", inst.path, "--output-format", format}
	for k, v := range params {
		args = append(args, "--param_"+k+"="+v)
	}
	args = append(args, "--query", statement)
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, inst.bin, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if err != nil {
		err = fmt.Errorf("clickhouse local: %w: %s", err, stderr.String())
		return
	}
	out = stdout.Bytes()
	return
}

func (inst localQueryer) QueryE(ctx context.Context, statement string, params map[string]string) (rec arrow.RecordBatch, err error) {
	out, err := inst.run(ctx, statement, params, "ArrowStream")
	if err != nil {
		return
	}
	rdr, err := ipc.NewReader(bytes.NewReader(out), ipc.WithAllocator(memory.NewGoAllocator()))
	if err != nil {
		return
	}
	defer rdr.Release()
	var batches []arrow.RecordBatch
	for rdr.Next() {
		b := rdr.RecordBatch()
		b.Retain()
		batches = append(batches, b)
	}
	err = rdr.Err()
	if err != nil {
		return
	}
	defer func() {
		for _, b := range batches {
			b.Release()
		}
	}()
	// A reply of several batches becomes one record, column by column.
	schema := rdr.Schema()
	cols := make([]arrow.Array, schema.NumFields())
	for i := range cols {
		if len(batches) == 0 {
			cols = emptyColumns(schema)
			break
		}
		parts := make([]arrow.Array, len(batches))
		for k, b := range batches {
			parts[k] = b.Column(i)
		}
		cols[i], err = array.Concatenate(parts, memory.NewGoAllocator())
		if err != nil {
			return
		}
	}
	rows := int64(0)
	for _, b := range batches {
		rows += b.NumRows()
	}
	rec = array.NewRecordBatch(schema, cols, rows)
	for _, c := range cols {
		c.Release()
	}
	return
}

// canonicalQueryer sends every statement in the canonical form a host such as
// play puts in front of its executor (ADR-0108), so that what conforms is the
// text that host would actually run.
type canonicalQueryer struct {
	inner localQueryer
	t     *testing.T
}

func (inst canonicalQueryer) QueryE(ctx context.Context, statement string, params map[string]string) (rec arrow.RecordBatch, err error) {
	canonical, err := passes.CanonicalizeFull(100).Run(statement)
	require.NoError(inst.t, err)
	require.NotEqual(inst.t, statement, canonical)
	return inst.inner.QueryE(ctx, canonical, params)
}

func emptyColumns(schema *arrow.Schema) (cols []arrow.Array) {
	for _, f := range schema.Fields() {
		b := array.NewBuilder(memory.NewGoAllocator(), f.Type)
		cols = append(cols, b.NewArray())
		b.Release()
	}
	return
}

func newLocalQueryer(t *testing.T) (q localQueryer) {
	t.Helper()
	bin, err := chlocalpool.LookupBinary()
	if err != nil {
		t.Skipf("no clickhouse binary: %v", err)
	}
	return localQueryer{bin: bin, path: t.TempDir()}
}

func (inst localQueryer) exec(t *testing.T, statement string) {
	t.Helper()
	_, err := inst.run(context.Background(), statement, nil, "Null")
	require.NoError(t, err)
}

// The analytic field every fixture stores: a wave in both components and a
// trend with the step, smooth enough that a reduction is predictable.
const fieldU = `10 * sin(radians(lon) * 2) + 3 * cos(radians(lat) * 3) + s`
const fieldV = `8 * cos(radians(lon)) * sin(radians(lat) * 2)`

func analyticU(lon, lat float64, step int) float32 {
	return float32(10*math.Sin(lon*math.Pi/180*2) + 3*math.Cos(lat*math.Pi/180*3) + float64(step))
}

func analyticV(lon, lat float64) float32 {
	return float32(8 * math.Cos(lon*math.Pi/180) * math.Sin(lat*math.Pi/180*2))
}

// gridLoader is the same field as a pyramid's step loader.
type gridLoader struct {
	west, north, dLon, dLat float64
	cols, rows              int
}

func (inst gridLoader) LoadStepE(_ context.Context, step int) (g vectorfield.Grid, err error) {
	g = vectorfield.Grid{West: inst.west, North: inst.north, DLon: inst.dLon, DLat: inst.dLat, Cols: inst.cols, Rows: inst.rows,
		U: make([]float32, inst.cols*inst.rows), V: make([]float32, inst.cols*inst.rows)}
	for r := range inst.rows {
		for c := range inst.cols {
			lon, lat := inst.west+float64(c)*inst.dLon, inst.north-float64(r)*inst.dLat
			g.U[r*inst.cols+c] = analyticU(lon, lat, step)
			g.V[r*inst.cols+c] = analyticV(lon, lat)
		}
	}
	return
}

func TestGlobalFieldConformsAndMatchesThePyramid(t *testing.T) {
	q := newLocalQueryer(t)
	q.exec(t, `CREATE TABLE wind (valid_time DateTime('UTC'), latitude Float64, longitude Float64, u10 Float32, v10 Float32)
ENGINE = MergeTree ORDER BY (valid_time, latitude, longitude) SETTINGS index_granularity = 1024`)
	q.exec(t, `INSERT INTO wind
SELECT toDateTime('2026-01-01 00:00:00', 'UTC') + s * 10800 AS t, 90 - r AS lat, -180 + c AS lon, `+fieldU+`, `+fieldV+`
FROM (SELECT number AS s FROM numbers(3)) AS a, (SELECT number AS r FROM numbers(181)) AS b, (SELECT number AS c FROM numbers(360)) AS d`)

	rel := Relation{
		Head: "WITH vector_field AS (SELECT valid_time AS t, latitude AS lat, longitude AS lon, u10 AS u, v10 AS v FROM wind)",
		From: "vector_field",
	}
	ctx := context.Background()
	src, err := NewSourceE(ctx, q, rel, Options{})
	require.NoError(t, err)

	meta := src.Describe()
	require.True(t, meta.PeriodicLon)
	require.Len(t, meta.Steps, 3)
	require.Equal(t, "2026-01-01T03:00:00Z", meta.Steps[1].Valid.Format("2006-01-02T15:04:05Z07:00"))
	require.InDelta(t, 1.0, meta.DLon, 1e-9)
	require.Greater(t, meta.SpeedMax, float32(5))

	vectorfieldtest.Run(t, src, vectorfieldtest.Options{Covered: true, MissingStep: -1})

	// The same field through the in-memory pyramid is the same window, where
	// the pyramid serves a level it built by halving.
	pyr, err := vectorfield.NewPyramidE(ctx, vectorfield.Meta{Steps: meta.Steps},
		gridLoader{west: -180, north: 90, dLon: 1, dLat: 1, cols: 360, rows: 181}, vectorfield.PyramidOptions{})
	require.NoError(t, err)
	for _, req := range []vectorfield.Request{
		{West: -30, East: 40, South: 30, North: 70, Step: 1, MaxCols: 300, MaxRows: 300},
		{West: -30, East: 40, South: 30, North: 70, Step: 2, MaxCols: 20, MaxRows: 12},
		{West: 150, East: 215, South: -40, North: 10, Step: 0, MaxCols: 18, MaxRows: 14},
	} {
		got, err := src.SampleE(ctx, req)
		require.NoError(t, err)
		want, err := pyr.SampleE(ctx, req)
		require.NoError(t, err)
		require.Equal(t, want.Cols, got.Cols)
		require.Equal(t, want.Rows, got.Rows)
		require.InDelta(t, want.West, got.West, 1e-9)
		require.InDelta(t, want.North, got.North, 1e-9)
		require.InDelta(t, want.DLon, got.DLon, 1e-9)
		for i := range want.U {
			require.InDelta(t, want.U[i], got.U[i], 1e-3, "u of sample %d", i)
			require.InDelta(t, want.V[i], got.V[i], 1e-3, "v of sample %d", i)
			require.InDelta(t, want.Speed[i], got.Speed[i], 1e-3, "speed of sample %d", i)
		}
	}

	t.Run("the statement text does not change with the request", func(t *testing.T) {
		served, _ := src.LastServed()
		require.Equal(t, src.WindowStatement(), served.Statement)
	})

	t.Run("a regional window prunes on the primary key through the relation", func(t *testing.T) {
		p := src.grid.plan(vectorfield.Request{West: -10, East: 10, South: 40, North: 50, Step: 1, MaxCols: 100, MaxRows: 100})
		out, err := q.run(ctx, "EXPLAIN indexes = 1 "+src.WindowStatement(), src.grid.windowParams(&p, src.stepText[1], true), "TSVRaw")
		require.NoError(t, err)
		m := regexp.MustCompile(`Granules: (\d+)/(\d+)`).FindAllSubmatch(out, -1)
		require.NotEmpty(t, m, "%s", out)
		last := m[len(m)-1]
		read, _ := strconv.Atoi(string(last[1]))
		total, _ := strconv.Atoi(string(m[0][2]))
		require.Less(t, read*8, total, "a tenth of one step's latitudes reads a small part of the table:\n%s", out)
	})
}

func TestRegionalFieldWithACoast(t *testing.T) {
	q := newLocalQueryer(t)
	// Float32 coordinates at a spacing that is not exact in binary, a
	// longitude convention other than the request's, missing data spelt three
	// ways, and no time column.
	q.exec(t, `CREATE TABLE current (lat Float32, lon Float32, u Nullable(Float32), v Nullable(Float32)) ENGINE = MergeTree ORDER BY (lat, lon)`)
	q.exec(t, `INSERT INTO current
SELECT 60 - r * 0.1 AS lat, 340 + c * 0.1 AS lon,
    multiIf(c < 20 AND r < 30, NULL, c < 20 AND r < 60, nan, `+fieldU+`) AS u,
    if(c < 20 AND r >= 60 AND r < 90, inf, `+fieldV+`) AS v
FROM (SELECT number AS r, 0 AS s FROM numbers(200)) AS b, (SELECT number AS c FROM numbers(150)) AS d`)

	ctx := context.Background()
	src, err := NewSourceE(ctx, q, Relation{From: "current"}, Options{})
	require.NoError(t, err)
	meta := src.Describe()
	require.False(t, meta.PeriodicLon)
	require.Len(t, meta.Steps, 1)
	require.InDelta(t, 0.1, meta.DLon, 1e-6)

	vectorfieldtest.Run(t, src, vectorfieldtest.Options{MissingStep: -1})

	t.Run("in canonical form", func(t *testing.T) {
		canonical, err := NewSourceE(ctx, canonicalQueryer{inner: q, t: t}, Relation{
			Head: "WITH vector_field AS (SELECT toDateTime64('2026-01-01 00:00:00', 3, 'UTC') AS t, lat, lon, u, v FROM current)",
			From: "vector_field",
		}, Options{})
		require.NoError(t, err)
		vectorfieldtest.Run(t, canonical, vectorfieldtest.Options{MissingStep: -1})
	})

	// A request in the -180…180 convention finds the field stored in 0…360.
	win, err := src.SampleE(ctx, vectorfield.Request{West: -20, East: -5, South: 40, North: 60, MaxCols: 400, MaxRows: 400})
	require.NoError(t, err)
	require.False(t, win.IsEmpty())
	require.InDelta(t, -20.1, win.West, 0.11)
	_, _, _, ok := win.Sample(-19.5, 58.5) // NULL
	require.False(t, ok)
	_, _, _, ok = win.Sample(-19.5, 55.5) // nan
	require.False(t, ok)
	_, _, _, ok = win.Sample(-19.5, 52.5) // inf
	require.False(t, ok)
	u, _, _, ok := win.Sample(-12, 50)
	require.True(t, ok)
	require.InDelta(t, analyticU(348, 50, 0), u, 0.05)
}

func TestRepeatedSeamColumnIsDropped(t *testing.T) {
	q := newLocalQueryer(t)
	q.exec(t, `CREATE TABLE g (lat Float64, lon Float64, u Float64, v Float64) ENGINE = MergeTree ORDER BY (lat, lon)`)
	q.exec(t, `INSERT INTO g SELECT 80 - r * 2 AS lat, c * 2 AS lon, `+fieldU+`, `+fieldV+`
FROM (SELECT number AS r, 0 AS s FROM numbers(81)) AS b, (SELECT number AS c FROM numbers(181)) AS d`)
	src, err := NewSourceE(context.Background(), q, Relation{From: "g"}, Options{})
	require.NoError(t, err)
	meta := src.Describe()
	require.True(t, meta.PeriodicLon)
	require.InDelta(t, 358.0, meta.East, 1e-9)
	vectorfieldtest.Run(t, src, vectorfieldtest.Options{Covered: true, MissingStep: -1})
}

func TestWhatAReductionWouldHideIsRefused(t *testing.T) {
	q := newLocalQueryer(t)
	ctx := context.Background()
	q.exec(t, `CREATE TABLE levels (level UInt16, lat Float64, lon Float64, u Float64, v Float64) ENGINE = MergeTree ORDER BY (level, lat, lon)`)
	q.exec(t, `INSERT INTO levels SELECT l, 10 - r, c, 1, 1
FROM (SELECT number AS l FROM numbers(2)) AS a, (SELECT number AS r FROM numbers(10)) AS b, (SELECT number AS c FROM numbers(10)) AS d`)

	_, err := NewSourceE(ctx, q, Relation{From: "levels"}, Options{})
	require.ErrorContains(t, err, "more rows than the grid has nodes")

	_, err = NewSourceE(ctx, q, Relation{From: "(SELECT lat, lon, u, v FROM levels WHERE level = 1)"}, Options{})
	require.NoError(t, err)

	_, err = NewSourceE(ctx, q, Relation{From: "(SELECT lat + sin(lat) * 0.3 AS lat, lon, u, v FROM levels WHERE level = 1)"}, Options{})
	require.ErrorContains(t, err, "not on a grid regular")

	_, err = NewSourceE(ctx, q, Relation{From: "(SELECT level AS t, lat, lon, u, v FROM levels)"}, Options{})
	require.ErrorContains(t, err, "must be a Date, DateTime or DateTime64")

	_, err = NewSourceE(ctx, q, Relation{From: "(SELECT lat, lon, u FROM levels)"}, Options{})
	require.ErrorContains(t, err, "not a field")
}
