package imagescope

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/fsmops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
)

// The operations catalog (ADR-0269, ADR-0373 §SD9): what an agent under a
// task grant reads and sets in an imagescope window. The image and the view
// of it are the two resources; the queries answer from a snapshot taken
// after the command stage and take their own arguments; the commands set
// values and never toggle them. The controls of the window go through the
// same handlers (ADR-0269 §SD8). Opening a file is not here: the file picker
// is the person's, and an agent is given no path to name. A stack open in
// the window adds its axes to get_view and the two commands that choose its
// selection; an image with a placement adds the frame and the two queries
// that convert between pixels and frame coordinates (ADR-0398 §M6).

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

	opGetAxis     = "get_axis"
	opSetPosition = "set_position"
	opSetBands    = "set_bands"
	opToWorld     = "to_world"
	opToPixel     = "to_pixel"

	// axisInlineMax bounds the labels and coordinates get_view lists of an
	// axis; get_axis pages the rest, axisPageMax at a time.
	axisInlineMax = 32
	axisPageMax   = 256

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
	Open           bool           `desc:"an image is open; every other field is empty when not"`
	Name           string         `desc:"the source's name: a file name without its path, or a sample's name"`
	Width          int            `desc:"columns"`
	Height         int            `desc:"rows"`
	Model          string         `desc:"colour model: gray, gray+alpha, rgb or rgba"`
	Kind           string         `desc:"sample kind of every plane: u8, u16 or i16 (one histogram bin per code), i32, f32 or f64 (binned)"`
	Planes         []string       `desc:"plane names in order — a band's label where the image was taken from a stack, otherwise the colour model's names; the names set_plane takes"`
	HasInvalid     bool           `json:",omitzero" desc:"the colour planes declare one stored value invalid: no measurement, as NaN is in a float plane"`
	InvalidValue   float64        `json:",omitzero" desc:"the stored value declared invalid, when HasInvalid"`
	Format         string         `desc:"container the samples were read from; empty for a sample image"`
	StoredBitDepth int            `desc:"bits per sample as the file stored them; 0 when unknown"`
	Lossy          bool           `desc:"the format's compression does not preserve samples: they are what the decoder reconstructs"`
	Notes          []string       `desc:"what to know about the samples before reading numbers off them"`
	SampleHash     string         `desc:"BLAKE3 digest of the planes, hex; equal images have equal hashes; empty while the image is still being read"`
	Placement      *PlacementInfo `json:",omitzero" desc:"where the pixels are, when the file says; absent when nothing places them"`
}

// PlacementInfo is where an image's pixels are (ADR-0398 §SD4).
type PlacementInfo struct {
	Frame       string     `desc:"reference-system (a coordinate reference system named by Code; coordinates as it defines them, not converted), celestial-gnomonic (the sky by a TAN projection; coordinates are decimal degrees of longitude and latitude) or physical (a scale in a unit; the origin means nothing)"`
	Code        string     `json:",omitzero" desc:"the system as the file names it: EPSG:2056, ICRS; recorded, not resolved"`
	AxisNames   [2]string  `desc:"the two coordinates' names: E and N, RA and Dec, x and y"`
	Units       [2]string  `desc:"the two coordinates' units; empty is unknown; deg for the sky"`
	PixelSize   [2]float64 `desc:"frame units one pixel spans along a row and along a column; projection-plane degrees on the sky"`
	Affine      [6]float64 `desc:"x = Affine[0] + Affine[1]·col + Affine[2]·row, y = Affine[3] + Affine[4]·col + Affine[5]·row, in continuous pixel coordinates; on the sky x and y are projection-plane degrees, which to_world turns into longitude and latitude"`
	RefLon      float64    `json:",omitzero" desc:"on the sky: the reference point's longitude, degrees"`
	RefLat      float64    `json:",omitzero" desc:"on the sky: the reference point's latitude, degrees"`
	Source      string     `desc:"the statement of the file the placement was derived from"`
	Unapplied   []string   `json:",omitzero" desc:"what the file declared that is not applied: another projection, a distortion, an alternate system"`
	Description string     `desc:"the same in one line, as the window shows it"`
}

// placementInfo is a placement as get_view reports it; nil for none.
func placementInfo(p imaging.Placement) *PlacementInfo {
	if p.IsZero() {
		return nil
	}
	sx, sy := p.PixelSize()
	out := &PlacementInfo{Frame: p.Frame.Kind.String(), Code: p.Frame.Code, AxisNames: p.Frame.AxisNames, Units: p.Frame.Units,
		PixelSize: [2]float64{sx, sy}, Affine: p.Affine, Source: p.Source, Unapplied: p.Unapplied,
		Description: imageview.DescribePlacement(p)}
	if p.Frame.Kind == imaging.FrameKindCelestialGnomonic {
		out.RefLon, out.RefLat = p.Frame.RefLon, p.Frame.RefLat
		out.Units = [2]string{"deg", "deg"}
	}
	return out
}

