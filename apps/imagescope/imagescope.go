package imagescope

import (
	"fmt"
	"image"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bgjobrow"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// Hover help. The texts are constants beside the render code so a scene can
// read them and the help book can quote them (keelson-app A5).
const (
	tipOpen = "Open a PNG or JPEG file. The system file picker chooses it: this app never sees your filesystem, only the one file you pick, which is read once."

	tipSample = "Open a synthetic image made from its own coordinates — nothing measured — to try the controls on."

	tipFit = "Show the whole image. Smaller than it is, the image is drawn from means of blocks of samples; the line under it says so."

	tipActual = "One sample per screen pixel."

	tipSelect = "While on, a drag on the image marks a rectangle of pixels instead of panning; its statistics appear below. The wheel still zooms."

	tipClearRegion = "Remove the marked region."

	tipAsStored = "A colour image as its samples say: red, green and blue through one set of limits, over a checkerboard where it is transparent. A greyscale image through its colour map."

	tipPlane = "This plane by itself, through a colour map, under limits of its own."

	tipLimitsData = "Limits at the smallest and largest value of the plane shown. One extreme sample is enough to set them."

	tipLimitsNominal = "Limits at the range the sample kind can hold: 0 to 255 or 0 to 65535."

	tipLimitsPercentile = "Limits at two percentiles of the plane shown, counted over the whole plane, so a few extreme samples cannot pull them wide. Samples outside the limits are drawn at the ends of the colour map; they are not changed."

	tipTransfer = "How a value between the limits becomes a level of the colour map. A stretch changes what is easy to see, not the samples: the readout and the statistics are the same under every one."

	tipInvert = "Run the colour map backwards."

	tipFlag = "Draw samples below the lower limit and above the upper limit in two marker colours instead of the ends of the colour map. At a reduced level the mark is on a mean of samples, so a few clipped samples inside a block can go unmarked."

	tipPalette = "The colour map for a single plane: a plain grey ramp, or one of the design system's perceptually uniform sequential and diverging scales."

	tipSmooth = "Interpolate between samples when the image is magnified. Off, each sample is a square, which is what it is."

	tipAdvanced = "Transfer function, colour map, clipping marks, smoothing and the sample images."

	tipHandover = "Publish the stored samples of the marked region — or of the whole image when none is marked — as a dataset, one row per pixel, and open play on it with a query that restates the statistics table. At most about four million pixels are sent."

	tipMethod = methodMeasure
)

// App is one image scope window.
type App struct {
	// ids is the per-instance WidgetIdStack. The host pre-pushes a
	// window-unique salt onto it before every Frame() call (ADR-0026 §SD9),
	// so every widget id the app derives is unique across all concurrently
	// open instances. Captured from ctx.Ids() in Mount; the app must NOT
	// Reset() it.
	ids *c.WidgetIdStack

	logger zerolog.Logger
	// bus carries the file picker, the publish and the open of play; nil on
	// hosts without a runtime (the tour stage), where a sample still opens.
	bus app.BusI
	// tasks runs the open, a measurement and the handover as keelson tasks
	// owned by this window; nil where there is no runtime.
	tasks task.TaskApiI
	scope *lifetime.Scope

	// viewer is the image widget. Its tile workers, its level build and its
	// histogram count are goroutines of the widget's own, stopped by Close
	// at Unmount.
	viewer *imageview.Viewer

	// open is the one-shot runner an image is read, decoded or made on (W2).
	// img, openErr and openNote are the render goroutine's copies, taken at
	// the top of Frame. openGen counts opens started and landedGen the last
	// that ended, so open_sample settles on the open it caused
	// (sampleCallGen is the generation at the call).
	open bgjob.Runner[opened]
	pic  *tiles.Store
	// arriving is a store an open in flight has started filling, handed
	// over so the window shows it as it fills; prev is what was shown
	// before it, put back if the open fails; openRecipe is the recipe the
	// store was first shown under, so the default can follow the data range
	// once it is known, unless the person changed it meanwhile.
	arriving      atomic.Pointer[tiles.Store]
	prev          *tiles.Store
	openRecipe    display.Recipe
	openErr       string
	openNote      string
	openGen       uint64
	landedGen     uint64
	sampleCallGen uint64

	// measure recomputes the marked region's statistics when the image or
	// the region changes (W2); measured, measureErr and measuring are this
	// frame's reading of it. statsRec is the record the table draws, built
	// once per measurement.
	measure    bgjob.Keyed[*measurement]
	measured   *measurement
	measureErr error
	measuring  bool
	statsFor   *measurement
	statsRec   arrow.RecordBatch
	statsCols  []recordtable.Column
	grid       recordtable.State
	sort       recordtable.SortState

	// The handover to play: one publisher, so a repeat replaces the dataset,
	// and the outcome of the last request, written off the frame.
	pub          *adhocdata.Publisher
	handoverMu   sync.Mutex
	handoverBusy bool
	handoverNote string
	handoverErr  string

	// Bound values of the check boxes keep one address across frames. A box
	// writes its bound value between frames, so invert and flag are compared
	// with the recipe before the boxes are drawn, and set from it only by
	// syncChecks, after something else changed the recipe.
	invert, flag, selecting, smooth bool
	// param is the transfer function's parameter as the slider holds it:
	// the recipe's, or the function's default when the recipe gives none.
	param float64
	// problem is why the last change to the view was refused; empty
	// otherwise.
	problem string
	// seedErr is why a seed variable was not applied.
	seedErr string

	// The window's phase, mirrored each frame, and the chip that draws it
	// (built in Mount; nil on the tour stage).
	phaseMachine *fsmview.Machine[phaseE]
	phaseChip    *fsmview.View[phaseE]

	// frameCtx is the current frame's context, through which the controls
	// route their commands as the person (ADR-0269 §SD8); nil outside a
	// frame.
	frameCtx app.FrameContextI
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		logger: log.Logger,
		pub:    adhocdata.NewWindowPublisher(datasetAlias),
	}
	inst.bindIds(c.NewWidgetIdStack())
	inst.phaseMachine = newPhaseMachine()
	return
}

