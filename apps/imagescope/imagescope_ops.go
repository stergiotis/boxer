package imagescope

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"math"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/fsmops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
)

// The operations catalog (ADR-0269, ADR-0373 §SD9): what an agent under a
// task grant reads and sets in an imagescope window. The image and the view
// of it are the two resources; the queries answer from a snapshot taken
// after the command stage and take their own arguments; the commands set
// values and never toggle them. The controls of the window go through the
// same handlers (ADR-0269 §SD8). Opening a file is not here: the file picker
// is the person's, and an agent is given no path to name.

const (
	resImage = "image"
	resView  = "view"

	opGetView    = "get_view"
	opReadPixels = "read_pixels"
	opMeasure    = "measure"
	opOpenSample = "open_sample"
	opSetRecipe  = "set_recipe"
	opSetPlane   = "set_plane"
	opSetRegion  = "set_region"
	opSetZoom    = "set_zoom"
	opSendToPlay = "send_to_play"

	// opsMaxPixels bounds read_pixels: the pixels one inline answer carries.
	opsMaxPixels = 1024
)

// Names the commands take.
const (
	planeAsStored = "as stored"

	limitsData       = "data"
	limitsNominal    = "nominal"
	limitsPercentile = "percentile"
	limitsManual     = "manual"

	zoomFit    = "fit"
	zoomActual = "actual"
)

// ImageInfo is the image resource's value: what is open, and where its
// samples came from.
type ImageInfo struct {
	Open           bool     `desc:"an image is open; every other field is empty when not"`
	Name           string   `desc:"the source's name: a file name without its path, or a sample's name"`
	Width          int      `desc:"columns"`
	Height         int      `desc:"rows"`
	Model          string   `desc:"colour model: gray, gray+alpha, rgb or rgba"`
	Kind           string   `desc:"sample kind of every plane: u8, u16 or f32"`
	Planes         []string `desc:"plane names in order; the names set_plane takes"`
	Format         string   `desc:"container the samples were read from; empty for a sample image"`
	StoredBitDepth int      `desc:"bits per sample as the file stored them; 0 when unknown"`
	Lossy          bool     `desc:"the format's compression does not preserve samples: they are what the decoder reconstructs"`
	Notes          []string `desc:"what to know about the samples before reading numbers off them"`
	SampleHash     string   `desc:"BLAKE3 digest of the planes, hex; equal images have equal hashes"`
}

// SetRecipeArgs is the display recipe, whole: set_recipe's argument and
// part of the view resource. Read get_view, change the fields that should
// change, and send it back.
type SetRecipeArgs struct {
	Limits      string  `desc:"where the limits come from: data (the shown plane's range), nominal (the sample kind's range), percentile, or manual"`
	Lo          float64 `json:",omitzero" desc:"lower limit in the plane's values; read for manual, reported for the others"`
	Hi          float64 `json:",omitzero" desc:"upper limit in the plane's values; above Lo"`
	PercentLo   float64 `json:",omitzero" desc:"lower percentile, 0 to 100; for percentile"`
	PercentHi   float64 `json:",omitzero" desc:"upper percentile, above PercentLo, at most 100; for percentile"`
	Transfer    string  `desc:"transfer function between the limits: linear, log, sqrt, asinh or gamma"`
	Param       float64 `json:",omitzero" desc:"the transfer function's parameter; 0 takes its default"`
	Palette     string  `json:",omitzero" desc:"colour map of a single plane: gray or a design-system scale; not used for a colour image as stored"`
	Invert      bool    `json:",omitzero" desc:"run the colour map backwards"`
	FlagClipped bool    `json:",omitzero" desc:"draw samples outside the limits in marker colours"`
}

// RegionArgs is a rectangle of pixels: columns [Col, Col+Width) and rows
// [Row, Row+Height). Pixel (col, row) covers [col, col+1) × [row, row+1)
// and row 0 is the top (ADR-0373 §SD3).
type RegionArgs struct {
	Col    int `json:",omitzero" desc:"first column"`
	Row    int `json:",omitzero" desc:"first row; 0 is the top"`
	Width  int `json:",omitzero" desc:"columns; 0 with Height 0 means no region"`
	Height int `json:",omitzero" desc:"rows"`
}

