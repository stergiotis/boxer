// Package keelsonfield serves a gridded vector field held in memory as a
// family of keelson tables (ADR-0291): the relation itself, its options, and
// one table per statement purpose of sqlfield, each taking that statement's
// parameters as named arguments (ADR-0290) and replying in that statement's
// shape. A sqlfield source pointed at the family reads it with `SELECT * FROM
// keelson(…)` alone, which a host without ClickHouse can answer.
//
// Each purpose table computes what its statement computes, from the planes:
// the same filters, the same grouping, the means in float64 rounded to
// float32, NaN and infinities excluded as isFinite excludes them. The parity
// test in sqlfield holds the two to the same answers.
package keelsonfield

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
)

// Table-name suffixes of a family, after its base name.
const (
	// SuffixOpts names the options table, what vector_field_opts carries.
	SuffixOpts = "_opts"
	// SuffixSteps names the steps statement's table.
	SuffixSteps = "_steps"
	// SuffixGeometry names the geometry statement's table.
	SuffixGeometry = "_geometry"
	// SuffixRegularity names the regularity statement's table.
	SuffixRegularity = "_regularity"
	// SuffixWindow names the window statement's table.
	SuffixWindow = "_window"
	// SuffixSummary names the summary statement's table.
	SuffixSummary = "_summary"
)

// TimeType is the ClickHouse type the family's t stands for; a step's text
// is its toString.
const TimeType = "DateTime('UTC')"

// stepLayout is toString of a DateTime('UTC').
const stepLayout = "2006-01-02 15:04:05"

// Field is a field to serve: its steps' valid times and one grid per step,
// all on the same geometry, and what a legend shows.
type Field struct {
	Name     string
	Unit     string
	SpeedMax float32
	Steps    []time.Time
	Grids    []vectorfield.Grid
}

// field is a validated Field with the per-step texts.
type field struct {
	Field
	text []string
}

func (inst *field) step(text string) (i int, ok bool) {
	i = slices.Index(inst.text, text)
	return i, i >= 0
}

// Register registers base and its family in reg, checking f first.
func Register(reg *introspect.Registry, base string, f Field) (err error) {
	ff, err := validate(base, f)
	if err != nil {
		return
	}
	src := &source{base: base, f: ff}
	src.once.Do(func() {})
	return register(reg, base, src)
}

// RegisterLazy registers base and its family in reg with a field load reads
// on first use, and checks it then: a binary that serves a field it seldom
// shows does not decode it at start (a tab decodes the GFS forecast in about
// a second). A load that fails is the error of every read.
func RegisterLazy(reg *introspect.Registry, base string, load func() (Field, error)) (err error) {
	if !introspect.ValidTableName(base + SuffixRegularity) {
		return eb.Build().Str("base", base).Errorf("keelsonfield: the base name, with its longest suffix, must be a table name")
	}
	return register(reg, base, &source{base: base, load: load})
}

func register(reg *introspect.Registry, base string, src *source) (err error) {
	for _, p := range []introspect.Provider{
		relationProvider{name: base, src: src},
		optsProvider{name: base + SuffixOpts, base: base, src: src},
		stepsProvider{name: base + SuffixSteps, src: src},
		geometryProvider{name: base + SuffixGeometry, src: src},
		regularityProvider{name: base + SuffixRegularity, src: src},
		windowProvider{name: base + SuffixWindow, src: src},
		summaryProvider{name: base + SuffixSummary, src: src},
	} {
		if err = reg.Register(p); err != nil {
			return eb.Build().Str("table", p.Name()).Errorf("keelsonfield: %w", err)
		}
	}
	return
}

// source is a family's field, loaded and checked once.
type source struct {
	once sync.Once
	load func() (Field, error)
	base string
	f    *field
	err  error
}

func (inst *source) get() (f *field, err error) {
	inst.once.Do(func() {
		var raw Field
		raw, inst.err = inst.load()
		if inst.err == nil {
			inst.f, inst.err = validate(inst.base, raw)
		}
	})
	return inst.f, inst.err
}

