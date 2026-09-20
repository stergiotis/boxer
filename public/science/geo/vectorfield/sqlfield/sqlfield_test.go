package sqlfield

import (
	"context"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/vectorfieldtest"
)

// fakeField answers the package's statements from a grid held in memory. Its
// window reply is computed from the parameters alone, the way the server
// computes it from the statement, so what the default lane checks is that the
// plan, its parameters and the decoding agree with each other; that the
// statement means the same is the integration lane's to check.
type fakeField struct {
	rel        Relation
	g          grid
	storedCols int // g.cols, plus one where the seam column is repeated
	steps      []time.Time
	timeType   string
	missing    func(col, row int) bool

	mu         sync.Mutex
	statements []string
}

func (inst *fakeField) value(col, row, step int) (u, v float64) {
	lon, lat := inst.g.west+float64(col)*inst.g.dLon, inst.g.north-float64(row)*inst.g.dLat
	return 10*math.Sin(lon*math.Pi/90) + float64(step), 8 * math.Cos(lat*math.Pi/60)
}

func (inst *fakeField) QueryE(_ context.Context, statement string, params map[string]string) (rec arrow.RecordBatch, err error) {
	inst.mu.Lock()
	inst.statements = append(inst.statements, statement)
	inst.mu.Unlock()
	alloc := memory.NewGoAllocator()
	hasTime := len(inst.steps) > 0
	switch statement {
	case ProbeStatement(inst.rel):
		fields := []arrow.Field{
			{Name: "lat", Type: arrow.PrimitiveTypes.Float64}, {Name: "lon", Type: arrow.PrimitiveTypes.Float64},
			{Name: "u", Type: arrow.PrimitiveTypes.Float32}, {Name: "v", Type: arrow.PrimitiveTypes.Float32},
		}
		if hasTime {
			fields = append(fields, arrow.Field{Name: "t", Type: arrow.PrimitiveTypes.Uint32})
		}
		schema := arrow.NewSchema(fields, nil)
		return array.NewRecordBatch(schema, emptyColumnsOf(schema), 0), nil
	case stepsStatement(inst.rel):
		b := newRecordBuilder(alloc, "ff_text:s", "ff_ms:i", "ff_type:s", "ff_count:u")
		for _, s := range inst.steps {
			b.add(s.Format("2006-01-02 15:04:05"), s.UnixMilli(), inst.timeType, uint64(inst.storedCols*inst.g.rows))
		}
		return b.record(), nil
	case geometryStatement(inst.rel, inst.timeTypeOrNone()):
		b := newRecordBuilder(alloc, "ff_south:f", "ff_north:f", "ff_west:f", "ff_east:f", "ff_rows:u", "ff_cols:u", "ff_count:u", "ff_speed_high:f")
		east := inst.g.west + float64(inst.storedCols-1)*inst.g.dLon
		b.add(inst.g.south(), inst.g.north, inst.g.west, east, uint64(inst.g.rows), uint64(inst.storedCols), uint64(inst.storedCols*inst.g.rows), 17.5)
		return b.record(), nil
	case regularityStatement(inst.rel, inst.timeTypeOrNone()):
		b := newRecordBuilder(alloc, "ff_off_x:f", "ff_off_y:f")
		b.add(1e-6, 0.0)
		return b.record(), nil
	case windowStatement(inst.rel, inst.timeTypeOrNone()):
		return inst.window(alloc, params)
	}
	return nil, eh.Errorf("the fake was sent a statement it does not know")
}

func (inst *fakeField) timeTypeOrNone() string {
	if len(inst.steps) == 0 {
		return ""
	}
	return inst.timeType
}

