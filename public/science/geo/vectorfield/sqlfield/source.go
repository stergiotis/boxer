package sqlfield

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
)

// QueryerI runs one statement with its parameters and returns the whole
// result as one record, which the caller releases. Parameter names are bare —
// an HTTP implementation adds its param_ prefix. It is called concurrently
// and must honour ctx.
//
// It is the seam that keeps a client out of this package's imports
// (ADR-0250 §SD4): whatever a host applies to its statements — rewrites,
// routing, a log stamp — it applies here too.
type QueryerI interface {
	QueryE(ctx context.Context, statement string, params map[string]string) (rec arrow.RecordBatch, err error)
}

// PurposeE is what a statement the source sends is for.
type PurposeE uint8

const (
	// PurposeDescribe is a statement of [NewSourceE].
	PurposeDescribe PurposeE = iota
	// PurposeWindow is a statement of [Source.SampleE].
	PurposeWindow
	// PurposeSummary is a statement of [Source.SummarizeE].
	PurposeSummary
)

type purposeKey struct{}

// PurposeOf is what the statement a [QueryerI] was handed ctx with is for. A
// host that supersedes a running query by a stable identity reads it to keep
// one identity per purpose: a window request may replace the window request
// before it, and must not replace a summary that happens to be running.
func PurposeOf(ctx context.Context) (purpose PurposeE) {
	purpose, _ = ctx.Value(purposeKey{}).(PurposeE)
	return
}

// Options tunes a [Source]; the zero value is usable.
type Options struct {
	// Meta names the field. Name, Quantity, Unit, Surface, Provenance,
	// SpeedMin, SpeedMax and ValidFraction are taken from it; the geometry
	// and the steps are read from the relation. A zero SpeedMax takes a high
	// quantile of the first step's magnitude, a zero ValidFraction one half.
	Meta vectorfield.Meta
	// Shape is the relation's shape when the caller has already probed it;
	// nil runs [ProbeStatement].
	Shape *Shape
	// MaxSteps bounds how many steps a relation may list. Zero takes 4096.
	MaxSteps int
}

const (
	defaultMaxSteps      = 4096
	defaultValidFraction = float32(0.5)
	// regularityTolerance is how far off the regular grid, in cells, a node
	// may stand. Coordinates stored as Float32 are off by about 1e-5.
	regularityTolerance = 0.05
)

// Served is one statement the source sent, for a host to show.
type Served struct {
	Statement string
	Params    map[string]string
	Rows      int
	Took      time.Duration
	Err       error
}

// Source is a [vectorfield.SourceI] whose windows are reduction queries over
// a field relation (ADR-0250 §SD2).
type Source struct {
	queryer  QueryerI
	rel      Relation
	meta     vectorfield.Meta
	grid     grid
	hasTime  bool
	timeType string
	stepText []string
	windowQ  string

	mu      sync.Mutex
	last    [PurposeSummary + 1]Served
	queries uint64
}

var _ vectorfield.SourceI = (*Source)(nil)

// NewSourceE describes the relation — its steps, then the geometry of the
// first — and refuses what a reduction would hide: a step with more rows than
// the grid has nodes, which is an unfiltered level or run that a mean would
// merge, and nodes off a regular grid.
func NewSourceE(ctx context.Context, queryer QueryerI, rel Relation, opts Options) (inst *Source, err error) {
	if queryer == nil {
		err = eh.Errorf("a source needs a queryer")
		return
	}
	if rel.From == "" {
		err = eh.Errorf("a relation needs something to read from")
		return
	}
	inst = &Source{queryer: queryer, rel: rel, meta: opts.Meta}
	if inst.meta.ValidFraction <= 0 || inst.meta.ValidFraction > 1 {
		inst.meta.ValidFraction = defaultValidFraction
	}
	err = inst.describeE(ctx, opts)
	if err != nil {
		inst = nil
		return
	}
	inst.windowQ = windowStatement(rel, inst.timeType)
	return
}

