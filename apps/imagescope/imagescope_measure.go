package imagescope

import (
	"context"
	"encoding/hex"
	"fmt"
	"image"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
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
const methodMeasure = "Computed from the region's stored samples at full resolution, never from what is drawn or from a reduced level. " +
	"The standard deviation is over these samples as the whole population (divisor n): it describes the region and estimates nothing beyond it. " +
	"Percentiles are nearest rank: the smallest sample with at least that share of the samples at or below it. " +
	"The planes are measured separately; nothing here accounts for transparency or for how the samples were encoded."

// methodMeasureFloat is added when a plane's percentiles are not exact.
const methodMeasureFloat = " A float plane's percentiles are located within one of 4096 equal bins over the region's range, so they are right to a bin's width."

// measureRegion computes the statistics of every plane over rect. n is the
// region's pixels times the planes: time is linear in n, and memory is one
// histogram per plane — one bin per sample value an integer kind can hold,
// 4096 bins for a float plane — whatever the region's size.
func measureRegion(ctx context.Context, img imaging.Image, rect image.Rectangle, report bgjob.Reporter) (m *measurement, err error) {
	names := img.Model().PlaneNames()
	m = &measurement{region: rect, exact: true, planes: make([]PlaneStatistics, 0, img.PlaneCount())}
	for i := range img.PlaneCount() {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		report(uint64(i), uint64(img.PlaneCount()), "measuring "+names[i])
		h, herr := stats.OfRegion(img.Plane(i), rect)
		if herr != nil {
			return nil, herr
		}
		m.exact = m.exact && h.IsExact()
		ps := PlaneStatistics{Plane: names[i], Samples: h.Valid, Invalid: h.Invalid}
		ps.Min, ps.Max, _ = h.Range()
		ps.Mean, _ = h.Mean()
		ps.StdDev, _ = h.StdDev()
		ps.Median, _ = h.Percentile(50)
		ps.P01, _ = h.Percentile(1)
		ps.P99, _ = h.Percentile(99)
		m.planes = append(m.planes, ps)
	}
	report(uint64(img.PlaneCount()), uint64(img.PlaneCount()), "")
	return m, nil
}

// method is the statement that goes with a measurement's numbers.
func (inst *measurement) method() (s string) {
	if inst.exact {
		return methodMeasure
	}
	return methodMeasure + methodMeasureFloat
}

// measureKey names a measurement: the image by the digest of its samples,
// which the image carries from when it was assembled, and the region. It is
// asked for every frame, so it must not read the samples.
func measureKey(img imaging.Image, rect image.Rectangle) (key string) {
	sum := img.Provenance().SampleHash
	return hex.EncodeToString(sum[:8]) + "|" + rect.String()
}

// demandMeasurement keeps the measurement of the marked region current: it
// is computed off the frame when the image or the region changes, and the
// frame reads whatever has landed (W2). Without a region there is nothing to
// measure.
func (inst *App) demandMeasurement() {
	rect, marked := inst.viewer.Region()
	if inst.img.IsZero() || !marked {
		inst.measured, inst.measureErr, inst.measuring = nil, nil, false
		return
	}
	img := inst.img
	val, done, err, busy := inst.measure.DemandReporting(measureKey(img, rect), func(ctx context.Context, report bgjob.Reporter) (*measurement, error) {
		return measureRegion(ctx, img, rect, report)
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
		for i, v := range []float64{p.Min, p.Max, p.Mean, p.StdDev, p.Median, p.P01, p.P99} {
			rb.Field(2 + i).(*array.Float64Builder).Append(v)
		}
	}
	return rb.NewRecordBatch()
}

// regionLine says which pixels a measurement is of.
func regionLine(r image.Rectangle) (s string) {
	return fmt.Sprintf("columns %d–%d, rows %d–%d · %d × %d = %d pixels",
		r.Min.X, r.Max.X-1, r.Min.Y, r.Max.Y-1, r.Dx(), r.Dy(), r.Dx()*r.Dy())
}