func (inst *fakeField) window(alloc memory.Allocator, params map[string]string) (rec arrow.RecordBatch, err error) {
	num := func(name string) int64 {
		v, pErr := strconv.ParseInt(params[name], 10, 64)
		if pErr != nil {
			err = pErr
		}
		return v
	}
	flt := func(name string) float64 {
		v, pErr := strconv.ParseFloat(params[name], 64)
		if pErr != nil {
			err = pErr
		}
		return v
	}
	factor, rowStart, rowEnd := num("ff_factor"), num("ff_row_start"), num("ff_row_end")
	colStart, colEnd, limit := num("ff_col_start"), num("ff_col_end"), num("ff_cap")
	latMin, latMax := flt("ff_lat_min"), flt("ff_lat_max")
	lon1, lon2 := [2]float64{flt("ff_lon_min1"), flt("ff_lon_max1")}, [2]float64{flt("ff_lon_min2"), flt("ff_lon_max2")}
	var turns []int64
	for _, s := range strings.Split(strings.Trim(params["ff_turns"], "[]"), ",") {
		v, pErr := strconv.ParseInt(s, 10, 64)
		if pErr != nil {
			err = pErr
		}
		turns = append(turns, v)
	}
	if err != nil {
		return
	}
	step := 0
	for i, s := range inst.steps {
		if s.Format("2006-01-02 15:04:05") == params["ff_t"] {
			step = i
		}
	}
	type sum struct {
		u, v, s float64
		n       uint32
	}
	bins := map[[2]int64]*sum{}
	for row := range inst.g.rows {
		lat := inst.g.north - float64(row)*inst.g.dLat
		if lat < latMin || lat > latMax {
			continue
		}
		for col := range inst.storedCols {
			lon := inst.g.west + float64(col)*inst.g.dLon
			if !(lon >= lon1[0] && lon <= lon1[1]) && !(lon >= lon2[0] && lon <= lon2[1]) {
				continue
			}
			for _, turn := range turns {
				ciu, ri := int64(col)+turn, int64(row)
				if ri < rowStart || ri > rowEnd || ciu < colStart || ciu > colEnd {
					continue
				}
				key := [2]int64{(ri - rowStart) / factor, (ciu - colStart) / factor}
				b := bins[key]
				if b == nil {
					b = &sum{}
					bins[key] = b
				}
				if inst.missing != nil && inst.missing(col, row) {
					continue
				}
				u, v := inst.value(col, row, step)
				b.u, b.v, b.s, b.n = b.u+u, b.v+v, b.s+math.Hypot(u, v), b.n+1
			}
		}
	}
	out := newRecordBuilder(alloc, "ff_r:i", "ff_c:i", "ff_mean_u:g", "ff_mean_v:g", "ff_mean_speed:g", "ff_valid:w")
	for key, b := range bins {
		if int64(out.rows) >= limit {
			break
		}
		n := float64(b.n)
		out.add(key[0], key[1], float32(b.u/n), float32(b.v/n), float32(b.s/n), b.n)
	}
	return out.record(), nil
}

// recordBuilder builds a small record from "name:kind" columns — s text,
// i Int64, u UInt64, w UInt32, f Float64, g Float32.
type recordBuilder struct {
	alloc    memory.Allocator
	names    []string
	kinds    []byte
	builders []array.Builder
	rows     int
}

func newRecordBuilder(alloc memory.Allocator, cols ...string) (inst *recordBuilder) {
	inst = &recordBuilder{alloc: alloc}
	for _, c := range cols {
		name, kind, _ := strings.Cut(c, ":")
		inst.names = append(inst.names, name)
		inst.kinds = append(inst.kinds, kind[0])
		var dt arrow.DataType
		switch kind[0] {
		case 's':
			dt = arrow.BinaryTypes.Binary // ClickHouse's default spelling of a String
		case 'i':
			dt = arrow.PrimitiveTypes.Int64
		case 'u':
			dt = arrow.PrimitiveTypes.Uint64
		case 'w':
			dt = arrow.PrimitiveTypes.Uint32
		case 'f':
			dt = arrow.PrimitiveTypes.Float64
		case 'g':
			dt = arrow.PrimitiveTypes.Float32
		}
		inst.builders = append(inst.builders, array.NewBuilder(alloc, dt))
	}
	return
}

func (inst *recordBuilder) add(values ...any) {
	for i, v := range values {
		switch b := inst.builders[i].(type) {
		case *array.BinaryBuilder:
			b.Append([]byte(v.(string)))
		case *array.Int64Builder:
			b.Append(v.(int64))
		case *array.Uint64Builder:
			b.Append(v.(uint64))
		case *array.Uint32Builder:
			b.Append(v.(uint32))
		case *array.Float64Builder:
			b.Append(v.(float64))
		case *array.Float32Builder:
			b.Append(v.(float32))
		}
	}
	inst.rows++
}

