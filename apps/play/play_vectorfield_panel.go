package play

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/sqlfield"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/flowoverlay"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/landoverlay"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timescrubber"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

// play_vectorfield_panel.go is the Vector field pane (ADR-0250): a gridded
// two-component field — wind, a current — that the buffer names in a
// `vector_field` CTE, drawn as drifting particles on a map.
//
// The pane never sees the field's rows. Its channel carries the CTE's SCHEMA,
// read under LIMIT 0, which is what the claim judges; the rows are reduced on
// the server, a window at a time, by the source the guest builds (§SD2).

const (
	// vf_t is the valid time of the step nearest the display time, and the
	// four bounds are the settled view in degrees (ADR-0250 §SD6). They are
	// written when they change and the control rests — never per animation
	// frame, which would re-run every node that reads them at the tick rate.
	signalVfT      SignalID = "vf_t"
	signalVfMinLat SignalID = "vf_min_lat"
	signalVfMaxLat SignalID = "vf_max_lat"
	signalVfMinLon SignalID = "vf_min_lon"
	signalVfMaxLon SignalID = "vf_max_lon"

	// vector_field_opts columns; every one optional (the ADR-0231 §SD5 form).
	vectorFieldOptNameCol     = "name"
	vectorFieldOptUnitCol     = "unit"
	vectorFieldOptSpeedMaxCol = "speed_max"

	vectorFieldSettle     = 250 * time.Millisecond
	vectorFieldTimeLayout = "2006-01-02 15:04:05.000"
	// vectorFieldSamplePx is the layer's default sample spacing; a summary is
	// decimated to what a window of the same view shows.
	vectorFieldSamplePx = 4
)

// vectorFieldClaim is the relation's shape, and the frame's signals for the
// one the pane reads back.
type vectorFieldClaim struct {
	shape sqlfield.Shape
	sig   SignalEnvI
}

// vectorFieldOptsClaim is the resolved column indices; -1 marks an absent
// column, which is every column's normal state.
type vectorFieldOptsClaim struct {
	nameCol, unitCol, speedMaxCol int
}

type vectorFieldOpts struct {
	name, unit string
	speedMax   float32
}

type vectorFieldPanel struct {
	driver *VectorFieldDriver
}

func (inst vectorFieldPanel) ID() PanelID { return "vectorfield" }

func (inst vectorFieldPanel) Channels() []ChannelSpec {
	return []ChannelSpec{
		{ID: chVectorField, Required: true, Label: "vector_field"},
		{ID: chVectorFieldOpts, Required: false, Label: "vector_field_opts"},
	}
}

func (inst vectorFieldPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	switch ch {
	case chVectorField:
		if schema == nil {
			reason = "Run a query with a `vector_field` CTE (columns `lat`, `lon`, `u`, `v`, and `t` for a time series) to see the flow on a map."
			return
		}
		shape, r := sqlfield.ShapeOf(schema)
		if r != "" {
			reason = "`vector_field`: " + r + "."
			return
		}
		claim = vectorFieldClaim{shape: shape, sig: sig}
	case chVectorFieldOpts:
		if schema == nil {
			reason = "no `vector_field_opts` CTE"
			return
		}
		oc := vectorFieldOptsClaim{nameCol: -1, unitCol: -1, speedMaxCol: -1}
		for i, f := range schema.Fields() {
			switch f.Name {
			case vectorFieldOptNameCol:
				oc.nameCol = i
			case vectorFieldOptUnitCol:
				oc.unitCol = i
			case vectorFieldOptSpeedMaxCol:
				oc.speedMaxCol = i
			}
		}
		claim = oc
	default:
		reason = "unknown channel"
	}
	return
}

func (inst vectorFieldPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	d := inst.driver
	if d == nil {
		return
	}
	field := filled[chVectorField]
	claim, _ := field.Claim.(vectorFieldClaim)
	var opts vectorFieldOpts
	if o, has := filled[chVectorFieldOpts]; has {
		if oc, isOpts := o.Claim.(vectorFieldOptsClaim); isOpts {
			opts = readVectorFieldOpts(o.Rec, oc)
		}
	}
	d.Render(claim, opts, emit)
}