func validate(base string, f Field) (ff *field, err error) {
	if !introspect.ValidTableName(base + SuffixRegularity) {
		return nil, eb.Build().Str("base", base).Errorf("keelsonfield: the base name, with its longest suffix, must be a table name")
	}
	if len(f.Steps) == 0 || len(f.Steps) != len(f.Grids) {
		return nil, eb.Build().Int("steps", len(f.Steps)).Int("grids", len(f.Grids)).Errorf("keelsonfield: a field needs one grid per step, and at least one")
	}
	g0 := f.Grids[0]
	if g0.Cols < 2 || g0.Rows < 2 || !(g0.DLon > 0) || !(g0.DLat > 0) {
		return nil, eb.Build().Int("cols", g0.Cols).Int("rows", g0.Rows).Errorf("keelsonfield: a grid needs two columns, two rows and positive spacing")
	}
	ff = &field{Field: f, text: make([]string, len(f.Steps))}
	for i, g := range f.Grids {
		if g.West != g0.West || g.North != g0.North || g.DLon != g0.DLon || g.DLat != g0.DLat || g.Cols != g0.Cols || g.Rows != g0.Rows {
			return nil, eb.Build().Int("step", i).Errorf("keelsonfield: every step must be on the first step's grid")
		}
		if len(g.U) != g.Cols*g.Rows || len(g.V) != g.Cols*g.Rows {
			return nil, eb.Build().Int("step", i).Errorf("keelsonfield: a plane's length is not cols × rows")
		}
		ff.text[i] = f.Steps[i].UTC().Format(stepLayout)
		if i > 0 && !f.Steps[i].After(f.Steps[i-1]) {
			return nil, eb.Build().Int("step", i).Errorf("keelsonfield: steps must be in increasing time")
		}
	}
	return
}

func (inst *field) grid() vectorfield.Grid { return inst.Grids[0] }

func (inst *field) lat(r int) float64 { g := inst.grid(); return g.North - float64(r)*g.DLat }
func (inst *field) lon(c int) float64 { g := inst.grid(); return g.West + float64(c)*g.DLon }

// --- the relation: t, lat, lon, u, v ---

type relationProvider struct {
	name string
	src  *source
}

var relationSchema = arrow.NewSchema([]arrow.Field{
	{Name: "t", Type: &arrow.TimestampType{Unit: arrow.Second, TimeZone: "UTC"}},
	{Name: "lat", Type: arrow.PrimitiveTypes.Float64},
	{Name: "lon", Type: arrow.PrimitiveTypes.Float64},
	{Name: "u", Type: arrow.PrimitiveTypes.Float32},
	{Name: "v", Type: arrow.PrimitiveTypes.Float32},
}, nil)

func (inst relationProvider) Name() string                    { return inst.name }
func (relationProvider) Schema() *arrow.Schema                { return relationSchema }
func (relationProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (inst relationProvider) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	f, err := inst.src.get()
	if err != nil {
		return nil, err
	}
	b := array.NewRecordBuilder(memory.NewGoAllocator(), relationSchema)
	defer b.Release()
	g := f.grid()
	n := len(f.Steps) * g.Cols * g.Rows
	for _, fb := range b.Fields() {
		fb.Reserve(n)
	}
	tb := b.Field(0).(*array.TimestampBuilder)
	lab, lob := b.Field(1).(*array.Float64Builder), b.Field(2).(*array.Float64Builder)
	ub, vb := b.Field(3).(*array.Float32Builder), b.Field(4).(*array.Float32Builder)
	for s, grid := range f.Grids {
		ts := arrow.Timestamp(f.Steps[s].Unix())
		for r := range g.Rows {
			for c := range g.Cols {
				i := r*g.Cols + c
				tb.Append(ts)
				lab.Append(f.lat(r))
				lob.Append(f.lon(c))
				ub.Append(grid.U[i])
				vb.Append(grid.V[i])
			}
		}
	}
	return b.NewRecordBatch(), nil
}

// --- options: what vector_field_opts carries, and the family's base ---

type optsProvider struct {
	name, base string
	src        *source
}

var optsSchema = arrow.NewSchema([]arrow.Field{
	{Name: "name", Type: arrow.BinaryTypes.String},
	{Name: "unit", Type: arrow.BinaryTypes.String},
	{Name: "speed_max", Type: arrow.PrimitiveTypes.Float64},
	{Name: "family", Type: arrow.BinaryTypes.String},
}, nil)

func (inst optsProvider) Name() string                    { return inst.name }
func (optsProvider) Schema() *arrow.Schema                { return optsSchema }
func (optsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (inst optsProvider) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	f, err := inst.src.get()
	if err != nil {
		return nil, err
	}
	b := array.NewRecordBuilder(memory.NewGoAllocator(), optsSchema)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).Append(f.Name)
	b.Field(1).(*array.StringBuilder).Append(f.Unit)
	b.Field(2).(*array.Float64Builder).Append(float64(f.SpeedMax))
	b.Field(3).(*array.StringBuilder).Append(inst.base)
	return b.NewRecordBatch(), nil
}

