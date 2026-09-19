package flowoverlay

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

func hourly(n int) (out []vectorfield.Step) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		out = append(out, vectorfield.Step{Valid: t0.Add(time.Duration(i) * time.Hour)})
	}
	return
}

func globalSource(t *testing.T, fn vectorfield.FieldFunc, nSteps int) *vectorfield.Pyramid {
	t.Helper()
	p, err := vectorfield.NewPyramidE(context.Background(),
		vectorfield.Meta{Name: "test", Unit: "m/s", SpeedMax: 30, Steps: hourly(nSteps)},
		vectorfield.NewGlobalAnalyticLoader(1, fn), vectorfield.PyramidOptions{})
	require.NoError(t, err)
	return p
}

// scene is a headless map with a layer on it and a clock the test owns.
type scene struct {
	t     *testing.T
	m     *portolan.Map
	layer *Layer
	clock time.Time
}

func newScene(t *testing.T, src vectorfield.SourceI, opts Options) *scene {
	t.Helper()
	s := newBenchScene(t, src, opts)
	s.t = t
	return s
}

// newBenchScene is newScene for a test or a benchmark.
func newBenchScene(t testing.TB, src vectorfield.SourceI, opts Options) *scene {
	t.Helper()
	t.Cleanup(scenetest.Install())
	s := &scene{clock: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	s.m = portolan.New(c.NewWidgetIdStack(), portolan.Options{NoTiles: true, Center: portolan.LL(45, 10), Zoom: 4})
	s.layer = New(src, opts)
	s.layer.now = func() time.Time { return s.clock }
	t.Cleanup(s.layer.Close)
	return s
}

// frame renders once, dt after the last frame.
func (s *scene) frame(dt time.Duration) {
	s.clock = s.clock.Add(dt)
	s.m.Render(800, 600, func(p portolan.Projector) { s.layer.Draw(p) })
}

// settle renders until cond holds. Requests run on goroutines of the layer's
// own, so the wait is in wall time while the layer's clock is the test's.
func (s *scene) settle(cond func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		require.True(s.t, time.Now().Before(deadline), "the scene did not settle")
		time.Sleep(2 * time.Millisecond)
		s.frame(0)
	}
}

func TestDrawPaintsTrailsOnceAWindowArrives(t *testing.T) {
	s := newScene(t, globalSource(t, vectorfield.Swirl(0), 1), Options{Seed: 1})
	s.frame(0)
	require.Zero(t, s.layer.Stats().Segments, "nothing is drawn before the first window")
	s.settle(func() bool { return s.layer.Stats().WindowCols > 0 })
	for range 30 {
		s.frame(34 * time.Millisecond) // 1.02 s: thirty ticks and a remainder
	}
	st := s.layer.Stats()
	require.Equal(t, 2400, st.Particles, "five per thousand square pixels of an 800 x 600 canvas")
	require.EqualValues(t, 30, st.Ticks)
	require.Greater(t, st.Segments, 5*st.Particles)
	require.LessOrEqual(t, st.WindowCols, int(800*(1+2*viewMargin)/defaultSamplePx)+2+2, "the window is bounded by the request and its gutter")
	require.EqualValues(t, 1, st.Fetches)

	u, v, speed, ok := s.layer.At(portolan.LL(45, 10))
	require.True(t, ok)
	wantU, wantV, _ := vectorfield.Swirl(0)(10, 45, 0)
	require.InDelta(t, wantU, u, 1.5)
	require.InDelta(t, wantV, v, 1.5)
	require.GreaterOrEqual(t, float64(speed)+1e-3, float64(u*u+v*v)/float64(speed+1e-6)*0.999)
}

