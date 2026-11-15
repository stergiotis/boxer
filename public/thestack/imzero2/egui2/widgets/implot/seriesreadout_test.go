package implot

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

func TestNearestSample(t *testing.T) {
	nan := math.NaN()
	sorted := []float64{0, 10, 20, 30}
	ys := []float64{1, 2, 3, 4}
	for _, c := range []struct {
		name      string
		xs, ys    []float64
		x         float64
		stepAfter bool
		want      int
	}{
		{"sorted, between", sorted, ys, 14, false, 1},
		{"sorted, nearer the next", sorted, ys, 16, false, 2},
		{"tie goes to the earlier", sorted, ys, 15, false, 1},
		{"left of the data", sorted, ys, -50, false, 0},
		{"right of the data", sorted, ys, 99, false, 3},
		{"unsorted", []float64{30, 0, 20, 10}, ys, 11, false, 3},
		{"NaN x skipped", []float64{0, nan, 20}, []float64{1, 2, 3}, 9, false, 0},
		{"NaN y skipped", []float64{0, 10, 20}, []float64{1, nan, 3}, 11, false, 2},
		{"stairs: last at or before", sorted, ys, 29, true, 2},
		{"stairs: exactly on a sample", sorted, ys, 20, true, 2},
		{"stairs: left of the data", sorted, ys, -1, true, -1},
		{"stairs: unsorted", []float64{30, 0, 20, 10}, ys, 25, true, 2},
		{"empty", nil, nil, 0, false, -1},
		{"all NaN", []float64{nan, nan}, []float64{1, 2}, 0, false, -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, nearestSample(c.xs, c.ys, c.x, c.stepAfter))
		})
	}
}

// readoutRig renders plots under a discarding channel with a scripted
// pointer; each frame ends with ScriptReset as a host's Sync would.
type readoutRig struct {
	ids *c.WidgetIdStack
	sm  *c.StateManager
	pl  map[string]*Plot
}

func newReadoutRig(t *testing.T) *readoutRig {
	t.Cleanup(scenetest.Install())
	r := &readoutRig{ids: c.NewWidgetIdStack(), sm: c.CurrentApplicationState.StateManager, pl: map[string]*Plot{}}
	t.Cleanup(func() {
		for _, p := range r.pl {
			Release(p.ScopeId())
		}
	})
	return r
}

func (r *readoutRig) draw(title string, decl func(p *Plot)) *Plot {
	p := Begin(r.ids, title, 400, 300)
	decl(p)
	p.End()
	r.pl[title] = p
	return p
}

// hover scripts the pointer over title's plot area at plot-space (x, y).
func (r *readoutRig) hover(title string, x, y float64) {
	var canvas, area widgethandle.WidgetHandle
	for range c.IdScope(r.ids.PrepareStr(title)) {
		canvas = widgethandle.Make(r.ids.PrepareStr("implot-canvas").Derive())
		area = widgethandle.Make(r.ids.PrepareStr("implot-area").Derive())
	}
	pr := r.pl[title].st.prev
	px, py := pr.pxX(x), pr.pxY(y)
	r.sm.ScriptCanvasCursor(canvas, c.CanvasCursorValue{PosX: px, PosY: py})
	r.sm.ScriptCanvasCursor(area, c.CanvasCursorValue{PosX: px, PosY: py})
	r.sm.ScriptPointer(c.PointerValue{X: px, Y: py, Valid: true})
	r.sm.ScriptResponse(canvas, c.ContainsPointerResponseFlags|c.HoveredResponseFlags)
}

var (
	readoutXs  = []float64{0, 1, 2, 3, 4}
	readoutA   = []float64{10, 11, 12, 13, 14}
	readoutB   = []float64{20, 21, 22, 23, 24}
	readoutXs2 = []float64{0.5, 2.5}
	readoutS   = []float64{5, 6}
)