// --- shared pieces of the purpose tables ---

// noSnapshot is the argument-free read of a purpose table, which has none.
func noSnapshot(name string) error {
	return eb.Build().Str("table", name).Errorf("keelsonfield: the table is read with its arguments")
}

func req(name string, t introspect.ArgTypeE) introspect.ArgSpec {
	return introspect.ArgSpec{Name: name, Type: t, Required: true}
}

// planeArgs are the arguments that place nodes on the bins of a plan.
var planeArgs = []introspect.ArgSpec{
	req("north", introspect.ArgTypeFloat64), req("west", introspect.ArgTypeFloat64),
	req("dlat", introspect.ArgTypeFloat64), req("dlon", introspect.ArgTypeFloat64),
	req("factor", introspect.ArgTypeInt64),
	req("lat_min", introspect.ArgTypeFloat64), req("lat_max", introspect.ArgTypeFloat64),
	req("lon_min1", introspect.ArgTypeFloat64), req("lon_max1", introspect.ArgTypeFloat64),
	req("lon_min2", introspect.ArgTypeFloat64), req("lon_max2", introspect.ArgTypeFloat64),
	req("cap", introspect.ArgTypeUInt64),
}

// bounds is the node filter of the window and summary statements, on the
// relation's own coordinates.
type bounds struct {
	latMin, latMax          float64
	lonMin1, lonMax1        float64
	lonMin2, lonMax2        float64
	north, west, dLat, dLon float64
	factor                  int64
	cap                     uint64
}

func boundsOf(a introspect.Args) (b bounds) {
	return bounds{
		latMin: a.Float64("lat_min"), latMax: a.Float64("lat_max"),
		lonMin1: a.Float64("lon_min1"), lonMax1: a.Float64("lon_max1"),
		lonMin2: a.Float64("lon_min2"), lonMax2: a.Float64("lon_max2"),
		north: a.Float64("north"), west: a.Float64("west"),
		dLat: a.Float64("dlat"), dLon: a.Float64("dlon"),
		factor: a.Int64("factor"), cap: a.UInt64("cap"),
	}
}

func (inst bounds) holds(lat, lon float64) bool {
	return lat >= inst.latMin && lat <= inst.latMax &&
		((lon >= inst.lonMin1 && lon <= inst.lonMax1) || (lon >= inst.lonMin2 && lon <= inst.lonMax2))
}

// ri and ci are toInt64(round(…)) of the statements: ClickHouse rounds a
// float half to even.
func (inst bounds) ri(lat float64) int64 {
	return int64(math.RoundToEven((inst.north - lat) / inst.dLat))
}
func (inst bounds) ci(lon float64) int64 {
	return int64(math.RoundToEven((lon - inst.west) / inst.dLon))
}

func isFinite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// --- steps: ff_text, ff_ms, ff_type, ff_count ---

type stepsProvider struct {
	name string
	src  *source
}

var stepsSchema = arrow.NewSchema([]arrow.Field{
	{Name: "ff_text", Type: arrow.BinaryTypes.String},
	{Name: "ff_ms", Type: arrow.PrimitiveTypes.Int64},
	{Name: "ff_type", Type: arrow.BinaryTypes.String},
	{Name: "ff_count", Type: arrow.PrimitiveTypes.Uint64},
}, nil)

func (inst stepsProvider) Name() string                    { return inst.name }
func (stepsProvider) Schema() *arrow.Schema                { return stepsSchema }
func (stepsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (inst stepsProvider) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	return nil, noSnapshot(inst.name)
}
func (stepsProvider) Args() []introspect.ArgSpec {
	return []introspect.ArgSpec{req("cap", introspect.ArgTypeUInt64)}
}
func (inst stepsProvider) SnapshotArgs(_ introspect.Projection, a introspect.Args) (arrow.RecordBatch, error) {
	f, err := inst.src.get()
	if err != nil {
		return nil, err
	}
	b := array.NewRecordBuilder(memory.NewGoAllocator(), stepsSchema)
	defer b.Release()
	g := f.grid()
	for i, t := range f.Steps {
		if uint64(i) >= a.UInt64("cap") {
			break
		}
		b.Field(0).(*array.StringBuilder).Append(f.text[i])
		b.Field(1).(*array.Int64Builder).Append(t.UnixMilli())
		b.Field(2).(*array.StringBuilder).Append(TimeType)
		b.Field(3).(*array.Uint64Builder).Append(uint64(g.Cols * g.Rows))
	}
	return b.NewRecordBatch(), nil
}

