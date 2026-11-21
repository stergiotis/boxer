package imageview

import (
	"context"
	"image"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// patternSource renders an 8-bit RGB pattern at any level it declares, as a
// page renderer would: the red sample of cell (x, y) of level k is
// (x + 3y) mod 256, green is the level offset by 100, blue the content
// version. It counts what it is asked.
type patternSource struct {
	info    SourceInfo
	version atomic.Uint32
	calls   atomic.Int64
	// wrong makes it answer tiles of the wrong size.
	wrong bool
}

func newPatternSource(w, h, reduced, magnified int) *patternSource {
	return &patternSource{info: SourceInfo{Width: w, Height: h, Model: imaging.ColorModelRGB, Kind: imaging.SampleKindU8,
		Reduced: reduced, Magnified: magnified, Rendered: true, Provenance: imaging.Provenance{SourceName: "pattern"}}}
}

func (inst *patternSource) Info() SourceInfo { return inst.info }

func (inst *patternSource) Tile(ctx context.Context, level, col, row int) (d TileData, err error) {
	inst.calls.Add(1)
	if err = ctx.Err(); err != nil {
		return d, err
	}
	rect := TileRect(inst.info, level, col, row)
	if inst.wrong {
		rect.Max.X--
	}
	n := rect.Dx() * rect.Dy()
	r, g, b := make([]uint8, n), make([]uint8, n), make([]uint8, n)
	for y := range rect.Dy() {
		for x := range rect.Dx() {
			i := y*rect.Dx() + x
			r[i] = uint8(rect.Min.X + x + 3*(rect.Min.Y+y))
			g[i] = uint8(100 + level)
			b[i] = uint8(inst.version.Load())
		}
	}
	planes := make([]imaging.Plane, 3)
	for i, s := range [][]uint8{r, g, b} {
		if planes[i], err = imaging.NewPlaneU8(rect.Dx(), rect.Dy(), s); err != nil {
			return d, err
		}
	}
	img, err := imaging.NewImage(imaging.ColorModelRGB, planes, imaging.Provenance{})
	return TileData{Samples: img, Region: image.Rect(0, 0, rect.Dx(), rect.Dy())}, err
}

func sourceTile(t *testing.T, v *Viewer, level, x, y int) []uint32 {
	t.Helper()
	px, w, h, err := v.src.TilePixels(context.Background(), portolan.TileCoords{X: x, Y: y, Z: level})
	require.NoError(t, err)
	require.Equal(t, tileSize, w)
	require.Equal(t, tileSize, h)
	return px
}

// A rendered source is drawn at every level it declares — above level 0
// too — through the default recipe, which shows its 8-bit colour as it was
// rendered.
func TestASourceIsAskedPerLevelAndShownAsRendered(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	src := newPatternSource(1000, 700, 2, 3)
	require.NoError(t, v.SetSource(src, false))
	assert.True(t, v.Image().IsZero(), "a source is not an image in memory")
	assert.Equal(t, src.info, v.SourceInfo())
	assert.Equal(t, display.LimitsSourceNominal, v.Recipe().Limits.Source)

	ps := sourceFor(v.SourceInfo())
	assert.Equal(t, -2, ps.MinNativeZoom)
	assert.Equal(t, 3, ps.MaxNativeZoom, "level k is portolan's zoom k")

	for _, level := range []int{-2, 0, 3} {
		px := sourceTile(t, v, level, 1, 0)
		// Cell (512 + 5, 7) of the level: red (517 + 21) mod 256.
		want := uint32((512+5+3*7)%256)<<24 | uint32(100+level)<<16 | 0xff
		lw, _ := LevelSize(1000, 700, level)
		if lw > 512+5 {
			assert.Equal(t, want, px[7*tileSize+5], "level %d shows the samples rendered for it", level)
		}
	}
	_, _, _, err := v.src.TilePixels(context.Background(), portolan.TileCoords{Z: 4})
	assert.Error(t, err, "a level the source does not declare")
}

// A recipe change maps the cached samples again and asks the source
// nothing; Invalidate drops them and asks again, for the new content.
func TestARecipeChangeAsksTheSourceNothingAndInvalidateAsksAgain(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	src := newPatternSource(1000, 700, 1, 1)
	require.NoError(t, v.SetSource(src, false))
	before := sourceTile(t, v, 0, 0, 0)
	assert.EqualValues(t, 1, src.calls.Load())

	r := v.Recipe()
	r.Invert = true
	require.NoError(t, v.SetRecipe(r))
	after := sourceTile(t, v, 0, 0, 0)
	assert.EqualValues(t, 1, src.calls.Load(), "the samples came from the cache")
	assert.Equal(t, 255-before[0]>>24, after[0]>>24, "and were mapped through the new recipe")

	src.version.Store(9)
	v.Invalidate()
	fresh := sourceTile(t, v, 0, 0, 0)
	assert.EqualValues(t, 2, src.calls.Load(), "content changed: asked again")
	assert.Equal(t, uint32(255-9), fresh[0]>>8&0xff, "the new content, still inverted")
}

// The cache keeps no more than its bound, evicting the least recently used;
// a source that is resident is read in place and not cached at all.
func TestTheSampleCacheIsBounded(t *testing.T) {
	one := int64(512 * 512 * 3)
	v := New(c.NewWidgetIdStack(), "", Options{SampleCacheBytes: 2 * one})
	defer v.Close()
	src := newPatternSource(2048, 512, 0, 0)
	require.NoError(t, v.SetSource(src, false))
	for x := range 3 {
		sourceTile(t, v, 0, x, 0)
	}
	bytes, tiles := v.src.cache.usage()
	assert.Equal(t, 2*one, bytes)
	assert.Equal(t, 2, tiles)
	sourceTile(t, v, 0, 2, 0)
	assert.EqualValues(t, 3, src.calls.Load(), "the latest tile is kept")
	sourceTile(t, v, 0, 0, 0)
	assert.EqualValues(t, 4, src.calls.Load(), "the oldest was evicted")

	v.SetImage(grayImage(t, 1100, 700))
	_ = tile(t, v, 0, 0)
	bytes, tiles = v.src.cache.usage()
	assert.Zero(t, bytes, "an image in memory is not cached again")
	assert.Zero(t, tiles)
}

// A source is refused when it describes nothing showable, and when it
// answers a tile it was not asked for; what was shown stays.
func TestABadSourceIsRefused(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	good := newPatternSource(300, 200, 0, 0)
	require.NoError(t, v.SetSource(good, false))
	for _, bad := range []SourceInfo{
		{},
		{Width: 10, Height: 10, Kind: imaging.SampleKindU8},
		{Width: 10, Height: 10, Model: imaging.ColorModelGray},
		{Width: 10, Height: 10, Model: imaging.ColorModelGray, Kind: imaging.SampleKindU8, Magnified: maxZoom + 1},
	} {
		s := newPatternSource(1, 1, 0, 0)
		s.info = bad
		assert.Error(t, v.SetSource(s, false))
		assert.Equal(t, good.info, v.SourceInfo(), "what was shown stays")
	}
	wrong := newPatternSource(300, 200, 0, 0)
	wrong.wrong = true
	require.NoError(t, v.SetSource(wrong, false))
	_, _, _, err := v.src.TilePixels(context.Background(), portolan.TileCoords{})
	assert.ErrorContains(t, err, "another size")
}

// The readout reports the pixel always, and its samples when the tile
// holding it is at hand; the source is never asked from the frame.
func TestTheReadoutOfASourceReadsOnlyWhatIsAtHand(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	src := newPatternSource(1000, 700, 0, 0)
	require.NoError(t, v.SetSource(src, false))
	r, ok := v.readoutAt(portolan.LL(-7.5, 517.5))
	require.True(t, ok)
	assert.Equal(t, Readout{Col: 517, Row: 7}, r, "no samples at hand yet")
	assert.Zero(t, src.calls.Load())

	sourceTile(t, v, 0, 1, 0)
	r, ok = v.readoutAt(portolan.LL(-7.5, 517.5))
	require.True(t, ok)
	assert.Equal(t, 3, r.Planes)
	assert.Equal(t, float64((517+21)%256), r.Stored[0])
	assert.Equal(t, float64(100), r.Stored[1])
	_, ok = v.readoutAt(portolan.LL(-750, 10))
	assert.False(t, ok, "outside the source")

	_, ok = v.Histogram()
	assert.False(t, ok, "no histogram without every sample at hand")
	p := v.Recipe()
	p.Limits = display.Limits{Source: display.LimitsSourcePercentile, PercentLo: 1, PercentHi: 99}
	require.NoError(t, v.SetPlane(0))
	assert.ErrorContains(t, v.SetRecipe(p), "set the limits")
}

// A new source keeps the view when asked to, as a page turn would; without
// it the source is fitted.
func TestANewSourceCanKeepTheView(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	require.NoError(t, v.SetSource(newPatternSource(1000, 700, 2, 3), false))
	v.m.View().SetSize(portolan.Pt(400, 300))
	v.m.View().SetView(portolan.LL(-100, 250), 1.5)
	v.fitPending = false

	require.NoError(t, v.SetSource(newPatternSource(1000, 700, 2, 3), true))
	assert.False(t, v.fitPending)
	assert.Equal(t, 1.5, v.m.View().Zoom())
	assert.Equal(t, portolan.LL(-100, 250), v.m.View().Center())

	require.NoError(t, v.SetSource(newPatternSource(1000, 700, 2, 3), false))
	assert.True(t, v.fitPending, "fitted on the next frame")

	require.NoError(t, v.SetSource(nil, false))
	assert.True(t, v.SourceInfo().IsZero(), "a nil source shows nothing")
}

// heldSource is a pattern source whose tiles wait until released, as a page
// still rendering when the next one is shown.
type heldSource struct {
	*patternSource
	gate chan struct{}
}

func (inst *heldSource) Tile(ctx context.Context, level, col, row int) (TileData, error) {
	inst.calls.Add(1)
	select {
	case <-inst.gate:
	case <-ctx.Done():
		return TileData{}, ctx.Err()
	}
	inst.calls.Add(-1)
	return inst.patternSource.Tile(ctx, level, col, row)
}

// A page turned while the page before is still rendering, to a page of the
// same size with the view kept: every tile of the new page is asked of the
// new source. A call under way for the old page used to be answered for the
// new one, which then never asked for the tile.
func TestASourceReplacedWhileItsTilesAreUnderWayIsNotShownForTheNext(t *testing.T) {
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	v := New(c.NewWidgetIdStack(), "", Options{HideColorBar: true, HideReadout: true, HideHistogram: true})
	defer v.Close()
	frame := func() {
		v.Render(400, 300)
		sm.ScriptReset()
	}
	first := &heldSource{patternSource: newPatternSource(1000, 1300, 2, 0), gate: make(chan struct{})}
	require.NoError(t, v.SetSource(first, false))
	deadline := time.Now().Add(5 * time.Second)
	for first.calls.Load() == 0 && time.Now().Before(deadline) {
		frame()
		time.Sleep(time.Millisecond)
	}
	require.Positive(t, first.calls.Load(), "tiles of the first page are under way")

	second := newPatternSource(1000, 1300, 2, 0)
	second.version.Store(7)
	require.NoError(t, v.SetSource(second, true))
	close(first.gate)
	frame()
	for v.m.Loading() && time.Now().Before(deadline) {
		frame()
		time.Sleep(time.Millisecond)
	}
	require.False(t, v.m.Loading(), "the second page loaded")
	loaded := v.m.Stats().TileLoad
	require.Positive(t, loaded)
	assert.GreaterOrEqual(t, second.calls.Load(), int64(loaded), "every tile shown was asked of the second page")
	assert.Zero(t, v.m.Stats().TileError)
}