func regionArgsOf(r image.Rectangle) RegionArgs {
	if r.Empty() {
		return RegionArgs{}
	}
	return RegionArgs{Col: r.Min.X, Row: r.Min.Y, Width: r.Dx(), Height: r.Dy()}
}

func (inst RegionArgs) rect() image.Rectangle {
	return image.Rect(inst.Col, inst.Row, inst.Col+inst.Width, inst.Row+inst.Height)
}

func (inst RegionArgs) isZero() bool { return inst.Width == 0 && inst.Height == 0 }

// ViewState is the view resource's value: which plane, the recipe, and the
// marked region.
type ViewState struct {
	Plane  string        `desc:"as stored, or the name of the single plane shown"`
	Recipe SetRecipeArgs `desc:"the display recipe in force"`
	Region RegionArgs    `desc:"the marked region; zero when none"`
}

// Handover is what the last send_to_play came to.
type Handover struct {
	Busy  bool   `desc:"a handover is in flight"`
	Note  string `desc:"what the last handover opened; empty otherwise"`
	Error string `desc:"why the last handover failed; empty otherwise"`
}

// View is get_view's result.
type View struct {
	Phase         string    `desc:"the window's phase: empty, opening, viewing, measuring or measured"`
	Image         ImageInfo `desc:"the image open"`
	View          ViewState `desc:"what is shown of it, and how"`
	Magnification float64   `desc:"screen pixels per image pixel in the current view"`
	Level         int       `desc:"0 when the view draws the samples themselves; k when each drawn cell is the mean of a 2^k × 2^k block"`
	LevelsReady   bool      `desc:"the reduced levels are built"`
	LimitsPending bool      `desc:"percentile limits asked for wait for the plane's histogram; View.Recipe carries the limits in force until they land"`
	Error         string    `desc:"why the last open failed; empty otherwise"`
	Note          string    `desc:"the last open's outcome that was not a failure; empty otherwise"`
	Refused       string    `desc:"why the person's last change to the view was refused; empty otherwise"`
	Measured      bool      `desc:"the marked region's statistics are computed; measure returns them"`
	MeasureError  string    `desc:"why the marked region could not be measured; empty otherwise"`
	Handover      Handover  `desc:"the last send_to_play"`
	SeedProblem   string    `desc:"why a seed variable was not applied; empty otherwise"`
}

// Pixels is read_pixels's result: a block of samples, bounded.
type Pixels struct {
	Region    RegionArgs  `desc:"the block returned, clipped to the image and to 1024 pixels"`
	Planes    []string    `desc:"plane names, in the order of Values"`
	Values    [][]float64 `desc:"one list per plane: the block's values in row-major order; an invalid sample is 0 and counted in Invalid"`
	Invalid   int         `desc:"invalid samples in the block (NaN in a float plane)"`
	Truncated bool        `desc:"the block asked for was larger than the one returned"`
}

// Measurement is measure's result.
type Measurement struct {
	Region RegionArgs        `desc:"the region measured"`
	Pixels int64             `desc:"pixels in the region"`
	Planes []PlaneStatistics `desc:"one entry per plane"`
	Method string            `desc:"what the numbers are and what they assume"`
}

// OpenSampleArgs names a sample image.
type OpenSampleArgs struct {
	Name string `desc:"field (1400 × 1000, 16-bit, one plane), colour (640 × 480, 8-bit rgba), small (48 × 32, 8-bit, one plane), float (640 × 480, 32-bit float with a block of invalid samples) or scaled (480 × 320, 16-bit under a value transform)"`
}

// OpenOutcome is what open_sample settles with.
type OpenOutcome struct {
	Phase string `desc:"the phase the open ended in"`
	Image string `desc:"the image now open, in one line"`
	Error string `desc:"why the open failed; empty otherwise"`
}