// bindIds gives the app the id stack it derives widget ids from. The viewer
// keeps the stack it was made with, so a new stack means a new viewer,
// showing what the old one showed.
func (inst *App) bindIds(ids *c.WidgetIdStack) {
	if inst.ids == ids && inst.viewer != nil {
		return
	}
	inst.ids = ids
	old := inst.viewer
	inst.viewer = imageview.New(ids, "viewer", imageview.Options{Tasks: inst.tasks})
	if old != nil {
		if inst.pic != nil {
			_ = inst.viewer.SetSource(imageview.StoreSource(inst.pic), false)
			if _, single := old.Plane(); single {
				i, _ := old.Plane()
				_ = inst.viewer.SetPlane(i)
			}
			_ = inst.viewer.SetRecipe(old.Recipe())
			if r, ok := old.Region(); ok {
				inst.viewer.SetRegion(r)
			}
		}
		old.Close()
	}
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

// Mount wires the window and, when the sample seed names one, opens on it.
// A seed that names no sample, or a region seed that does not parse, is
// reported in the status row and the window opens empty or unmarked.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	// The task API first: the viewer made for the host's id stack runs its
	// level build and histogram counts as tasks of this window.
	inst.tasks = task.ForApp(ctx)
	inst.bindIds(ctx.Ids())
	inst.logger = ctx.Log()
	inst.bus = ctx.Bus()
	inst.scope = app.LifetimeOf(ctx)
	inst.measure.Configure(inst.tasks, measureKind, measureTitle)
	inst.phaseChip = fsmview.New(inst.ids, "image-phase", inst.phaseMachine, fsmview.Options[phaseE]{
		Title:      "Image phase",
		Tethered:   true,
		BadgeTone:  phaseTone,
		AutoAnchor: true,
	})
	seed := SampleSeed.Get()
	if seed == "" {
		return
	}
	if serr := checkSampleName(seed); serr != nil {
		inst.seedErr = sampleSeedName + ": " + refusalText(serr)
		inst.logger.Warn().Str("value", seed).Msg("imagescope: " + inst.seedErr)
		return
	}
	var region image.Rectangle
	if rs := RegionSeed.Get(); rs != "" {
		var rerr error
		if region, rerr = parseRegionSeed(rs); rerr != nil {
			inst.seedErr = regionSeedName + " is not column, row, width, height: " + refusalText(rerr)
			inst.logger.Warn().Str("value", rs).Msg("imagescope: " + inst.seedErr)
		}
	}
	inst.openSample(seed, region)
	return
}

