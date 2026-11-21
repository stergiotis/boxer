package imagescope

// Regions out of the window (ADR-0399 §M2; keelson-app D3, D4): the
// figures of every region as one ad-hoc dataset and a line's profile as
// another, each handed to the playground; and the regions document — and
// an operation's record — saved to a file the person picks, or the regions
// document read from one, through the file broker's dialogs.

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/playground"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging/regions"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

const (
	// regionsAlias and profileAlias are the aliases the two datasets are
	// published under.
	regionsAlias = "regions"
	profileAlias = "profile"

	regionsHandoverKind  = "imagescope.regions-handover"
	regionsHandoverTitle = "send the regions' figures to play"
	profileHandoverKind  = "imagescope.profile-handover"
	profileHandoverTitle = "send a line's profile to play"
	documentKind         = "imagescope.document"
	documentTitle        = "save or read a document"

	opHandleWrite = ".write"
)

// outcome is what the last request of one kind came to, written off the
// frame and read by it.
type outcome struct {
	mu   sync.Mutex
	busy bool
	note string
	err  string
}

// begin marks a request under way; false when one already is.
func (inst *outcome) begin() bool {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.busy {
		return false
	}
	inst.busy, inst.note, inst.err = true, "", ""
	return true
}

func (inst *outcome) end(note string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.busy = false
	if err != nil {
		inst.err = refusalText(err)
		return
	}
	inst.note = note
}

func (inst *outcome) state() (busy bool, note, errText string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.busy, inst.note, inst.err
}

// spawnRequest runs a blocking request off the frame as a task of the
// window, counted into its lifetime scope (W5).
func (inst *App) spawnRequest(kind, title string, o *outcome, body func() (string, error)) {
	if !o.begin() {
		return
	}
	run := func(context.Context) {
		_ = task.RunToCompletion(inst.tasks, task.SpawnOpts{Kind: kind, Title: title}, func() error {
			note, err := body()
			o.end(note, err)
			return err
		})
	}
	if inst.scope != nil {
		inst.scope.Go(run)
		return
	}
	go run(context.Background()) // lifetime: no scope on this host (tour, tests); the request is bounded by the bus timeouts
}

// ---- the figures of the regions ----

