package imagescope

import (
	"context"
	"fmt"
	"image"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

const (
	measureKind  = "imagescope.measure"
	measureTitle = "measure a region"
)

// PlaneStatistics is one plane's statistics over a region, in the plane's
// values. Every figure is computed from the region's samples at full
// resolution (ADR-0373 §SD8).
type PlaneStatistics struct {
	Plane   string  `desc:"the plane's name in the colour model: gray, red, green, blue or alpha"`
	Samples uint64  `desc:"valid samples counted"`
	Invalid uint64  `desc:"samples left out as invalid (NaN in a float plane)"`
	Min     float64 `desc:"smallest value"`
	Max     float64 `desc:"largest value"`
	Mean    float64 `desc:"arithmetic mean"`
	StdDev  float64 `desc:"standard deviation over the samples as the whole population (divisor n)"`
	Median  float64 `desc:"50th percentile, nearest rank"`
	P01     float64 `desc:"1st percentile, nearest rank"`
	P99     float64 `desc:"99th percentile, nearest rank"`
}

// measurement is the statistics of one region of one image. It is never
// modified once made.
type measurement struct {
	region image.Rectangle
	planes []PlaneStatistics
	exact  bool // every plane's percentiles are sample values (integer planes)
}

// methodMeasure states what the statistics are and what they assume, where
// the numbers are (keelson-app F3).
const methodMeasure = "Computed from the region's stored samples at full resolution, never from what is drawn or from a reduced level, and stated in values: the samples through the plane's value transform, where it has one. " +
	"Invalid samples (NaN in a float plane) are counted apart and left out of every other figure. " +
	"The standard deviation is over these samples as the whole population (divisor n): it describes the region and estimates nothing beyond it. " +
	"Percentiles are nearest rank: the smallest sample with at least that share of the samples at or below it. " +
	"The planes are measured separately; nothing here accounts for transparency or for how the samples were encoded."

// methodMeasureFloat is added when a plane's percentiles are not exact.
const methodMeasureFloat = " A float plane's percentiles are located within one of 4096 equal bins over the region's range, so they are right to a bin's width."

// measureRegion computes the statistics of every plane over rect from the
// store's level-0 tiles. n is the region's pixels times the planes: time is
// linear in n, and memory is one histogram per plane per tile — one bin per
// sample value an integer kind can hold, 4096 bins for a float plane —
// whatever the region's size. A float plane is read twice, once for the
// range its bins divide.
func measureRegion(ctx context.Context, s *tiles.Store, rect image.Rectangle, report bgjob.Reporter) (m *measurement, err error) {
	names := s.Model().PlaneNames()
	count := s.Model().PlaneCount()
	m = &measurement{region: rect, exact: s.Kind().IsInteger(), planes: make([]PlaneStatistics, 0, count)}
	for i := range count {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		report(uint64(i), uint64(count), "measuring "+names[i])
		h, herr := s.Histogram(ctx, i, rect)
		if herr != nil {
			return nil, herr
		}
		ps := PlaneStatistics{Plane: names[i], Samples: h.Valid(), Invalid: h.Invalid()}
		ps.Min, ps.Max, _ = h.Range()
		ps.Mean, _ = h.Mean()
		ps.StdDev, _ = h.StdDev()
		ps.Median, _ = h.Percentile(50)
		ps.P01, _ = h.Percentile(1)
		ps.P99, _ = h.Percentile(99)
		m.planes = append(m.planes, ps)
	}
	report(uint64(count), uint64(count), "")
	return m, nil
}

// method is the statement that goes with a measurement's numbers.
func (inst *measurement) method() (s string) {
	if inst.exact {
		return methodMeasure
	}
	return methodMeasure + methodMeasureFloat
}

// measureKey names a measurement: the store, by its identity, and the
// region. It is asked for every frame, so it reads no sample.
func measureKey(s *tiles.Store, rect image.Rectangle) (key string) {
	return fmt.Sprintf("%p|%s", s, rect)
}

// demandMeasurement keeps the measurement of the marked region current: it
// is computed off the frame when the image or the region changes, and the
// frame reads whatever has landed (W2). Without a region there is nothing to
// measure.
func (inst *App) demandMeasurement() {
	rect, marked := inst.viewer.Region()
	if inst.pic == nil || !marked {
		inst.measured, inst.measureErr, inst.measuring = nil, nil, false
		return
	}
	pic := inst.pic
	val, done, err, busy := inst.measure.DemandReporting(measureKey(pic, rect), func(ctx context.Context, report bgjob.Reporter) (*measurement, error) {
		return measureRegion(ctx, pic, rect, report)
	})
	inst.measuring = busy
	inst.measured, inst.measureErr = nil, nil
	if done && !busy {
		inst.measured, inst.measureErr = val, err
	}
}

// statsSchema is the statistics table: one row per plane.
var statsSchema = arrow.NewSchema([]arrow.Field{
	{Name: "plane", Type: arrow.BinaryTypes.String},
	{Name: "samples", Type: arrow.PrimitiveTypes.Uint64},
	{Name: "invalid", Type: arrow.PrimitiveTypes.Uint64},
	{Name: "min", Type: arrow.PrimitiveTypes.Float64},
	{Name: "max", Type: arrow.PrimitiveTypes.Float64},
	{Name: "mean", Type: arrow.PrimitiveTypes.Float64},
	{Name: "sd", Type: arrow.PrimitiveTypes.Float64},
	{Name: "median", Type: arrow.PrimitiveTypes.Float64},
	{Name: "p01", Type: arrow.PrimitiveTypes.Float64},
	{Name: "p99", Type: arrow.PrimitiveTypes.Float64},
}, nil)

// statsRecord is a measurement as the Arrow record the window's table draws
// (keelson-app D1). The caller releases it.
func statsRecord(m *measurement) arrow.RecordBatch {
	rb := array.NewRecordBuilder(memory.DefaultAllocator, statsSchema)
	defer rb.Release()
	for _, p := range m.planes {
		rb.Field(0).(*array.StringBuilder).Append(p.Plane)
		rb.Field(1).(*array.Uint64Builder).Append(p.Samples)
		rb.Field(2).(*array.Uint64Builder).Append(p.Invalid)
		for i, v := range []float64{p.Min, p.Max, p.Mean, p.StdDev, p.Median, p.P01, p.P99} {
			rb.Field(3 + i).(*array.Float64Builder).Append(v)
		}
	}
	return rb.NewRecordBatch()
}

// regionLine says which pixels a measurement is of.
func regionLine(r image.Rectangle) (s string) {
	return fmt.Sprintf("columns %d–%d, rows %d–%d · %d × %d = %d pixels",
		r.Min.X, r.Max.X-1, r.Min.Y, r.Max.Y-1, r.Dx(), r.Dy(), r.Dx()*r.Dy())
}