// stepOf resolves the t argument; an unknown step reads no rows, as a
// predicate on t that matches none would.
func (inst *field) stepOf(a introspect.Args) (s int, ok bool) { return inst.step(a.String("t")) }

// --- geometry: one step's extent, node counts and a high speed quantile ---

type geometryProvider struct {
	name string
	src  *source
}

var geometrySchema = arrow.NewSchema([]arrow.Field{
	{Name: "ff_south", Type: arrow.PrimitiveTypes.Float64},
	{Name: "ff_north", Type: arrow.PrimitiveTypes.Float64},
	{Name: "ff_west", Type: arrow.PrimitiveTypes.Float64},
	{Name: "ff_east", Type: arrow.PrimitiveTypes.Float64},
	{Name: "ff_rows", Type: arrow.PrimitiveTypes.Uint64},
	{Name: "ff_cols", Type: arrow.PrimitiveTypes.Uint64},
	{Name: "ff_count", Type: arrow.PrimitiveTypes.Uint64},
	{Name: "ff_speed_high", Type: arrow.PrimitiveTypes.Float64},
}, nil)

func (inst geometryProvider) Name() string                    { return inst.name }
func (geometryProvider) Schema() *arrow.Schema                { return geometrySchema }
func (geometryProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (inst geometryProvider) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	return nil, noSnapshot(inst.name)
}
func (geometryProvider) Args() []introspect.ArgSpec {
	return []introspect.ArgSpec{req("t", introspect.ArgTypeString)}
}
func (inst geometryProvider) SnapshotArgs(_ introspect.Projection, a introspect.Args) (arrow.RecordBatch, error) {
	f, err := inst.src.get()
	if err != nil {
		return nil, err
	}
	b := array.NewRecordBuilder(memory.NewGoAllocator(), geometrySchema)
	defer b.Release()
	g := f.grid()
	s, ok := f.stepOf(a)
	south, north := f.lat(g.Rows-1), f.lat(0)
	west, east := f.lon(0), f.lon(g.Cols-1)
	rows, cols, count := uint64(g.Rows), uint64(g.Cols), uint64(g.Rows*g.Cols)
	high := math.NaN()
	if ok {
		grid := f.Grids[s]
		speeds := make([]float64, 0, len(grid.U))
		for i := range grid.U {
			u, v := float64(grid.U[i]), float64(grid.V[i])
			if sp := math.Sqrt(u*u + v*v); isFinite(sp) {
				speeds = append(speeds, sp)
			}
		}
		high = quantile(speeds, 0.995)
	} else {
		// No rows: the aggregates of an empty set.
		south, north, west, east, rows, cols, count = 0, 0, 0, 0, 0, 0, 0
	}
	for i, v := range []float64{south, north, west, east} {
		b.Field(i).(*array.Float64Builder).Append(v)
	}
	for i, v := range []uint64{rows, cols, count} {
		b.Field(4 + i).(*array.Uint64Builder).Append(v)
	}
	b.Field(7).(*array.Float64Builder).Append(high)
	return b.NewRecordBatch(), nil
}

// quantile interpolates linearly between the order statistics around
// q·(n−1). ClickHouse's quantile samples above 8192 values, so the two agree
// closely rather than exactly; the value only seeds a palette's range.
func quantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	slices.Sort(xs)
	pos := q * float64(len(xs)-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, len(xs)-1)
	return xs[lo] + (pos-float64(lo))*(xs[hi]-xs[lo])
}

// --- regularity: how far the nodes stand off the implied grid ---

type regularityProvider struct {
	name string
	src  *source
}

var regularitySchema = arrow.NewSchema([]arrow.Field{
	{Name: "ff_off_x", Type: arrow.PrimitiveTypes.Float64},
	{Name: "ff_off_y", Type: arrow.PrimitiveTypes.Float64},
}, nil)