func (inst *Source) describeE(ctx context.Context, opts Options) (err error) {
	shape := opts.Shape
	if shape == nil {
		var rec arrow.RecordBatch
		rec, err = inst.queryE(ctx, PurposeDescribe, ProbeStatement(inst.rel), nil)
		if err != nil {
			return
		}
		probed, reason := ShapeOf(rec.Schema())
		rec.Release()
		if reason != "" {
			err = eb.Build().Str("reason", reason).Errorf("the relation is not a field")
			return
		}
		shape = &probed
	}
	inst.hasTime = shape.HasTime

	stepCounts := []uint64{0}
	inst.meta.Steps = []vectorfield.Step{{}}
	inst.stepText = []string{""}
	if inst.hasTime {
		stepCounts, err = inst.describeStepsE(ctx, opts)
		if err != nil {
			return
		}
	}

	params := map[string]string{}
	if inst.hasTime {
		params["ff_t"] = inst.stepText[0]
	}
	var rec arrow.RecordBatch
	rec, err = inst.queryE(ctx, PurposeDescribe, geometryStatement(inst.rel, inst.timeType), params)
	if err != nil {
		return
	}
	defer rec.Release()
	if rec.NumRows() != 1 {
		err = eb.Build().Int64("rows", rec.NumRows()).Errorf("the geometry statement returned other than one row")
		return
	}
	var south, north, west, east, speedHigh float64
	var rows, cols, count int64
	for _, f := range []struct {
		name string
		dst  *float64
	}{{"ff_south", &south}, {"ff_north", &north}, {"ff_west", &west}, {"ff_east", &east}, {"ff_speed_high", &speedHigh}} {
		*f.dst, _, err = floatAt(rec, f.name, 0)
		if err != nil {
			return
		}
	}
	for _, f := range []struct {
		name string
		dst  *int64
	}{{"ff_rows", &rows}, {"ff_cols", &cols}, {"ff_count", &count}} {
		*f.dst, err = intAt(rec, f.name, 0)
		if err != nil {
			return
		}
	}
	if count == 0 {
		err = eh.Errorf("the relation's first step has no rows")
		return
	}
	if rows < 2 || cols < 2 {
		err = eb.Build().Int64("rows", rows).Int64("cols", cols).
			Errorf("a field needs at least two distinct latitudes and two distinct longitudes")
		return
	}
	nodes := rows * cols
	if !inst.hasTime {
		stepCounts[0] = uint64(count)
	}
	for step, n := range stepCounts {
		if n > uint64(nodes) {
			err = eb.Build().Int("step", step).Str("t", inst.stepText[step]).Uint64("rows", n).Int64("nodes", nodes).
				Errorf("a step holds more rows than the grid has nodes: filter the relation to one level, run and member")
			return
		}
	}

	g := grid{
		west: west, north: north,
		dLon: (east - west) / float64(cols-1),
		dLat: (north - south) / float64(rows-1),
		cols: int(cols), rows: int(rows),
	}
	switch {
	case isFullCircle(g.cols, g.dLon):
		g.periodic = true
	case isFullCircle(g.cols-1, g.dLon):
		// The first column is repeated as the last; the repeat is left out.
		g.periodic = true
		g.cols--
	}
	err = inst.checkRegularE(ctx, g, params)
	if err != nil {
		return
	}
	inst.grid = g
	inst.meta.West, inst.meta.East = g.west, g.east()
	inst.meta.North, inst.meta.South = g.north, g.south()
	inst.meta.DLon, inst.meta.DLat = g.dLon, g.dLat
	inst.meta.PeriodicLon = g.periodic
	if !(inst.meta.SpeedMax > 0) && speedHigh > 0 && !math.IsInf(speedHigh, 0) {
		inst.meta.SpeedMax = float32(speedHigh)
	}
	return
}

func (inst *Source) describeStepsE(ctx context.Context, opts Options) (counts []uint64, err error) {
	maxSteps := opts.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	var rec arrow.RecordBatch
	rec, err = inst.queryE(ctx, PurposeDescribe, stepsStatement(inst.rel), map[string]string{"ff_cap": formatInt(int64(maxSteps) + 1)})
	if err != nil {
		return
	}
	defer rec.Release()
	n := int(rec.NumRows())
	if n == 0 {
		err = eh.Errorf("the relation has no rows")
		return
	}
	if n > maxSteps {
		err = eb.Build().Int("maxSteps", maxSteps).Errorf("the relation lists more steps than the bound: filter it to one run")
		return
	}
	inst.meta.Steps = make([]vectorfield.Step, n)
	inst.stepText = make([]string, n)
	counts = make([]uint64, n)
	for i := range n {
		var ms, count int64
		var typeName string
		inst.stepText[i], err = stringAt(rec, "ff_text", i)
		if err != nil {
			return
		}
		ms, err = intAt(rec, "ff_ms", i)
		if err != nil {
			return
		}
		count, err = intAt(rec, "ff_count", i)
		if err != nil {
			return
		}
		typeName, err = stringAt(rec, "ff_type", i)
		if err != nil {
			return
		}
		if i == 0 {
			if !timeTypePattern.MatchString(typeName) {
				err = eb.Build().Str("type", typeName).
					Errorf("the column t must be a Date, DateTime or DateTime64 and not nullable")
				return
			}
			inst.timeType = typeName
		}
		inst.meta.Steps[i] = vectorfield.Step{Valid: time.UnixMilli(ms).UTC()}
		counts[i] = uint64(count)
	}
	return
}