func readoutPlot(link *CrosshairLink, hide string) func(p *Plot) {
	return func(p *Plot) {
		p.SetupAxisLimits(AxisX1, 0, 4, CondAlways).SetupAxisLimits(AxisY1, 0, 30, CondAlways).
			SetupCrosshair(link).HoverSeries()
		if hide != "" {
			p.SetSeriesHidden(hide, true)
		}
		p.Line("a", readoutXs, readoutA)
		p.Line("b", readoutXs, readoutB)
		p.Stairs("steps", readoutXs2, readoutS)
		p.Line("", readoutXs, readoutA) // unlabelled: no row
	}
}

// Hovering reads every visible labelled series at its own sample: the lines
// at the nearest x, the stairs at the step in force.
func TestSeriesReadoutRows(t *testing.T) {
	r := newReadoutRig(t)
	r.draw("##ro", readoutPlot(nil, ""))
	_, _, ok := r.pl["##ro"].SeriesReadout()
	require.False(t, ok, "a readout without a pointer")

	r.sm.ScriptReset()
	r.hover("##ro", 2.3, 5)
	p := r.draw("##ro", readoutPlot(nil, ""))
	x, rows, ok := p.SeriesReadout()
	require.True(t, ok)
	require.InDelta(t, 2.3, x, 0.02)
	got := map[string][3]float64{}
	for _, row := range rows {
		got[row.Label] = [3]float64{float64(row.Index), row.X, row.Y}
	}
	require.Equal(t, map[string][3]float64{
		"a":     {2, 2, 12},
		"b":     {2, 2, 22},
		"steps": {0, 0.5, 5},
	}, got)

	r.sm.ScriptReset()
	r.hover("##ro", 2.3, 5)
	p = r.draw("##ro", readoutPlot(nil, "b"))
	_, rows, _ = p.SeriesReadout()
	for _, row := range rows {
		require.NotEqual(t, "b", row.Label, "a hidden series was read")
	}
	r.sm.ScriptReset()
	r.draw("##ro", readoutPlot(nil, ""))
	_, _, ok = r.pl["##ro"].SeriesReadout()
	require.False(t, ok, "the readout outlived the pointer")
}

// Two plots on one link: hovering the first puts its x on the link, the
// second reads its own series there, and the line goes with the pointer.
func TestCrosshairLinkAcrossPlots(t *testing.T) {
	r := newReadoutRig(t)
	var link CrosshairLink
	frame := func(hoverA bool) (a, b *Plot) {
		r.sm.ScriptReset()
		if hoverA {
			r.hover("##a", 3.1, 5)
		}
		return r.draw("##a", readoutPlot(&link, "")), r.draw("##b", readoutPlot(&link, ""))
	}
	frame(false)
	_, _, ok := r.pl["##b"].SeriesReadout()
	require.False(t, ok)

	_, b := frame(true)
	lx, ok := link.At()
	require.True(t, ok)
	require.InDelta(t, 3.1, lx, 0.02)
	x, rows, ok := b.SeriesReadout()
	require.True(t, ok, "the linked plot drew no readout")
	require.Equal(t, lx, x)
	require.Equal(t, 3, rows[0].Index)

	_, b = frame(false)
	_, ok = link.At()
	require.False(t, ok, "the hovered plot did not withdraw its x")
	_, _, ok = b.SeriesReadout()
	require.False(t, ok, "the linked plot kept the line after the pointer left")

	// An x set from outside the plots shows in both, and goes stale.
	clock := time.Unix(1_800_000_000, 0)
	clockNow = func() time.Time { return clock }
	t.Cleanup(func() { clockNow = time.Now })
	link.Set(1)
	a, b := frame(false)
	for _, p := range []*Plot{a, b} {
		_, rows, ok := p.SeriesReadout()
		require.True(t, ok)
		require.Equal(t, 1, rows[0].Index)
	}
	clock = clock.Add(2 * crosshairStale)
	_, ok = link.At()
	require.False(t, ok, "a stale x still reads")
	link.Clear()
}
