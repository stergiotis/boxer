package imageview

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
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
	plane      int
	recipe     display.Recipe
	palette    *display.Palette
	fitPending bool
	hover      Readout
	hoverOk    bool
}

// New makes a viewer with no image. Every id it derives is scoped under
// scopeKey on ids; empty takes "imageview".
func New(ids *c.WidgetIdStack, scopeKey string, opts Options) *Viewer {
	if scopeKey == "" {
		scopeKey = "imageview"
	}
	inst := &Viewer{Opts: opts, ids: ids, key: scopeKey, src: &tileSource{}, plane: asStored}
	inst.m = portolan.New(ids, "view", portolan.Options{
		Source:        sourceFor(imaging.Image{}),
		Loader:        portolan.LoaderOptions{Pixels: inst.src},
		CRS:           portolan.Simple,
		NoInitialView: true,
		NearestTiles:  !opts.Smooth,
		// Only arrows and Escape are the map's; nothing here needs them
		// taken from a host that has its own.
		NoKeyboard: true,
	})
	return inst
}

// sourceFor describes an image to portolan as a tile source: its tiles exist
// at the full-resolution level only, inside the image's bounds, and the view
// may zoom well past both ends of that.
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
	}
	if !img.IsZero() {
		src.Bounds = boundsOf(img)
	}
	return src
}

// boundsOf is the image's extent on portolan's flat coordinate system:
// longitude is the column and latitude the negated row.
func boundsOf(img imaging.Image) portolan.LatLngBounds {
	return portolan.LatLngBoundsOf(portolan.LL(-float64(img.Height()), 0), portolan.LL(0, float64(img.Width())))
}

// Close stops the viewer's workers. It must not be rendered afterwards.
func (inst *Viewer) Close() { inst.m.Close() }

// Image is the image being shown; the zero image when there is none.
func (inst *Viewer) Image() imaging.Image { return inst.img }

// SetImage shows another image, as stored and under its default recipe, and
// fits it to the view on the next frame. The zero image shows nothing.
func (inst *Viewer) SetImage(img imaging.Image) {
	inst.img = img
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
	inst.m.InvalidateTiles()
	return nil
}

// singlePlane reports whether the current view is one plane through a
// palette, which is when a colour bar means something.
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
	sh := &shown{img: inst.img, checker: checkerColours()}
	var err error
	switch {
	case plane == asStored && inst.img.Model().IsColor():
		sh.colours, err = display.CompileColor(recipe, inst.img.Kind(), inst.img.Plane(0).Transform())
	case plane == asStored:
		sh.plane = inst.img.Plane(0)
		sh.alpha, _ = inst.img.Alpha()
		sh.planes, err = display.CompilePlane(recipe, sh.plane.Kind(), sh.plane.Transform(), palette)
	default:
		sh.plane = inst.img.Plane(plane)
		sh.planes, err = display.CompilePlane(recipe, sh.plane.Kind(), sh.plane.Transform(), palette)
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
		cw, ch := max(w-barW, fallbackCanvas), max(h-readoutH, fallbackCanvas)

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
			mev = inst.m.Render(cw, ch, nil)
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