func (inst regularityProvider) Name() string                    { return inst.name }
func (regularityProvider) Schema() *arrow.Schema                { return regularitySchema }
func (regularityProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (inst regularityProvider) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	return nil, noSnapshot(inst.name)
}
func (regularityProvider) Args() []introspect.ArgSpec {
	return []introspect.ArgSpec{
		req("t", introspect.ArgTypeString),
		req("west", introspect.ArgTypeFloat64), req("north", introspect.ArgTypeFloat64),
		req("dlon", introspect.ArgTypeFloat64), req("dlat", introspect.ArgTypeFloat64),
	}
}
func (inst regularityProvider) SnapshotArgs(_ introspect.Projection, a introspect.Args) (arrow.RecordBatch, error) {
	f, err := inst.src.get()
	if err != nil {
		return nil, err
	}
	b := array.NewRecordBuilder(memory.NewGoAllocator(), regularitySchema)
	defer b.Release()
	g := f.grid()
	var offX, offY float64
	if _, ok := f.stepOf(a); ok {
		west, north, dLon, dLat := a.Float64("west"), a.Float64("north"), a.Float64("dlon"), a.Float64("dlat")
		for c := range g.Cols {
			x := (f.lon(c) - west) / dLon
			offX = max(offX, math.Abs(x-math.RoundToEven(x)))
		}
		for r := range g.Rows {
			y := (north - f.lat(r)) / dLat
			offY = max(offY, math.Abs(y-math.RoundToEven(y)))
		}
	}
	b.Field(0).(*array.Float64Builder).Append(offX)
	b.Field(1).(*array.Float64Builder).Append(offY)
	return b.NewRecordBatch(), nil
}

// --- window: one step's nodes reduced to the bins of a plan ---

type windowProvider struct {
	name string
	src  *source
}

var windowSchema = arrow.NewSchema([]arrow.Field{
	{Name: "ff_r", Type: arrow.PrimitiveTypes.Int64},
	{Name: "ff_c", Type: arrow.PrimitiveTypes.Int64},
	{Name: "ff_mean_u", Type: arrow.PrimitiveTypes.Float32},
	{Name: "ff_mean_v", Type: arrow.PrimitiveTypes.Float32},
	{Name: "ff_mean_speed", Type: arrow.PrimitiveTypes.Float32},
	{Name: "ff_valid", Type: arrow.PrimitiveTypes.Uint32},
}, nil)

func (inst windowProvider) Name() string                    { return inst.name }
func (windowProvider) Schema() *arrow.Schema                { return windowSchema }
func (windowProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (inst windowProvider) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	return nil, noSnapshot(inst.name)
}
func (windowProvider) Args() []introspect.ArgSpec {
	return append([]introspect.ArgSpec{
		req("t", introspect.ArgTypeString),
		req("row_start", introspect.ArgTypeInt64), req("row_end", introspect.ArgTypeInt64),
		req("col_start", introspect.ArgTypeInt64), req("col_end", introspect.ArgTypeInt64),
		req("turns", introspect.ArgTypeString),
	}, planeArgs...)
}

type binAcc struct {
	u, v, speed float64
	valid       uint32
}

func (inst windowProvider) SnapshotArgs(_ introspect.Projection, a introspect.Args) (batch arrow.RecordBatch, err error) {
	f, err := inst.src.get()
	if err != nil {
		return nil, err
	}
	turns, err := parseInts(a.String("turns"))
	if err != nil {
		return nil, eb.Build().Str("table", inst.name).Errorf("keelsonfield: turns: %w", err)
	}
	bd := boundsOf(a)
	if bd.factor < 1 {
		return nil, eb.Build().Int64("factor", bd.factor).Errorf("keelsonfield: factor must be positive")
	}
	rowStart, rowEnd := a.Int64("row_start"), a.Int64("row_end")
	colStart, colEnd := a.Int64("col_start"), a.Int64("col_end")
	bins := make(map[[2]int64]*binAcc)
	var order [][2]int64
	if s, ok := f.stepOf(a); ok {
		g, grid := f.grid(), f.Grids[s]
		for r := range g.Rows {
			lat := f.lat(r)
			ri := bd.ri(lat)
			if ri < rowStart || ri > rowEnd {
				continue
			}
			for c := range g.Cols {
				lon := f.lon(c)
				if !bd.holds(lat, lon) {
					continue
				}
				i := r*g.Cols + c
				u, v := float64(grid.U[i]), float64(grid.V[i])
				ok := isFinite(u) && isFinite(v)
				for _, turn := range turns {
					ciu := bd.ci(lon) + turn
					if ciu < colStart || ciu > colEnd {
						continue
					}
					key := [2]int64{(ri - rowStart) / bd.factor, (ciu - colStart) / bd.factor}
					acc := bins[key]
					if acc == nil {
						acc = &binAcc{}
						bins[key] = acc
						order = append(order, key)
					}
					if ok {
						acc.u += u
						acc.v += v
						acc.speed += math.Sqrt(u*u + v*v)
						acc.valid++
					}
				}
			}
		}
	}
	b := array.NewRecordBuilder(memory.NewGoAllocator(), windowSchema)
	defer b.Release()
	for n, key := range order {
		if uint64(n) >= bd.cap {
			break
		}
		acc := bins[key]
		mean := func(sum float64) float32 {
			if acc.valid == 0 {
				return float32(math.NaN())
			}
			return float32(sum / float64(acc.valid))
		}
		b.Field(0).(*array.Int64Builder).Append(key[0])
		b.Field(1).(*array.Int64Builder).Append(key[1])
		b.Field(2).(*array.Float32Builder).Append(mean(acc.u))
		b.Field(3).(*array.Float32Builder).Append(mean(acc.v))
		b.Field(4).(*array.Float32Builder).Append(mean(acc.speed))
		b.Field(5).(*array.Uint32Builder).Append(acc.valid)
	}
	return b.NewRecordBatch(), nil
}