// Unmount stops the jobs and the viewer's workers. An open in flight is
// dropped: the picker cannot be recalled, and what it returns is discarded.
func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	inst.open.Invalidate()
	inst.measure.Close()
	inst.viewer.Close()
	if inst.statsRec != nil {
		inst.statsRec.Release()
		inst.statsRec, inst.statsFor = nil, nil
	}
	return
}

// Frame renders the app body. The host has already pre-pushed a
// window-unique salt onto inst.ids (ADR-0026 §SD9); the app must not Reset()
// the stack or wrap the body in an instance salt of its own.
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.frameCtx = ctx
	inst.takeResults()
	inst.mirrorPhase()
	inst.renderBody()
	inst.frameCtx = nil
	return
}

// takeResults moves what the jobs left onto the instance once, at the top
// of the frame, so every pane below reads one stable value.
func (inst *App) takeResults() {
	inst.takeOpen()
	inst.demandMeasurement()
}

func (inst *App) renderBody() {
	for range c.PanelTopInside(inst.ids.PrepareStr("controls")).Resizable(false).KeepIter() {
		inst.renderControls()
	}
	if inst.pic != nil {
		for range c.PanelBottomInside(inst.ids.PrepareStr("measure")).Resizable(false).KeepIter() {
			inst.renderMeasurePane()
		}
	}
	for range c.PanelCentralInside().KeepIter() {
		if inst.pic == nil {
			inst.renderEmpty()
			continue
		}
		inst.viewer.RenderFill(640, 480)
	}
}

// renderStatusRow is the window's instrument panel (L7): the phase, the
// running open with its cancel, and the last outcome — a failure as a toned
// badge with its reason, anything else as a note.
func (inst *App) renderStatusRow() {
	for range c.Horizontal().KeepIter() {
		if inst.phaseChip != nil {
			inst.renderPhase()
		} else {
			c.Label(inst.phaseLine()).Send()
		}
		bgjobrow.Render(bgjobrow.Input{Ids: inst.ids, ScopeKey: "open-row", Job: &inst.open, Note: "opening…", Cancel: true, Inline: true})
		// The viewer's own work, as rows of the same kind (W4). A cancelled
		// level build leaves the view reducing the tiles it needs one by one.
		bgjobrow.Render(bgjobrow.Input{Ids: inst.ids, ScopeKey: "levels-row", Job: inst.viewer.LevelJob(), Note: "building levels…", Cancel: true, Inline: true})
		bgjobrow.Render(bgjobrow.Input{Ids: inst.ids, ScopeKey: "histogram-row", Job: inst.viewer.HistogramJob(), Note: "counting the histogram…", Inline: true})
		switch {
		case inst.openErr != "":
			badge.New(inst.ids.PrepareStr("open-err"), "open failed: "+inst.openErr).
				Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
		case inst.openNote != "" && !inst.open.Running():
			c.Label(inst.openNote).Send()
		}
	}
	if inst.seedErr != "" {
		c.Label(inst.seedErr).Send()
	}
	if inst.problem != "" {
		badge.New(inst.ids.PrepareStr("problem"), "refused: "+inst.problem).
			Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
	}
	// What the samples are, before any number is read off them (F3).
	if inst.pic == nil {
		return
	}
	for i, note := range sourceNotes(inst.pic.Provenance()) {
		badge.New(inst.ids.PrepareSeq(uint64(i)), note).Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
	}
}