// readVectorFieldOpts reads the settings row. A value a column cannot carry
// leaves the default in charge: a settings table must not be able to blank a
// drawing.
func readVectorFieldOpts(rec arrow.RecordBatch, oc vectorFieldOptsClaim) (o vectorFieldOpts) {
	if rec == nil || rec.NumRows() == 0 {
		return
	}
	if oc.nameCol >= 0 {
		o.name = formatCell(rec, oc.nameCol, 0)
	}
	if oc.unitCol >= 0 {
		o.unit = formatCell(rec, oc.unitCol, 0)
	}
	if oc.speedMaxCol >= 0 {
		if v, ok := numericCellValue(rec.Column(oc.speedMaxCol), 0); ok && v > 0 && !math.IsInf(v, 0) {
			o.speedMax = float32(v)
		}
	}
	return
}

// VectorFieldDriver is the pane's state: the lanes for the two CTEs, the
// guest, the map that hosts it and the controls.
type VectorFieldDriver struct {
	ids    *c.WidgetIdStack
	client *Client
	guest  *vectorFieldGuest
	// openQuery hands a buffer to a new playground window.
	openQuery func(sql string)

	probeLane *nodeLane
	optsLane  *nodeLane

	// Set by the tab body each frame, before the dispatch.
	rel          sqlfield.Relation
	relParams    map[string]string
	probeLoading bool
	probeErr     error

	pm    *portolan.Map
	land  *landoverlay.Layer
	atlas *worldmap.Atlas

	// scrubber is the time strip (ADR-0251); it owns the display time and
	// playback. pos and playing mirror it for the signal logic below.
	scrubber    *timescrubber.Scrubber
	steps       []timescrubber.Step
	barColors   *colormap.Config
	barColorMax float32
	settledView vectorfield.Request
	hasSettled  bool

	noTiles bool
	paused  bool
	playing bool
	density float64
	opacity float64
	pos     float64

	// fitted is the identity of the field the view was last framed for.
	fitted string

	// The two write-when-rested signals.
	lastViewHash  uint64
	viewStableAt  time.Time
	posChangedAt  time.Time
	lastPos       float64
	emittedStep   int
	emittedT      string
	seenT         string
	emittedBounds [4]float64

	now func() time.Time
}

func NewVectorFieldDriver(ids *c.WidgetIdStack, client *Client, openQuery func(sql string)) *VectorFieldDriver {
	d := &VectorFieldDriver{
		ids:         ids,
		client:      client,
		guest:       newVectorFieldGuest(client),
		openQuery:   openQuery,
		probeLane:   newNodeLane(clientExecutor{client: client, opts: newExecOptions("vectorfield-shape")}, memory.NewGoAllocator(), vectorFieldFetchTimeout),
		optsLane:    newNodeLane(clientExecutor{client: client, opts: newExecOptions("vectorfield-opts")}, memory.NewGoAllocator(), vectorFieldFetchTimeout),
		land:        &landoverlay.Layer{},
		scrubber:    timescrubber.New(ids, timescrubber.Options{ScopeKey: "vf-time", ValueName: "mean speed in view"}),
		noTiles:     !basemap.Configured(),
		density:     5,
		opacity:     0.9,
		emittedStep: -1,
		now:         time.Now,
	}
	if a, err := worldmap.LoadAtlas(); err == nil {
		d.atlas = a
	}
	return d
}

// forgetLanes is the Run's force. The two lanes always re-execute, which is
// cheap and picks up a changed settings row. The described field is dropped
// on a Run a person asked for, or when it had failed — but not on a Live
// auto-run: a Live query that follows this pane's own vf_t re-runs on every
// step, and describing the field again each time would restart the particles
// under the reader's eyes. A changed relation is a new identity either way.
func (inst *VectorFieldDriver) forgetLanes(auto bool) {
	if inst == nil {
		return
	}
	inst.probeLane.forget()
	inst.optsLane.forget()
	if !auto || inst.guest.err != nil {
		inst.guest.Forget()
	}
}