// The simulation advances on a fixed tick: however the same stretch of time
// is cut into frames, the particles end in the same place.
func TestTicksDoNotDependOnTheFrameRate(t *testing.T) {
	run := func(frames []time.Duration) (xs []float64, ticks uint64) {
		s := newScene(t, globalSource(t, vectorfield.Swirl(0), 1), Options{Seed: 5, TickHz: 25})
		s.settle(func() bool { return s.layer.Stats().WindowCols > 0 })
		for _, dt := range frames {
			s.frame(dt)
		}
		return append([]float64(nil), s.layer.sim.x...), s.layer.sim.ticks
	}
	var at60, at120, ragged []time.Duration
	// Each cut spans 1010 ms: twenty-five ticks of 40 ms and a remainder that
	// no rounding turns into a twenty-sixth.
	for range 101 {
		at60 = append(at60, 10*time.Millisecond)
	}
	for range 202 {
		at120 = append(at120, 5*time.Millisecond)
	}
	for range 20 {
		ragged = append(ragged, 7*time.Millisecond, 31*time.Millisecond, 12*time.Millisecond)
	}
	ragged = append(ragged, 10*time.Millisecond)
	a, ta := run(at60)
	b, tb := run(at120)
	r, tr := run(ragged)
	require.EqualValues(t, 25, ta)
	require.Equal(t, ta, tb, "a 120 Hz display runs the same ticks as a 60 Hz one")
	require.Equal(t, ta, tr)
	require.Equal(t, a, b)
	require.Equal(t, a, r)
}

func TestStallIsNotCaughtUpOn(t *testing.T) {
	s := newScene(t, globalSource(t, vectorfield.Swirl(0), 1), Options{Seed: 5})
	s.settle(func() bool { return s.layer.Stats().WindowCols > 0 })
	s.frame(time.Second / 30)
	before := s.layer.sim.ticks
	s.frame(10 * time.Second)
	require.EqualValues(t, maxTicksPerFrame, s.layer.sim.ticks-before, "ten seconds away is a few ticks, not three hundred")
}

// The capture mode: a fixed number of ticks from a fixed seed, then rest. The
// bytes painted are then a function of the seed, the view and the field.
func TestFixedTicksPaintTheSameBytes(t *testing.T) {
	paint := func(seed uint64) (sum uint64, ticks uint64) {
		src := globalSource(t, vectorfield.Swirl(0), 1)
		sumFn, zero, reset := scenetest.InstallHashing()
		defer reset()
		m := portolan.New(c.NewWidgetIdStack(), portolan.Options{NoTiles: true, Center: portolan.LL(45, 10), Zoom: 4})
		layer := New(src, Options{Seed: seed, FixedTicks: 100})
		defer layer.Close()
		render := func() { m.Render(800, 600, func(p portolan.Projector) { layer.Draw(p) }) }
		deadline := time.Now().Add(10 * time.Second)
		for layer.Stats().Ticks < 100 {
			require.True(t, time.Now().Before(deadline))
			time.Sleep(2 * time.Millisecond)
			render()
		}
		render()
		zero()
		render()
		return sumFn(), layer.Stats().Ticks
	}
	a, ta := paint(11)
	b, tb := paint(11)
	other, _ := paint(12)
	require.EqualValues(t, 100, ta)
	require.EqualValues(t, 100, tb, "it rests at the fixed count")
	require.Equal(t, a, b)
	require.NotEqual(t, a, other)
}

// gatedSource answers each request only when the test lets it, in any order,
// and ignores cancellation — the slow reply for an old view that arrives
// after a newer one.
type gatedSource struct {
	inner vectorfield.SourceI
	mu    sync.Mutex
	gates []chan struct{}
	reqs  []vectorfield.Request
}

func (g *gatedSource) Describe() vectorfield.Meta { return g.inner.Describe() }

func (g *gatedSource) SampleE(_ context.Context, req vectorfield.Request) (vectorfield.Window, error) {
	gate := make(chan struct{})
	g.mu.Lock()
	g.gates = append(g.gates, gate)
	g.reqs = append(g.reqs, req)
	g.mu.Unlock()
	<-gate
	return g.inner.SampleE(context.Background(), req)
}

func (g *gatedSource) pending() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.gates)
}

func TestLateReplyForAnOldViewIsDropped(t *testing.T) {
	src := &gatedSource{inner: globalSource(t, vectorfield.Swirl(0), 1)}
	s := newScene(t, src, Options{Seed: 1})
	s.frame(0)
	s.settle(func() bool { return src.pending() == 1 })

	// The view moves right out of what was asked for.
	s.m.View().SetView(portolan.LL(-30, -120), 6)
	s.frame(time.Second)
	s.settle(func() bool { return src.pending() == 2 })
	require.EqualValues(t, 2, s.layer.Stats().Fetches)

	close(src.gates[1]) // the newer reply first
	s.settle(func() bool { return s.layer.Stats().WindowCols > 0 })
	onScreen := s.layer.geom.req
	require.Equal(t, src.reqs[1].West, onScreen.West)

	close(src.gates[0]) // and then the old one
	s.settle(func() bool { return s.layer.Stats().DroppedReplies == 1 })
	require.Equal(t, onScreen, s.layer.geom.req, "the old reply did not replace the newer window")
}

