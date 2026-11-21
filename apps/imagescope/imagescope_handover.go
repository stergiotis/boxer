package imagescope

// Send the pixels of the marked region to play as an ad-hoc dataset
// (ADR-0373 §SD9; keelson-app D3, D4). The window's table is a summary; the
// dataset is the samples it summarises, one row per pixel, so a reader who
// wants another statistic, a filter or a join has the real thing. The buffer
// play opens with reproduces the table's columns that SQL states the same
// way.

import (
	"context"
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/playground"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

const (
	// datasetAlias is the alias the pixels are published under.
	datasetAlias = "pixels"

	handoverKind  = "imagescope.handover"
	handoverTitle = "send a region's pixels to play"

	// maxHandoverPixels bounds the rows one handover publishes: about four
	// megapixels, which with two coordinates and four 16-bit planes is some
	// 64 MiB of Arrow. A larger region is refused, naming the remedy, rather
	// than cut short — a truncated region is a different region.
	maxHandoverPixels = 4 << 20
)

// handoverRegion is what a handover would publish: the marked region, or
// the whole image when none is marked. It refuses an image or region above
// the bound.
func handoverRegion(pic *tiles.Store, region image.Rectangle) (r image.Rectangle, err error) {
	if pic == nil {
		return r, app.RefuseOperation("there is no image; open one first")
	}
	r = region
	what := "the marked region"
	if r.Empty() {
		r, what = image.Rect(0, 0, pic.Width(), pic.Height()), "the whole image"
	}
	if n := int64(r.Dx()) * int64(r.Dy()); n > maxHandoverPixels {
		return r, app.RefuseOperation(fmt.Sprintf("%s has %d pixels and at most %d are sent; mark a smaller region", what, n, maxHandoverPixels))
	}
	return r, nil
}

// pixelsArrow renders the samples of a region of an image — read out of it,
// its top left pixel at at — as an Arrow IPC stream: one row per pixel in
// row-major order, its column and row in the image, and one column per
// plane holding the stored sample in the plane's own kind. Rows and bytes
// are proportional to the region's pixels, which the caller has bounded.
func pixelsArrow(img imaging.Image, at image.Point) (stream []byte, rows int64, err error) {
	rect := image.Rect(at.X, at.Y, at.X+img.Width(), at.Y+img.Height())
	names := img.Model().PlaneNames()
	fields := []arrow.Field{
		{Name: "col", Type: arrow.PrimitiveTypes.Uint32},
		{Name: "row", Type: arrow.PrimitiveTypes.Uint32},
	}
	var sampleType arrow.DataType
	switch img.Kind() {
	case imaging.SampleKindU8:
		sampleType = arrow.PrimitiveTypes.Uint8
	case imaging.SampleKindU16:
		sampleType = arrow.PrimitiveTypes.Uint16
	default:
		sampleType = arrow.PrimitiveTypes.Float32
	}
	for _, n := range names {
		fields = append(fields, arrow.Field{Name: n, Type: sampleType})
	}
	rb := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
	defer rb.Release()
	n := rect.Dx() * rect.Dy()
	rb.Reserve(n)
	cols, rws := rb.Field(0).(*array.Uint32Builder), rb.Field(1).(*array.Uint32Builder)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			cols.UnsafeAppend(uint32(x))
			rws.UnsafeAppend(uint32(y))
		}
	}
	w := img.Width()
	for i := range names {
		p := img.Plane(i)
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			lo, hi := (y-rect.Min.Y)*w, (y-rect.Min.Y+1)*w
			switch b := rb.Field(2 + i).(type) {
			case *array.Uint8Builder:
				b.AppendValues(p.U8()[lo:hi], nil)
			case *array.Uint16Builder:
				b.AppendValues(p.U16()[lo:hi], nil)
			case *array.Float32Builder:
				b.AppendValues(p.F32()[lo:hi], nil)
			}
		}
	}
	rec := rb.NewRecordBatch()
	defer rec.Release()
	stream, err = adhocdata.EncodeRecord(rec)
	return stream, int64(n), err
}