// renderControls is the top panel: the status row, the controls a first
// look needs — open, navigate, which plane, where the limits come from —
// and one fold for the rest (L5).
func (inst *App) renderControls() {
	inst.renderStatusRow()
	have := inst.pic != nil
	for range c.Horizontal().KeepIter() {
		open := c.Button(inst.ids.PrepareStr("open"), c.Atoms().Text("Open…").Keep())
		if !have {
			// With nothing shown, opening is what the window is for (L2).
			open = open.Kind(c.ButtonKindPrimary)
		}
		for range c.HoverText(tipOpen).KeepIter() {
			if open.SendResp().HasPrimaryClicked() && !inst.open.Running() {
				inst.openFile()
			}
		}
		if !have {
			continue
		}
		if inst.button("fit", "Fit", tipFit, false) {
			inst.viewGesture(opSetZoom, SetZoomArgs{Mode: zoomFit}, func() error { return inst.setZoom(zoomFit) })
		}
		if inst.button("actual", "1:1", tipActual, false) {
			inst.viewGesture(opSetZoom, SetZoomArgs{Mode: zoomActual}, func() error { return inst.setZoom(zoomActual) })
		}
		for range c.HoverText(tipSelect).KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("select"), inst.selecting, "Select region").SendRespVal(&inst.selecting)
		}
		if inst.selecting != inst.viewer.Selecting() {
			inst.viewer.SetSelecting(inst.selecting)
		}
		if _, marked := inst.viewer.Region(); marked && inst.button("clear-region", "Clear region", tipClearRegion, false) {
			inst.viewGesture(opSetRegion, RegionArgs{}, func() error { return inst.setRegion(RegionArgs{}) })
		}
	}
	if !have {
		return
	}
	recipe := inst.viewer.Recipe()
	plane, single := inst.viewer.Plane()
	for range c.Horizontal().KeepIter() {
		c.Label("Show").Selectable(false).Send()
		for range c.IdScope(inst.ids.PrepareStr("plane")) {
			if inst.button("stored", planeAsStored, tipAsStored, !single) {
				inst.viewGesture(opSetPlane, SetPlaneArgs{Plane: planeAsStored}, func() error { return inst.setPlane(planeAsStored) })
			}
			for i, name := range inst.pic.Model().PlaneNames() {
				// A greyscale image's one plane is the image as stored.
				if !inst.pic.Model().IsColor() && !inst.pic.Model().HasAlpha() {
					break
				}
				if inst.button(name, name, tipPlane, single && plane == i) {
					inst.viewGesture(opSetPlane, SetPlaneArgs{Plane: name}, func() error { return inst.setPlane(name) })
				}
			}
		}
		c.Label("Limits").Selectable(false).Send()
		for range c.IdScope(inst.ids.PrepareStr("limits")) {
			src := recipe.Limits.Source
			if inst.button("data", "data", tipLimitsData, src == display.LimitsSourceData) {
				inst.recipeGesture(func(a *SetRecipeArgs) { a.Limits = limitsData })
			}
			if inst.button("nominal", "nominal", tipLimitsNominal, src == display.LimitsSourceNominal) {
				inst.recipeGesture(func(a *SetRecipeArgs) { a.Limits = limitsNominal })
			}
			if inst.viewer.IsSinglePlane() {
				for _, pct := range percentileChoices {
					current := src == display.LimitsSourcePercentile && recipe.Limits.PercentLo == pct[0] && recipe.Limits.PercentHi == pct[1]
					label := fmt.Sprintf("%g–%g %%", pct[0], pct[1])
					if inst.button(label, label, tipLimitsPercentile, current) {
						inst.recipeGesture(func(a *SetRecipeArgs) { a.Limits, a.PercentLo, a.PercentHi = limitsPercentile, pct[0], pct[1] })
					}
				}
			}
		}
	}
	for range c.HoverText(tipAdvanced).KeepIter() {
		for range c.CollapsingHeader(inst.ids.PrepareStr("advanced"),
			c.WidgetText().Text("Advanced — transfer function, colour map, clipping, smoothing, samples").Keep()).KeepIter() {
			inst.renderAdvancedControls(recipe)
		}
	}
}