// regionsSchema is the dataset of the regions' figures: one row per region
// and plane of the image shown.
var regionsSchema = arrow.NewSchema([]arrow.Field{
	{Name: "region", Type: arrow.BinaryTypes.String},
	{Name: "label", Type: arrow.BinaryTypes.String},
	{Name: "shape", Type: arrow.BinaryTypes.String},
	{Name: "origin", Type: arrow.BinaryTypes.String},
	{Name: "frame", Type: arrow.BinaryTypes.String},
	{Name: "mapping", Type: arrow.BinaryTypes.String},
	{Name: "method", Type: arrow.BinaryTypes.String},
	{Name: "rule", Type: arrow.BinaryTypes.String},
	{Name: "image", Type: arrow.BinaryTypes.String},
	{Name: "selection", Type: arrow.BinaryTypes.String},
	{Name: "plane", Type: arrow.BinaryTypes.String},
	{Name: "pixels", Type: arrow.PrimitiveTypes.Int64},
	{Name: "valid", Type: arrow.PrimitiveTypes.Uint64},
	{Name: "invalid", Type: arrow.PrimitiveTypes.Uint64},
	{Name: "min", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	{Name: "max", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	{Name: "mean", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	{Name: "sd", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	{Name: "median", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	{Name: "p01", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	{Name: "p99", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	{Name: "area", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	{Name: "area_sum", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	{Name: "area_mean", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
}, nil)

// regionsArrow is the figures of measured regions as an Arrow IPC stream;
// a figure that is not a number — no valid sample — is null.
func regionsArrow(ms []RegionMeasurement, regs []regionEntry) (stream []byte, rows int64, err error) {
	rb := array.NewRecordBuilder(memory.DefaultAllocator, regionsSchema)
	defer rb.Release()
	num := func(i int, v float64) {
		b := rb.Field(i).(*array.Float64Builder)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			b.AppendNull()
			return
		}
		b.Append(v)
	}
	for _, m := range ms {
		var r regions.Region
		if i := regionIndex(regs, m.Region); i >= 0 {
			r = regs[i].r
		}
		sel := ""
		if m.Selection != nil {
			sel = m.Selection.Text
		}
		for _, p := range m.Planes {
			for k, s := range []string{m.Region, m.Label, r.Shape.Kind().String(), r.Origin, r.Frame.String(), m.Mapping.How, m.Method, p.Rule, m.Image, sel, p.Plane} {
				rb.Field(k).(*array.StringBuilder).Append(s)
			}
			rb.Field(11).(*array.Int64Builder).Append(m.Pixels)
			rb.Field(12).(*array.Uint64Builder).Append(p.Valid)
			rb.Field(13).(*array.Uint64Builder).Append(p.Invalid)
			for k, v := range []float64{p.Min, p.Max, p.Mean, p.StdDev, p.Median, p.P01, p.P99} {
				num(14+k, v)
			}
			if p.Area != nil {
				num(21, p.Area.Area)
				num(22, p.Area.Sum)
				num(23, p.Area.Mean)
			} else {
				for k := 21; k <= 23; k++ {
					rb.Field(k).(*array.Float64Builder).AppendNull()
				}
			}
			rows++
		}
	}
	rec := rb.NewRecordBatch()
	defer rec.Release()
	stream, err = adhocdata.EncodeRecord(rec)
	return stream, rows, err
}

// regionsSql is the buffer the play window opens with on the figures.
const regionsSql = "-- The figures of the regions drawn on the image shown in imagescope, one row per\n" +
	"-- region and plane, computed from the samples at full resolution. rule says which\n" +
	"-- pixels a figure counts: centre, point or line; the area columns weight every pixel\n" +
	"-- by the fraction of it inside the shape, and are null where they were not asked for.\n" +
	"SELECT label, shape, plane, pixels, valid, invalid, mean, sd, median, area_mean\n" +
	"FROM keelson('regions')\nORDER BY label, plane\n"

// handoverRegions measures every region drawn on the image shown and
// hands the figures to play. It runs off the frame: the regions and the
// image are taken on the render goroutine and measured in the request's
// task, at most maxRegions of them.
func (inst *App) handoverRegions() error {
	if inst.bus == nil {
		return app.RefuseOperation("no bus on this host; the handover needs the app runtime")
	}
	if inst.pic == nil {
		return errNoImage
	}
	pic, stk, imageId, area := inst.pic, inst.stackShown(), inst.shownId, inst.measureArea
	regs := append([]regionEntry(nil), inst.regs...)
	var drawn []regionEntry
	for _, e := range regs {
		if placeRegion(e.r, pic, stk).Drawn {
			drawn = append(drawn, e)
		}
	}
	if len(drawn) == 0 {
		return app.RefuseOperation("no region is drawn on the image shown; add one with add_region, or show an image a region applies to")
	}
	sel := shownSelection(stk)
	inst.spawnRequest(regionsHandoverKind, regionsHandoverTitle, &inst.regionsOut, func() (string, error) {
		src := regions.StoreSource(pic)
		ms := make([]RegionMeasurement, 0, len(drawn))
		for _, e := range drawn {
			m, err := regions.Measure(inst.ioCtx, src, e.r, regions.MeasureOptions{Area: area})
			if err != nil {
				return "", eb.Build().Str("region", e.r.Label).Errorf("measure the region: %w", err)
			}
			rm := &regionMeasure{regionId: e.r.Id, m: m, area: area, imageId: imageId, selection: sel}
			ms = append(ms, regionMeasurementOf(rm, e.r.Label, false))
		}
		stream, rows, err := regionsArrow(ms, regs)
		if err != nil {
			return "", eh.Errorf("encode the figures: %w", err)
		}
		pub, err := inst.regionsPub.Publish(inst.ioCtx, inst.bus, stream)
		if err != nil {
			return "", eh.Errorf("publish the figures: %w", err)
		}
		cfg := playground.Launch{Sql: regionsSql, AutoRun: true, Endpoint: playground.EndpointIntrospection,
			Datasets: []string{pub.Alias}, DatasetNames: []string{regionsAlias}}
		if _, err = playground.Open(inst.ioCtx, inst.bus, cfg); err != nil {
			return "", eh.Errorf("open play: %w", err)
		}
		return fmt.Sprintf("opened in play — %d rows of %d regions as %s (rev %d)", rows, len(ms), pub.Handle, pub.Revision), nil
	})
	return nil
}

// ---- the profile of a line ----

// profileHeaders are the profile's columns: where each pixel is, and a
// value per plane.
func profileHeaders(rp *regionProfile) []string {
	return append([]string{"step", "col", "row", "length", "distance", "frame_distance"}, columnNames(rp.p.Labels)...)
}

// profileRecord is a profile as an Arrow record: one row per pixel, in
// order along the line, each plane's value or null for an invalid sample.
// The caller releases it.
func profileRecord(rp *regionProfile) arrow.RecordBatch {
	p := rp.p
	fields := []arrow.Field{
		{Name: "step", Type: arrow.PrimitiveTypes.Int64},
		{Name: "col", Type: arrow.PrimitiveTypes.Int64},
		{Name: "row", Type: arrow.PrimitiveTypes.Int64},
		{Name: "length", Type: arrow.PrimitiveTypes.Float64},
		{Name: "distance", Type: arrow.PrimitiveTypes.Float64},
		{Name: "frame_distance", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	}
	for _, n := range columnNames(p.Labels) {
		fields = append(fields, arrow.Field{Name: n, Type: arrow.PrimitiveTypes.Float64, Nullable: true})
	}
	rb := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
	defer rb.Release()
	rb.Reserve(p.Len())
	for i, st := range p.All() {
		rb.Field(0).(*array.Int64Builder).Append(int64(i))
		rb.Field(1).(*array.Int64Builder).Append(int64(st.Col))
		rb.Field(2).(*array.Int64Builder).Append(int64(st.Row))
		rb.Field(3).(*array.Float64Builder).Append(st.Length)
		rb.Field(4).(*array.Float64Builder).Append(st.Distance)
		if math.IsNaN(st.FrameDistance) {
			rb.Field(5).(*array.Float64Builder).AppendNull()
		} else {
			rb.Field(5).(*array.Float64Builder).Append(st.FrameDistance)
		}
		for k := range p.Labels {
			b := rb.Field(6 + k).(*array.Float64Builder)
			if _, v, ok := p.Sample(k, i); ok {
				b.Append(v)
			} else {
				b.AppendNull()
			}
		}
	}
	return rb.NewRecordBatch()
}

// profileSql is the buffer the play window opens with on a profile.
func profileSql(rp *regionProfile) string {
	names := columnNames(rp.p.Labels)
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = quoteIdent(n)
	}
	return "-- The profile of a line drawn in imagescope: the pixels it passes through, in\n" +
		"-- order, each once, with the length of the line inside it and the distance along\n" +
		"-- the line to the middle of that length. Nothing is interpolated; a null is an\n" +
		"-- invalid sample.\n" +
		"SELECT step, col, row, distance, " + strings.Join(q, ", ") + "\nFROM keelson('profile')\nORDER BY step\n"
}

// handoverProfile profiles a line over the image shown and hands it to
// play, refusing a line of more pixels than the pixel handover sends.
func (inst *App) handoverProfile(id string) error {
	if inst.bus == nil {
		return app.RefuseOperation("no bus on this host; the handover needs the app runtime")
	}
	if inst.pic == nil {
		return errNoImage
	}
	i := regionIndex(inst.regs, id)
	if i < 0 {
		return errNoRegion(id)
	}
	e, pic, stk, imageId := inst.regs[i], inst.pic, inst.stackShown(), inst.shownId
	if e.r.Shape.Kind() != regions.ShapeKindLine {
		return app.RefuseOperation("region " + strconv.Quote(e.r.Label) + " is a " + e.r.Shape.Kind().String() + "; a profile is of a line")
	}
	d := placeRegion(e.r, pic, stk)
	if !d.Drawn {
		return app.RefuseOperation("region " + strconv.Quote(e.r.Label) + " is not profiled on the image shown: it is " + d.Reason)
	}
	if n := profileBound(d.Pixels, pic); n > maxHandoverPixels {
		return app.RefuseOperation(fmt.Sprintf("the line crosses up to %d pixels and at most %d are sent; draw a shorter one", n, maxHandoverPixels))
	}
	sel := shownSelection(stk)
	inst.spawnRequest(profileHandoverKind, profileHandoverTitle, &inst.profileOut, func() (string, error) {
		p, err := regions.Profile(inst.ioCtx, regions.StoreSource(pic), e.r, regions.ProfileOptions{})
		if err != nil {
			return "", eh.Errorf("profile the line: %w", err)
		}
		rp := newRegionProfile(id, "", p, imageId, sel)
		rec := profileRecord(rp)
		defer rec.Release()
		stream, err := adhocdata.EncodeRecord(rec)
		if err != nil {
			return "", eh.Errorf("encode the profile: %w", err)
		}
		pub, err := inst.profilePub.Publish(inst.ioCtx, inst.bus, stream)
		if err != nil {
			return "", eh.Errorf("publish the profile: %w", err)
		}
		cfg := playground.Launch{Sql: profileSql(rp), AutoRun: true, Endpoint: playground.EndpointIntrospection,
			Datasets: []string{pub.Alias}, DatasetNames: []string{profileAlias}}
		if _, err = playground.Open(inst.ioCtx, inst.bus, cfg); err != nil {
			return "", eh.Errorf("open play: %w", err)
		}
		return fmt.Sprintf("opened in play — %d pixels of %s as %s (rev %d)", p.Len(), e.r.Label, pub.Handle, pub.Revision), nil
	})
	return nil
}

// profileBound bounds the pixels a line in pixels crosses inside an image:
// at most the columns plus the rows it spans, plus one.
func profileBound(line regions.Shape, pic *tiles.Store) int64 {
	a, b := line.Ends()
	w, h := float64(pic.Width()), float64(pic.Height())
	cols := math.Min(math.Abs(b.X-a.X), w) + 1
	rows := math.Min(math.Abs(b.Y-a.Y), h) + 1
	return int64(cols + rows)
}

// ---- documents ----

// saveDocument asks the person where to write a document and writes it
// there through the file broker; the broker records the write (D4).
func (inst *App) saveDocument(name string, body []byte, o *outcome) error {
	if inst.bus == nil {
		return app.RefuseOperation("no bus on this host; saving needs the app runtime and its file broker")
	}
	bus := inst.bus
	req, err := fsbroker.MarshalDialogRequest(fsbroker.DialogRequest{SuggestedName: name})
	if err != nil {
		return err
	}
	inst.spawnRequest(documentKind, documentTitle, o, func() (string, error) {
		raw, err := app.RequestWithin(inst.ioCtx, bus, fsbroker.DialogTimeout, fsbroker.SubjectDialogWrite, req)
		if err != nil {
			return "", eh.Errorf("the file picker did not answer: %w", err)
		}
		dr, err := fsbroker.UnmarshalDialogReply(raw)
		if err != nil {
			return "", eh.Errorf("the file picker's reply was not understood: %w", err)
		}
		if !dr.Granted {
			return "not saved: no file was chosen", nil
		}
		wraw, werr := app.RequestWithin(inst.ioCtx, bus, fsbroker.HandleOpTimeout, dr.HandleSubjectPrefix+opHandleWrite, body)
		_, _ = app.RequestWithin(inst.ioCtx, bus, fsbroker.HandleOpTimeout, dr.HandleSubjectPrefix+opHandleClose, nil)
		if werr != nil {
			return "", eh.Errorf("write the file: %w", werr)
		}
		if wr, perr := fsbroker.UnmarshalDialogReply(wraw); perr == nil && !wr.Granted {
			return "", eb.Build().Str("reason", wr.Reason).Errorf("the file broker refused the write")
		}
		return fmt.Sprintf("saved %d bytes as %s", len(body), dr.DisplayName), nil
	})
	return nil
}

// loadRegionsDocument asks the person for a regions document and reads it
// through the file broker; the frame that finds it read sets it as the
// person's set_regions_document.
func (inst *App) loadRegionsDocument() error {
	if inst.bus == nil {
		return app.RefuseOperation("no bus on this host; reading a file needs the app runtime and its file broker")
	}
	bus := inst.bus
	inst.spawnRequest(documentKind, documentTitle, &inst.documentOut, func() (string, error) {
		raw, err := app.RequestWithin(inst.ioCtx, bus, fsbroker.DialogTimeout, fsbroker.SubjectDialogRead, nil)
		if err != nil {
			return "", eh.Errorf("the file picker did not answer: %w", err)
		}
		dr, err := fsbroker.UnmarshalDialogReply(raw)
		if err != nil {
			return "", eh.Errorf("the file picker's reply was not understood: %w", err)
		}
		if !dr.Granted {
			return "not read: no file was chosen", nil
		}
		body, err := app.RequestWithin(inst.ioCtx, bus, fsbroker.HandleOpTimeout, dr.HandleSubjectPrefix+opHandleRead, nil)
		_, _ = app.RequestWithin(inst.ioCtx, bus, fsbroker.HandleOpTimeout, dr.HandleSubjectPrefix+opHandleClose, nil)
		if err != nil {
			return "", eh.Errorf("read the file: %w", err)
		}
		if len(body) > maxRegionsDocumentBytes {
			return "", eb.Build().Int("bytes", len(body)).Int("limit", maxRegionsDocumentBytes).Errorf("the document is over the size read")
		}
		inst.loadedDoc.Store(&loadedDocument{name: dr.DisplayName, body: body})
		return "read " + dr.DisplayName, nil
	})
	return nil
}

// loadedDocument is a regions document read off the frame, waiting for the
// frame to set it.
type loadedDocument struct {
	name string
	body []byte
}

// takeLoadedDocument sets a document read since the last frame as the
// person's set_regions_document.
func (inst *App) takeLoadedDocument() {
	d := inst.loadedDoc.Swap(nil)
	if d == nil {
		return
	}
	args := RegionsDocument{Document: string(d.body)}
	gestureOr(inst, opSetRegionsDocument, args, func() {
		if err := inst.setRegionsDocument(d.body, originPerson); err != nil {
			inst.regionNote = d.name + ": " + refusalText(err)
		}
	})
}

// Hover help of the region handovers.
const (
	tipRegionsHandover = "Measure every region drawn on the image shown and publish the figures as a dataset, one row per region and plane, then open play on it."
	tipProfileHandover = "Publish the profile of the line selected as a dataset, one row per pixel along the line, then open play on it. A line of more than about four million pixels is refused."
	tipSaveRegions     = "Write the regions document to a file you pick. " + tipRegionsDoc
	tipLoadRegions     = "Read a regions document from a file you pick; it replaces the regions of the window."
)

// renderRegionsHandover draws the buttons that take the regions out of the
// window, and what the last request came to.
func (inst *App) renderRegionsHandover() {
	for range c.Horizontal().KeepIter() {
		if len(inst.regs) > 0 && inst.button("regions-play", "Send figures to play", tipRegionsHandover, false) {
			gestureOr(inst, opSendRegionsToPlay, appops.None{}, func() { inst.regionNote = refusalText(inst.handoverRegions()) })
		}
		if i := regionIndex(inst.regs, inst.selRegion); i >= 0 && inst.regs[i].r.Shape.Kind() == regions.ShapeKindLine &&
			inst.button("profile-play", "Send profile to play", tipProfileHandover, false) {
			id := inst.selRegion
			gestureOr(inst, opSendProfileToPlay, RegionIdArgs{Id: id}, func() { inst.regionNote = refusalText(inst.handoverProfile(id)) })
		}
	}
	for range c.Horizontal().KeepIter() {
		if len(inst.regs) > 0 && inst.button("save-regions", "Save regions…", tipSaveRegions, false) {
			if doc, err := regionsDocument(inst.regs); err != nil {
				inst.regionNote = refusalText(err)
			} else {
				inst.regionNote = refusalText(inst.saveDocument("regions.json", doc, &inst.documentOut))
			}
		}
		if inst.button("load-regions", "Load regions…", tipLoadRegions, false) {
			inst.regionNote = refusalText(inst.loadRegionsDocument())
		}
	}
	for _, o := range []*outcome{&inst.regionsOut, &inst.profileOut, &inst.documentOut} {
		busy, note, errText := o.state()
		switch {
		case busy:
			c.Label("working…").Send()
			c.RequestRepaint()
		case errText != "":
			c.Label("failed: " + errText).Send()
		case note != "":
			c.Label(note).Send()
		}
	}
	if inst.bus == nil {
		c.Label("(handovers and files need the app runtime — no bus on this host)").Send()
	}
}