func (inst *recordBuilder) record() arrow.RecordBatch {
	fields := make([]arrow.Field, len(inst.names))
	cols := make([]arrow.Array, len(inst.names))
	for i, b := range inst.builders {
		cols[i] = b.NewArray()
		fields[i] = arrow.Field{Name: inst.names[i], Type: cols[i].DataType()}
		b.Release()
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(inst.rows))
	for _, c := range cols {
		c.Release()
	}
	return rec
}

func emptyColumnsOf(schema *arrow.Schema) (cols []arrow.Array) {
	for _, f := range schema.Fields() {
		b := array.NewBuilder(memory.NewGoAllocator(), f.Type)
		cols = append(cols, b.NewArray())
		b.Release()
	}
	return
}

func threeSteps() []time.Time {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return []time.Time{t0, t0.Add(3 * time.Hour), t0.Add(9 * time.Hour)}
}

func TestAPeriodicFieldConforms(t *testing.T) {
	fake := &fakeField{
		rel: Relation{From: "wind"}, timeType: "DateTime('UTC')", steps: threeSteps(),
		g: grid{west: -180, north: 90, dLon: 1.5, dLat: 1.5, cols: 240, rows: 121, periodic: true}, storedCols: 240,
	}
	src, err := NewSourceE(context.Background(), fake, fake.rel, Options{Meta: vectorfield.Meta{Name: "wind"}})
	require.NoError(t, err)
	meta := src.Describe()
	require.True(t, meta.PeriodicLon)
	require.Equal(t, "wind", meta.Name)
	require.Equal(t, threeSteps()[2], meta.Steps[2].Valid)
	require.InDelta(t, 17.5, meta.SpeedMax, 1e-6)
	vectorfieldtest.Run(t, src, vectorfieldtest.Options{Covered: true, MissingStep: -1})
}

func TestARepeatedSeamColumnIsLeftOut(t *testing.T) {
	fake := &fakeField{
		rel: Relation{From: "wind"},
		g:   grid{west: 0, north: 80, dLon: 2, dLat: 2, cols: 180, rows: 81, periodic: true}, storedCols: 181,
	}
	src, err := NewSourceE(context.Background(), fake, fake.rel, Options{})
	require.NoError(t, err)
	meta := src.Describe()
	require.True(t, meta.PeriodicLon)
	require.InDelta(t, 358, meta.East, 1e-9)
	require.Len(t, meta.Steps, 1)
	vectorfieldtest.Run(t, src, vectorfieldtest.Options{Covered: true, MissingStep: -1})
}

func TestARegionalFieldWithACoastConforms(t *testing.T) {
	fake := &fakeField{
		rel: Relation{Head: "WITH f AS (SELECT 1)", From: "f"}, timeType: "DateTime64(3)", steps: threeSteps(),
		g: grid{west: 340, north: 60, dLon: 0.1, dLat: 0.1, cols: 151, rows: 203}, storedCols: 151,
		missing: func(col, row int) bool { return col < 20 && row < 90 },
	}
	src, err := NewSourceE(context.Background(), fake, fake.rel, Options{})
	require.NoError(t, err)
	require.False(t, src.Describe().PeriodicLon)
	vectorfieldtest.Run(t, src, vectorfieldtest.Options{MissingStep: -1})

	// The request's longitude convention is not the relation's.
	win, err := src.SampleE(context.Background(), vectorfield.Request{West: -20, East: -5, South: 40, North: 60, MaxCols: 400, MaxRows: 400})
	require.NoError(t, err)
	require.InDelta(t, -20.1, win.West, 0.11)
	_, _, _, ok := win.Sample(-19.5, 58.5)
	require.False(t, ok, "missing data stays missing")
	_, _, _, ok = win.Sample(-12, 50)
	require.True(t, ok)

	// A coarse window keeps a bin at the coast only where half of it is data.
	coarse, err := src.SampleE(context.Background(), vectorfield.Request{West: -20, East: -5, South: 40, North: 60, MaxCols: 10, MaxRows: 10})
	require.NoError(t, err)
	require.Greater(t, coarse.Level, 0)
	missing := 0
	for _, u := range coarse.U {
		if u != u {
			missing++
		}
	}
	require.Greater(t, missing, 0)
	require.Less(t, missing, len(coarse.U))
}

