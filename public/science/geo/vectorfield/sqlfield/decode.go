package sqlfield

import (
	"math"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
)

// Shape is what a relation's schema says of it.
type Shape struct {
	// HasTime says the relation carries t; without it the field has one step.
	HasTime bool
}

// ShapeOf judges a relation's schema against the contract of ADR-0250 §SD1.
// reason is empty when the relation is a field, and otherwise says what is
// wrong in words a person writing the query can act on. It looks at names and
// Arrow types only: whether t is a time is the server's to say, since a
// DateTime reaches Arrow as an integer, and the describe step asks it.
func ShapeOf(schema *arrow.Schema) (shape Shape, reason string) {
	if schema == nil {
		reason = "no schema"
		return
	}
	for _, need := range []struct{ name, what string }{
		{ColLat, "degrees north"},
		{ColLon, "degrees east"},
		{ColU, "the eastward component"},
		{ColV, "the northward component"},
	} {
		idx := schema.FieldIndices(need.name)
		if len(idx) == 0 {
			reason = "a vector field needs a column `" + need.name + "` (" + need.what + ")"
			return
		}
		if !isNumeric(schema.Field(idx[0]).Type) {
			reason = "the column `" + need.name + "` must be numeric, and is " + schema.Field(idx[0]).Type.String()
			return
		}
	}
	shape.HasTime = len(schema.FieldIndices(ColTime)) > 0
	return
}

func isNumeric(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.FLOAT16, arrow.FLOAT32, arrow.FLOAT64,
		arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64,
		arrow.DECIMAL128, arrow.DECIMAL256:
		return true
	}
	return false
}

func columnE(rec arrow.RecordBatch, name string) (col arrow.Array, err error) {
	idx := rec.Schema().FieldIndices(name)
	if len(idx) == 0 {
		err = eb.Build().Str("column", name).Errorf("the reply lacks a column")
		return
	}
	col = rec.Column(idx[0])
	return
}

// floatAt reads a floating cell; ok is false for NULL.
func floatAt(rec arrow.RecordBatch, name string, row int) (v float64, ok bool, err error) {
	col, err := columnE(rec, name)
	if err != nil {
		return
	}
	if col.IsNull(row) {
		v = math.NaN()
		return
	}
	ok = true
	switch a := col.(type) {
	case *array.Float64:
		v = a.Value(row)
	case *array.Float32:
		v = float64(a.Value(row))
	default:
		ok = false
		err = eb.Build().Str("column", name).Stringer("type", col.DataType()).Errorf("the reply's column is not floating point")
	}
	return
}

func intAt(rec arrow.RecordBatch, name string, row int) (v int64, err error) {
	col, err := columnE(rec, name)
	if err != nil {
		return
	}
	if col.IsNull(row) {
		err = eb.Build().Str("column", name).Errorf("the reply's column holds a NULL")
		return
	}
	switch a := col.(type) {
	case *array.Int64:
		v = a.Value(row)
	case *array.Uint64:
		v = int64(a.Value(row))
	case *array.Int32:
		v = int64(a.Value(row))
	case *array.Uint32:
		v = int64(a.Value(row))
	default:
		err = eb.Build().Str("column", name).Stringer("type", col.DataType()).Errorf("the reply's column is not an integer")
	}
	return
}

// stringAt reads a text cell. ClickHouse writes a String as Arrow binary
// unless told otherwise, so both spellings are read.
func stringAt(rec arrow.RecordBatch, name string, row int) (v string, err error) {
	col, err := columnE(rec, name)
	if err != nil {
		return
	}
	switch a := col.(type) {
	case *array.String:
		v = a.Value(row)
	case *array.LargeString:
		v = a.Value(row)
	case *array.Binary:
		v = string(a.Value(row))
	case *array.LargeBinary:
		v = string(a.Value(row))
	default:
		err = eb.Build().Str("column", name).Stringer("type", col.DataType()).Errorf("the reply's column is not text")
	}
	return
}

// decodeWindowE lays a window statement's reply into planes. Every sample
// starts missing; a bin is kept where its valid nodes reach validFraction of
// the nodes it covers, the policy at coasts and edges the pyramid applies
// level by level.
//
// The reply is not trusted to fit the plan (ADR-0250 §SD2): a bin outside it,
// more bins than it holds, or more valid nodes in a bin than the bin covers —
// two rows for one node — is an error and not a picture.
func (inst grid) decodeWindowE(p *windowPlan, rec arrow.RecordBatch, validFraction float32) (win vectorfield.Window, err error) {
	n := p.cols * p.rows
	if rec.NumRows() > int64(n) {
		err = eb.Build().Int64("rows", rec.NumRows()).Int("bins", n).Errorf("the reply holds more bins than the window asked for")
		return
	}
	win = vectorfield.Window{
		West: p.west, North: p.north, DLon: p.dLon, DLat: p.dLat,
		Cols: p.cols, Rows: p.rows, Level: p.level,
		U: make([]float32, n), V: make([]float32, n), Speed: make([]float32, n),
	}
	nan := float32(math.NaN())
	for i := range n {
		win.U[i], win.V[i], win.Speed[i] = nan, nan, nan
	}
	for row := range int(rec.NumRows()) {
		var wr, wc, valid int64
		wr, err = intAt(rec, "ff_r", row)
		if err != nil {
			return
		}
		wc, err = intAt(rec, "ff_c", row)
		if err != nil {
			return
		}
		valid, err = intAt(rec, "ff_valid", row)
		if err != nil {
			return
		}
		if wr < 0 || wr >= int64(p.rows) || wc < 0 || wc >= int64(p.cols) {
			err = eb.Build().Int64("row", wr).Int64("col", wc).Int("rows", p.rows).Int("cols", p.cols).
				Errorf("the reply holds a bin outside the window asked for")
			return
		}
		cells := inst.cellsIn(p, int(wc), int(wr))
		if valid > int64(cells) {
			err = eb.Build().Int64("valid", valid).Int("nodes", cells).
				Errorf("a bin holds more rows than it has nodes: filter the relation to one level, run and member")
			return
		}
		if valid == 0 || float32(valid)/float32(cells) < validFraction {
			continue
		}
		var u, v, speed float64
		var okU, okV, okS bool
		u, okU, err = floatAt(rec, "ff_mean_u", row)
		if err != nil {
			return
		}
		v, okV, err = floatAt(rec, "ff_mean_v", row)
		if err != nil {
			return
		}
		speed, okS, err = floatAt(rec, "ff_mean_speed", row)
		if err != nil {
			return
		}
		if !okU || !okV || !okS || math.IsNaN(u) || math.IsNaN(v) || math.IsNaN(speed) ||
			math.IsInf(u, 0) || math.IsInf(v, 0) || math.IsInf(speed, 0) {
			continue
		}
		o := int(wr)*p.cols + int(wc)
		win.U[o], win.V[o] = float32(u), float32(v)
		// The scalar mean is never shorter than the vector mean; rounding to
		// float32 on the server may say otherwise by a unit in the last place.
		win.Speed[o] = max(float32(speed), float32(math.Hypot(u, v)))
	}
	return
}
