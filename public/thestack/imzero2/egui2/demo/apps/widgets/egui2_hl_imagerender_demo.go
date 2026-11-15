package widgets

import (
	"context"
	"fmt"
	"image"
	"math"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/science/imaging"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imageview"
)

// The rendering-source demo (ADR-0374 M4): imageview showing something that
// is drawn when asked rather than held — a test chart computed from its
// coordinates at whatever level the view needs, sharp at sixteen times —
// with a host overlay of labelled marks, a page turn that keeps the view,
// and a count of the tiles rendered, which a contrast change does not move.

// chartCentre and chartSize place the test chart's star, in level-0 pixels.
var (
	chartCentre = [2]float64{1000, 700}
	chartSize   = image.Pt(2000, 1400)
)

// chartSource renders the chart: a 72-spoke star, rings every 100 pixels a
// third of a pixel wide, and a grid every 50 pixels a fifth of a pixel wide,
// which only a magnified render resolves. Each cell is the mean of four
// samples of the chart inside it. Page 2 draws its rings in another colour.
type chartSource struct {
	info     imageview.SourceInfo
	page     atomic.Int32
	rendered atomic.Int64
}

func newChartSource() *chartSource {
	s := &chartSource{info: imageview.SourceInfo{Width: chartSize.X, Height: chartSize.Y,
		Model: imaging.ColorModelRGB, Kind: imaging.SampleKindU8, Reduced: 3, Magnified: 4, Rendered: true,
		Provenance: imaging.Provenance{SourceName: "test chart"}}}
	s.page.Store(1)
	return s
}

func (inst *chartSource) Info() imageview.SourceInfo { return inst.info }

func (inst *chartSource) Tile(ctx context.Context, level, col, row int) (d imageview.TileData, err error) {
	inst.rendered.Add(1)
	rect := imageview.TileRect(inst.info, level, col, row)
	scale := math.Ldexp(1, level) // cells per level-0 pixel
	page := inst.page.Load()
	n := rect.Dx() * rect.Dy()
	var planes [3][]uint8
	for i := range planes {
		planes[i] = make([]uint8, n)
	}
	for y := range rect.Dy() {
		if err = ctx.Err(); err != nil {
			return d, err
		}
		for x := range rect.Dx() {
			var sum [3]float64
			for _, o := range [4][2]float64{{0.25, 0.25}, {0.75, 0.25}, {0.25, 0.75}, {0.75, 0.75}} {
				u := (float64(rect.Min.X+x) + o[0]) / scale
				v := (float64(rect.Min.Y+y) + o[1]) / scale
				rgb := chartAt(u, v, page)
				for i := range sum {
					sum[i] += rgb[i]
				}
			}
			for i := range planes {
				planes[i][y*rect.Dx()+x] = uint8(sum[i]/4 + 0.5)
			}
		}
	}
	ps := make([]imaging.Plane, 3)
	for i := range ps {
		if ps[i], err = imaging.NewPlaneU8(rect.Dx(), rect.Dy(), planes[i]); err != nil {
			return d, err
		}
	}
	img, err := imaging.NewImage(imaging.ColorModelRGB, ps, imaging.Provenance{})
	return imageview.TileData{Samples: img, Region: image.Rect(0, 0, rect.Dx(), rect.Dy())}, err
}

// chartAt is the chart's colour at a point of level-0 pixels.
func chartAt(u, v float64, page int32) [3]float64 {
	dx, dy := u-chartCentre[0], v-chartCentre[1]
	r := math.Hypot(dx, dy)
	if r < 650 {
		if ring := math.Abs(r - 100*math.Round(r/100)); r > 50 && ring < 1.0/6 {
			if page == 2 {
				return [3]float64{40, 160, 70}
			}
			return [3]float64{200, 40, 40}
		}
	}
	if math.Mod(u, 50) < 0.2 || math.Mod(v, 50) < 0.2 {
		return [3]float64{60, 90, 200}
	}
	if r < 600 && int(math.Floor((math.Atan2(dy, dx)+math.Pi)/(2*math.Pi)*72))%2 == 0 {
		return [3]float64{30, 30, 40}
	}
	return [3]float64{240, 240, 236}
}