func TestEveryRequestSendsTheSameText(t *testing.T) {
	fake := &fakeField{
		rel: Relation{From: "wind"}, timeType: "DateTime", steps: threeSteps(),
		g: grid{west: -180, north: 90, dLon: 1, dLat: 1, cols: 360, rows: 181, periodic: true}, storedCols: 360,
	}
	src, err := NewSourceE(context.Background(), fake, fake.rel, Options{})
	require.NoError(t, err)
	fake.statements = nil
	for _, req := range []vectorfield.Request{
		{West: -30, East: 40, South: 30, North: 70, Step: 1, MaxCols: 300, MaxRows: 300},
		{West: 170, East: 200, South: -12.345, North: 7.5, Step: 2, MaxCols: 17, MaxRows: 9},
	} {
		_, err = src.SampleE(context.Background(), req)
		require.NoError(t, err)
	}
	require.Len(t, fake.statements, 2)
	require.Equal(t, fake.statements[0], fake.statements[1], "a request's values ride parameters (ADR-0250 §SD3)")
	served, queries := src.LastServed()
	require.Equal(t, "2026-01-01 09:00:00", served.Params["ff_t"])
	require.Equal(t, uint64(2+4), queries)
}

func TestTheStepSlotTakesOnlyATimeType(t *testing.T) {
	for _, ok := range []string{"Date", "Date32", "DateTime", "DateTime('UTC')", "DateTime('Europe/Zurich')", "DateTime64(3)", "DateTime64(9, 'Etc/GMT+5')", "DateTime64(6,'UTC')"} {
		require.True(t, timeTypePattern.MatchString(ok), ok)
	}
	for _, bad := range []string{"", "UInt32", "Nullable(DateTime)", "LowCardinality(DateTime)", "DateTime}", "DateTime('UTC') } OR 1 --", "DateTime64(3, 'a'b')", "DateTime64(3)\n", "String"} {
		require.False(t, timeTypePattern.MatchString(bad), bad)
	}
}

func TestAReplyThatDoesNotFitThePlanIsRefused(t *testing.T) {
	g := grid{west: 0, north: 10, dLon: 1, dLat: 1, cols: 11, rows: 11}
	p := g.plan(vectorfield.Request{West: 2, East: 8, South: 2, North: 8, MaxCols: 3, MaxRows: 3})
	require.Equal(t, 2, p.factor)
	alloc := memory.NewGoAllocator()
	reply := func(r, c int64, valid uint32) arrow.RecordBatch {
		b := newRecordBuilder(alloc, "ff_r:i", "ff_c:i", "ff_mean_u:g", "ff_mean_v:g", "ff_mean_speed:g", "ff_valid:w")
		b.add(r, c, float32(1), float32(1), float32(1.5), valid)
		return b.record()
	}
	_, err := g.decodeWindowE(&p, reply(0, int64(p.cols), 4), 0.5)
	require.ErrorContains(t, err, "outside the window")
	_, err = g.decodeWindowE(&p, reply(-1, 0, 4), 0.5)
	require.ErrorContains(t, err, "outside the window")
	_, err = g.decodeWindowE(&p, reply(0, 0, 5), 0.5)
	require.ErrorContains(t, err, "more rows than it has nodes")

	win, err := g.decodeWindowE(&p, reply(0, 0, 1), 0.5)
	require.NoError(t, err)
	require.True(t, win.U[0] != win.U[0], "one valid node in four is under the valid fraction")
	win, err = g.decodeWindowE(&p, reply(0, 0, 2), 0.5)
	require.NoError(t, err)
	require.Equal(t, float32(1), win.U[0])
	require.GreaterOrEqual(t, win.Speed[0], float32(math.Sqrt2))

	// The grid's last column is half a bin wide, and one node fills it.
	last := int64(p.cols - 1)
	require.Equal(t, 2, g.cellsIn(&p, int(last), 0)) // column 10 only, two rows
	_, err = g.decodeWindowE(&p, reply(0, last, 3), 0.5)
	require.ErrorContains(t, err, "more rows than it has nodes")
}

