package imageview

import (
	"context"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/levels"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
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
}

// Readout is one pixel of the image as its samples have it: the stored
// sample of each plane, in the model's plane order. Valid is false for an
// invalid sample of a float plane.
type Readout struct {
	Col, Row int
	Planes   int
	Stored   [4]float64
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
	// LimitsChanged reports that a drag of a limit handle changed the
	// recipe this frame; Viewer.Recipe has the new one.
	LimitsChanged bool
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

	img        imaging.Image
	pyramid    *levels.Pyramid
	plane      int
	recipe     display.Recipe
	palette    *display.Palette
	fitPending bool
	hover      Readout
	hoverOk    bool
	selecting  bool

	// The level build runs on a goroutine of the viewer's own: buildGen
	// names the image it is for, built is where it leaves its result for
	// Render to pick up, and Close waits for it.
	buildGen    uint64
	buildCancel context.CancelFunc
	built       atomic.Pointer[buildResult]
	building    sync.WaitGroup

	// region is the rectangle of pixels marked on the image, empty when
	// there is none.
	region image.Rectangle

	// Histograms are counted per plane, on the same goroutines' terms.
	hists             map[int]*stats.Histogram
	histWanted        map[int]bool
	histDone          chan histResult
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

type buildResult struct {
	gen     uint64
	pyramid *levels.Pyramid
}

// New makes a viewer with no image. Every id it derives is scoped under
// scopeKey on ids; empty takes "imageview".
func New(ids *c.WidgetIdStack, scopeKey string, opts Options) *Viewer {
	if scopeKey == "" {
		scopeKey = "imageview"
	}
	inst := &Viewer{
		Opts: opts, ids: ids, key: scopeKey, src: &tileSource{}, plane: asStored,
		hists: make(map[int]*stats.Histogram, 4), histWanted: make(map[int]bool, 4),
		// Room for every plane of an image, so a counting goroutine never
		// waits on a viewer that has stopped rendering.
		histDone: make(chan histResult, 8),
	}
	inst.m = portolan.New(ids, "view", portolan.Options{
		Source:            sourceFor(imaging.Image{}),
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

// sourceFor describes an image to portolan as a tile source: its tiles exist
// from full resolution down through its reduced levels, inside the image's
// bounds, and the view may zoom well past both ends of that. Between two
// levels the finer one is drawn, so a screen pixel never has less than one
// sample behind it (ADR-0373 §SD6).
func sourceFor(img imaging.Image) portolan.TileSource {
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
	if !img.IsZero() {
		src.Bounds = boundsOf(img)
		src.MinNativeZoom = -levels.CountFor(img.Width(), img.Height(), levels.Options{})
	}
	return src
}

// boundsOf is the image's extent on portolan's flat coordinate system:
// longitude is the column and latitude the negated row.
func boundsOf(img imaging.Image) portolan.LatLngBounds {
	return portolan.LatLngBoundsOf(portolan.LL(-float64(img.Height()), 0), portolan.LL(0, float64(img.Width())))
}

// Close stops the viewer's workers and waits for a level build under way.
// The viewer must not be rendered afterwards.
func (inst *Viewer) Close() {
	inst.stopBuild()
	inst.m.Close()
}

// stopBuild cancels the level build, if one is running, and waits for it.
func (inst *Viewer) stopBuild() {
	if inst.buildCancel != nil {
		inst.buildCancel()
		inst.buildCancel = nil
	}
	inst.building.Wait()
	inst.built.Store(nil)
	for len(inst.histDone) > 0 {
		<-inst.histDone
	}
	clear(inst.hists)
	clear(inst.histWanted)
	inst.pendingPercentile = false
	inst.drag = limitDrag{}
}

// startBuild reduces the image on a goroutine. Until it is done the tiles
// below full resolution are reduced one by one as they are asked for, to the
// same cells.
func (inst *Viewer) startBuild(img imaging.Image) {
	inst.buildGen++
	if levels.CountFor(img.Width(), img.Height(), levels.Options{}) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	inst.buildCancel = cancel
	gen := inst.buildGen
	inst.building.Go(func() {
		if pyramid, err := levels.Build(ctx, img, levels.Options{}); err == nil {
			inst.built.Store(&buildResult{gen: gen, pyramid: pyramid})
		}
	})
}

// adoptLevels takes over a finished build. The tiles on screen are already
// the cells the build made, so nothing is invalidated: later tiles are read
// from the levels instead of reduced on the spot.
func (inst *Viewer) adoptLevels() {
	result := inst.built.Swap(nil)
	if result == nil || result.gen != inst.buildGen || inst.img.IsZero() {
		return
	}
	inst.pyramid = result.pyramid
	_ = inst.publish(inst.plane, inst.recipe)
}

// LevelsReady reports whether the image's reduced levels are built; an image
// small enough to need none is ready at once.
func (inst *Viewer) LevelsReady() bool {
	return inst.img.IsZero() || inst.pyramid != nil ||
		levels.CountFor(inst.img.Width(), inst.img.Height(), levels.Options{}) == 0
}

// Level is the level the current view is drawn from: 0 for the image's own
// samples, k when every cell drawn is the mean of a 2^k × 2^k block of them.
func (inst *Viewer) Level() int {
	if inst.img.IsZero() || !inst.m.View().Loaded() {
		return 0
	}
	k := -int(math.Ceil(inst.m.View().Zoom() - 1e-6))
	return min(max(k, 0), levels.CountFor(inst.img.Width(), inst.img.Height(), levels.Options{}))
}

// Image is the image being shown; the zero image when there is none.
func (inst *Viewer) Image() imaging.Image { return inst.img }

// SetImage shows another image, as stored and under its default recipe, and
// fits it to the view on the next frame. The zero image shows nothing.
func (inst *Viewer) SetImage(img imaging.Image) {
	inst.stopBuild()
	inst.img = img
	inst.pyramid = nil
	inst.region = image.Rectangle{}
	inst.plane = asStored
	inst.hoverOk = false
	inst.fitPending = !img.IsZero()
	if img.IsZero() {
		inst.src.cur.Store(nil)
	} else if err := inst.publish(asStored, display.DefaultRecipe(img)); err != nil {
		// A default recipe always validates; an image this package cannot
		// map (a float colour image) is shown as its first plane instead.
		_ = inst.publish(0, display.DefaultPlaneRecipe(img.Plane(0)))
	}
	inst.m.SetSource(sourceFor(img))
	if !img.IsZero() {
		inst.startBuild(img)
	}
}

// Recipe is the recipe the image is shown through.
func (inst *Viewer) Recipe() display.Recipe { return inst.recipe }

// SetRecipe shows the image through another recipe. One that cannot be
// compiled — limits that are not a range, a palette this package does not
// have — is refused and the view stays as it was.
func (inst *Viewer) SetRecipe(recipe display.Recipe) error {
	if inst.img.IsZero() {
		return eb.Build().Errorf("no image to show a recipe on")
	}
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
	if index < 0 || index >= inst.img.PlaneCount() {
		return eb.Build().Int("plane", index).Int("planes", inst.img.PlaneCount()).Errorf("image has no such plane")
	}
	if index == inst.plane {
		return nil
	}
	recipe := display.DefaultPlaneRecipe(inst.img.Plane(index))
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
	if inst.img.IsZero() || inst.plane == asStored {
		return nil
	}
	recipe := display.DefaultRecipe(inst.img)
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
	return !inst.img.IsZero() && (inst.plane != asStored || !inst.img.Model().IsColor())
}

// publish compiles a recipe for a view of the image and, when that works,
// makes it what the workers map with. The viewer's own fields change only on
// success.
func (inst *Viewer) publish(plane int, recipe display.Recipe) error {
	palette, ok := paletteFor(recipe.Palette)
	if !ok {
		return eb.Build().Str("palette", recipe.Palette).Strs("known", PaletteNames()).Errorf("no colour map of that name")
	}
	sh := &shown{img: inst.img, pyramid: inst.pyramid, plane: plane, checker: checkerColours()}
	var err error
	if plane == asStored && inst.img.Model().IsColor() {
		sh.colours, err = display.CompileColor(recipe, inst.img.Kind(), inst.img.Plane(0).Transform())
	} else {
		p := inst.img.Plane(max(plane, 0))
		sh.planes, err = display.CompilePlane(recipe, p.Kind(), p.Transform(), palette)
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
func (inst *Viewer) Fit() { inst.fitPending = !inst.img.IsZero() }

// ActualSize shows the image at one screen pixel per image pixel, about the
// centre of the current view.
func (inst *Viewer) ActualSize() {
	if inst.m.View().Loaded() {
		inst.m.View().SetZoom(0)
	}
}

// readoutAt reads the samples of the pixel a point of the flat coordinate
// system falls in.
func (inst *Viewer) readoutAt(ll portolan.LatLng) (r Readout, ok bool) {
	col, row := int(math.Floor(ll.Lng)), int(math.Floor(-ll.Lat))
	if inst.img.IsZero() || !inst.img.Plane(0).Contains(col, row) {
		return r, false
	}
	r = Readout{Col: col, Row: row, Planes: inst.img.PlaneCount()}
	for i := range r.Planes {
		r.Stored[i], r.Valid[i] = inst.img.Plane(i).StoredAt(col, row)
	}
	return r, true
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
	if !inst.img.IsZero() {
		if r := region.Intersect(image.Rect(0, 0, inst.img.Width(), inst.img.Height())); !r.Empty() {
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
	if inst.img.IsZero() {
		return image.Rectangle{}
	}
	sw, ne := box.SouthWest, box.NorthEast
	r := image.Rect(int(math.Floor(sw.Lng)), int(math.Floor(-ne.Lat)), int(math.Ceil(ne.Lng)), int(math.Ceil(-sw.Lat)))
	return r.Intersect(image.Rect(0, 0, inst.img.Width(), inst.img.Height()))
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
		if inst.img.IsZero() {
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
		if inst.singlePlane() && !inst.Opts.HideHistogram {
			histH = histogramHeight + layoutGap
			inst.wantHistogram()
		}
		cw, ch := max(w-barW, fallbackCanvas), max(h-readoutH-histH, fallbackCanvas)

		inst.m.SetNearestTiles(!inst.Opts.Smooth)
		if inst.fitPending {
			// The view needs its size before it can fit anything to it.
			inst.m.View().SetSize(portolan.Pt(float64(cw), float64(ch)))
			if err := inst.m.View().FitBounds(boundsOf(inst.img), portolan.FitOptions{}); err == nil {
				inst.fitPending = false
			}
		}
		var mev portolan.Events
		for range c.HorizontalTop().KeepIter() {
			mev = inst.m.Render(cw, ch, inst.paintRegion)
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
		names := inst.img.Model().PlaneNames()
		for i := range inst.hover.Planes {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(names[i])
			b.WriteByte(' ')
			if inst.hover.Valid[i] {
				b.WriteString(formatSample(inst.hover.Stored[i], inst.img.Kind()))
			} else {
				b.WriteString("invalid")
			}
		}
	} else {
		b.WriteString("—")
	}
	fmt.Fprintf(&b, "   ×%s", formatMagnification(inst.Magnification()))
	if k := inst.Level(); k > 0 {
		// What is drawn is not the samples: say so, and say what it is.
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