func (inst *VectorFieldDriver) close() {
	if inst == nil {
		return
	}
	inst.probeLane.close()
	inst.optsLane.close()
	inst.guest.Close()
	if inst.pm != nil {
		inst.pm.Close()
	}
}

// Render draws the controls and the map with the guest on it.
func (inst *VectorFieldDriver) Render(claim vectorFieldClaim, opts vectorFieldOpts, emit SignalEmitterI) {
	g := inst.guest
	g.Ensure(inst.rel, inst.relParams, claim.shape)
	g.Opts.Density = float32(inst.density)
	g.Opts.Opacity = float32(inst.opacity)
	g.Opts.Paused = inst.paused
	g.Opts.SpeedMax = opts.speedMax

	meta, has := g.Meta()
	if pos, moved := inst.followTimeSignal(claim.sig, has); moved {
		inst.scrubber.Transport.Seek(pos, len(meta.Steps))
	}
	if inst.hasSettled {
		g.EnsureSummary(inst.settledView)
	}

	inst.renderControls(meta, has, opts)
	inst.pos, inst.playing = inst.scrubber.Transport.Pos, inst.scrubber.Transport.Playing
	g.SetStepPosition(inst.pos)

	if inst.pm == nil {
		inst.pm = portolan.New(inst.ids, portolan.Options{
			Source:     basemap.PortolanSource(),
			Loader:     basemap.PortolanLoader(),
			Center:     portolan.LL(30, 0),
			Zoom:       2,
			NoTiles:    inst.noTiles,
			Background: 0x0e141bff,
		})
	}
	inst.pm.SetNoTiles(inst.noTiles)
	if has && inst.fitted != g.identity && g.identity != "" && inst.pm.View().Loaded() {
		// A regional field is framed once when it arrives; a global one
		// leaves the view where the reader put it.
		inst.fitted = g.identity
		if !meta.PeriodicLon {
			_ = inst.pm.View().FitBounds(
				portolan.LatLngBoundsOf(portolan.LL(meta.South, foldLon(meta.West)), portolan.LL(meta.North, foldLon(meta.West)+(meta.East-meta.West))),
				portolan.FitOptions{Padding: portolan.Point{X: 24, Y: 24}})
		}
	}

	inst.emitWhenRested(meta, has, emit)

	inst.pm.RenderFill(960, 560, func(p portolan.Projector) {
		if inst.noTiles {
			ls := landoverlay.DefaultStyle()
			ls.Land, ls.Border = color.Hex(0x262d36ff), color.Hex(0x4a5563ff)
			inst.land.Draw(p, inst.atlas, ls)
		}
		g.Draw(p)
	})
}

// foldLon brings a longitude into -180…180, the frame the map frames in.
func foldLon(lon float64) float64 {
	return lon - 360*math.Floor((lon+180)/360)
}

func (inst *VectorFieldDriver) renderControls(meta vectorfield.Meta, has bool, opts vectorFieldOpts) {
	if has && len(meta.Steps) > 1 {
		inst.renderTimeStrip(meta, opts)
	}
	for range c.Horizontal().KeepIter() {
		c.SliderF64(inst.ids.PrepareStr("vf-density"), inst.density, 1, 12).
			Text("particles per 1000 px²").SendRespVal(&inst.density)
		c.SliderF64(inst.ids.PrepareStr("vf-opacity"), inst.opacity, 0.1, 1.0).
			Text("opacity").SendRespVal(&inst.opacity)
		c.Checkbox(inst.ids.PrepareStr("vf-pause"), inst.paused, "pause").SendRespVal(&inst.paused)
		c.Checkbox(inst.ids.PrepareStr("vf-notiles"), inst.noTiles, "no basemap").SendRespVal(&inst.noTiles)
		if inst.guest.src != nil && inst.openQuery != nil {
			if c.Button(inst.ids.PrepareStr("vf-open-query"),
				c.Atoms().Text("Window query…").Keep()).SendResp().HasPrimaryClicked() {
				inst.openQuery(inst.servedBuffer())
			}
		}
	}
	c.Label(inst.statusLine(meta, has, opts)).Wrap().Send()
	diagWeak(inst.hoverLine(meta, has, opts))
}