func (inst *Source) checkRegularE(ctx context.Context, g grid, stepParams map[string]string) (err error) {
	params := map[string]string{
		"ff_west":  formatFloat(g.west),
		"ff_north": formatFloat(g.north),
		"ff_dlon":  formatFloat(g.dLon),
		"ff_dlat":  formatFloat(g.dLat),
	}
	for k, v := range stepParams {
		params[k] = v
	}
	var rec arrow.RecordBatch
	rec, err = inst.queryE(ctx, PurposeDescribe, regularityStatement(inst.rel, inst.timeType), params)
	if err != nil {
		return
	}
	defer rec.Release()
	if rec.NumRows() != 1 {
		err = eb.Build().Int64("rows", rec.NumRows()).Errorf("the regularity statement returned other than one row")
		return
	}
	var offX, offY float64
	offX, _, err = floatAt(rec, "ff_off_x", 0)
	if err != nil {
		return
	}
	offY, _, err = floatAt(rec, "ff_off_y", 0)
	if err != nil {
		return
	}
	if !(offX <= regularityTolerance) || !(offY <= regularityTolerance) {
		err = eb.Build().Float64("offLonCells", offX).Float64("offLatCells", offY).
			Errorf("the nodes are not on a grid regular in latitude and longitude: resample the relation first")
	}
	return
}

// Describe implements [vectorfield.SourceI].
func (inst *Source) Describe() (meta vectorfield.Meta) { return inst.meta }

// LastServed is the statement sent most recently for a purpose and what came
// of it, and how many statements the source has sent in all.
func (inst *Source) LastServed(purpose PurposeE) (served Served, queries uint64) {
	inst.mu.Lock()
	served, queries = inst.last[purpose], inst.queries
	inst.mu.Unlock()
	return
}

// WindowStatement is the text every window request of this source sends.
func (inst *Source) WindowStatement() string { return inst.windowQ }

// SampleE implements [vectorfield.SourceI]. Window.Version is constant: the
// source cannot see the relation's data change, and a host that knows it has
// builds a new source.
func (inst *Source) SampleE(ctx context.Context, req vectorfield.Request) (win vectorfield.Window, err error) {
	if !(req.West < req.East) || !(req.South < req.North) {
		err = eb.Build().
			Float64("west", req.West).Float64("east", req.East).
			Float64("south", req.South).Float64("north", req.North).
			Errorf("a window request needs west < east and south < north")
		return
	}
	if req.MaxCols < 2 || req.MaxRows < 2 {
		err = eb.Build().Int("maxCols", req.MaxCols).Int("maxRows", req.MaxRows).
			Errorf("a window request needs room for at least two columns and two rows")
		return
	}
	if req.Step < 0 || req.Step >= len(inst.meta.Steps) {
		err = eb.Build().Int("step", req.Step).Int("steps", len(inst.meta.Steps)).Errorf("%w", vectorfield.ErrStepOutOfRange)
		return
	}
	p := inst.grid.plan(req)
	if p.empty {
		return
	}
	var rec arrow.RecordBatch
	rec, err = inst.queryE(ctx, PurposeWindow, inst.windowQ, inst.grid.windowParams(&p, inst.stepText[req.Step], inst.hasTime))
	if err != nil {
		return
	}
	defer rec.Release()
	win, err = inst.grid.decodeWindowE(&p, rec, inst.meta.ValidFraction)
	if err != nil {
		return
	}
	win.Step = req.Step
	win.Version = 1
	return
}

func (inst *Source) queryE(ctx context.Context, purpose PurposeE, statement string, params map[string]string) (rec arrow.RecordBatch, err error) {
	started := time.Now()
	rec, err = inst.queryer.QueryE(context.WithValue(ctx, purposeKey{}, purpose), statement, params)
	served := Served{Statement: statement, Params: params, Took: time.Since(started), Err: err}
	if err == nil {
		served.Rows = int(rec.NumRows())
	}
	inst.mu.Lock()
	inst.last[purpose] = served
	inst.queries++
	inst.mu.Unlock()
	return
}