// SetPlaneArgs names what to show.
type SetPlaneArgs struct {
	Plane string `desc:"as stored, or a plane name from get_view's Image.Planes"`
}

// SetZoomArgs names a zoom.
type SetZoomArgs struct {
	Mode string `desc:"fit (the whole image) or actual (one sample per screen pixel)"`
}

// asRefusal is errors.As for a refusal.
func asRefusal(err error, r **app.OperationRefusal) bool { return errors.As(err, r) }

// refuse turns an error of the display or viewer packages into a refusal
// that carries its message; a refusal passes through.
func refuse(err error) error {
	if err == nil {
		return nil
	}
	var r *app.OperationRefusal
	if errors.As(err, &r) {
		return err
	}
	return app.RefuseOperation(err.Error())
}

var errNoImage = app.RefuseOperation("there is no image; open_sample opens one, or the person opens a file with Open…")

// imageInfo is the image resource's value.
func imageInfo(img imaging.Image) (out ImageInfo) {
	if img.IsZero() {
		return
	}
	p := img.Provenance()
	sum := p.SampleHash
	return ImageInfo{Open: true, Name: p.SourceName, Width: img.Width(), Height: img.Height(),
		Model: img.Model().String(), Kind: img.Kind().String(), Planes: slices.Clone(img.Model().PlaneNames()),
		Format: p.Format, StoredBitDepth: int(p.StoredBitDepth), Lossy: p.Lossy, Notes: sourceNotes(img),
		SampleHash: hex.EncodeToString(sum[:])}
}

// recipeArgs is a recipe as set_recipe takes it.
func recipeArgs(r display.Recipe) (out SetRecipeArgs) {
	out = SetRecipeArgs{Limits: r.Limits.Source.String(), Lo: r.Limits.Lo, Hi: r.Limits.Hi,
		PercentLo: r.Limits.PercentLo, PercentHi: r.Limits.PercentHi,
		Transfer: r.Transfer.String(), Param: r.Param, Palette: r.Palette, Invert: r.Invert, FlagClipped: r.FlagClipped}
	if out.Palette == "" {
		out.Palette = imageview.PaletteGray
	}
	return
}

// viewState is the view resource's value.
func (inst *App) viewState() (out ViewState) {
	if inst.img.IsZero() {
		return
	}
	out.Plane = planeAsStored
	if i, single := inst.viewer.Plane(); single {
		out.Plane = inst.img.Model().PlaneNames()[i]
	}
	out.Recipe = recipeArgs(inst.viewer.Recipe())
	r, _ := inst.viewer.Region()
	out.Region = regionArgsOf(r)
	return
}