// parseInts reads an Array(Int64) literal: [a,b,…].
func parseInts(s string) (out []int64, err error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, eh.Errorf("not an array literal")
	}
	body := strings.TrimSpace(s[1 : len(s)-1])
	if body == "" {
		return nil, nil
	}
	for _, part := range strings.Split(body, ",") {
		var n int64
		n, err = strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return
}

// --- summary: every step inside the bounds reduced to two numbers ---

type summaryProvider struct {
	name string
	src  *source
}

var summarySchema = arrow.NewSchema([]arrow.Field{
	{Name: "ff_text", Type: arrow.BinaryTypes.String},
	{Name: "ff_mean_speed", Type: arrow.PrimitiveTypes.Float32},
	{Name: "ff_max_speed", Type: arrow.PrimitiveTypes.Float32},
	{Name: "ff_valid", Type: arrow.PrimitiveTypes.Uint32},
}, nil)

func (inst summaryProvider) Name() string                    { return inst.name }
func (summaryProvider) Schema() *arrow.Schema                { return summarySchema }
func (summaryProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (inst summaryProvider) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	return nil, noSnapshot(inst.name)
}
func (summaryProvider) Args() []introspect.ArgSpec { return planeArgs }
func (inst summaryProvider) SnapshotArgs(_ introspect.Projection, a introspect.Args) (batch arrow.RecordBatch, err error) {
	f, err := inst.src.get()
	if err != nil {
		return nil, err
	}
	bd := boundsOf(a)
	if bd.factor < 1 {
		return nil, eb.Build().Int64("factor", bd.factor).Errorf("keelsonfield: factor must be positive")
	}
	g := f.grid()
	b := array.NewRecordBuilder(memory.NewGoAllocator(), summarySchema)
	defer b.Release()
	var written uint64
	for s, grid := range f.Grids {
		var sumW, sumWS, peak float64
		var valid uint32
		any := false
		for r := range g.Rows {
			lat := f.lat(r)
			if bd.ri(lat)%bd.factor != 0 {
				continue
			}
			w := max(math.Cos(lat*math.Pi/180), 0)
			for c := range g.Cols {
				lon := f.lon(c)
				if !bd.holds(lat, lon) || bd.ci(lon)%bd.factor != 0 {
					continue
				}
				any = true
				i := r*g.Cols + c
				u, v := float64(grid.U[i]), float64(grid.V[i])
				sp := math.Sqrt(u*u + v*v)
				if !isFinite(sp) {
					continue
				}
				sumW += w
				sumWS += w * sp
				peak = max(peak, sp)
				valid++
			}
		}
		if !any {
			continue
		}
		if written >= bd.cap {
			break
		}
		written++
		mean := math.NaN()
		if sumW > 0 {
			mean = sumWS / sumW
		}
		b.Field(0).(*array.StringBuilder).Append(f.text[s])
		b.Field(1).(*array.Float32Builder).Append(float32(mean))
		b.Field(2).(*array.Float32Builder).Append(float32(peak))
		b.Field(3).(*array.Uint32Builder).Append(valid)
	}
	return b.NewRecordBatch(), nil
}
