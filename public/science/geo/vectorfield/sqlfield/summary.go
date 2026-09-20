package sqlfield

import (
	"context"
	"math"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
)

// StepSummary is the magnitude of one step inside some bounds.
type StepSummary struct {
	// Mean is the mean magnitude weighted by the cosine of latitude, so that
	// it is a mean over area and the crowded polar rows of a lat/lon grid do
	// not outvote the rest. NaN where the bounds hold no valid node.
	Mean float32
	// Max is the largest magnitude among the nodes read — a sample's
	// maximum, since the nodes are decimated; a gust between them is not in
	// it. NaN where the bounds hold no valid node.
	Max float32
	// Valid is how many nodes the two were taken from.
	Valid uint32
}

// summaryStatement reduces every step inside a plan's bounds to two numbers.
// It reads every step, where a window reads one, so the nodes are decimated
// to the plan's factor: one node in factor² is what a window of that view
// would show anyway. The text is fixed per relation, like the window's
// (ADR-0250 §SD3).
func summaryStatement(rel Relation, timeType string) string {
	stepCol, groupBy := "'' AS ff_text,", ""
	inner := ""
	if timeType != "" {
		stepCol, groupBy = "toString(t) AS ff_text,", "\nGROUP BY t\nORDER BY t ASC"
		inner = "\n        t,"
	}
	return rel.statement(`SELECT
    ` + stepCol + `
    toFloat32(avgWeightedIf(ff_speed, ff_weight, isFinite(ff_speed))) AS ff_mean_speed,
    toFloat32(maxIf(ff_speed, isFinite(ff_speed))) AS ff_max_speed,
    toUInt32(countIf(isFinite(ff_speed))) AS ff_valid
FROM (
    SELECT` + inner + `
        sqrt(toFloat64(u) * toFloat64(u) + toFloat64(v) * toFloat64(v)) AS ff_speed,
        greatest(cos(radians(toFloat64(lat))), 0) AS ff_weight,
        toInt64(round(({ff_north:Float64} - toFloat64(lat)) / {ff_dlat:Float64})) AS ff_ri,
        toInt64(round((toFloat64(lon) - {ff_west:Float64}) / {ff_dlon:Float64})) AS ff_ci
    FROM ` + rel.From + `
    WHERE lat >= {ff_lat_min:Float64} AND lat <= {ff_lat_max:Float64}
        AND ((lon >= {ff_lon_min1:Float64} AND lon <= {ff_lon_max1:Float64})
            OR (lon >= {ff_lon_min2:Float64} AND lon <= {ff_lon_max2:Float64}))
)
WHERE modulo(ff_ri, {ff_factor:Int64}) = 0 AND modulo(ff_ci, {ff_factor:Int64}) = 0` + groupBy + `
LIMIT {ff_cap:UInt64}`)
}

// SummarizeE reduces every step inside the request's bounds to a
// [StepSummary], one per step of [Source.Describe] and in that order. The
// request's Step is ignored; MaxCols and MaxRows choose the decimation, as
// they choose a window's resolution.
//
// It is one query over all steps. On a table ordered by time first it reads
// the bounds' share of every step, which is what a time control showing "when
// is it strong here" costs per settled view.
func (inst *Source) SummarizeE(ctx context.Context, req vectorfield.Request) (out []StepSummary, err error) {
	if !(req.West < req.East) || !(req.South < req.North) || req.MaxCols < 2 || req.MaxRows < 2 {
		err = eb.Build().
			Float64("west", req.West).Float64("east", req.East).
			Float64("south", req.South).Float64("north", req.North).
			Int("maxCols", req.MaxCols).Int("maxRows", req.MaxRows).
			Errorf("a summary request needs west < east, south < north and room for two columns and two rows")
		return
	}
	nan := float32(math.NaN())
	out = make([]StepSummary, len(inst.meta.Steps))
	for i := range out {
		out[i] = StepSummary{Mean: nan, Max: nan}
	}
	p := inst.grid.plan(req)
	if p.empty {
		return
	}
	params := inst.grid.windowParams(&p, "", false)
	for _, unused := range []string{"ff_row_start", "ff_row_end", "ff_col_start", "ff_col_end", "ff_turns"} {
		delete(params, unused)
	}
	params["ff_cap"] = formatInt(int64(len(out)) + 1)

	var rec arrow.RecordBatch
	rec, err = inst.queryE(ctx, PurposeSummary, summaryStatement(inst.rel, inst.timeType), params)
	if err != nil {
		return
	}
	defer rec.Release()
	if rec.NumRows() > int64(len(out)) {
		err = eb.Build().Int64("rows", rec.NumRows()).Int("steps", len(out)).
			Errorf("the summary lists more steps than the relation was described with: describe it again")
		return
	}
	byText := make(map[string]int, len(inst.stepText))
	for i, text := range inst.stepText {
		byText[text] = i
	}
	for row := range int(rec.NumRows()) {
		var text string
		var valid int64
		var mean, peak float64
		text, err = stringAt(rec, "ff_text", row)
		if err != nil {
			return
		}
		step, known := byText[text]
		if !known {
			continue // a step that appeared since the relation was described
		}
		valid, err = intAt(rec, "ff_valid", row)
		if err != nil {
			return
		}
		mean, _, err = floatAt(rec, "ff_mean_speed", row)
		if err != nil {
			return
		}
		peak, _, err = floatAt(rec, "ff_max_speed", row)
		if err != nil {
			return
		}
		if valid <= 0 || math.IsNaN(mean) || math.IsInf(mean, 0) || math.IsInf(peak, 0) {
			continue
		}
		out[step] = StepSummary{Mean: float32(mean), Max: float32(peak), Valid: uint32(valid)}
	}
	return
}