// setRecipe applies a whole recipe to the view: every field is set, none is
// toggled, so the same arguments twice leave the same view. Percentile
// limits are counted over the whole plane off the frame, so they land a
// little after the call; get_view reports the limits in force.
func (inst *App) setRecipe(in SetRecipeArgs) (err error) {
	if inst.img.IsZero() {
		return errNoImage
	}
	recipe := inst.viewer.Recipe()
	if err = recipe.Transfer.UnmarshalText([]byte(in.Transfer)); err != nil {
		names := make([]string, 0, len(display.AllTransfers))
		for _, t := range display.AllTransfers {
			names = append(names, t.String())
		}
		return app.RefuseOperation("no transfer function named " + in.Transfer + "; the names are " + strings.Join(names, ", "))
	}
	recipe.Param, recipe.Invert, recipe.FlagClipped = in.Param, in.Invert, in.FlagClipped
	if inst.viewer.IsSinglePlane() {
		recipe.Palette = in.Palette
		if in.Palette != "" && !slices.Contains(imageview.PaletteNames(), in.Palette) {
			return app.RefuseOperation("no colour map named " + in.Palette + "; the names are " + strings.Join(imageview.PaletteNames(), ", "))
		}
	}
	i, _ := inst.viewer.Plane()
	shown := inst.img.Plane(max(i, 0))
	switch in.Limits {
	case limitsData:
		l, ok := display.DataLimits(shown)
		if !ok {
			return app.RefuseOperation("the plane shown has no range to take limits from; set them manually")
		}
		recipe.Limits = l
	case limitsNominal:
		l, ok := display.NominalLimits(shown.Kind(), shown.Transform())
		if !ok {
			return app.RefuseOperation("a float plane has no nominal range; use data, percentile or manual")
		}
		recipe.Limits = l
	case limitsManual:
		if math.IsNaN(in.Lo) || math.IsNaN(in.Hi) || !(in.Hi > in.Lo) {
			return app.RefuseOperation("manual limits need Hi above Lo")
		}
		recipe.Limits = display.Limits{Lo: in.Lo, Hi: in.Hi, Source: display.LimitsSourceManual}
	case limitsPercentile:
		if !inst.viewer.IsSinglePlane() {
			return app.RefuseOperation("percentile limits apply to a single plane; set_plane shows one")
		}
		if !(in.PercentLo >= 0 && in.PercentLo < in.PercentHi && in.PercentHi <= 100) {
			return app.RefuseOperation("percentiles are an ascending pair within 0 and 100")
		}
		recipe.Limits = display.Limits{Source: display.LimitsSourcePercentile, PercentLo: in.PercentLo, PercentHi: in.PercentHi}
	default:
		return app.RefuseOperation("limits are data, nominal, percentile or manual")
	}
	if err = inst.viewer.SetRecipe(recipe); err != nil {
		return refuse(err)
	}
	inst.syncChecks()
	return nil
}

// setPlane shows the image as stored or one named plane of it, under that
// view's default recipe with the colour map kept.
func (inst *App) setPlane(name string) (err error) {
	if inst.img.IsZero() {
		return errNoImage
	}
	if name == planeAsStored {
		err = inst.viewer.SetAsStored()
	} else {
		i := slices.Index(inst.img.Model().PlaneNames(), name)
		if i < 0 {
			return app.RefuseOperation("no plane named " + name + "; this image has " + strings.Join(inst.img.Model().PlaneNames(), ", ") + ", and as stored")
		}
		err = inst.viewer.SetPlane(i)
	}
	inst.syncChecks()
	return refuse(err)
}

// setRegion marks a region, or clears the mark for the zero region. A
// region that reaches outside the image is refused, naming the image's
// size, so that what is measured is what was asked for.
func (inst *App) setRegion(in RegionArgs) (err error) {
	if inst.img.IsZero() {
		return errNoImage
	}
	if in.isZero() {
		inst.viewer.SetRegion(image.Rectangle{})
		return nil
	}
	if err = checkRegion(inst.img, in); err != nil {
		return err
	}
	inst.viewer.SetRegion(in.rect())
	return nil
}

// checkRegion refuses a region that is empty or not inside the image.
func checkRegion(img imaging.Image, in RegionArgs) error {
	if in.Width <= 0 || in.Height <= 0 {
		return app.RefuseOperation("a region's width and height are positive")
	}
	if !in.rect().In(image.Rect(0, 0, img.Width(), img.Height())) {
		return app.RefuseOperation(fmt.Sprintf("the region reaches outside the image, which is %d columns by %d rows", img.Width(), img.Height()))
	}
	return nil
}

// setZoom fits the image or shows it at one sample per screen pixel.
func (inst *App) setZoom(mode string) error {
	if inst.img.IsZero() {
		return errNoImage
	}
	switch mode {
	case zoomFit:
		inst.viewer.Fit()
	case zoomActual:
		inst.viewer.ActualSize()
	default:
		return app.RefuseOperation("zoom is fit or actual")
	}
	return nil
}