// renderTimeStrip hands the scrubber this frame's steps: each at its valid
// time, with what the layer has of it and its speed inside the settled view.
// The bars take the particles' palette and range, so a colour on the strip is
// the colour that step's flow has on the map.
func (inst *VectorFieldDriver) renderTimeStrip(meta vectorfield.Meta, opts vectorFieldOpts) {
	g := inst.guest
	n := len(meta.Steps)
	if cap(inst.steps) < n {
		inst.steps = make([]timescrubber.Step, n)
	}
	inst.steps = inst.steps[:n]
	nan := float32(math.NaN())
	for i := range inst.steps {
		st := timescrubber.Step{At: meta.Steps[i].Valid, Value: nan, Peak: nan}
		switch g.StepState(i) {
		case flowoverlay.StepStateHeld:
			st.State = timescrubber.StepStateHeld
		case flowoverlay.StepStateLoading:
			st.State = timescrubber.StepStateLoading
		case flowoverlay.StepStateMissing:
			st.State = timescrubber.StepStateMissing
		}
		if i < len(g.summary) {
			st.Value, st.Peak = g.summary[i].Mean, g.summary[i].Max
		}
		inst.steps[i] = st
	}
	speedMax := opts.speedMax
	if !(speedMax > 0) {
		speedMax = meta.SpeedMax
	}
	if speedMax > 0 && (inst.barColors == nil || inst.barColorMax != speedMax) {
		cfg := colormap.NewConfig(flowoverlay.DefaultPalette, 0, float64(speedMax))
		inst.barColors, inst.barColorMax = cfg, speedMax
	}
	sc := inst.scrubber
	sc.Opts.ValueUnit = opts.unit
	sc.ValueColor = nil
	if inst.barColors != nil {
		sc.ValueColor = func(v float32) uint32 { return inst.barColors.At(float64(v)) | 0xff }
	}
	sc.RenderFillWidth(inst.steps, 960)
}

// statusLine says what is on screen, or why nothing is.
func (inst *VectorFieldDriver) statusLine(meta vectorfield.Meta, has bool, opts vectorFieldOpts) string {
	g := inst.guest
	switch {
	case inst.probeErr != nil:
		return "query error: " + inst.probeErr.Error()
	case g.err != nil:
		return "field error: " + g.err.Error()
	case !has && (g.Loading() || inst.probeLoading):
		return "describing the field…"
	case !has:
		return "no field"
	}
	stats := g.layer.Stats()
	name := opts.name
	if name == "" {
		name = "vector_field"
	}
	kind := "regional"
	if meta.PeriodicLon {
		kind = "global"
	}
	line := fmt.Sprintf("%s · %s grid %.4g° × %.4g° · %d steps · window %d × %d at level %d · %d requests",
		name, kind, meta.DLon, meta.DLat, len(meta.Steps), stats.WindowCols, stats.WindowRows, stats.WindowLevel, stats.Fetches)
	if served, _ := g.src.LastServed(sqlfield.PurposeWindow); served.Err == nil && served.Took > 0 {
		line += fmt.Sprintf(" · last %d rows in %s", served.Rows, served.Took.Round(time.Millisecond))
	}
	if g.Loading() {
		line += " · describing the next field…"
	}
	if g.summaryErr != nil {
		line += " · the per-step summary failed: " + g.summaryErr.Error()
	}
	if stats.LastError != nil {
		line += " · last request failed: " + stats.LastError.Error()
	}
	return line
}