// A trail segment is one tick of one particle. Nothing the layer paints may
// be longer than a pace — not after a pan, not after particles were seeded
// again, not while trails drain.
func TestNoSegmentIsLongerThanAPace(t *testing.T) {
	s := newScene(t, globalSource(t, vectorfield.Swirl(0), 1), Options{Seed: 9, ResetBase: 0.05})
	s.settle(func() bool { return s.layer.Stats().WindowCols > 0 })
	longest := func() (worst float64) {
		l := s.layer
		for i := range l.cols {
			worst = max(worst, math.Hypot(float64(l.x1s[i]-l.x0s[i]), float64(l.y1s[i]-l.y0s[i])))
		}
		return
	}
	for k := range 200 {
		if k%25 == 24 {
			s.m.View().PanBy(portolan.Pt(90, -35))
		}
		s.frame(time.Second / 30)
		require.LessOrEqual(t, longest(), float64(defaultMaxPace)*1.01, "frame %d", k)
	}
	require.Greater(t, s.layer.Stats().Segments, 1000)
}

func TestSynchronousCaptureIsCompleteOnTheFirstFrame(t *testing.T) {
	s := newScene(t, globalSource(t, vectorfield.Swirl(0), 1), Options{Seed: 1, FixedTicks: 90, Synchronous: true})
	s.frame(0)
	st := s.layer.Stats()
	require.EqualValues(t, 90, st.Ticks)
	require.Greater(t, st.Segments, st.Particles)
	require.False(t, st.InFlight)
}

func TestPanInsideTheWindowAsksForNothing(t *testing.T) {
	s := newScene(t, globalSource(t, vectorfield.Swirl(0), 1), Options{Seed: 1})
	s.settle(func() bool { return s.layer.Stats().WindowCols > 0 })
	for range 20 {
		s.m.View().PanBy(portolan.Pt(10, 5)) // 200 x 100 pixels in all, inside the margin
		s.frame(time.Second)
	}
	require.EqualValues(t, 1, s.layer.Stats().Fetches, "the margin is what a pan spends")
	s.m.View().PanBy(portolan.Pt(600, 0))
	s.frame(time.Second)
	s.settle(func() bool { return s.layer.Stats().Fetches == 2 && !s.layer.Stats().InFlight })
}

// A view resting near the zoom at which the wanted level changes must not
// alternate between two windows.
func TestZoomNearALevelBoundaryDoesNotAlternate(t *testing.T) {
	// A quarter-degree field, so that zoom 4 is served from a coarser level
	// and there is a finer one to zoom into.
	src, err := vectorfield.NewPyramidE(context.Background(),
		vectorfield.Meta{SpeedMax: 30, Steps: hourly(1)},
		vectorfield.NewGlobalAnalyticLoader(0.25, vectorfield.Uniform(8, 3)), vectorfield.PyramidOptions{})
	require.NoError(t, err)
	s := newScene(t, src, Options{Seed: 1})
	s.settle(func() bool { return s.layer.Stats().WindowCols > 0 })
	base := s.layer.Stats().Fetches
	for k := range 40 {
		z := 4.0
		if k%2 == 1 {
			z = 4.3
		}
		s.m.View().SetView(portolan.LL(45, 10), z)
		s.frame(time.Second)
		time.Sleep(time.Millisecond)
	}
	s.settle(func() bool { return !s.layer.Stats().InFlight })
	require.LessOrEqual(t, s.layer.Stats().Fetches-base, uint64(1))

	rested := s.layer.Stats().Fetches
	s.m.View().SetView(portolan.LL(45, 10), 7)
	s.frame(time.Second)
	s.settle(func() bool { return s.layer.Stats().Fetches > rested && !s.layer.Stats().InFlight })
	require.Zero(t, s.layer.Stats().WindowLevel, "a real zoom-in does get a finer window")
}

