package imageview

import (
	"context"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/levels"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// Options configures a viewer. The zero value is the default; the viewer
// reads its Opts every frame, so a change is an assignment.
type Options struct {
	// Smooth interpolates a magnified image. Off, each pixel is drawn as a
	// square, which is what looking at samples wants.
	Smooth bool
	// HideColorBar leaves the colour bar out of a single-plane view.
	HideColorBar bool
	// HideReadout leaves out the line under the image that names the pixel
	// under the pointer; Events still carry it.
	HideReadout bool
	// HideHistogram leaves out the strip under a single-plane view that
	// shows the plane's histogram and the two limits as handles to drag.
	HideHistogram bool
	// WheelScrolls makes the wheel scroll the image, up and down and — with
	// shift — sideways, as a document reader expects; the wheel then zooms
	// only with the zoom modifier, as a pinch does (ADR-0374 §SD5).
	WheelScrolls bool
	// Keys are keys the viewer takes while it has focus — a press on the
	// image gives it — and reports in [Events.Keys] without acting on them:
	// Page Up and Page Down for a document, the arrows for a host that
	// moves by them. Nothing is taken when it is empty.
	Keys keycodes.Mask
	// Overlay, when not nil, paints over the image every frame, after it and
	// before the marked region: hits, selections, annotations, hit regions
	// with labels. It draws with c.Paint* in canvas coordinates, which the
	// [Projector] converts to and from image pixels (§SD4).
	Overlay func(p Projector)
	// SampleCacheBytes bounds the samples kept of a source that is not
	// resident, so a recipe change maps them again without asking the source
	// (ADR-0374 §SD3, §SD9); 0 takes [DefaultSampleCacheBytes].
	SampleCacheBytes int64
	// Tasks is the keelson task API the viewer's background work runs
	// under — the level build and the histogram counts — so each is a task
	// the host lists, can cancel, and drains when the window closes. In a
	// keelson app it is task.ForApp(ctx); nil runs the work untracked.
	Tasks task.TaskApiI
}

// Kinds of the viewer's background tasks.
const (
	levelsKind     = "imageview.levels"
	levelsTitle    = "build an image's reduced levels"
	histogramKind  = "imageview.histogram"
	histogramTitle = "count a plane's histogram"
)

// JobI is one of the viewer's background jobs as the standard job row
// (widgets/bgjobrow) draws it: its state, progress and cancel.
type JobI interface {
	Snapshot() bgjob.Snapshot
	Cancel()
}

// Readout is one pixel of the image as its samples have it, each plane in
// the model's plane order: the stored sample, and the value it stands for
// through the plane's value transform — the same number unless the plane has
// one. Valid is false for an invalid sample of a float plane. Planes is 0
// when the pixel's samples are not at hand: a source's tile not loaded yet.
type Readout struct {
	Col, Row int
	Planes   int
	Stored   [4]float64
	Values   [4]float64
	Valid    [4]bool
}

// Events is what a frame of the viewer reports. The readouts describe the
// previous frame's pointer, like every canvas readback.
type Events struct {
	// Hover is the pixel under the pointer.
	Hover   Readout
	HoverOk bool
	// Clicked is the pixel a click landed on.
	Clicked   Readout
	ClickedOk bool
	// RegionChanged reports that a box drawn while selecting set a new
	// region this frame; Viewer.Region has it.
	RegionChanged bool
	// ViewChanged reports that the view moved or zoomed since the last frame,
	// whoever moved it.
	ViewChanged bool
	// Drag is the rectangle of pixels a drag is marking while Selecting, as
	// it stands this frame — before it is released and becomes the region;
	// DragOk says a drag is under way.
	Drag   image.Rectangle
	DragOk bool
	// LimitsChanged reports that a drag of a limit handle changed the
	// recipe this frame; Viewer.Recipe has the new one.
	LimitsChanged bool
	// Keys are the presses of [Options.Keys] taken since the last frame,
	// auto-repeats included, in order, with their modifiers. The slice is
	// the viewer's and is valid until the next Render.
	Keys []c.CapturedKey
	// Err is set when the viewer could not draw what it was asked to and
	// drew a message in its place.
	Err error
}

const (
	// minZoom and maxZoom bound the magnification at 1/256 and 64, as
	// powers of two.
	minZoom = -8
	maxZoom = 6
	// asStored is the plane index meaning "the image as stored".
	asStored = -1
	// uploadBudgetBytes is how much tile data one frame ships to the host
	// (ADR-0373 §SD6, §SD11): about what the image upload-cost trial found a
	// headless host to take within a 30 Hz frame.
	uploadBudgetBytes = 16 << 20

	colorBarWidth  = 72
	readoutHeight  = 22
	layoutGap      = 8
	barStripWidth  = 14
	barFontSize    = 11
	barSteps       = 96
	fallbackCanvas = 64
)

// Viewer shows one image. Make it with [New], give it an image with
// [Viewer.SetImage], render it every frame, and [Viewer.Close] it: it owns
// portolan's loader workers.
type Viewer struct {
	// Opts is read every frame.
	Opts Options

	ids *c.WidgetIdStack
	key string
	m   *portolan.Map
	src *tileSource

	// source is what is shown and info what it said about itself; img is
	// the image when the source is one in memory (SetImage), and the zero
	// image otherwise. gen files a source's cached tiles: it moves with
	// every new source and every Invalidate.
	source     SourceI
	info       SourceInfo
	gen        uint64
	img        imaging.Image
	pyramid    *levels.Pyramid
	plane      int
	recipe     display.Recipe
	palette    *display.Palette
	fitPending bool
	hover      Readout
	hoverOk    bool
	selecting  bool
	// pendingView and pendingShow hold a SetView or a Reveal asked for before
	// the view could take it.
	pendingView *pendingView
	pendingShow *image.Rectangle

	// The level build runs once per image and the histogram count once per
	// plane asked for, both as background jobs under Opts.Tasks. A stale
	// result — for an image no longer shown — is dropped by the job itself.
	levelJob bgjob.Runner[levels.Pyramid]
	histJob  bgjob.Keyed[*stats.Histogram]

	// region is the rectangle of pixels marked on the image, empty when
	// there is none.
	region image.Rectangle

	// hists holds the histograms counted for the image shown, by plane.
	hists             map[int]*stats.Histogram
	pendingPercentile bool
	percentLo         float64
	percentHi         float64
	drag              limitDrag
	// Scratch of the histogram strip, reused across frames.
	histBars                       []uint64
	histBarsFor                    histBarsKey
	histTop                        uint64
	histX0, histY0, histX1, histY1 []float32
	histCols                       []uint32
}

// New makes a viewer with no image. Every id it derives is scoped under
// scopeKey on ids; empty takes "imageview".
func New(ids *c.WidgetIdStack, scopeKey string, opts Options) *Viewer {
	if scopeKey == "" {
		scopeKey = "imageview"
	}
	inst := &Viewer{
		Opts: opts, ids: ids, key: scopeKey, src: &tileSource{}, plane: asStored,
		hists: make(map[int]*stats.Histogram, 4),
	}
	inst.m = portolan.New(ids, "view", portolan.Options{
		WheelPans:         opts.WheelScrolls,
		Source:            sourceFor(SourceInfo{}),
		Loader:            portolan.LoaderOptions{Pixels: inst.src},
		CRS:               portolan.Simple,
		NoInitialView:     true,
		NearestTiles:      !opts.Smooth,
		UploadBudgetBytes: uploadBudgetBytes,
		// Only arrows and Escape are the map's; nothing here needs them
		// taken from a host that has its own.
		NoKeyboard: true,
	})
	return inst
}

// sourceFor describes a source to portolan as a tile source: its tiles exist
// at the levels the source declares, inside its bounds, and the view may
// zoom well past both ends of them. Between two levels the finer one is
// drawn, so a screen pixel never has less than one cell behind it
// (ADR-0373 §SD6). A level is a zoom: level k is portolan's zoom k.
func sourceFor(info SourceInfo) portolan.TileSource {
	src := portolan.TileSource{
		TileSize:         portolan.Pt(tileSize, tileSize),
		MinZoom:          minZoom,
		MaxZoom:          maxZoom,
		HasMinNativeZoom: true,
		HasMaxNativeZoom: true,
		NoWrap:           true,
		Opacity:          1,
		KeepBuffer:       1,
		PreferFiner:      true,
	}
	if !info.IsZero() {
		src.Bounds = boundsFor(info.Width, info.Height)
		src.MinNativeZoom, src.MaxNativeZoom = -info.Reduced, info.Magnified
	}
	return src
}

// boundsFor is an extent of pixels on portolan's flat coordinate system:
// longitude is the column and latitude the negated row.
func boundsFor(width, height int) portolan.LatLngBounds {
	return portolan.LatLngBoundsOf(portolan.LL(-float64(height), 0), portolan.LL(0, float64(width)))
}

// Close stops the viewer's tile workers and cancels its background jobs.
// It does not wait for them: a job ends at its next check of its context
// and drops its result, and under a keelson task API the host drains it
// with the window. The viewer must not be rendered afterwards.
func (inst *Viewer) Close() {
	inst.dropWork()
	inst.histJob.Close()
	inst.m.Close()
}

// dropWork cancels the jobs for the image shown and forgets what they
// produced.
func (inst *Viewer) dropWork() {
	inst.levelJob.Invalidate()
	inst.histJob.Invalidate()
	clear(inst.hists)
	inst.pendingPercentile = false
	inst.drag = limitDrag{}
}

// startBuild reduces the image as a background job. Until it lands the
// tiles below full resolution are reduced one by one as they are asked for,
// to the same cells. Its cost is linear in the image's samples (ADR-0373
// §SD6).
func (inst *Viewer) startBuild(img imaging.Image) {
	if levels.CountFor(img.Width(), img.Height(), levels.Options{}) == 0 {
		return
	}
	inst.levelJob.StartReporting(inst.Opts.Tasks, bgjob.Spec{Kind: levelsKind, Title: levelsTitle},
		func(ctx context.Context, report bgjob.Reporter) (*levels.Pyramid, error) {
			return levels.Build(ctx, img, levels.Options{Progress: func(done, total int64) {
				report(uint64(done), uint64(total), "reducing")
			}})
		})
}

// adoptLevels takes over a finished build. The tiles on screen are already
// the cells the build made, so nothing is invalidated: later tiles are read
// from the levels instead of reduced on the spot.
func (inst *Viewer) adoptLevels() {
	pyramid, _, ok := inst.levelJob.TakeResult()
	if !ok || inst.img.IsZero() {
		return
	}
	inst.pyramid = pyramid
	inst.source = &imageSource{img: inst.img, pyramid: pyramid}
	_ = inst.publish(inst.plane, inst.recipe)
}

// LevelJob is the build of the image's reduced levels. Cancelled, the view
// goes on reducing the tiles it needs one by one.
func (inst *Viewer) LevelJob() JobI { return &inst.levelJob }

// HistogramJob is the count of the shown plane's histogram.
func (inst *Viewer) HistogramJob() JobI { return &inst.histJob }

// LevelsReady reports whether the image's reduced levels are built; an image
// small enough to need none is ready at once.
func (inst *Viewer) LevelsReady() bool {
	return inst.img.IsZero() || inst.pyramid != nil ||
		levels.CountFor(inst.img.Width(), inst.img.Height(), levels.Options{}) == 0
}

// Level is the level the current view is drawn from: 0 for the image's own
// samples, k when every cell drawn is the mean of a 2^k × 2^k block of them.
func (inst *Viewer) Level() int {
	if inst.info.IsZero() || !inst.m.View().Loaded() {
		return 0
	}
	k := -int(math.Ceil(inst.m.View().Zoom() - 1e-6))
	return min(max(k, 0), inst.info.Reduced)
}

// Image is the image being shown when it was set with [Viewer.SetImage];
// the zero image otherwise.
func (inst *Viewer) Image() imaging.Image { return inst.img }

// SourceInfo is what the source shown said about itself; the zero value
// when nothing is shown.
func (inst *Viewer) SourceInfo() SourceInfo { return inst.info }

// SetImage shows an image in memory, as stored and under its default recipe,
// and fits it to the view on the next frame; its reduced levels are built in
// the background. The zero image shows nothing.
func (inst *Viewer) SetImage(img imaging.Image) {
	inst.dropWork()
	inst.img, inst.pyramid = img, nil
	if img.IsZero() {
		inst.clearSource()
		return
	}
	inst.useSource(&imageSource{img: img}, false)
	if err := inst.publish(asStored, display.DefaultRecipe(img)); err != nil {
		// A default recipe always validates; an image this package cannot
		// map (a float colour image) is shown as its first plane instead.
		_ = inst.publish(0, display.DefaultPlaneRecipe(img.Plane(0)))
	}
	inst.showSource()
	inst.startBuild(img)
}

// SetSource shows a source — a renderer that draws a tile when asked, a
// store a decoder filled (ADR-0374 §SD1) — as stored and under a default
// recipe: the nominal range of its sample kind, or 0 to 1 for floats. With
// keepView the view stays where it is, as when the next page of a document
// replaces this one; otherwise it fits the source on the next frame. A
// source whose description the viewer cannot show is refused and what was
// shown stays.
//
// What needs every sample at hand — the histogram, percentile limits, the
// readout of a pixel whose tile is not loaded — is offered for an image in
// memory and not for a source.
func (inst *Viewer) SetSource(src SourceI, keepView bool) error {
	if src == nil {
		inst.dropWork()
		inst.img, inst.pyramid = imaging.Image{}, nil
		inst.clearSource()
		return nil
	}
	info := src.Info()
	if err := info.Validate(); err != nil {
		return err
	}
	inst.dropWork()
	inst.img, inst.pyramid = imaging.Image{}, nil
	inst.useSource(src, keepView)
	defer inst.showSource()
	if err := inst.publish(asStored, defaultRecipeFor(src, info, asStored)); err != nil {
		return inst.publish(0, defaultRecipeFor(src, info, 0))
	}
	return nil
}

// Invalidate says the source's content changed and its geometry did not:
// the samples kept of it are dropped, and the tiles in view are asked for
// again, coarser levels first, the old ones standing until they are
// replaced (ADR-0374 §SD3). For an image in memory there is nothing to
// drop; the tiles are mapped again.
func (inst *Viewer) Invalidate() {
	if inst.info.IsZero() {
		return
	}
	inst.gen++
	inst.src.cache.clear()
	_ = inst.publish(inst.plane, inst.recipe)
	inst.m.InvalidateTiles()
}

// useSource makes src the source shown and resets what belonged to the last.
// The map goes on showing the last until [Viewer.showSource], which is called
// once the new source is published to the workers.
func (inst *Viewer) useSource(src SourceI, keepView bool) {
	inst.source, inst.info = src, src.Info()
	inst.gen++
	inst.src.cache.clear()
	inst.region = image.Rectangle{}
	inst.plane = asStored
	inst.hoverOk = false
	inst.fitPending = !keepView || !inst.m.View().Loaded()
	inst.pendingView, inst.pendingShow = nil, nil
}

// showSource hands the map the source published last. It comes after the
// publish because the map asks for the new source's tiles at once, and a
// worker answers a tile from the source published when it runs: a source
// published after the request would leave the request answered by the last
// one, under the new source's revision.
func (inst *Viewer) showSource() {
	inst.m.SetSource(sourceFor(inst.info))
}

// clearSource shows nothing.
func (inst *Viewer) clearSource() {
	inst.source, inst.info = nil, SourceInfo{}
	inst.gen++
	inst.src.cache.clear()
	inst.region = image.Rectangle{}
	inst.plane = asStored
	inst.hoverOk, inst.fitPending = false, false
	inst.src.cur.Store(nil)
	inst.m.SetSource(sourceFor(SourceInfo{}))
}

// defaultRecipeFor is the recipe a source opens under, as stored or one
// plane, as an image in memory opens: a single plane over its data range
// when the source knows it, and otherwise — and for a colour image — the
// nominal range of its kind through the plane's transform, linear, which
// shows 8-bit colour as it was rendered; 0 to 1 for a float source with
// neither.
func defaultRecipeFor(src SourceI, info SourceInfo, plane int) display.Recipe {
	single := plane != asStored || !info.Model.IsColor()
	if rs, ok := src.(RangeSourceI); ok && single {
		i := max(plane, 0)
		if lo, hi, ok := rs.StoredRange(i); ok && hi > lo {
			t := info.Transform(i)
			a, b := t.Apply(lo), t.Apply(hi)
			return display.Recipe{Limits: display.Limits{Lo: min(a, b), Hi: max(a, b), Source: display.LimitsSourceData}}
		}
	}
	if l, ok := display.NominalLimits(info.Kind, info.Transform(max(plane, 0))); ok {
		return display.Recipe{Limits: l}
	}
	return display.Recipe{Limits: display.Limits{Lo: 0, Hi: 1, Source: display.LimitsSourceManual}}
}

// Recipe is the recipe the image is shown through.
func (inst *Viewer) Recipe() display.Recipe { return inst.recipe }

// SetRecipe shows the image through another recipe. One that cannot be
// compiled — limits that are not a range, a palette this package does not
// have — is refused and the view stays as it was.
//
// Limits whose Source is [display.LimitsSourcePercentile] are computed here
// from PercentLo and PercentHi; their Lo and Hi are not read. They need the
// histogram of the plane shown, which is counted off the frame: when it is
// ready the limits are set at once, and otherwise the rest of the recipe is
// applied at once and the limits by the Render that finds the histogram
// counted, [Viewer.LimitsPending] reporting true meanwhile. Percentile
// limits are refused for a colour composite, whose limits are not a property
// of one plane, and when the two percentiles fall on one value. A later
// recipe of any other kind replaces a percentile stretch still pending.
func (inst *Viewer) SetRecipe(recipe display.Recipe) error {
	if inst.info.IsZero() {
		return eb.Build().Errorf("no image to show a recipe on")
	}
	if recipe.Limits.Source != display.LimitsSourcePercentile {
		inst.pendingPercentile = false
		return inst.applyRecipe(recipe)
	}
	lo, hi := recipe.Limits.PercentLo, recipe.Limits.PercentHi
	if !inst.singlePlane() {
		return eb.Build().Errorf("percentile limits apply to a single plane; show one first")
	}
	if !inst.histogramAvailable() {
		return eb.Build().Errorf("percentile limits need every sample at hand, which a source does not offer; set the limits")
	}
	if !(lo >= 0 && lo < hi && hi <= 100) {
		return eb.Build().Float64("lo", lo).Float64("hi", hi).Errorf("percentiles must be an ascending pair within 0 and 100")
	}
	if h, ok := inst.Histogram(); ok {
		limits, ok := display.PercentileLimits(h, lo, hi)
		if !ok {
			return eb.Build().Float64("lo", lo).Float64("hi", hi).Errorf("the two percentiles fall on one value; there is no range between them")
		}
		inst.pendingPercentile = false
		recipe.Limits = limits
		return inst.applyRecipe(recipe)
	}
	// Until the histogram is counted the limits in force stay.
	recipe.Limits = inst.recipe.Limits
	if err := inst.applyRecipe(recipe); err != nil {
		return err
	}
	inst.pendingPercentile, inst.percentLo, inst.percentHi = true, lo, hi
	return nil
}

// LimitsPending reports whether percentile limits asked for through
// [Viewer.SetRecipe] wait for the histogram they are computed from; until
// they land, [Viewer.Recipe] carries the limits in force.
func (inst *Viewer) LimitsPending() bool { return inst.pendingPercentile }

// applyRecipe compiles a recipe with resolved limits and shows it.
func (inst *Viewer) applyRecipe(recipe display.Recipe) error {
	if recipe == inst.recipe {
		return nil
	}
	if err := inst.publish(inst.plane, recipe); err != nil {
		return err
	}
	inst.m.InvalidateTiles()
	return nil
}

// Plane reports what is shown: the index of the single plane, or ok false
// for the image as stored.
func (inst *Viewer) Plane() (index int, ok bool) {
	return inst.plane, inst.plane != asStored
}

// SetPlane shows one plane of the image by itself, through a colour map,
// under that plane's default recipe. Setting the plane already shown changes
// nothing, the recipe included.
func (inst *Viewer) SetPlane(index int) error {
	if index < 0 || index >= inst.info.Model.PlaneCount() {
		return eb.Build().Int("plane", index).Int("planes", inst.info.Model.PlaneCount()).Errorf("image has no such plane")
	}
	if index == inst.plane {
		return nil
	}
	recipe := defaultRecipeFor(inst.source, inst.info, index)
	if !inst.img.IsZero() {
		recipe = display.DefaultPlaneRecipe(inst.img.Plane(index))
	}
	recipe.Palette = inst.recipe.Palette
	if err := inst.publish(index, recipe); err != nil {
		return err
	}
	// A stretch asked for on another plane is not carried over to this one.
	inst.pendingPercentile, inst.drag = false, limitDrag{}
	inst.m.InvalidateTiles()
	return nil
}

// SetAsStored returns to the image as stored, under its default recipe.
// When it is already shown that way nothing changes.
func (inst *Viewer) SetAsStored() error {
	if inst.info.IsZero() || inst.plane == asStored {
		return nil
	}
	recipe := defaultRecipeFor(inst.source, inst.info, asStored)
	if !inst.img.IsZero() {
		recipe = display.DefaultRecipe(inst.img)
	}
	recipe.Palette = inst.recipe.Palette
	if err := inst.publish(asStored, recipe); err != nil {
		return err
	}
	inst.pendingPercentile, inst.drag = false, limitDrag{}
	inst.m.InvalidateTiles()
	return nil
}

// IsSinglePlane reports whether what is shown is one plane through a colour
// map — a plane picked out of a colour image, or a greyscale image — and not
// a colour composite. It is when there is a histogram, a colour bar and a
// choice of colour map.
func (inst *Viewer) IsSinglePlane() bool { return inst.singlePlane() }

// singlePlane is IsSinglePlane.
func (inst *Viewer) singlePlane() bool {
	return !inst.info.IsZero() && (inst.plane != asStored || !inst.info.Model.IsColor())
}

// publish compiles a recipe for a view of the image and, when that works,
// makes it what the workers map with. The viewer's own fields change only on
// success.
func (inst *Viewer) publish(plane int, recipe display.Recipe) error {
	palette, ok := paletteFor(recipe.Palette)
	if !ok {
		return eb.Build().Str("palette", recipe.Palette).Strs("known", PaletteNames()).Errorf("no colour map of that name")
	}
	limit := inst.Opts.SampleCacheBytes
	if limit <= 0 {
		limit = DefaultSampleCacheBytes
	}
	sh := &shown{source: inst.source, info: inst.info, gen: inst.gen, plane: plane, checker: checkerColours(), cacheBytes: limit}
	var err error
	if plane == asStored && inst.info.Model.IsColor() {
		sh.colours, err = display.CompileColor(recipe, inst.info.Kind, inst.info.Transform(0))
	} else {
		sh.planes, err = display.CompilePlane(recipe, inst.info.Kind, inst.info.Transform(max(plane, 0)), palette)
	}
	if err != nil {
		return err
	}
	inst.plane, inst.recipe, inst.palette = plane, recipe, palette
	inst.src.cur.Store(sh)
	return nil
}

// Magnification is how many screen pixels one image pixel takes: 1 at full
// resolution, 2 when every pixel is a 2 × 2 square.
func (inst *Viewer) Magnification() float64 {
	if !inst.m.View().Loaded() {
		return 1
	}
	return math.Pow(2, inst.m.View().Zoom())
}

// Fit shows the whole image on the next frame.
func (inst *Viewer) Fit() { inst.fitPending = !inst.info.IsZero() }

// ActualSize shows the image at one screen pixel per image pixel, about the
// centre of the current view.
func (inst *Viewer) ActualSize() {
	if inst.m.View().Loaded() {
		inst.m.View().SetZoom(0)
	}
}

// readoutAt reads the samples of the pixel a point of the flat coordinate
// system falls in. The pixel is always reported when it is inside the
// image; its samples are read from the image in memory, from a resident
// source, or from the cached tile that holds it, and Planes is 0 when none
// of those has them — the source is never asked from the frame.
func (inst *Viewer) readoutAt(ll portolan.LatLng) (r Readout, ok bool) {
	col, row := int(math.Floor(ll.Lng)), int(math.Floor(-ll.Lat))
	if inst.info.IsZero() || col < 0 || row < 0 || col >= inst.info.Width || row >= inst.info.Height {
		return r, false
	}
	r = Readout{Col: col, Row: row}
	samples, at := inst.img, image.Pt(col, row)
	if samples.IsZero() {
		sh := inst.src.cur.Load()
		if sh == nil {
			return r, true
		}
		var d TileData
		have := false
		if tt, ok := sh.source.(tryTileI); ok {
			d, have = tt.tryTile(0, col/tileSize, row/tileSize)
			at = d.Region.Min.Add(image.Pt(col%tileSize, row%tileSize))
		} else if inst.info.Resident {
			var err error
			d, err = sh.source.Tile(context.Background(), 0, col/tileSize, row/tileSize)
			have = err == nil
			at = d.Region.Min.Add(image.Pt(col%tileSize, row%tileSize))
		} else {
			d, at, have = inst.src.cachedSample(sh, col, row)
		}
		if !have {
			return r, true
		}
		samples = d.Samples
	}
	r.Planes = samples.PlaneCount()
	for i := range r.Planes {
		r.Stored[i], r.Valid[i] = samples.Plane(i).StoredAt(at.X, at.Y)
		r.Values[i] = inst.info.Transform(i).Apply(r.Stored[i])
	}
	return r, true
}

// View is what is shown: the image pixel at the centre of the view, in
// continuous pixel coordinates (pixel (col, row) covers [col, col+1) ×
// [row, row+1)), and the magnification, screen pixels per image pixel. ok
// is false before the view has been laid out.
func (inst *Viewer) View() (centerX, centerY, magnification float64, ok bool) {
	v := inst.m.View()
	if !v.Loaded() {
		return 0, 0, 0, false
	}
	ctr := v.Center()
	return ctr.Lng, -ctr.Lat, math.Pow(2, v.Zoom()), true
}

// SetView centres the view on a point in image pixels at a magnification,
// held within the zooms the viewer allows. Before the view is laid out it is
// applied on the next frame, after a pending fit, which it replaces.
func (inst *Viewer) SetView(centerX, centerY, magnification float64) {
	if !(magnification > 0) || math.IsNaN(centerX) || math.IsNaN(centerY) {
		return
	}
	inst.fitPending = false
	inst.pendingShow = nil
	inst.pendingView = &pendingView{x: centerX, y: centerY, mag: magnification}
	inst.applyPendingView()
}

// Reveal brings a rectangle of pixels into view: nothing moves when it is in
// view already; it is centred, at the magnification shown, when it fits;
// and the view zooms out to it when it does not. Before the view is laid out
// it is applied on the next frame.
func (inst *Viewer) Reveal(r image.Rectangle) {
	if inst.info.IsZero() {
		return
	}
	r = r.Intersect(image.Rect(0, 0, inst.info.Width, inst.info.Height))
	if r.Empty() {
		return
	}
	inst.pendingView = nil
	inst.pendingShow = &r
	inst.applyPendingView()
}

// pendingView is a view asked for before it could be applied.
type pendingView struct{ x, y, mag float64 }

// applyPendingView applies a SetView or a Reveal once the view can take it.
func (inst *Viewer) applyPendingView() {
	v := inst.m.View()
	// A view is set as soon as its size is known; a reveal waits for the fit
	// or the view it is revealed within.
	if inst.fitPending || v.Size().X <= 0 || v.Size().Y <= 0 {
		return
	}
	switch {
	case inst.pendingView != nil:
		pv := inst.pendingView
		inst.pendingView = nil
		zoom := min(max(math.Log2(pv.mag), minZoom), maxZoom)
		v.SetView(portolan.LL(-pv.y, pv.x), zoom)
	case inst.pendingShow != nil && v.Loaded():
		r := *inst.pendingShow
		inst.pendingShow = nil
		if seen := inst.visiblePixels(); r.In(seen) {
			return
		}
		mag := math.Pow(2, v.Zoom())
		size := v.Size()
		cx, cy := float64(r.Min.X+r.Max.X)/2, float64(r.Min.Y+r.Max.Y)/2
		if float64(r.Dx())*mag <= size.X && float64(r.Dy())*mag <= size.Y {
			v.PanTo(portolan.LL(-cy, cx))
			return
		}
		_ = v.FitBounds(boundsOfRect(r), portolan.FitOptions{})
	}
}

// boundsOfRect is a rectangle of pixels on portolan's flat coordinate system.
func boundsOfRect(r image.Rectangle) portolan.LatLngBounds {
	return portolan.LatLngBoundsOf(portolan.LL(-float64(r.Max.Y), float64(r.Min.X)), portolan.LL(-float64(r.Min.Y), float64(r.Max.X)))
}

// visiblePixels is the pixels any part of which is in view.
func (inst *Viewer) visiblePixels() image.Rectangle {
	v := inst.m.View()
	if !v.Loaded() {
		return image.Rectangle{}
	}
	return pixelsOf(v.Bounds(), inst.info)
}

// pixelsOf is the pixels a box on portolan's flat coordinate system touches,
// clipped to the image.
func pixelsOf(box portolan.LatLngBounds, info SourceInfo) image.Rectangle {
	sw, ne := box.SouthWest, box.NorthEast
	r := image.Rect(int(math.Floor(sw.Lng)), int(math.Floor(-ne.Lat)), int(math.Ceil(ne.Lng)), int(math.Ceil(-sw.Lat)))
	return r.Intersect(image.Rect(0, 0, info.Width, info.Height))
}

// Projector converts between image pixels and the canvas the image is drawn
// in, for an [Options.Overlay]: c.Paint* calls made with what it returns land
// on the pixels they name. Image pixels are continuous: pixel (col, row)
// covers [col, col+1) × [row, row+1).
type Projector struct {
	p    portolan.Projector
	info SourceInfo
}

// ToCanvas is the canvas point of an image point.
func (inst Projector) ToCanvas(x, y float64) (cx, cy float32) {
	pt := inst.p.ToCanvas(portolan.LL(-y, x))
	return float32(pt.X), float32(pt.Y)
}

// ToPixel is the image point of a canvas point.
func (inst Projector) ToPixel(cx, cy float32) (x, y float64) {
	ll := inst.p.ToLatLng(portolan.Pt(float64(cx), float64(cy)))
	return ll.Lng, -ll.Lat
}

// Visible is the pixels of the image any part of which is in view: what an
// overlay with many marks need consider.
func (inst Projector) Visible() image.Rectangle { return pixelsOf(inst.p.View().Bounds(), inst.info) }

// Magnification is screen pixels per image pixel this frame.
func (inst Projector) Magnification() float64 { return math.Pow(2, inst.p.View().Zoom()) }

// paintOver is what portolan's overlay draws: the host's overlay, then the
// marked region over it.
func (inst *Viewer) paintOver(p portolan.Projector) {
	if inst.Opts.Overlay != nil {
		inst.Opts.Overlay(Projector{p: p, info: inst.info})
	}
	inst.paintRegion(p)
}

// Region is the rectangle of pixels marked on the image: columns
// [Min.X, Max.X) and rows [Min.Y, Max.Y), per the image's pixel convention.
// ok is false when none is marked.
func (inst *Viewer) Region() (region image.Rectangle, ok bool) {
	return inst.region, !inst.region.Empty()
}

// SetRegion marks a rectangle of pixels, clipped to the image; an empty
// rectangle, or one that misses the image, clears the mark. The mark is drawn
// over the image and changes nothing about how the image is shown.
func (inst *Viewer) SetRegion(region image.Rectangle) {
	inst.region = image.Rectangle{}
	if !inst.info.IsZero() {
		if r := region.Intersect(image.Rect(0, 0, inst.info.Width, inst.info.Height)); !r.Empty() {
			inst.region = r
		}
	}
}

// Selecting reports whether a drag marks a region instead of panning.
func (inst *Viewer) Selecting() bool { return inst.selecting }

// SetSelecting makes a drag on the image mark a region — reported by
// [Events.RegionChanged] when the drag is released — instead of panning.
// The wheel still zooms. It holds until set otherwise.
func (inst *Viewer) SetSelecting(on bool) {
	inst.selecting = on
	inst.m.SetBoxSelect(on)
}

// pixelsIn is the pixels a box on the view touches: every pixel any part of
// which lies inside it, clipped to the image.
func (inst *Viewer) pixelsIn(box portolan.LatLngBounds) image.Rectangle {
	if inst.info.IsZero() {
		return image.Rectangle{}
	}
	return pixelsOf(box, inst.info)
}

// paintRegion outlines the marked region over the image, on the pixels'
// outer edges.
func (inst *Viewer) paintRegion(p portolan.Projector) {
	r := inst.region
	if r.Empty() {
		return
	}
	lats := []float64{-float64(r.Min.Y), -float64(r.Min.Y), -float64(r.Max.Y), -float64(r.Max.Y)}
	lngs := []float64{float64(r.Min.X), float64(r.Max.X), float64(r.Max.X), float64(r.Min.X)}
	fill := styletokens.AccentDefault
	fill.A = 0x20
	p.ConvexPolygon(lats, lngs, color.Hex(fill.AsHex()), color.Hex(styletokens.AccentDefault.AsHex()), styletokens.StrokeRegular)
}

// RenderFill is Render sized to the pane the viewer sits in, as reported one
// frame late; the fallback is used on the first frame and in a hidden tab.
func (inst *Viewer) RenderFill(fallbackW, fallbackH float32) Events {
	w, h := fallbackW, fallbackH
	for range c.IdScope(inst.ids.PrepareStr(inst.key)) {
		if pw, ph, ok := c.CapturePaneSize(inst.ids.ProbeSeq("pane")); ok && pw > 0 && ph > 0 {
			w, h = pw, ph
		}
	}
	return inst.Render(w, h)
}

// Render draws the viewer into w × h at the current layout position: the
// image, a colour bar beside it when one plane is shown, and the readout
// line beneath.
func (inst *Viewer) Render(w, h float32) (ev Events) {
	inst.adoptLevels()
	inst.adoptHistograms()
	for range c.IdScope(inst.ids.PrepareStr(inst.key)) {
		if inst.info.IsZero() {
			c.Label("No image").Send()
			return
		}
		barW, readoutH := float32(0), float32(0)
		if inst.singlePlane() && !inst.Opts.HideColorBar {
			barW = colorBarWidth + layoutGap
		}
		if !inst.Opts.HideReadout {
			readoutH = readoutHeight + layoutGap
		}
		histH := float32(0)
		// A histogram counts every sample: an image in memory has them at
		// hand, and a source may count them itself.
		if inst.singlePlane() && !inst.Opts.HideHistogram && inst.histogramAvailable() {
			histH = histogramHeight + layoutGap
			inst.wantHistogram()
		}
		cw, ch := max(w-barW, fallbackCanvas), max(h-readoutH-histH, fallbackCanvas)

		inst.m.SetNearestTiles(!inst.Opts.Smooth)
		inst.m.SetWheelPans(inst.Opts.WheelScrolls)
		inst.m.SetHostKeys(inst.Opts.Keys)
		// The view needs its size before it can fit, centre or show anything.
		inst.m.View().SetSize(portolan.Pt(float64(cw), float64(ch)))
		if inst.fitPending {
			if err := inst.m.View().FitBounds(boundsFor(inst.info.Width, inst.info.Height), portolan.FitOptions{}); err == nil {
				inst.fitPending = false
			}
		}
		inst.applyPendingView()
		var mev portolan.Events
		for range c.HorizontalTop().KeepIter() {
			mev = inst.m.Render(cw, ch, inst.paintOver)
			if barW > 0 {
				inst.paintColorBar(colorBarWidth, ch)
			}
		}
		inst.hover, inst.hoverOk = Readout{}, false
		if ll, ok := inst.m.Hover(); ok {
			inst.hover, inst.hoverOk = inst.readoutAt(ll)
		}
		ev.Hover, ev.HoverOk = inst.hover, inst.hoverOk
		if mev.ClickedOk {
			ev.Clicked, ev.ClickedOk = inst.readoutAt(mev.Clicked)
		}
		ev.ViewChanged = mev.ViewEvents.Any()
		ev.Keys = mev.Keys
		if mev.BoxingOk && inst.selecting {
			ev.Drag = inst.pixelsIn(mev.Boxing)
			ev.DragOk = !ev.Drag.Empty()
		}
		if mev.SelectedOk {
			if r := inst.pixelsIn(mev.Selected); !r.Empty() && r != inst.region {
				inst.region, ev.RegionChanged = r, true
			}
		}
		if histH > 0 {
			ev.LimitsChanged = inst.renderHistogram(cw)
		}
		if !inst.Opts.HideReadout {
			c.Label(inst.readoutText()).Selectable(false).Send()
		}
	}
	return
}

// readoutText is the line under the image: where the pointer is, what the
// samples there are, and how large a pixel is drawn.
func (inst *Viewer) readoutText() string {
	var b strings.Builder
	if inst.hoverOk {
		fmt.Fprintf(&b, "x %d  y %d   ", inst.hover.Col, inst.hover.Row)
		names := inst.info.Model.PlaneNames()
		for i := range inst.hover.Planes {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(names[i])
			b.WriteByte(' ')
			switch {
			case !inst.hover.Valid[i]:
				b.WriteString("invalid")
			case inst.info.Transform(i).IsIdentity():
				b.WriteString(formatSample(inst.hover.Stored[i], inst.info.Kind))
			default:
				// The value is what the limits, the colour bar and the
				// histogram are in; the stored sample is what the file holds.
				fmt.Fprintf(&b, "%s (stored %s)", formatValue(inst.hover.Values[i]), formatSample(inst.hover.Stored[i], inst.info.Kind))
			}
		}
	} else {
		b.WriteString("—")
	}
	fmt.Fprintf(&b, "   ×%s", formatMagnification(inst.Magnification()))
	if k := inst.Level(); k > 0 && !inst.info.Rendered {
		// What is drawn is not the samples: say so, and say what it is. A
		// rendered source draws each level anew, so there is nothing to say.
		fmt.Fprintf(&b, "   drawn from means of %d × %d", 1<<k, 1<<k)
	}
	return b.String()
}

func formatSample(v float64, kind imaging.SampleKindE) string {
	if kind.IsInteger() {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.6g", v)
}

// formatValue writes a value of the image for a label: five significant
// digits, which shows a 16-bit sample whole, and no exponent until it is
// shorter than the digits.
func formatValue(v float64) string {
	return strconv.FormatFloat(v, 'g', 5, 64)
}

func formatMagnification(m float64) string {
	if m >= 1 {
		return fmt.Sprintf("%.3g", m)
	}
	return fmt.Sprintf("1/%.3g", 1/m)
}

// paintColorBar draws the recipe as a strip in a canvas of its own: values
// run evenly from the lower limit at the bottom to the upper at the top, and
// each is drawn in the colour the image gives it, so the transfer function
// shows as the way the colours bunch along the strip.
func (inst *Viewer) paintColorBar(w, h float32) {
	lo, hi := inst.recipe.Limits.Lo, inst.recipe.Limits.Hi
	step := h / barSteps
	for i := range barSteps {
		value := hi - (float64(i)+0.5)/barSteps*(hi-lo)
		colour := inst.palette.Colors[int(math.Round(inst.recipe.Level(value)*255))]
		y := float32(i) * step
		c.PaintRectFilled(0, y, barStripWidth, y+step+0.5, 0, color.Hex(colour)).Send()
	}
	text, line := color.Hex(styletokens.NeutralTextSecondary.AsHex()), color.Hex(styletokens.NeutralBorderDefault.AsHex())
	c.PaintRectStroke(0, 0, barStripWidth, h, 0, line, styletokens.StrokeHair).Send()
	const ticks = 4
	for i := range ticks + 1 {
		f := float32(i) / ticks
		y := f * h
		// The first label hangs below its tick and the last sits above it,
		// so neither is cut by the canvas.
		anchor := uint8(1)
		switch i {
		case 0:
			anchor = 0
		case ticks:
			anchor = 2
		}
		c.PaintLine(barStripWidth, y, barStripWidth+3, y, line, styletokens.StrokeHair).Send()
		c.PaintText(barStripWidth+5, y, 0, anchor, formatValue(hi-float64(f)*(hi-lo)), barFontSize, text).Send()
	}
	c.PaintCanvas(inst.ids.PrepareStr("bar"), w, h).Send()
}