// chartMarks are what the overlay labels, in level-0 pixels.
var chartMarks = []struct {
	label string
	at    [2]float64
}{
	{"star centre", chartCentre},
	{"ring 300", [2]float64{chartCentre[0] + 300, chartCentre[1]}},
	{"grid corner", [2]float64{100, 100}},
}

type imageRenderDemoState struct {
	v        *imageview.Viewer
	src      *chartSource
	ids      *c.WidgetIdStack
	scrolls  bool
	inverted bool
}

func newImageRenderDemoState(ids *c.WidgetIdStack) *imageRenderDemoState {
	st := &imageRenderDemoState{src: newChartSource(), ids: ids}
	st.v = imageview.New(ids, "imagerender-demo", imageview.Options{HideHistogram: true, Overlay: st.paintMarks})
	_ = st.v.SetSource(st.src, false)
	return st
}

// paintMarks is the host's overlay: a ring around each mark and a hit
// region with its label, so the marks have names in the accessibility tree.
func (st *imageRenderDemoState) paintMarks(p imageview.Projector) {
	mark := color.Hex(styletokens.AccentDefault.AsHex())
	for i, m := range chartMarks {
		cx, cy := p.ToCanvas(m.at[0], m.at[1])
		c.PaintCircleStroke(cx, cy, 9, mark, styletokens.StrokeStrong).Send()
		c.PaintHitRect(st.ids.PrepareSeq(uint64(i)), cx-9, cy-9, cx+9, cy+9).Label(m.label).Tooltip(m.label).Send()
	}
}

func demoImageRender(ids *c.WidgetIdStack, st *imageRenderDemoState) {
	for range c.HorizontalTop().KeepIter() {
		if imageViewDemoButton(ids, "render", 0, "fit", false) {
			st.v.Fit()
		}
		if imageViewDemoButton(ids, "render", 1, "16× on the star", false) {
			st.v.SetView(chartCentre[0]+6, chartCentre[1]-4, 16)
		}
		if imageViewDemoButton(ids, "render", 2, "next page", false) {
			// A page turn: another source of the same size, the view kept.
			next := newChartSource()
			next.page.Store(3 - st.src.page.Load())
			st.src = next
			_ = st.v.SetSource(next, true)
			st.inverted = false
		}
		if imageViewDemoButton(ids, "render", 3, "invert", st.inverted) {
			st.inverted = !st.inverted
			r := st.v.Recipe()
			r.Invert = st.inverted
			_ = st.v.SetRecipe(r)
		}
		c.Checkbox(ids.PrepareStr("scrolls"), st.scrolls, "wheel scrolls").SendRespVal(&st.scrolls)
		st.v.Opts.WheelScrolls = st.scrolls
	}
	st.v.Render(960, 520)
	_, _, mag, _ := st.v.View()
	c.Label(fmt.Sprintf("page %d · ×%g · tiles rendered %d", st.src.page.Load(), math.Round(mag*100)/100, st.src.rendered.Load())).Selectable(false).Send()
}

func init() {
	registry.Register(registry.Demo{
		Name: "imageview-render", Category: "Graphics & canvas", Title: icons.IconFileImage + " imageview (a rendered source)",
		Stage:       [2]float32{1024, 680},
		Flags:       registry.DemoFlagNeedsLargeArea | registry.DemoFlagNonDeterministic,
		Kind:        registry.DemoKindUX,
		Description: "imageview showing a source that draws its tiles when asked (ADR-0374): a test chart computed at the level the view needs, so it stays sharp at sixteen times, where its fifth-of-a-pixel grid resolves. The marks are the host's overlay, with labelled hit regions. A page turn keeps the view; inverting maps the tiles already rendered again and renders nothing, which the count of tiles rendered shows. Tiles render on worker goroutines, so a capture taken at once can show them partly drawn.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			return newImageRenderDemoState(ids)
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoImageRender(ids, state.(*imageRenderDemoState))
		},
	})
}