// hoverLine reads the field under the pointer. Speed is the scalar mean and
// direction the vector mean's; at a low zoom the two differ wherever
// directions disagree inside a sample, and the line says which is which.
func (inst *VectorFieldDriver) hoverLine(meta vectorfield.Meta, has bool, opts vectorFieldOpts) string {
	if !has || inst.pm == nil {
		return " "
	}
	ll, ok := inst.pm.Hover()
	if !ok {
		return "hover the map to read the field · the animation shows direction and relative speed, not transport"
	}
	u, v, speed, found := inst.guest.layer.At(ll)
	if !found {
		return fmt.Sprintf("at %.2f, %.2f: no data", ll.Lat, ll.Lng)
	}
	unit := opts.unit
	if unit == "" {
		unit = meta.Unit
	}
	if unit != "" {
		unit = " " + unit
	}
	from := math.Mod(math.Atan2(-float64(u), -float64(v))*180/math.Pi+360, 360)
	return fmt.Sprintf("at %.2f, %.2f: %.3g%s (scalar mean) from %03.0f° (vector mean %.3g)",
		ll.Lat, ll.Lng, speed, unit, from, math.Hypot(float64(u), float64(v)))
}

// followTimeSignal moves the display time when someone else wrote vf_t — a
// SET, the Signals editor, a history restore. The pane's own write comes back
// one frame later and is told apart by its value.
func (inst *VectorFieldDriver) followTimeSignal(sig SignalEnvI, has bool) (pos float64, moved bool) {
	if sig == nil || !has {
		return
	}
	p, ok := sig.Get(signalVfT)
	if !ok || p.Raw == inst.seenT {
		return
	}
	inst.seenT = p.Raw
	if p.Raw == inst.emittedT {
		return
	}
	t, err := time.ParseInLocation(vectorFieldTimeLayout, p.Raw, time.UTC)
	if err != nil {
		t, err = time.ParseInLocation("2006-01-02 15:04:05", p.Raw, time.UTC)
	}
	if err != nil {
		return
	}
	return inst.guest.SetTime(t), true
}

// emitWhenRested publishes the nearest step's time and the view, each once it
// has stopped moving (ADR-0250 §SD6). Playback counts as rest for the time: a
// step lasts seconds, and a reader following the clock wants it to tick.
func (inst *VectorFieldDriver) emitWhenRested(meta vectorfield.Meta, has bool, emit SignalEmitterI) {
	now := inst.now()
	if has && len(meta.Steps) > 0 {
		if inst.pos != inst.lastPos && !inst.playing {
			inst.posChangedAt = now
		}
		inst.lastPos = inst.pos
		step := min(max(int(math.Round(inst.pos)), 0), len(meta.Steps)-1)
		raw := meta.Steps[step].Valid.UTC().Format(vectorFieldTimeLayout)
		if (step != inst.emittedStep || raw != inst.emittedT) && now.Sub(inst.posChangedAt) >= vectorFieldSettle {
			inst.emittedStep, inst.emittedT = step, raw
			emit.Emit(signalVfT, raw)
		}
	}
	if inst.pm == nil {
		return
	}
	v := inst.pm.View()
	if !v.Loaded() {
		return
	}
	if vh := inst.pm.ViewHash(); vh != inst.lastViewHash {
		inst.lastViewHash = vh
		inst.viewStableAt = now
	}
	if inst.viewStableAt.IsZero() || now.Sub(inst.viewStableAt) < vectorFieldSettle {
		return
	}
	b := v.Bounds()
	if west, east := b.GetWest(), b.GetEast(); west < east && b.GetSouth() < b.GetNorth() {
		if east-west > 360 {
			mid := (west + east) / 2
			west, east = mid-180, mid+180
		}
		size := v.Size()
		inst.settledView = vectorfield.Request{
			West: west, East: east, South: b.GetSouth(), North: b.GetNorth(),
			MaxCols: max(int(size.X/vectorFieldSamplePx), 2), MaxRows: max(int(size.Y/vectorFieldSamplePx), 2),
		}
		inst.hasSettled = true
	}
	round := func(x float64) float64 { return math.Round(x*1e6) / 1e6 }
	bounds := [4]float64{round(b.GetSouth()), round(b.GetNorth()), round(b.GetWest()), round(b.GetEast())}
	if bounds == inst.emittedBounds {
		return
	}
	inst.emittedBounds = bounds
	emit.Emit(signalVfMinLat, bounds[0])
	emit.Emit(signalVfMaxLat, bounds[1])
	emit.Emit(signalVfMinLon, bounds[2])
	emit.Emit(signalVfMaxLon, bounds[3])
}