// SetRecipeArgs is the display recipe, whole: set_recipe's argument and
// part of the view resource. Read get_view, change the fields that should
// change, and send it back.
type SetRecipeArgs struct {
	Limits         string  `desc:"where the limits come from: data (the shown plane's range; a linked composite's shown planes together), nominal (the sample kind's range), percentile, zscale (around the bulk of the values from a sample of them; ADR-0401 §SD7), or manual"`
	Lo             float64 `json:",omitzero" desc:"lower limit in the plane's values; read for manual, reported for the others"`
	Hi             float64 `json:",omitzero" desc:"upper limit in the plane's values; above Lo"`
	PercentLo      float64 `json:",omitzero" desc:"lower percentile, 0 to 100; for percentile"`
	PercentHi      float64 `json:",omitzero" desc:"upper percentile, above PercentLo, at most 100; for percentile"`
	ZScaleContrast float64 `json:",omitzero" desc:"for zscale: the slope of the line fitted to the sorted sample is divided by it, so a smaller contrast widens the limits; 0 takes 0.25, and what was used is reported"`
	ZScaleSamples  int32   `json:",omitzero" desc:"for zscale: the most grid points the sample is read at, up to 1048576; 0 takes 1000, and what was used is reported"`
	Transfer       string  `desc:"transfer function between the limits: linear, log, sqrt, asinh or gamma"`
	Param          float64 `json:",omitzero" desc:"the transfer function's parameter; 0 takes its default"`
	Palette        string  `json:",omitzero" desc:"colour map of a single plane: gray or a design-system scale; not used for a colour image as stored"`
	Invert         bool    `json:",omitzero" desc:"run the colour map backwards"`
	FlagClipped    bool    `json:",omitzero" desc:"draw samples outside the limits in marker colours"`
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

// ViewState is the view resource's value: which plane or composite, the
// recipe, the marked region and, for a stack, the selection asked for.
type ViewState struct {
	Plane      string         `desc:"as stored, the name of the single plane shown, or composite"`
	PlaneIndex int            `json:",omitzero" desc:"the index of the single plane shown in Image.Planes; 0 for as stored"`
	Recipe     SetRecipeArgs  `desc:"the display recipe in force; of a composite, the layer in hand's"`
	Composite  *CompositeArgs `json:",omitzero" desc:"the composite shown, whole, linked or not, as set_composite takes it; absent when none is (ADR-0401 §SD2)"`
	Region     RegionArgs     `desc:"the marked region; zero when none"`
	Selection  *SelectionInfo `json:",omitzero" desc:"for a stack: the selection asked for; absent for an image that is no stack's"`
}

// Handover is what the last send_to_play came to.
type Handover struct {
	Busy  bool   `desc:"a handover is in flight"`
	Note  string `desc:"what the last handover opened; empty otherwise"`
	Error string `desc:"why the last handover failed; empty otherwise"`
}

// View is get_view's result.
type View struct {
	Phase         string         `desc:"the window's phase: empty, opening, viewing, measuring or measured"`
	Image         ImageInfo      `desc:"the image open"`
	View          ViewState      `desc:"what is shown of it, and how"`
	Mode          string         `desc:"how the image is shown: as stored, single plane, or composite"`
	Composite     *CompositeInfo `json:",omitzero" desc:"the composite shown: its layers with their colours and the limits in force, whether it is linked, the layer in hand and the statement drawn when it is not linked; absent when none is"`
	Clipped       *ClippedInfo   `json:",omitzero" desc:"the share of the valid samples the limits in force clip, below and above, of the single plane shown or the composite's layer in hand, from its histogram; absent until the histogram is counted, and for a colour image as stored"`
	Magnification float64        `desc:"screen pixels per image pixel in the current view"`
	Level         int            `desc:"0 when the view draws the samples themselves; k when each drawn cell is the mean of a 2^k × 2^k block"`
	LevelsReady   bool           `desc:"the reduced levels are built"`
	LimitsPending bool           `desc:"percentile limits asked for wait for the plane's histogram, or zscale limits for their sample of the plane; View.Recipe carries the limits in force until they land"`
	Error         string         `desc:"why the last open failed; empty otherwise"`
	Note          string         `desc:"the last open's outcome that was not a failure; empty otherwise"`
	Refused       string         `desc:"why the person's last change to the view was refused; empty otherwise"`
	Measured      bool           `desc:"the marked region's statistics are computed; measure returns them"`
	MeasureError  string         `desc:"why the marked region could not be measured; empty otherwise"`
	Handover      Handover       `desc:"the last send_to_play"`
	SeedProblem   string         `desc:"why a seed variable was not applied; empty otherwise"`

	ShownImage      string   `desc:"the id of the image shown: opened, or r1, r2, … for a result; running while a run's result fills; list_images lists them"`
	Images          int      `desc:"images in the window: the one opened and the results"`
	Running         bool     `desc:"a run of an operation is under way; run_operation settles when it ends"`
	RunError        string   `json:",omitzero" desc:"why the last run made nothing"`
	RunNote         string   `json:",omitzero" desc:"what the last run made, or that it was cancelled"`
	Regions         int      `desc:"regions in the window; list_regions lists them"`
	SelectedRegion  string   `json:",omitzero" desc:"the id of the region selected"`
	Tool            string   `desc:"the tool in the person's hand: select, point, line, rectangle, ellipse or polygon"`
	RegionMeasuring bool     `desc:"a measurement of regions is under way"`
	Profiling       bool     `desc:"a line's profile is under way"`
	RegionsHandover Handover `desc:"the last send_regions_to_play"`
	ProfileHandover Handover `desc:"the last send_profile_to_play"`

	StackSelection bool       `desc:"the image shown is a selection of a stack; Stack says which"`
	Stack          *StackInfo `json:",omitzero" desc:"the stack the image shown is a selection of: its axes, the selection shown and the one asked for; absent for an image that is no stack's"`
	SelectionNote  string     `json:",omitzero" desc:"what the last selection change kept of the view, or had to change: limits taken again from the data, a view of another kind"`
	SelectionError string     `json:",omitzero" desc:"why the last selection asked for could not be taken; the one shown stayed"`
}

// AxisInfo is one axis of a stack other than rows and columns.
type AxisInfo struct {
	Index     int       `desc:"the axis's place in the stack's order, slowest first"`
	Name      string    `desc:"the name set_position takes"`
	Role      string    `desc:"band (its positions are measured together; one or three are shown, set_bands) or selector (one position is shown, set_position)"`
	Size      int       `desc:"positions on the axis, 0 to Size-1"`
	Unit      string    `json:",omitzero" desc:"the unit of Coords; empty is unknown"`
	From      int       `json:",omitzero" desc:"the position Labels and Coords begin at"`
	Labels    []string  `json:",omitzero" desc:"a label per position from From: a band's name; absent when the axis has none, and a position is then called by its number"`
	Coords    []float64 `json:",omitzero" desc:"a coordinate per position from From, in Unit: a wavelength, a time, a depth; absent when the axis has none"`
	Truncated bool      `json:",omitzero" desc:"the axis has positions past the ones listed; get_axis pages them"`
}

// axisInfo is axis i of a stack with its labels and coordinates from
// position from, at most count of them.
func axisInfo(stk *stackView, i, from, count int) AxisInfo {
	a := stk.axes[i]
	from = min(max(from, 0), a.Size)
	to := min(a.Size, from+count)
	out := AxisInfo{Index: i, Name: a.Name, Role: a.Role.String(), Size: a.Size, Unit: a.Unit, From: from, Truncated: to < a.Size}
	if a.Labels != nil {
		out.Labels = slices.Clone(a.Labels[from:to])
	}
	if a.Coords != nil {
		out.Coords = slices.Clone(a.Coords[from:to])
	}
	return out
}

// AxisPosition is where a selector axis stands.
type AxisPosition struct {
	Axis     string  `desc:"the axis's name"`
	Position int     `desc:"the position, 0 to the axis's size - 1"`
	Label    string  `desc:"the position's label, or its number when the axis has none"`
	Coord    float64 `json:",omitzero" desc:"the position's coordinate, when the axis has them"`
	HasCoord bool    `json:",omitzero" desc:"the axis has coordinates, and Coord is this position's"`
	Unit     string  `json:",omitzero" desc:"the unit of Coord"`
}

// BandRef is one band shown.
type BandRef struct {
	Index int    `desc:"the band's position on the band axis, as set_bands takes it"`
	Label string `desc:"the band's label, or its number when the axis has none"`
}

// SelectionInfo is which image of a stack: a position on every selector
// axis, and the bands.
type SelectionInfo struct {
	Positions []AxisPosition `desc:"one per selector axis, in the stack's order"`
	Bands     []BandRef      `json:",omitzero" desc:"the bands, as red, green and blue when three; empty for the image as stored and for a stack without a band axis"`
	AsStored  bool           `json:",omitzero" desc:"the image as stored: every band of the colour model the stack's bands are"`
	Composite bool           `json:",omitzero" desc:"the image was taken for a composite: Bands are its planes, each band its layers show once, the first repeated when there are two; set_composite asks for it"`
	Text      string         `desc:"the same in one line, as the window says it"`
}

// selectionInfo is a selection of a stack as the catalog reports it.
func selectionInfo(stk *stackView, sel selection) SelectionInfo {
	out := SelectionInfo{Text: stk.describe(sel), AsStored: sel.bands == nil && stk.band >= 0, Composite: sel.composite}
	for i, a := range stk.axes {
		if a.Role != imaging.AxisRoleSelector || i >= len(sel.at) {
			continue
		}
		p := AxisPosition{Axis: a.Name, Position: sel.at[i], Label: stk.label(i, sel.at[i]), Unit: a.Unit}
		if a.Coords != nil {
			p.Coord, p.HasCoord = a.Coords[sel.at[i]], true
		}
		out.Positions = append(out.Positions, p)
	}
	for _, b := range sel.bands {
		out.Bands = append(out.Bands, BandRef{Index: b, Label: stk.label(stk.band, b)})
	}
	return out
}

// selectionOf is a selection the catalog reported, read back: what
// restoring the view resource asks for again.
func selectionOf(stk *stackView, in SelectionInfo) (sel selection, ok bool) {
	sel = selection{at: make([]int, len(stk.axes)), composite: in.Composite}
	for _, p := range in.Positions {
		i := stk.axisIndex(p.Axis)
		if i < 0 || p.Position < 0 || p.Position >= stk.axes[i].Size {
			return sel, false
		}
		sel.at[i] = p.Position
	}
	if !in.AsStored && stk.band >= 0 {
		sel.bands = make([]int, 0, len(in.Bands))
		for _, b := range in.Bands {
			sel.bands = append(sel.bands, b.Index)
		}
	}
	return sel, true
}

// StackInfo is the stack the image shown is a selection of.
type StackInfo struct {
	Name    string        `desc:"the source's name"`
	Kind    string        `desc:"sample kind of every plane"`
	Planes  int           `desc:"planes in the stack: the product of the axes' sizes"`
	Bytes   int64         `desc:"what the stack's samples take in memory while it is open"`
	Model   string        `json:",omitzero" desc:"the colour model the band axis is — gray, rgb, rgba…; the image as stored is that; empty when the bands are no colour model"`
	Axes    []AxisInfo    `desc:"the axes other than rows and columns, slowest first; each lists at most 32 labels and coordinates, get_axis the rest"`
	Shown   SelectionInfo `desc:"the selection the image shown was taken at; read_pixels, measure and send_to_play are of it"`
	Asked   SelectionInfo `desc:"the selection last set; equal to Shown unless Pending"`
	Pending bool          `desc:"a selection was set and is being taken; set_position and set_bands settle when it is shown"`
}

// stackInfo is the stack as get_view reports it; nil for none.
func stackInfo(stk *stackView, want selection, pending bool) *StackInfo {
	if stk == nil {
		return nil
	}
	st := stk.stack
	out := &StackInfo{Name: st.Provenance().SourceName, Kind: st.Kind().String(), Planes: st.PlaneCount(), Bytes: st.Bytes(),
		Shown: selectionInfo(stk, stk.shown), Asked: selectionInfo(stk, want), Pending: pending}
	if m := st.Model(); m != 0 {
		out.Model = m.String()
	}
	for i := range stk.axes {
		out.Axes = append(out.Axes, axisInfo(stk, i, 0, axisInlineMax))
	}
	return out
}

// GetAxisArgs names a page of an axis.
type GetAxisArgs struct {
	Axis  string `desc:"the axis's name, from get_view's Stack.Axes"`
	From  int    `json:",omitzero" desc:"the first position to list"`
	Count int    `json:",omitzero" desc:"how many positions to list, at most 256; 0 lists 64"`
}

// SetPositionArgs sets the position on a selector axis.
type SetPositionArgs struct {
	Axis     string `desc:"the selector axis's name, from get_view's Stack.Axes"`
	Position int    `json:",omitzero" desc:"the position, 0 to the axis's size - 1"`
}

// SetBandsArgs sets the bands shown.
type SetBandsArgs struct {
	Bands []int `json:",omitzero" desc:"band positions: one, shown as a single plane through a colour map; three, shown as red, green and blue under one recipe; or none, for the image as stored when the stack's bands are a colour model"`
}

// SelectionOutcome is what set_position and set_bands settle with.
type SelectionOutcome struct {
	Phase     string        `desc:"the phase the change ended in"`
	Selection SelectionInfo `desc:"the selection shown now"`
	Note      string        `json:",omitzero" desc:"what the change kept of the view, or had to change"`
	Error     string        `json:",omitzero" desc:"why the selection could not be taken; the one shown stayed"`
}

// PixelPoint is a point of the image in continuous pixel coordinates: pixel
// (col, row) covers [col, col+1) × [row, row+1), row 0 at the top; the
// centre of pixel (3, 4) is (3.5, 4.5).
type PixelPoint struct {
	Col float64 `json:",omitzero" desc:"continuous column"`
	Row float64 `json:",omitzero" desc:"continuous row; 0 is the top edge"`
}

// WorldPoint is a point in the frame of the image's placement.
type WorldPoint struct {
	A float64 `json:",omitzero" desc:"the first frame coordinate: easting, x, or longitude (RA) in decimal degrees"`
	B float64 `json:",omitzero" desc:"the second frame coordinate: northing, y, or latitude (Dec) in decimal degrees"`
}

// FrameReading is a point both ways: pixels and frame.
type FrameReading struct {
	Col       float64   `desc:"continuous column"`
	Row       float64   `desc:"continuous row"`
	PixelCol  int       `desc:"the pixel the point falls in: floor(Col)"`
	PixelRow  int       `desc:"floor(Row)"`
	Inside    bool      `desc:"the pixel is one of the image's"`
	A         float64   `desc:"the first frame coordinate"`
	B         float64   `desc:"the second frame coordinate"`
	AxisNames [2]string `desc:"the coordinates' names"`
	Units     [2]string `desc:"the coordinates' units; deg on the sky"`
	Code      string    `json:",omitzero" desc:"the frame's system as the file names it"`
	Frame     string    `desc:"the frame's kind, as get_view's Image.Placement.Frame"`
	Text      string    `desc:"the frame coordinates as the window's readout writes them"`
}

// frameReading is a point read both ways under a placement.
func frameReading(p imaging.Placement, w, h int, col, row float64) (out FrameReading, ok bool) {
	pos, ok := imageview.FramePositionAt(p, col, row)
	if !ok {
		return out, false
	}
	out = FrameReading{Col: col, Row: row, PixelCol: int(math.Floor(col)), PixelRow: int(math.Floor(row)),
		A: pos.A, B: pos.B, AxisNames: [2]string{pos.AxisName(0), pos.AxisName(1)}, Units: p.Frame.Units,
		Code: p.Frame.Code, Frame: p.Frame.Kind.String(), Text: pos.Text()}
	if p.Frame.Kind == imaging.FrameKindCelestialGnomonic {
		out.Units = [2]string{"deg", "deg"}
	}
	out.Inside = out.PixelCol >= 0 && out.PixelRow >= 0 && out.PixelCol < w && out.PixelRow < h
	return out, true
}

// errNoPlacement is the refusal of a frame query for an image nothing
// places.
var errNoPlacement = app.RefuseOperation("the image has no placement: nothing in its file says where its pixels are, so there are no frame coordinates — column and row are all there is")

// Pixels is read_pixels's result: a block of samples, bounded.
type Pixels struct {
	Region    RegionArgs     `desc:"the block returned, clipped to the image and to 1024 pixels"`
	Planes    []string       `desc:"plane names, in the order of Values"`
	Values    [][]float64    `desc:"one list per plane: the block's values in row-major order; an invalid sample is 0 and counted in Invalid"`
	Invalid   int            `desc:"invalid samples in the block: NaN in a float plane, and the stored value the planes declare invalid"`
	Truncated bool           `desc:"the block asked for was larger than the one returned"`
	Selection *SelectionInfo `json:",omitzero" desc:"for a stack: the selection the values were read from — the one shown, which may differ from one set since"`
}

// Measurement is measure's result.
type Measurement struct {
	Region    RegionArgs        `desc:"the region measured"`
	Pixels    int64             `desc:"pixels in the region"`
	Planes    []PlaneStatistics `desc:"one entry per plane"`
	Method    string            `desc:"what the numbers are and what they assume"`
	Selection *SelectionInfo    `json:",omitzero" desc:"for a stack: the selection the statistics were computed on — the one shown, which may differ from one set since"`
}

// OpenSampleArgs names a sample image.
type OpenSampleArgs struct {
	Name string `desc:"field (1400 × 1000, 16-bit, one plane), colour (640 × 480, 8-bit rgba), small (48 × 32, 8-bit, one plane), float (640 × 480, 32-bit float with a block of invalid samples), scaled (480 × 320, 16-bit under a value transform), elevation (400 × 300, signed 16-bit metres with a border declared invalid), double (320 × 240, 64-bit float on an offset 32-bit floats cannot resolve), counts (320 × 240, signed 32-bit with a corner declared invalid), or the stack bands (160 × 120, 16-bit, a time axis of three and a band axis B2, B3, B4, B8, 0 declared invalid, placed in EPSG:32632 at 10 m a pixel)"`
}

// OpenOutcome is what open_sample settles with.
type OpenOutcome struct {
	Phase string `desc:"the phase the open ended in"`
	Image string `desc:"the image now open, in one line"`
	Error string `desc:"why the open failed; empty otherwise"`
}

// SetPlaneArgs names what to show.
type SetPlaneArgs struct {
	Plane   string `json:",omitzero" desc:"as stored, or a plane name from get_view's Image.Planes; not read when ByIndex"`
	ByIndex bool   `json:",omitzero" desc:"show the plane at Index instead: the way to name one of two planes of one band"`
	Index   int    `json:",omitzero" desc:"with ByIndex: the plane's index in get_view's Image.Planes"`
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
func imageInfo(pic *tiles.Store) (out ImageInfo) {
	if pic == nil {
		return
	}
	p := pic.Provenance()
	out = ImageInfo{Open: true, Name: p.SourceName, Width: pic.Width(), Height: pic.Height(),
		Model: pic.Model().String(), Kind: pic.Kind().String(), Planes: pic.PlaneLabels(),
		Format: p.Format, StoredBitDepth: int(p.StoredBitDepth), Lossy: p.Lossy, Notes: storeNotes(pic),
		Placement: placementInfo(pic.Placement())}
	out.InvalidValue, out.HasInvalid = pic.Invalid()
	if p.SampleHash != ([32]byte{}) {
		out.SampleHash = hex.EncodeToString(p.SampleHash[:])
	}
	return out
}

// recipeArgs is a recipe as set_recipe takes it.
func recipeArgs(r display.Recipe) (out SetRecipeArgs) {
	out = SetRecipeArgs{Limits: r.Limits.Source.String(), Lo: r.Limits.Lo, Hi: r.Limits.Hi,
		PercentLo: r.Limits.PercentLo, PercentHi: r.Limits.PercentHi, ZScaleContrast: r.Limits.ZScaleContrast, ZScaleSamples: r.Limits.ZScaleSamples,
		Transfer: r.Transfer.String(), Param: r.Param, Palette: r.Palette, Invert: r.Invert, FlagClipped: r.FlagClipped}
	if out.Palette == "" {
		out.Palette = imageview.PaletteGray
	}
	return
}

// viewState is the view resource's value.
func (inst *App) viewState() (out ViewState) {
	if inst.pic == nil {
		return
	}
	out.Plane = planeAsStored
	if i, single := inst.viewer.Plane(); single {
		out.Plane, out.PlaneIndex = inst.pic.PlaneLabels()[i], i
	}
	out.Recipe = recipeArgs(inst.viewer.Recipe())
	if args, ok := inst.compositeArgsNow(); ok {
		out.Plane, out.PlaneIndex, out.Composite = planeComposite, 0, &args
	}
	if inst.compAsk != nil {
		asked := inst.compAsk.args
		out.Plane, out.Composite = planeComposite, &asked
	}
	r, _ := inst.viewer.Region()
	out.Region = regionArgsOf(r)
	if inst.stk != nil {
		sel := selectionInfo(inst.stk, inst.want)
		out.Selection = &sel
	}
	return
}

// setRecipe applies a whole recipe to the view: every field is set, none is
// toggled, so the same arguments twice leave the same view. Percentile
// limits are counted over the whole plane off the frame, and zscale limits
// of a store are sampled off it, so they land a little after the call;
// get_view reports the limits in force. Of a composite the recipe is the
// layer in hand's, and every layer's when it is linked: data limits are
// then the range of the shown layers' planes together, and percentile and
// zscale limits are found from the layer in hand's plane (ADR-0401 §SD2).
func (inst *App) setRecipe(in SetRecipeArgs) (err error) {
	if inst.pic == nil {
		return errNoImage
	}
	var planes []int
	switch layers, composite := inst.viewer.Composite(); {
	case composite:
		hand, _ := inst.viewer.LayerInHand()
		planes = []int{layers[hand].Plane}
		if inst.viewer.CompositeLinked() {
			planes = planes[:0]
			for _, l := range layers {
				if !l.Hidden {
					planes = append(planes, l.Plane)
				}
			}
			if len(planes) == 0 {
				planes = append(planes, layers[hand].Plane)
			}
		}
	case inst.viewer.IsSinglePlane():
		i, _ := inst.viewer.Plane()
		planes = []int{max(i, 0)}
	default:
		// A colour image as stored: its limits are one set for its colour
		// planes, and data limits are their range together.
		planes = []int{0, 1, 2}
	}
	single := inst.viewer.IsSinglePlane()
	recipe, err := inst.parseRecipe(in, inst.viewer.Recipe(), planes, single, single || inst.viewer.IsComposite())
	if err != nil {
		return err
	}
	if err = inst.viewer.SetRecipe(recipe); err != nil {
		return refuse(err)
	}
	inst.syncChecks()
	return nil
}

// parseRecipe reads set_recipe's arguments over a base recipe: data limits
// are the range of the given planes together, nominal limits the kind's
// range through the first one's transform; percentile and zscale limits
// carry what to find them with, which the viewer does, and are refused
// unless measured — a single plane or a composite's layer, whose limits are
// one plane's. A colour map is read for a single plane only.
func (inst *App) parseRecipe(in SetRecipeArgs, base display.Recipe, planes []int, single, measured bool) (recipe display.Recipe, err error) {
	recipe = base
	if err = recipe.Transfer.UnmarshalText([]byte(in.Transfer)); err != nil {
		names := make([]string, 0, len(display.AllTransfers))
		for _, t := range display.AllTransfers {
			names = append(names, t.String())
		}
		return recipe, app.RefuseOperation("no transfer function named " + in.Transfer + "; the names are " + strings.Join(names, ", "))
	}
	recipe.Param, recipe.Invert, recipe.FlagClipped = in.Param, in.Invert, in.FlagClipped
	if single {
		recipe.Palette = in.Palette
		if in.Palette != "" && !slices.Contains(imageview.PaletteNames(), in.Palette) {
			return recipe, app.RefuseOperation("no colour map named " + in.Palette + "; the names are " + strings.Join(imageview.PaletteNames(), ", "))
		}
	}
	switch in.Limits {
	case limitsData:
		for _, i := range planes {
			if _, _, ok := inst.pic.StoredRange(i); !ok {
				return recipe, app.RefuseOperation("the plane shown has no range to take limits from yet; it is still being read, or has no valid sample; set them manually")
			}
		}
		l, ok := dataLimitsOver(inst.pic, planes)
		if !ok {
			return recipe, app.RefuseOperation("the plane shown holds one value; there is no range to take limits from")
		}
		recipe.Limits = l
	case limitsNominal:
		l, ok := display.NominalLimits(inst.pic.Kind(), inst.pic.Transform(planes[0]))
		if !ok {
			return recipe, app.RefuseOperation("a float plane has no nominal range; use data, percentile, zscale or manual")
		}
		recipe.Limits = l
	case limitsManual:
		if math.IsNaN(in.Lo) || math.IsNaN(in.Hi) || !(in.Hi > in.Lo) {
			return recipe, app.RefuseOperation("manual limits need Hi above Lo")
		}
		recipe.Limits = display.Limits{Lo: in.Lo, Hi: in.Hi, Source: display.LimitsSourceManual}
	case limitsPercentile:
		if !measured {
			return recipe, app.RefuseOperation("percentile limits apply to a single plane or a composite's layer; set_plane shows one, set_composite makes a composite")
		}
		if !(in.PercentLo >= 0 && in.PercentLo < in.PercentHi && in.PercentHi <= 100) {
			return recipe, app.RefuseOperation("percentiles are an ascending pair within 0 and 100")
		}
		recipe.Limits = display.Limits{Source: display.LimitsSourcePercentile, PercentLo: in.PercentLo, PercentHi: in.PercentHi}
	case limitsZScale:
		if !measured {
			return recipe, app.RefuseOperation("zscale limits apply to a single plane or a composite's layer; set_plane shows one, set_composite makes a composite")
		}
		if !(in.ZScaleContrast >= 0) || math.IsInf(in.ZScaleContrast, 0) || in.ZScaleSamples < 0 || in.ZScaleSamples > imageview.MaxZScaleSamples {
			return recipe, app.RefuseOperation(fmt.Sprintf("zscale takes a positive contrast and a sample count up to %d, or 0 for the defaults of %g and %d", imageview.MaxZScaleSamples, display.ZScaleContrast, display.ZScaleSamples))
		}
		recipe.Limits = display.Limits{Source: display.LimitsSourceZScale, ZScaleContrast: in.ZScaleContrast, ZScaleSamples: in.ZScaleSamples}
	default:
		return recipe, app.RefuseOperation("limits are data, nominal, percentile, zscale or manual")
	}
	return recipe, nil
}

// setPlane shows the image as stored or one named plane of it, under that
// view's default recipe with the colour map kept.
func (inst *App) setPlane(name string) (err error) {
	if inst.pic == nil {
		return errNoImage
	}
	if name == planeAsStored {
		err = inst.viewer.SetAsStored()
	} else {
		labels := inst.pic.PlaneLabels()
		i := slices.Index(labels, name)
		if i < 0 {
			return app.RefuseOperation("no plane named " + name + "; this image has " + strings.Join(labels, ", ") + ", and as stored")
		}
		err = inst.viewer.SetPlane(i)
	}
	inst.syncChecks()
	return refuse(err)
}

// setPlaneIndex shows the plane at an index by itself: the way to name one
// of two planes that carry one band's label.
func (inst *App) setPlaneIndex(i int) error {
	if inst.pic == nil {
		return errNoImage
	}
	labels := inst.pic.PlaneLabels()
	if i < 0 || i >= len(labels) {
		return app.RefuseOperation(fmt.Sprintf("plane %d is outside this image, whose planes are 0 to %d: %s", i, len(labels)-1, strings.Join(labels, ", ")))
	}
	err := inst.viewer.SetPlane(i)
	inst.syncChecks()
	return refuse(err)
}

// setRegion marks a region, or clears the mark for the zero region. A
// region that reaches outside the image is refused, naming the image's
// size, so that what is measured is what was asked for.
func (inst *App) setRegion(in RegionArgs) (err error) {
	if inst.pic == nil {
		return errNoImage
	}
	if in.isZero() {
		inst.viewer.SetRegion(image.Rectangle{})
		return nil
	}
	if err = checkRegion(inst.pic, in); err != nil {
		return err
	}
	inst.viewer.SetRegion(in.rect())
	return nil
}

// checkRegion refuses a region that is empty or not inside the image.
func checkRegion(pic *tiles.Store, in RegionArgs) error {
	if in.Width <= 0 || in.Height <= 0 {
		return app.RefuseOperation("a region's width and height are positive")
	}
	if !in.rect().In(image.Rect(0, 0, pic.Width(), pic.Height())) {
		return app.RefuseOperation(fmt.Sprintf("the region reaches outside the image, which is %d columns by %d rows", pic.Width(), pic.Height()))
	}
	return nil
}

// setZoom fits the image or shows it at one sample per screen pixel.
func (inst *App) setZoom(mode string) error {
	if inst.pic == nil {
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
	pic           *tiles.Store
	view          ViewState
	magnification float64
	level         int
	levelsReady   bool
	limitsPending bool
	mode          string
	composite     *CompositeInfo
	clipped       *ClippedInfo
	opening       bool
	openErr       string
	openNote      string
	problem       string
	measured      *measurement
	measureErr    string
	handover      Handover
	seedErr       string
	// stk is the stack the image shown is a selection of, nil for none;
	// want is the selection asked for, pending that it is not shown yet.
	stk        *stackView
	want       selection
	pending    bool
	selectNote string
	selectErr  string
	// stkShown is stk when the image shown is its selection, nil when it
	// is a result.
	stkShown *stackView

	// The regions, the images and the work on them, as the frame left
	// them: the slices and the maps' values are never modified in place.
	regs            []regionEntry
	selRegion       string
	tool            toolE
	regMeasures     map[string]*regionMeasure
	profile         *regionProfile
	regionMeasuring bool
	profiling       bool
	regionsOut      Handover
	profileOut      Handover
	images          []imageEntry
	shownId         string
	running         bool
	runErr          string
	runNote         string
}

// runSource is the snapshot's images as a plan reads them.
func (inst opsSnap) runSource() runSource {
	return runSource{images: inst.images, shownId: inst.shownId, stk: inst.stk}
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn = opsSnap{phase: inst.observePhase(), pic: inst.pic, view: inst.viewState(), opening: inst.open.Running(),
		openErr: inst.openErr, openNote: inst.openNote, problem: inst.problem, measured: inst.measured, seedErr: inst.seedErr,
		stk: inst.stk, want: inst.want.clone(), pending: inst.selectionPending(), selectNote: inst.selectNote, selectErr: inst.selectErr,
		stkShown: inst.stackShown(), regs: inst.regs, selRegion: inst.selRegion, tool: inst.tool, profile: inst.profile,
		regionMeasuring: inst.regionJob.Running(), profiling: inst.profileJob.Running(), images: inst.images, shownId: inst.shownId,
		running: inst.runJob.Running(), runErr: inst.runErr, runNote: inst.runNote}
	sn.regMeasures = make(map[string]*regionMeasure, len(inst.regMeasures))
	for k, v := range inst.regMeasures {
		sn.regMeasures[k] = v
	}
	sn.regionsOut.Busy, sn.regionsOut.Note, sn.regionsOut.Error = inst.regionsOut.state()
	sn.profileOut.Busy, sn.profileOut.Note, sn.profileOut.Error = inst.profileOut.state()
	if inst.pic != nil {
		sn.magnification, sn.level, sn.levelsReady = inst.viewer.Magnification(), inst.viewer.Level(), inst.viewer.LevelsReady()
		sn.limitsPending = inst.viewer.LimitsPending()
		sn.mode, sn.composite, sn.clipped = inst.viewMode(), inst.compositeInfo(), inst.clippedInfo()
	}
	if inst.measureErr != nil {
		sn.measureErr = inst.measureErr.Error()
	}
	sn.handover.Busy, sn.handover.Note, sn.handover.Error = inst.handoverState()
	return
}

// selectionPending reports a selection asked for that is not shown yet.
func (inst *App) selectionPending() bool {
	return inst.stk != nil && (inst.selBusy || !inst.want.equal(inst.stk.shown))
}

// shownSelection is the selection a store of a stack was taken at, for a
// result to name; nil for an image that is no stack's.
func shownSelection(stk *stackView) *SelectionInfo {
	if stk == nil {
		return nil
	}
	sel := selectionInfo(stk, stk.shown)
	return &sel
}

// readPixels answers read_pixels from the store: the block clipped to the
// image, then to the bound by dropping whole rows, and read out of the
// tiles, waiting for any not read yet.
func readPixels(pic *tiles.Store, in RegionArgs) (out Pixels, err error) {
	if in.Width == 0 && in.Height == 0 {
		in.Width, in.Height = 1, 1
	}
	if in.Width <= 0 || in.Height <= 0 {
		return out, app.RefuseOperation("a block's width and height are positive; both 0 reads one pixel")
	}
	r := in.rect().Intersect(image.Rect(0, 0, pic.Width(), pic.Height()))
	if r.Empty() {
		return out, app.RefuseOperation(fmt.Sprintf("the block is outside the image, which is %d columns by %d rows", pic.Width(), pic.Height()))
	}
	out.Truncated = r != in.rect()
	if r.Dx() > opsMaxPixels {
		r.Max.X, out.Truncated = r.Min.X+opsMaxPixels, true
	}
	if rows := opsMaxPixels / r.Dx(); r.Dy() > rows {
		r.Max.Y, out.Truncated = r.Min.Y+rows, true
	}
	out.Region = regionArgsOf(r)
	block, err := pic.Read(context.Background(), r)
	if err != nil {
		return out, refuse(err)
	}
	out.Planes = pic.PlaneLabels()
	out.Values = make([][]float64, block.PlaneCount())
	for i := range out.Values {
		p := block.Plane(i)
		vals := make([]float64, 0, r.Dx()*r.Dy())
		for y := range r.Dy() {
			for x := range r.Dx() {
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

// measurementOf renders a measurement as measure's answer, naming the
// selection of a stack it was computed on.
func measurementOf(m *measurement, stk *stackView) Measurement {
	return Measurement{Region: regionArgsOf(m.region), Pixels: int64(m.region.Dx()) * int64(m.region.Dy()),
		Planes: slices.Clone(m.planes), Method: m.method(), Selection: shownSelection(stk)}
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resImage, "the image open: its name, size, colour model, sample kind and where its samples came from", func(inst *App) any { return imageInfo(inst.pic) })
	s.Resource(resView, "what is shown of the image and how: the plane or the composite, the display recipe and the marked region", func(inst *App) any { return inst.viewState() })
	s.Restorable(resView, func(inst *App, v any) bool {
		vs, ok := v.(ViewState)
		if !ok || inst.pic == nil {
			return false
		}
		// A selection is asked for first; the plane and the recipe set on the
		// image shown are kept when it lands, the view being of one kind.
		if vs.Selection != nil && inst.stk != nil {
			sel, ok := selectionOf(inst.stk, *vs.Selection)
			if !ok {
				return false
			}
			inst.setWant(sel)
		}
		if vs.Composite != nil {
			// The composite carries its own recipes, the link with them.
			if inst.setComposite(*vs.Composite) != nil {
				return false
			}
			return inst.setRegion(vs.Region) == nil
		}
		plane := inst.setPlane(vs.Plane)
		if vs.Plane != planeAsStored && vs.PlaneIndex > 0 {
			plane = inst.setPlaneIndex(vs.PlaneIndex)
		}
		if plane != nil || inst.setRecipe(vs.Recipe) != nil {
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
		Summary: "read the phase, the image open, how it is shown — as stored, one plane, or a composite of layers — and the recipe, the share of samples the limits clip, the marked region, the magnification and the level drawn",
		Reads:   []string{resImage, resView}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{Phase: sn.phase.String(), Image: imageInfo(sn.pic), View: sn.view, Mode: sn.mode, Composite: sn.composite, Clipped: sn.clipped,
				Magnification: sn.magnification,
				Level:         sn.level, LevelsReady: sn.levelsReady, LimitsPending: sn.limitsPending, Error: sn.openErr, Note: sn.openNote, Refused: sn.problem,
				Measured: sn.measured != nil, MeasureError: sn.measureErr, Handover: sn.handover, SeedProblem: sn.seedErr,
				StackSelection: sn.stkShown != nil, Stack: stackInfo(sn.stk, sn.want, sn.pending),
				SelectionNote: sn.selectNote, SelectionError: sn.selectErr,
				ShownImage: sn.shownId, Images: len(sn.images), Running: sn.running, RunError: sn.runErr, RunNote: sn.runNote,
				Regions: len(sn.regs), SelectedRegion: sn.selRegion, Tool: sn.tool.String(),
				RegionMeasuring: sn.regionMeasuring, Profiling: sn.profiling, RegionsHandover: sn.regionsOut, ProfileHandover: sn.profileOut}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opReadPixels, Version: 1,
		Summary: "read a block of the image's samples, every plane, at most 1024 pixels; the values are the samples themselves, whatever the recipe shows",
		Reads:   []string{resImage}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in RegionArgs) (out Pixels, err error) {
			if sn.pic == nil {
				return out, errNoImage
			}
			if out, err = readPixels(sn.pic, in); err == nil {
				out.Selection = shownSelection(sn.stkShown)
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opMeasure, Version: 1,
		Summary: "statistics of every plane over a region, from the samples at full resolution: count, range, mean, standard deviation, median, 1st and 99th percentile, with the method stated; the zero region means the marked one",
		Reads:   []string{resImage, resView}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in RegionArgs) (out Measurement, err error) {
			if sn.pic == nil {
				return out, errNoImage
			}
			if in.isZero() {
				in = sn.view.Region
				if in.isZero() {
					return out, app.RefuseOperation("no region is marked; pass one, or mark one with set_region")
				}
			}
			if err = checkRegion(sn.pic, in); err != nil {
				return out, err
			}
			if m := sn.measured; m != nil && m.pic == sn.pic && m.region == in.rect() {
				return measurementOf(m, sn.stkShown), nil
			}
			m, merr := measureRegion(context.Background(), sn.pic, in.rect(), func(uint64, uint64, string) {})
			if merr != nil {
				return out, refuse(merr)
			}
			return measurementOf(m, sn.stkShown), nil
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
		return OpenOutcome{Phase: inst.observePhase().String(), Image: inst.describeShown(), Error: inst.openErr}, true, nil
	})

	needsImage := func(sn opsSnap) (ok bool, reason string) {
		if sn.pic == nil {
			return false, "there is no image; open_sample opens one, or the person opens a file with Open…"
		}
		return true, ""
	}

	appops.Command(s, app.OperationSpec{Name: opSetRecipe, Version: 1,
		Summary: "set the display recipe, whole: limits, transfer function, colour map, inversion and clipping marks; of a composite, the layer in hand's, or every layer's when it is linked; the samples are not changed",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true,
		Gesture: "the Limits buttons, and the controls under Advanced", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"percentile limits are computed from the plane's histogram, counted over the whole plane off the frame, and zscale limits from a sample of about a thousand of its values, read off the frame for a store; until they are, get_view's LimitsPending is true and the limits in force stay",
			"of a linked composite, data limits are the range of the shown layers' planes together, and percentile and zscale limits are found from the layer in hand's plane",
			"a name that is not a transfer function or a colour map is refused naming the ones that are"}},
		func(inst *App, _ app.OperationCall, in SetRecipeArgs) (out appops.None, err error) {
			err = inst.setRecipe(in)
			return
		})
	s.Available(opSetRecipe, needsImage)

	appops.Command(s, app.OperationSpec{Name: opSetPlane, Version: 1,
		Summary: "show the image as stored, or one plane of it by itself through a colour map, named or by its index",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true,
		Gesture: "the Show buttons", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"the recipe returns to that view's default, keeping the colour map; set_recipe after it"}},
		func(inst *App, _ app.OperationCall, in SetPlaneArgs) (out appops.None, err error) {
			if in.ByIndex {
				err = inst.setPlaneIndex(in.Index)
				return
			}
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
			if _, err = handoverRegion(inst.pic, region); err != nil {
				return
			}
			inst.startHandover()
			return
		})
	needsStack := func(sn opsSnap) (ok bool, reason string) {
		switch {
		case sn.pic == nil:
			return false, "there is no image; open_sample opens one, or the person opens a file with Open…"
		case sn.stk == nil:
			return false, "the image open is not a stack's: open_sample with bands opens one, or the person opens a TIFF, FITS, NPY or NPZ file"
		}
		return true, ""
	}

	appops.Query(s, app.OperationSpec{Name: opGetAxis, Version: 1,
		Summary: "list the labels and coordinates of a stack's axis from a position, at most 256: get_view lists 32 of each axis",
		Reads:   []string{resImage}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetAxisArgs) (out AxisInfo, err error) {
			if ok, reason := needsStack(sn); !ok {
				return out, app.RefuseOperation(reason)
			}
			i := sn.stk.axisIndex(in.Axis)
			if i < 0 {
				names := make([]string, len(sn.stk.axes))
				for k, a := range sn.stk.axes {
					names[k] = a.Name
				}
				return out, app.RefuseOperation("no axis named " + strconv.Quote(in.Axis) + "; the axes are " + quotedList(names))
			}
			if in.From < 0 || in.From >= sn.stk.axes[i].Size {
				return out, app.RefuseOperation(fmt.Sprintf("position %d is outside %q, whose positions are 0 to %d", in.From, in.Axis, sn.stk.axes[i].Size-1))
			}
			count := in.Count
			if count <= 0 {
				count = 64
			}
			return axisInfo(sn.stk, i, in.From, min(count, axisPageMax)), nil
		})

	appops.Command(s, app.OperationSpec{Name: opSetPosition, Version: 1,
		Summary: "show the plane at a position of a stack's selector axis — a time, a depth, a page; the call settles when it is shown",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true, Async: true,
		Gesture: "the stepper and the slider of the axis's row", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"the view, the marked region and, while the view stays of one kind, the plane and the recipe are kept; limits taken from the data are taken again from the new plane, and get_view's SelectionNote says so",
			"read_pixels, measure and send_to_play are of the selection shown, which each result names"}},
		func(inst *App, _ app.OperationCall, in SetPositionArgs) (out appops.None, err error) {
			err = inst.setPosition(in.Axis, in.Position)
			return
		})
	appops.Settles(s, opSetPosition, settleSelection)
	s.Available(opSetPosition, needsStack)

	appops.Command(s, app.OperationSpec{Name: opSetBands, Version: 1,
		Summary: "show one band of a stack through a colour map, three as red, green and blue under one recipe, or none for the image as stored; the call settles when it is shown",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true, Async: true,
		Gesture: "the Bands row: as stored, one band, three bands, and the band pickers", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"three bands of signed 32-bit or float samples open as a linked composite in red, green and blue over the range of their data, as set_composite would show them; the result's Note says so",
			"three bands of a stack whose bands are no colour model open over the range of their data together, linear, one recipe for all three; SelectionNote says so",
			"from one band to three, or back, the view changes kind and opens under its default recipe"}},
		func(inst *App, _ app.OperationCall, in SetBandsArgs) (out appops.None, err error) {
			err = inst.setBands(in.Bands)
			return
		})
	appops.Settles(s, opSetBands, settleSelection)
	s.Available(opSetBands, needsStack)

	appops.Query(s, app.OperationSpec{Name: opToWorld, Version: 1,
		Summary: "the frame coordinates of a point given in continuous pixel coordinates, under the image's placement: a reference system's own coordinates, or decimal degrees on the sky",
		Reads:   []string{resImage}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in PixelPoint) (out FrameReading, err error) {
			if sn.pic == nil {
				return out, errNoImage
			}
			p := sn.pic.Placement()
			if p.IsZero() {
				return out, errNoPlacement
			}
			out, ok := frameReading(p, sn.pic.Width(), sn.pic.Height(), in.Col, in.Row)
			if !ok {
				return out, app.RefuseOperation("the frame has no finite coordinates at that point")
			}
			return out, nil
		})

	appops.Query(s, app.OperationSpec{Name: opToPixel, Version: 1,
		Summary: "the continuous pixel coordinates of a point given in the frame of the image's placement, and the pixel it falls in",
		Reads:   []string{resImage}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in WorldPoint) (out FrameReading, err error) {
			if sn.pic == nil {
				return out, errNoImage
			}
			p := sn.pic.Placement()
			if p.IsZero() {
				return out, errNoPlacement
			}
			col, row, ok := p.Pixel(in.A, in.B)
			if !ok {
				if p.Frame.Kind == imaging.FrameKindCelestialGnomonic {
					return out, app.RefuseOperation(fmt.Sprintf("longitude %g, latitude %g is on the hemisphere opposite the reference point, which the gnomonic projection does not show", in.A, in.B))
				}
				return out, app.RefuseOperation("the point has no pixel coordinates under this placement")
			}
			out, _ = frameReading(p, sn.pic.Width(), sn.pic.Height(), col, row)
			out.A, out.B = in.A, in.B
			return out, nil
		})

	s.Available(opSendToPlay, func(sn opsSnap) (ok bool, reason string) {
		switch {
		case sn.pic == nil:
			return false, "there is no image; open_sample opens one, or the person opens a file with Open…"
		case sn.handover.Busy:
			return false, "a handover is in flight"
		}
		return true, ""
	})
	regionOperations(s)
	runOperations(s)
	compositeOperations(s)
	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)

// settleSelection settles set_position and set_bands once the selection
// asked for is shown, or was dropped.
func settleSelection(inst *App, _ app.OperationCall) (out SelectionOutcome, settled bool, err error) {
	if inst.selectionPending() {
		return out, false, nil
	}
	out = SelectionOutcome{Phase: inst.observePhase().String(), Note: inst.selectNote, Error: inst.selectErr}
	if inst.stk != nil {
		out.Selection = selectionInfo(inst.stk, inst.stk.shown)
	}
	return out, true, nil
}

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
	case SetPositionArgs:
		gestureOr(inst, op, a, apply)
	case SetBandsArgs:
		gestureOr(inst, op, a, apply)
	case CompositeArgs:
		gestureOr(inst, op, a, apply)
	case SetLayerArgs:
		gestureOr(inst, op, a, apply)
	case ImageIdArgs:
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