func TestDisplayTimeBlendsBetweenSteps(t *testing.T) {
	// The field turns from a westerly to a southerly over one step.
	fn := func(_, _ float64, step int) (float32, float32, bool) {
		if step == 0 {
			return 10, 0, true
		}
		return 0, 10, true
	}
	s := newScene(t, globalSource(t, fn, 3), Options{Seed: 1})
	meta := s.layer.Meta()

	s.layer.SetTime(meta.Steps[0].Valid.Add(15 * time.Minute))
	require.InDelta(t, 0.25, s.layer.StepPosition(), 1e-9)
	require.Equal(t, meta.Steps[0].Valid.Add(15*time.Minute), s.layer.Time())
	s.settle(func() bool { return len(s.layer.steps) == 2 })
	u, v, speed, ok := s.layer.At(portolan.LL(45, 10))
	require.True(t, ok)
	require.InDelta(t, 7.5, u, 1e-3)
	require.InDelta(t, 2.5, v, 1e-3)
	require.InDelta(t, 10, speed, 1e-3, "the scalar mean is blended by itself and takes no dip")

	s.layer.SetTime(meta.Steps[0].Valid.Add(-time.Hour))
	require.Zero(t, s.layer.StepPosition(), "before the first step shows the first step")
	s.layer.SetTime(meta.Steps[2].Valid.Add(time.Hour))
	require.InDelta(t, 2, s.layer.StepPosition(), 1e-9)

	// Moving on within the same view asks only for the step that is new.
	fetches := s.layer.Stats().Fetches
	s.layer.SetStepPosition(1.5)
	s.frame(time.Second)
	s.settle(func() bool { _, ok := s.layer.steps[2]; return ok && !s.layer.Stats().InFlight })
	require.Equal(t, fetches+1, s.layer.Stats().Fetches)
	_, has := s.layer.steps[0]
	require.False(t, has, "a step the display time has left is let go")
}

type missingStep struct {
	vectorfield.SourceI
	step int
}

func (m missingStep) SampleE(ctx context.Context, req vectorfield.Request) (vectorfield.Window, error) {
	if req.Step == m.step {
		return vectorfield.Window{}, vectorfield.ErrStepMissing
	}
	return m.SourceI.SampleE(ctx, req)
}

func TestMissingStepShowsTheOtherAlone(t *testing.T) {
	src := missingStep{SourceI: globalSource(t, vectorfield.Uniform(10, 0), 3), step: 1}
	s := newScene(t, src, Options{Seed: 1})
	s.layer.SetStepPosition(0.8)
	s.settle(func() bool { return len(s.layer.steps) == 1 && s.layer.missing[1] && !s.layer.Stats().InFlight })
	fetches := s.layer.Stats().Fetches
	for range 10 {
		s.frame(time.Second)
	}
	require.Equal(t, fetches, s.layer.Stats().Fetches, "a missing step is not asked for again every frame")
	require.Greater(t, s.layer.Stats().Segments, 0)
	u, _, _, ok := s.layer.At(portolan.LL(45, 10))
	require.True(t, ok)
	require.InDelta(t, 10, u, 1e-3)
	require.NoError(t, s.layer.Stats().LastError)
}

func TestSetTimeWithUnevenSteps(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	steps := []vectorfield.Step{{Valid: t0}, {Valid: t0.Add(time.Hour)}, {Valid: t0.Add(4 * time.Hour)}, {Valid: t0.Add(10 * time.Hour)}}
	p, err := vectorfield.NewPyramidE(context.Background(), vectorfield.Meta{Steps: steps, SpeedMax: 30},
		vectorfield.NewGlobalAnalyticLoader(2, vectorfield.Uniform(1, 0)), vectorfield.PyramidOptions{})
	require.NoError(t, err)
	layer := New(p, Options{})
	defer layer.Close()
	layer.SetTime(t0.Add(150 * time.Minute))
	require.InDelta(t, 1.5, layer.StepPosition(), 1e-9, "hourly and then three-hourly: halfway through the second gap")
	layer.SetTime(t0.Add(7 * time.Hour))
	require.InDelta(t, 2.5, layer.StepPosition(), 1e-9)
	require.Equal(t, t0.Add(7*time.Hour), layer.Time())
}