// handoverSql is the buffer the opened play window is seeded with: the
// statistics table, one row per plane, computed by ClickHouse from the
// pixels, in values as the window's table is. The dataset holds stored
// samples, so a plane's value transform is written into the query, and a
// float plane's invalid samples — NaN, which an aggregate would carry into
// its result — are left out as the window leaves them out. The percentile
// columns of the window's table are nearest-rank percentiles, a rule
// ClickHouse's quantile functions do not all share, so only the median —
// quantileExactLow's lower median — is restated here.
func handoverSql(alias string, pic *tiles.Store) (sql string) {
	var b strings.Builder
	b.WriteString("-- Pixels of the region marked in imagescope, one row per pixel: col, row and a\n")
	b.WriteString("-- column per plane holding the stored sample. This restates the window's table.\n")
	for i, p := range pic.Model().PlaneNames() {
		if i > 0 {
			b.WriteString("UNION ALL\n")
		}
		v := p
		if t := pic.Transform(i); !t.IsIdentity() {
			v = fmt.Sprintf("(%s * %s + %s)", p, strconv.FormatFloat(t.Scale, 'g', -1, 64), strconv.FormatFloat(t.Offset, 'g', -1, 64))
		}
		fmt.Fprintf(&b, "SELECT '%[1]s' AS plane, count() AS samples, min(%[2]s) AS min, max(%[2]s) AS max,\n"+
			"       avg(%[2]s) AS mean, stddevPop(%[2]s) AS sd, quantileExactLow(0.5)(%[2]s) AS median\n"+
			"FROM keelson('%[3]s')\n", p, v, alias)
		if pic.Kind() == imaging.SampleKindF32 {
			fmt.Fprintf(&b, "WHERE NOT isNaN(%s)\n", p)
		}
	}
	return b.String()
}

// startHandover runs requestHandover off the frame: counted into the
// window's lifetime scope, so the host's drain sees it (W5), and as a
// keelson task, so it has a row. A host that provides no scope gets a plain
// goroutine — there is nothing to drain it into.
func (inst *App) startHandover() {
	pic := inst.pic
	region, _ := inst.viewer.Region()
	run := func(context.Context) {
		_ = task.RunToCompletion(inst.tasks, task.SpawnOpts{Kind: handoverKind, Title: handoverTitle}, func() error {
			return inst.requestHandover(pic, region)
		})
	}
	if inst.scope != nil {
		inst.scope.Go(run)
		return
	}
	go run(context.Background()) // lifetime: no scope on this host (tour, tests); the request is bounded by the bus timeouts
}

// requestHandover publishes the region and opens a play window on it. Both
// halves are synchronous bus round-trips, so this runs off the render
// goroutine; the outcome lands in the handover fields for the button to
// show, after the reply (W8). A second request while one is in flight is
// dropped.
func (inst *App) requestHandover(pic *tiles.Store, region image.Rectangle) (err error) {
	inst.handoverMu.Lock()
	if inst.handoverBusy {
		inst.handoverMu.Unlock()
		return
	}
	inst.handoverBusy, inst.handoverErr, inst.handoverNote = true, "", ""
	inst.handoverMu.Unlock()

	note, err := inst.handover(pic, region)

	inst.handoverMu.Lock()
	inst.handoverBusy = false
	if err != nil {
		inst.handoverErr = err.Error()
		inst.logger.Warn().Err(err).Msg("imagescope: handover to play failed")
	} else {
		inst.handoverNote = note
	}
	inst.handoverMu.Unlock()
	return
}

// handover is the blocking body: publish — the publisher republishes onto
// the handle it holds, so repeated presses do not leak datasets — and then
// ask the window host for a play window.
func (inst *App) handover(pic *tiles.Store, region image.Rectangle) (note string, err error) {
	if inst.bus == nil {
		return "", eh.Errorf("no bus on this host; the handover needs the app runtime")
	}
	rect, err := handoverRegion(pic, region)
	if err != nil {
		return "", err
	}
	samples, err := pic.Read(context.Background(), rect)
	if err != nil {
		return "", eh.Errorf("read the pixels: %w", err)
	}
	stream, rows, err := pixelsArrow(samples, rect.Min)
	if err != nil {
		return "", eh.Errorf("encode the pixels: %w", err)
	}
	pub, err := inst.pub.Publish(inst.ioCtx, inst.bus, stream)
	if err != nil {
		return "", eh.Errorf("publish the pixels: %w", err)
	}
	cfg := playground.Launch{
		Sql:          handoverSql(datasetAlias, pic),
		AutoRun:      true,
		Endpoint:     playground.EndpointIntrospection,
		Datasets:     []string{pub.Alias},
		DatasetNames: []string{datasetAlias},
	}
	if _, err = playground.Open(inst.ioCtx, inst.bus, cfg); err != nil {
		return "", eh.Errorf("open play: %w", err)
	}
	return fmt.Sprintf("opened in play — %d pixels of %s as %s (rev %d)", rows, regionLine(rect), pub.Handle, pub.Revision), nil
}

// handoverState snapshots the fields the button and its status line read.
func (inst *App) handoverState() (busy bool, note string, errText string) {
	inst.handoverMu.Lock()
	defer inst.handoverMu.Unlock()
	return inst.handoverBusy, inst.handoverNote, inst.handoverErr
}

// clearHandover drops the last handover's outcome, which described another
// image.
func (inst *App) clearHandover() {
	inst.handoverMu.Lock()
	inst.handoverNote, inst.handoverErr = "", ""
	inst.handoverMu.Unlock()
}