// servedBuffer is the source's last statement as a buffer that runs on its
// own: every parameter it was sent with as a SET, then the statement. It is
// what makes a window's cost a paste away from EXPLAIN.
func (inst *VectorFieldDriver) servedBuffer() string {
	served, _ := inst.guest.src.LastServed(sqlfield.PurposeWindow)
	params := make(map[string]string, len(served.Params)+len(inst.relParams))
	for k, v := range inst.relParams {
		params[k] = v
	}
	for k, v := range served.Params {
		params["param_"+k] = v
	}
	names := make([]string, 0, len(params))
	for k := range params {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("-- One window of the Vector field pane, as it was sent (ADR-0250).\n")
	for _, k := range names {
		b.WriteString("SET " + k + " = " + quoteCHString(params[k]) + ";\n")
	}
	b.WriteString(served.Statement)
	return b.String()
}

// renderVectorFieldTab is the Vector field dock-tab body. Like the Network
// tab it ignores the active result: its inputs are the `vector_field` and
// `vector_field_opts` CTEs by name, and of the first only the schema.
func (inst *PlayApp) renderVectorFieldTab() {
	d := inst.vectorFieldDriver
	if d == nil {
		return
	}
	var fieldRec, optsRec arrow.RecordBatch
	var fieldSchema, optsSchema *arrow.Schema
	d.probeLoading, d.probeErr = false, nil
	if node, ok := findSplitNode(inst.currentSplit, vectorFieldNodeID); ok {
		if rel, isRel := vectorFieldRelation(inst.currentSplit, vectorFieldNodeID); isRel {
			params := resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig)
			v := d.probeLane.demand(compiledNode{SQL: sqlfield.ProbeStatement(rel), NodeID: vectorFieldNodeID, Params: params})
			d.rel, d.relParams = rel, params
			d.probeLoading, d.probeErr = v.loading, v.err
			fieldRec, fieldSchema = v.rec, v.schema
		}
	}
	if node, ok := findSplitNode(inst.currentSplit, vectorFieldOptsNodeID); ok {
		v := d.optsLane.demand(compiledNode{
			SQL:    fuseNode(inst.currentSplit, vectorFieldOptsNodeID),
			NodeID: vectorFieldOptsNodeID,
			Params: resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig),
		})
		optsRec, optsSchema = v.rec, v.schema
	}
	defer func() {
		if fieldRec != nil {
			fieldRec.Release()
		}
		if optsRec != nil {
			optsRec.Release()
		}
	}()

	inputs := map[ChannelID]channelInput{
		chVectorField: {node: vectorFieldNodeID, rec: fieldRec, schema: fieldSchema, sig: inst.frameSig},
	}
	if optsRec != nil || optsSchema != nil {
		inputs[chVectorFieldOpts] = channelInput{node: vectorFieldOptsNodeID, rec: optsRec, schema: optsSchema, sig: inst.frameSig}
	}
	reject := dispatchPanel(vectorFieldPanel{driver: d}, inputs, inst.sigEmit)
	if reject == "" {
		return
	}
	switch {
	case d.probeErr != nil:
		diagWeak("`vector_field`: " + d.probeErr.Error())
	case d.probeLoading:
		diagWeak("reading the field's shape…")
	default:
		diagWeak(reject)
	}
}

// openVectorFieldQuery opens a served window statement in a playground of its
// own; the round-trip blocks, so it leaves the frame loop.
func (inst *PlayApp) openVectorFieldQuery(sql string) {
	go inst.requestOpenPlayground(launchcfg.PlayLaunch{Sql: sql})
}