// percentileChoices are the percentile stretches the window offers; any
// other pair is set through set_recipe.
var percentileChoices = [][2]float64{{1, 99}, {0.1, 99.9}}

// renderAdvancedControls is the fold's body: what a first look does not
// need.
func (inst *App) renderAdvancedControls(recipe display.Recipe) {
	for range c.Horizontal().KeepIter() {
		c.Label("Transfer").Selectable(false).Send()
		for range c.IdScope(inst.ids.PrepareStr("transfer")) {
			for _, tr := range display.AllTransfers {
				if inst.button(tr.String(), tr.String(), tipTransfer, recipe.Transfer == tr) {
					inst.recipeGesture(func(a *SetRecipeArgs) { a.Transfer, a.Param = tr.String(), 0 })
				}
			}
		}
		// A toggle since the last frame shows as a bound value that differs
		// from the recipe; a recipe the viewer refuses puts the box back.
		if inst.invert != recipe.Invert || inst.flag != recipe.FlagClipped {
			inst.recipeGesture(func(a *SetRecipeArgs) { a.Invert, a.FlagClipped = inst.invert, inst.flag })
			inst.syncChecks()
		}
		inst.renderParamSlider(recipe)
		for range c.HoverText(tipInvert).KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("invert"), inst.invert, "Invert").SendRespVal(&inst.invert)
		}
		for range c.HoverText(tipFlag).KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("flag"), inst.flag, "Flag clipped").SendRespVal(&inst.flag)
		}
		for range c.HoverText(tipSmooth).KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("smooth"), inst.smooth, "Smooth").SendRespVal(&inst.smooth)
		}
		inst.viewer.Opts.Smooth = inst.smooth
	}
	if inst.viewer.IsSinglePlane() {
		for range c.Horizontal().KeepIter() {
			c.Label("Colour map").Selectable(false).Send()
			for range c.IdScope(inst.ids.PrepareStr("palette")) {
				for _, name := range imageview.PaletteNames() {
					current := recipe.Palette == name || (recipe.Palette == "" && name == imageview.PaletteGray)
					if inst.button(name, name, tipPalette, current) {
						inst.recipeGesture(func(a *SetRecipeArgs) { a.Palette = name })
					}
				}
			}
		}
	}
	inst.renderSampleButtons()
}

// renderSampleButtons offers the synthetic images.
func (inst *App) renderSampleButtons() {
	for range c.Horizontal().KeepIter() {
		c.Label("Sample").Selectable(false).Send()
		for range c.IdScope(inst.ids.PrepareStr("sample")) {
			for _, name := range samples.Names() {
				if inst.button(name, name, tipSample, false) && !inst.open.Running() {
					gestureOr(inst, opOpenSample, OpenSampleArgs{Name: name}, func() { inst.openSample(name, image.Rectangle{}) })
				}
			}
		}
	}
}

// renderEmpty is the window with no image: what happened, and the two ways
// on (L4).
func (inst *App) renderEmpty() {
	switch {
	case inst.open.Running():
		c.Label("Opening an image — the status row above shows where it is.").Send()
		return
	case inst.bus == nil:
		c.Label("No image. There is no bus on this host, so a file cannot be opened here; a sample can.").Send()
	default:
		c.Label("No image. Open a PNG or JPEG file with Open…, or start from a sample.").Send()
	}
	inst.renderSampleButtons()
}

// renderMeasurePane is the bottom panel: the marked region's statistics, as
// a table of one row per plane, and the handover to play.
func (inst *App) renderMeasurePane() {
	rect, marked := inst.viewer.Region()
	switch {
	case !marked:
		c.Label("No region is marked. Turn on Select region and drag on the image; its statistics appear here.").Send()
	case inst.measuring:
		bgjobrow.Render(bgjobrow.Input{Ids: inst.ids, ScopeKey: "measure-row", Job: &inst.measure, Note: "measuring…", Cancel: true, Inline: true})
	case inst.measureErr != nil:
		badge.New(inst.ids.PrepareStr("measure-err"), "the region could not be measured: "+inst.measureErr.Error()).
			Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
	case inst.measured != nil:
		metric("Region: "+regionLine(rect), inst.measured.method())
		inst.renderStatsTable(inst.measured)
	}
	inst.renderHandover()
}