func TestShapeOfStatesWhatIsWrong(t *testing.T) {
	f := func(name string, dt arrow.DataType) arrow.Field { return arrow.Field{Name: name, Type: dt} }
	f64 := arrow.PrimitiveTypes.Float64
	shape, reason := ShapeOf(arrow.NewSchema([]arrow.Field{f("lat", f64), f("lon", f64), f("u", arrow.PrimitiveTypes.Int16), f("v", f64)}, nil))
	require.Empty(t, reason)
	require.False(t, shape.HasTime)
	shape, reason = ShapeOf(arrow.NewSchema([]arrow.Field{f("t", arrow.PrimitiveTypes.Uint32), f("lat", f64), f("lon", f64), f("u", f64), f("v", f64)}, nil))
	require.Empty(t, reason)
	require.True(t, shape.HasTime)
	_, reason = ShapeOf(arrow.NewSchema([]arrow.Field{f("lat", f64), f("lng", f64), f("u", f64), f("v", f64)}, nil))
	require.Contains(t, reason, "`lon`")
	_, reason = ShapeOf(arrow.NewSchema([]arrow.Field{f("lat", f64), f("lon", f64), f("u", arrow.BinaryTypes.String), f("v", f64)}, nil))
	require.Contains(t, reason, "`u` must be numeric")
}

// TestPlanHoldsTheWindowContract checks, for arbitrary grids and requests,
// what the conformance suite checks for a handful: the request bounds the
// size, the spacing is inside the band a renderer's hysteresis assumes, and
// the longitude ranges reach every native column the plan reads.
func TestPlanHoldsTheWindowContract(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		periodic := rapid.Bool().Draw(t, "periodic")
		g := grid{north: rapid.Float64Range(-20, 90).Draw(t, "north"), periodic: periodic}
		g.rows = rapid.IntRange(2, 400).Draw(t, "rows")
		if periodic {
			g.cols = rapid.IntRange(8, 720).Draw(t, "cols")
			g.dLon = 360 / float64(g.cols)
			g.west = rapid.SampledFrom([]float64{-180, 0, -179.5}).Draw(t, "west")
		} else {
			g.cols = rapid.IntRange(2, 400).Draw(t, "cols")
			g.dLon = rapid.Float64Range(0.01, 0.5).Draw(t, "dLon")
			g.west = rapid.Float64Range(-170, 300).Draw(t, "west")
		}
		g.dLat = rapid.Float64Range(0.01, 0.4).Draw(t, "dLat")
		west := rapid.Float64Range(-400, 400).Draw(t, "reqWest")
		south := rapid.Float64Range(-89, 88).Draw(t, "reqSouth")
		req := vectorfield.Request{
			West: west, East: west + rapid.Float64Range(0.05, 360).Draw(t, "spanLon"),
			South: south, North: south + rapid.Float64Range(0.05, 90).Draw(t, "spanLat"),
			MaxCols: rapid.IntRange(2, 600).Draw(t, "maxCols"), MaxRows: rapid.IntRange(2, 600).Draw(t, "maxRows"),
		}
		p := g.plan(req)
		if p.empty {
			return
		}
		inside := 0
		for c := range p.cols {
			if lon := p.west + float64(c)*p.dLon; lon > req.West && lon < req.East {
				inside++
			}
		}
		require.LessOrEqual(t, inside, req.MaxCols)
		if p.factor > 1 {
			wantLon := (req.East - req.West) / float64(req.MaxCols)
			wantLat := (req.North - req.South) / float64(req.MaxRows)
			require.True(t, p.dLon < 2*wantLon || p.dLat < 2*wantLat, "less than twice as coarse as asked on the binding axis")
		}
		require.GreaterOrEqual(t, p.rowStart, int64(0))
		require.Less(t, p.rowEnd, int64(g.rows))
		period := int64(g.cols)
		for ciu := p.colStart; ciu <= p.colEnd; ciu++ {
			col := ciu
			if periodic {
				col = ((ciu % period) + period) % period
			}
			require.True(t, col >= 0 && col < period)
			lon := g.west + float64(col)*g.dLon
			in := (lon >= p.lonRanges[0][0] && lon <= p.lonRanges[0][1]) || (lon >= p.lonRanges[1][0] && lon <= p.lonRanges[1][1])
			require.True(t, in, "native column %d is inside a longitude range", col)
			carried := false
			for _, turn := range p.turns {
				carried = carried || col+turn == ciu
			}
			require.True(t, carried, "a turn carries native column %d to %d", col, ciu)
		}
	})
}