// opsSnap is what queries read: a copy taken on the render goroutine after
// the command stage. The image and the measurement are immutable values.
type opsSnap struct {
	phase         phaseE
	img           imaging.Image
	view          ViewState
	magnification float64
	level         int
	levelsReady   bool
	limitsPending bool
	opening       bool
	openErr       string
	openNote      string
	problem       string
	measured      *measurement
	measureErr    string
	handover      Handover
	seedErr       string
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn = opsSnap{phase: inst.observePhase(), img: inst.img, view: inst.viewState(), opening: inst.open.Running(),
		openErr: inst.openErr, openNote: inst.openNote, problem: inst.problem, measured: inst.measured, seedErr: inst.seedErr}
	if !inst.img.IsZero() {
		sn.magnification, sn.level, sn.levelsReady = inst.viewer.Magnification(), inst.viewer.Level(), inst.viewer.LevelsReady()
		sn.limitsPending = inst.viewer.LimitsPending()
	}
	if inst.measureErr != nil {
		sn.measureErr = inst.measureErr.Error()
	}
	sn.handover.Busy, sn.handover.Note, sn.handover.Error = inst.handoverState()
	return
}

// readPixels answers read_pixels from an image: the block clipped to the
// image, then to the bound by dropping whole rows.
func readPixels(img imaging.Image, in RegionArgs) (out Pixels, err error) {
	if in.Width == 0 && in.Height == 0 {
		in.Width, in.Height = 1, 1
	}
	if in.Width <= 0 || in.Height <= 0 {
		return out, app.RefuseOperation("a block's width and height are positive; both 0 reads one pixel")
	}
	r := in.rect().Intersect(image.Rect(0, 0, img.Width(), img.Height()))
	if r.Empty() {
		return out, app.RefuseOperation(fmt.Sprintf("the block is outside the image, which is %d columns by %d rows", img.Width(), img.Height()))
	}
	out.Truncated = r != in.rect()
	if r.Dx() > opsMaxPixels {
		r.Max.X, out.Truncated = r.Min.X+opsMaxPixels, true
	}
	if rows := opsMaxPixels / r.Dx(); r.Dy() > rows {
		r.Max.Y, out.Truncated = r.Min.Y+rows, true
	}
	out.Region = regionArgsOf(r)
	out.Planes = slices.Clone(img.Model().PlaneNames())
	out.Values = make([][]float64, img.PlaneCount())
	for i := range out.Values {
		p := img.Plane(i)
		vals := make([]float64, 0, r.Dx()*r.Dy())
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				v, ok := p.ValueAt(x, y)
				if !ok {
					v = 0
					out.Invalid++
				}
				vals = append(vals, v)
			}
		}
		out.Values[i] = vals
	}
	return out, nil
}