// Hover texts of the method-dependent columns: the method's statement,
// where the number is (F3).
const (
	tipSamples = "Valid samples counted in the region."
	tipInvalid = "Samples left out as invalid: NaN in a float plane. Every other figure is over the valid samples only."
	tipSd      = "Standard deviation over the region's samples as the whole population (divisor n). It describes the region and estimates nothing beyond it."
	tipMedian  = "50th percentile, nearest rank: the smallest sample with at least half of the samples at or below it."
	tipP01     = "1st percentile, nearest rank."
	tipP99     = "99th percentile, nearest rank."
)

// statsColumns are the table's columns. A figure is written with six
// significant digits: a mean of 16-bit samples has no more to give, and
// fifteen would claim it had.
func (inst *App) statsColumns() []recordtable.Column {
	if inst.statsCols != nil {
		return inst.statsCols
	}
	figure := func(col int) func(row int64) recordtable.Cell {
		return func(row int64) recordtable.Cell {
			rec := inst.statsRec
			if rec == nil || row < 0 || row >= rec.NumRows() {
				return recordtable.Cell{}
			}
			return recordtable.Cell{Text: formatFigure(rec.Column(col).(*array.Float64).Value(int(row)))}
		}
	}
	inst.statsCols = []recordtable.Column{
		{Arrow: 0, Header: "plane", NoSort: true},
		{Arrow: 1, Header: "samples", Hover: tipSamples, NoSort: true},
		{Arrow: 2, Header: "invalid", Hover: tipInvalid, NoSort: true},
		{Arrow: 3, Header: "min", Cell: figure(3), NoSort: true},
		{Arrow: 4, Header: "max", Cell: figure(4), NoSort: true},
		{Arrow: 5, Header: "mean", Cell: figure(5), NoSort: true},
		{Arrow: 6, Header: "sd", Hover: tipSd, Cell: figure(6), NoSort: true},
		{Arrow: 7, Header: "median", Hover: tipMedian, Cell: figure(7), NoSort: true},
		{Arrow: 8, Header: "1 %", Hover: tipP01, Cell: figure(8), NoSort: true},
		{Arrow: 9, Header: "99 %", Hover: tipP99, Cell: figure(9), NoSort: true},
	}
	return inst.statsCols
}

// formatFigure writes a statistic with six significant digits.
func formatFigure(v float64) string { return strconv.FormatFloat(v, 'g', 6, 64) }

// statsTableHeight is room for the header and the four planes an image can
// have.
const statsTableHeight = 150

// renderStatsTable draws a measurement as an Arrow record (D1). The record
// is rebuilt when the measurement is another one, never per frame.
func (inst *App) renderStatsTable(m *measurement) {
	if inst.statsFor != m {
		if inst.statsRec != nil {
			inst.statsRec.Release()
		}
		inst.statsRec, inst.statsFor = statsRecord(m), m
	}
	rec := inst.statsRec
	order := inst.sort.OrderFor(rec, rec.NumRows())
	res := recordtable.RenderGrid(recordtable.Input{
		Ids: inst.ids, ScopeKey: "stats", Record: rec, Columns: inst.statsColumns(),
		Sort: &inst.sort, Order: order, Selected: -1, State: &inst.grid,
		NoRowNumbers: true, MaxHeight: statsTableHeight,
	})
	inst.sort.Apply(res)
}

