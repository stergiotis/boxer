package sysmreplay

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The load preview (ADR-0197 §SD10, second query).
//
// Unlike coverage, this one reads a metric *value*, which lives in a leeway
// section rather than in an envelope column. It does not hand-write the array
// arithmetic that would take — the store already publishes the SQL its own
// component definitions generate ([sysmfacts.SysmetricsComponentSQL], ADR-0189
// §SD6), so the projection here is the generated artefact verbatim, wrapped in
// a GROUP BY. Its `Filter` rides with it for the reason that artefact set
// documents: a projection locates an attribute by indexOf and answers
// plausibly and wrongly on a row carrying a membership twice.

// PreviewPoint is one bucket of the load preview.
type PreviewPoint struct {
	// StartMS is the bin's inclusive lower bound, epoch milliseconds UTC.
	StartMS int64
	// Value is the mean of the metric over the bin, in the metric's own units
	// (CPU busy percent, 0..100).
	Value float64
}

// DefaultPreviewBuckets is how many bins Preview aims for. It is a screen-width
// number for the same reason coverage's is: the strip is a few hundred pixels
// and a finer grid returns rows nothing can distinguish.
const DefaultPreviewBuckets = 240

// Preview reports mean CPU busy percent per time bin, for the strip that shows
// where the interesting part of a range is.
//
// CPU total is the metric because it is the one every host has and the one a
// person scanning for "when was this box busy" means. PSI would answer "which
// resource was the bottleneck" better, and is the natural second series once
// one is not enough.
//
// It is a blocking database read over a section, which is a bigger query than
// coverage. Call it off the render thread.
func (inst *Reader) Preview(ctx context.Context, w Window, bucket time.Duration) (points []PreviewPoint, err error) {
	if w.From.IsZero() || w.To.IsZero() {
		err = eh.Errorf("sysmreplay: preview needs a bounded window")
		return
	}
	if !w.To.After(w.From) {
		err = eh.Errorf("sysmreplay: preview window ends before it starts")
		return
	}
	if inst.exec == nil {
		err = eh.Errorf("sysmreplay: preview needs Options.Exec — the reader was built without one")
		return
	}
	bucket = previewBucket(w, bucket)

	for rec, rerr := range inst.exec.QueryArrow(ctx, inst.previewSQL(w, bucket)) {
		if rerr != nil {
			points = nil
			err = eh.Errorf("sysmreplay: preview query: %w", rerr)
			return
		}
		starts, ok := rec.Column(0).(*array.Int64)
		if !ok {
			rec.Release()
			points = nil
			err = eb.Build().Stringer("dataType", rec.Column(0).DataType()).Errorf("sysmreplay: preview bucket column is not int64")
			return
		}
		vals, ok := rec.Column(1).(*array.Float64)
		if !ok {
			rec.Release()
			points = nil
			err = eb.Build().Stringer("dataType", rec.Column(1).DataType()).Errorf("sysmreplay: preview value column is not float64")
			return
		}
		for i := range int(rec.NumRows()) {
			points = append(points, PreviewPoint{StartMS: starts.Value(i), Value: vals.Value(i)})
		}
		rec.Release()
	}
	return
}

// previewBucket resolves the bin width the same way coverage does.
func previewBucket(w Window, bucket time.Duration) (out time.Duration) {
	out = bucket
	if out <= 0 {
		out = w.To.Sub(w.From) / DefaultPreviewBuckets
	}
	out = max(out, time.Second)
	return
}

// previewSQL wraps the generated projection in a bucketed average.
//
// The projection is a CAST to a named tuple, so the field comes off it by name
// — which is what keeps this from depending on the tuple's field order, a thing
// the generator is free to change.
func (inst *Reader) previewSQL(w Window, bucket time.Duration) (sql string) {
	a := sysmfacts.SysmetricsComponentSQL.Kinds["SysCpu"]
	col := sysmfacts.SysmetricsColOrder
	secs := int64(bucket / time.Second)

	var b strings.Builder
	b.WriteString("SELECT toInt64(toUnixTimestamp(toStartOfInterval(t.ts, INTERVAL ")
	b.WriteString(strconv.FormatInt(secs, 10))
	b.WriteString(" SECOND))) * 1000 AS b, toFloat64(avg(t.v)) AS v FROM (SELECT ")
	b.WriteString(col)
	b.WriteString(" AS ts, (")
	b.WriteString(a.Projection)
	b.WriteString(").TotalPercent AS v FROM ")
	b.WriteString(sysmfacts.SysmetricsTableName)
	b.WriteString(" WHERE (")
	b.WriteString(a.Filter)
	b.WriteString(") AND ")
	b.WriteString(sysmfacts.SysmetricsColKey)
	b.WriteString(" = ")
	b.WriteString(formatKey(EntityKey(inst.host, DomainCPU)))
	b.WriteString(" AND ")
	b.WriteString(col)
	b.WriteString(" >= fromUnixTimestamp64Nano(")
	b.WriteString(strconv.FormatInt(w.From.UnixNano(), 10))
	b.WriteString(") AND ")
	b.WriteString(col)
	b.WriteString(" < fromUnixTimestamp64Nano(")
	b.WriteString(strconv.FormatInt(w.To.UnixNano(), 10))
	b.WriteString(")) AS t GROUP BY b ORDER BY b")
	sql = b.String()
	return
}