// measurementOf renders a measurement as measure's answer.
func measurementOf(m *measurement) Measurement {
	return Measurement{Region: regionArgsOf(m.region), Pixels: int64(m.region.Dx()) * int64(m.region.Dy()),
		Planes: slices.Clone(m.planes), Method: m.method()}
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resImage, "the image open: its name, size, colour model, sample kind and where its samples came from", func(inst *App) any { return imageInfo(inst.img) })
	s.Resource(resView, "what is shown of the image and how: the plane, the display recipe and the marked region", func(inst *App) any { return inst.viewState() })
	s.Restorable(resView, func(inst *App, v any) bool {
		vs, ok := v.(ViewState)
		if !ok || inst.img.IsZero() {
			return false
		}
		if inst.setPlane(vs.Plane) != nil || inst.setRecipe(vs.Recipe) != nil {
			return false
		}
		return inst.setRegion(vs.Region) == nil
	})

	fsmops.Mount(s, "image", "the window's phase, from no image to a measured region", func(inst *App) opfsm.SourceI {
		if inst.phaseMachine == nil {
			return nil
		}
		return inst.phaseMachine
	}, fsmops.Options{History: 16})

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the phase, the image open, the plane and recipe it is shown through, the marked region, the magnification and the level drawn",
		Reads:   []string{resImage, resView}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{Phase: sn.phase.String(), Image: imageInfo(sn.img), View: sn.view, Magnification: sn.magnification,
				Level: sn.level, LevelsReady: sn.levelsReady, LimitsPending: sn.limitsPending, Error: sn.openErr, Note: sn.openNote, Refused: sn.problem,
				Measured: sn.measured != nil, MeasureError: sn.measureErr, Handover: sn.handover, SeedProblem: sn.seedErr}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opReadPixels, Version: 1,
		Summary: "read a block of the image's samples, every plane, at most 1024 pixels; the values are the samples themselves, whatever the recipe shows",
		Reads:   []string{resImage}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in RegionArgs) (out Pixels, err error) {
			if sn.img.IsZero() {
				return out, errNoImage
			}
			return readPixels(sn.img, in)
		})

	appops.Query(s, app.OperationSpec{Name: opMeasure, Version: 1,
		Summary: "statistics of every plane over a region, from the samples at full resolution: count, range, mean, standard deviation, median, 1st and 99th percentile, with the method stated; the zero region means the marked one",
		Reads:   []string{resImage, resView}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in RegionArgs) (out Measurement, err error) {
			if sn.img.IsZero() {
				return out, errNoImage
			}
			if in.isZero() {
				in = sn.view.Region
				if in.isZero() {
					return out, app.RefuseOperation("no region is marked; pass one, or mark one with set_region")
				}
			}
			if err = checkRegion(sn.img, in); err != nil {
				return out, err
			}
			if m := sn.measured; m != nil && m.region == in.rect() {
				return measurementOf(m), nil
			}
			m, merr := measureRegion(context.Background(), sn.img, in.rect(), func(uint64, uint64, string) {})
			if merr != nil {
				return out, refuse(merr)
			}
			return measurementOf(m), nil
		})

	appops.Command(s, app.OperationSpec{Name: opOpenSample, Version: 1,
		Summary: "open a synthetic sample image in place of what is shown; the call settles when it is shown",
		Effect:  app.OperationEffectDocument, Writes: []string{resImage, resView}, Agents: true, Async: true,
		Gesture: "a button of the Sample row, under Advanced or on the empty window", Rebuild: app.OperationRebuildRefold, Fork: app.OperationForkPerInstance,
		Follows: []string{"the view returns to the image as stored under its default recipe, and the marked region is cleared",
			"a file is opened by the person with Open…; no operation names a file"}},
		func(inst *App, _ app.OperationCall, in OpenSampleArgs) (out appops.None, err error) {
			if err = checkSampleName(in.Name); err != nil {
				return
			}
			if inst.open.Running() {
				err = app.RefuseOperation("an open is in flight; read get_view until its phase leaves opening")
				return
			}
			inst.sampleCallGen = inst.openGen
			inst.openSample(in.Name, image.Rectangle{})
			return
		})
	appops.Settles(s, opOpenSample, func(inst *App, _ app.OperationCall) (out OpenOutcome, settled bool, err error) {
		if inst.landedGen <= inst.sampleCallGen || inst.open.Running() {
			return out, false, nil
		}
		return OpenOutcome{Phase: inst.observePhase().String(), Image: describeImage(inst.img), Error: inst.openErr}, true, nil
	})

	needsImage := func(sn opsSnap) (ok bool, reason string) {
		if sn.img.IsZero() {
			return false, "there is no image; open_sample opens one, or the person opens a file with Open…"
		}
		return true, ""
	}

	appops.Command(s, app.OperationSpec{Name: opSetRecipe, Version: 1,
		Summary: "set the display recipe, whole: limits, transfer function, colour map, inversion and clipping marks; the samples are not changed",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true,
		Gesture: "the Limits buttons, and the controls under Advanced", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"percentile limits are computed from the plane's histogram, counted over the whole plane off the frame; until it is, get_view's LimitsPending is true and the limits in force stay",
			"a name that is not a transfer function or a colour map is refused naming the ones that are"}},
		func(inst *App, _ app.OperationCall, in SetRecipeArgs) (out appops.None, err error) {
			err = inst.setRecipe(in)
			return
		})
	s.Available(opSetRecipe, needsImage)

	appops.Command(s, app.OperationSpec{Name: opSetPlane, Version: 1,
		Summary: "show the image as stored, or one named plane of it by itself through a colour map",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true,
		Gesture: "the Show buttons", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"the recipe returns to that view's default, keeping the colour map; set_recipe after it"}},
		func(inst *App, _ app.OperationCall, in SetPlaneArgs) (out appops.None, err error) {
			err = inst.setPlane(in.Plane)
			return
		})
	s.Available(opSetPlane, needsImage)

	appops.Command(s, app.OperationSpec{Name: opSetRegion, Version: 1,
		Summary: "mark a rectangle of pixels, whose statistics the window then computes, or clear the mark with the zero region",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true,
		Gesture: "a drag on the image with Select region on; Clear region", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"the statistics are computed off the frame; get_view's Measured says when measure returns them without recomputing",
			"a region that reaches outside the image is refused naming the image's size"}},
		func(inst *App, _ app.OperationCall, in RegionArgs) (out appops.None, err error) {
			err = inst.setRegion(in)
			return
		})
	s.Available(opSetRegion, needsImage)

	appops.Command(s, app.OperationSpec{Name: opSetZoom, Version: 1,
		Summary: "fit the whole image in the view, or show it at one sample per screen pixel",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true,
		Gesture: "the Fit and 1:1 buttons", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance},
		func(inst *App, _ app.OperationCall, in SetZoomArgs) (out appops.None, err error) {
			err = inst.setZoom(in.Mode)
			return
		})
	s.Available(opSetZoom, needsImage)

	appops.Command(s, app.OperationSpec{Name: opSendToPlay, Version: 1,
		Summary: "publish the stored samples of the marked region, or of the whole image when none is marked, as an ad-hoc dataset of one row per pixel and open the playground on it, seeded with the query behind the statistics table",
		Effect:  app.OperationEffectConsequential, Reads: []string{resImage, resView}, Agents: true,
		Gesture: "the Send to play button", Fork: app.OperationForkPerInstance,
		Follows: []string{"the request runs off the frame; get_view's Handover reports what opened or why it failed",
			"more than about four million pixels are refused; mark a smaller region"}},
		func(inst *App, _ app.OperationCall, _ appops.None) (out appops.None, err error) {
			if inst.bus == nil {
				err = app.RefuseOperation("no bus on this host; the handover needs the app runtime")
				return
			}
			region, _ := inst.viewer.Region()
			if _, err = handoverRegion(inst.img, region); err != nil {
				return
			}
			inst.startHandover()
			return
		})
	s.Available(opSendToPlay, func(sn opsSnap) (ok bool, reason string) {
		switch {
		case sn.img.IsZero():
			return false, "there is no image; open_sample opens one, or the person opens a file with Open…"
		case sn.handover.Busy:
			return false, "a handover is in flight"
		}
		return true, ""
	})
	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)

// gestureOr routes the person's control through the catalog (ADR-0269
// §SD8), so the command log sees it as theirs, and applies it directly where
// no host serves the catalog (the tour, tests).
func gestureOr[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

// viewGesture is gestureOr for a control that changes the view: a refusal
// of the direct path is shown in the status row.
func (inst *App) viewGesture(op string, in any, direct func() error) {
	inst.problem = ""
	apply := func() { inst.problem = refusalText(direct()) }
	switch a := in.(type) {
	case SetZoomArgs:
		gestureOr(inst, op, a, apply)
	case SetPlaneArgs:
		gestureOr(inst, op, a, apply)
	case RegionArgs:
		gestureOr(inst, op, a, apply)
	case SetRecipeArgs:
		gestureOr(inst, op, a, apply)
	default:
		apply()
	}
}

// recipeGesture sets the recipe in force with one change made to it.
func (inst *App) recipeGesture(change func(a *SetRecipeArgs)) {
	args := recipeArgs(inst.viewer.Recipe())
	change(&args)
	inst.viewGesture(opSetRecipe, args, func() error { return inst.setRecipe(args) })
}