// renderHandover draws the Send to play button and what the last request
// came to. The request runs off the frame; its outcome shows on a later one.
func (inst *App) renderHandover() {
	busy, note, errText := inst.handoverState()
	for range c.Horizontal().KeepIter() {
		label := "Send to play"
		if busy {
			label = "Opening…"
		}
		for range c.HoverText(tipHandover).KeepIter() {
			if c.Button(inst.ids.PrepareStr("handover"), c.Atoms().Text(label).Keep()).
				SendResp().HasPrimaryClicked() && !busy && inst.bus != nil {
				gestureOr(inst, opSendToPlay, appops.None{}, func() {
					region, _ := inst.viewer.Region()
					if _, err := handoverRegion(inst.pic, region); err != nil {
						inst.problem = refusalText(err)
						return
					}
					inst.startHandover()
				})
			}
		}
		switch {
		case inst.bus == nil:
			metric("(needs the app runtime — no bus on this host)", tipHandover)
		case errText != "":
			metric("handover failed: "+errText, tipHandover)
		case note != "":
			metric(note, tipHandover)
		}
	}
	if busy {
		c.RequestRepaint()
	}
}

// button draws one labelled button under a hover text and reports a click.
// selected marks the choice in force.
func (inst *App) button(key, label, tip string, selected bool) (clicked bool) {
	for range c.HoverText(tip).KeepIter() {
		clicked = c.Button(inst.ids.PrepareStr(key), c.Atoms().Text(label).Keep()).Selected(selected).SendResp().HasPrimaryClicked()
	}
	return
}

// metric is a label with the statement that goes with it as its hover.
func metric(text string, tip string) {
	for range c.HoverText(tip).KeepIter() {
		c.Label(text).Send()
	}
}

// syncChecks sets the check boxes and the parameter slider to what the
// viewer's recipe says, after anything but them changed the recipe.
func (inst *App) syncChecks() {
	r := inst.viewer.Recipe()
	inst.invert, inst.flag = r.Invert, r.FlagClipped
	inst.param = effectiveParam(r)
}

// effectiveParam is the transfer parameter a recipe is mapped with.
func effectiveParam(r display.Recipe) float64 {
	if r.Param > 0 {
		return r.Param
	}
	return r.Transfer.DefaultParam()
}

// paramRanges is, per transfer function that takes a parameter, the
// slider's range and its tooltip. The ranges are wide on purpose: a float
// image can span many decades, and log's parameter is about ten to the
// number of decades it brings into view.
var paramRanges = map[display.TransferE]struct {
	lo, hi float64
	label  string
	tip    string
}{
	display.TransferLog:   {1, 1e9, "a", "log(a·x + 1) / log(a + 1) of the position x between the limits: a brings about log₁₀(a) decades above the lower limit into view. The default is 1000, three decades."},
	display.TransferAsinh: {1e-9, 1, "b", "asinh(x / b) / asinh(1 / b): nearly linear for x below b, logarithmic above, so it treats values near the lower limit and either side of zero alike. Smaller b reaches further down; the default is 0.1, about one decade."},
	display.TransferGamma: {0.05, 20, "γ", "x raised to γ: below 1 lifts the low values, above 1 presses them down. The default is 1, which is linear."},
}

// renderParamSlider draws the slider for the transfer function's parameter,
// for a function that takes one, and applies a value moved since the last
// frame (the check boxes' pattern).
func (inst *App) renderParamSlider(recipe display.Recipe) {
	pr, ok := paramRanges[recipe.Transfer]
	if !ok {
		return
	}
	if inst.param != effectiveParam(recipe) && inst.param > 0 {
		p := inst.param
		inst.recipeGesture(func(a *SetRecipeArgs) { a.Param = p })
		inst.syncChecks()
	}
	for range c.HoverText(pr.tip).KeepIter() {
		c.SliderF64(inst.ids.PrepareStr("param"), inst.param, pr.lo, pr.hi).Logarithmic(true).
			Text(pr.label).MaxDecimals(9).SendRespVal(&inst.param)
	}
}

// refusalText is an error's reason as the window shows it.
func refusalText(err error) (s string) {
	if err == nil {
		return ""
	}
	if r := (*app.OperationRefusal)(nil); asRefusal(err, &r) {
		return r.Reason
	}
	return strings.TrimSpace(err.Error())
}

// Compile-time check that the runners are what the standard row draws.
var (
	_ bgjobrow.JobI = (*bgjob.Runner[opened])(nil)
	_ bgjobrow.JobI = (*bgjob.Keyed[*measurement])(nil)
)
