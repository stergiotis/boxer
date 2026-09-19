package vectorfield_test

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/vectorfieldtest"
)

func steps(n int) (out []vectorfield.Step) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		out = append(out, vectorfield.Step{Valid: t0.Add(time.Duration(i) * time.Hour), Reference: t0})
	}
	return
}

func newPyramid(t *testing.T, loader vectorfield.StepLoaderI, nSteps int, opts vectorfield.PyramidOptions) *vectorfield.Pyramid {
	t.Helper()
	p, err := vectorfield.NewPyramidE(context.Background(), vectorfield.Meta{Name: "test", Unit: "m/s", Steps: steps(nSteps)}, loader, opts)
	require.NoError(t, err)
	return p
}

// regional is a 256 x 128 grid over [0, 51] x [20, 45.4]: powers of two, so
// every level halves without a partial sample.
func regional(fn vectorfield.FieldFunc) *vectorfield.AnalyticLoader {
	return &vectorfield.AnalyticLoader{West: 0, North: 45.4, DLon: 0.2, DLat: 0.2, Cols: 256, Rows: 128, Fn: fn}
}

// everyLevel samples the whole extent at request sizes that walk the pyramid
// from the native grid to a reduction on the fly past the top level.
func everyLevel(t *testing.T, p *vectorfield.Pyramid, visit func(win *vectorfield.Window)) {
	t.Helper()
	meta := p.Describe()
	levels := map[float64]bool{}
	for _, maxCols := range []int{4096, 200, 100, 50, 25, 12, 6, 3} {
		win, err := p.SampleE(context.Background(), vectorfield.Request{
			West: meta.West, East: meta.East, South: meta.South, North: meta.North,
			MaxCols: maxCols, MaxRows: maxCols,
		})
		require.NoError(t, err)
		require.False(t, win.IsEmpty())
		levels[win.DLon] = true
		visit(&win)
	}
	require.GreaterOrEqual(t, len(levels), 4, "the request sizes reached several resolutions")
}

func TestConformance(t *testing.T) {
	land := func(lon, lat float64) bool { return lon < 20 || lat > 40 }
	cases := []struct {
		name   string
		loader vectorfield.StepLoaderI
		opts   vectorfieldtest.Options
	}{
		{"global uniform", vectorfield.NewGlobalAnalyticLoader(0.5, vectorfield.Uniform(3, -4)), vectorfieldtest.Options{Covered: true, MissingStep: -1}},
		{"global swirl, odd rows", vectorfield.NewGlobalAnalyticLoader(1, vectorfield.Swirl(2)), vectorfieldtest.Options{Covered: true, MissingStep: -1}},
		{"global on 0..360", &vectorfield.AnalyticLoader{West: 0, North: 90, DLon: 0.75, DLat: 0.75, Cols: 480, Rows: 241, Fn: vectorfield.Swirl(0)}, vectorfieldtest.Options{Covered: true, MissingStep: -1}},
		{"regional rotation", regional(vectorfield.Rotation(25, 32, 0.5)), vectorfieldtest.Options{Covered: true, MissingStep: -1}},
		{"regional, odd dimensions", &vectorfield.AnalyticLoader{West: -10, North: 60, DLon: 0.1, DLat: 0.1, Cols: 301, Rows: 203, Fn: vectorfield.Uniform(1, 1)}, vectorfieldtest.Options{Covered: true, MissingStep: -1}},
		{"regional with a coast", regional(vectorfield.Masked(vectorfield.Uniform(2, 0), land)), vectorfieldtest.Options{MissingStep: -1}},
		{"a missing step", &missingStepLoader{inner: regional(vectorfield.Uniform(1, 0)), missing: 1}, vectorfieldtest.Options{Covered: true, MissingStep: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vectorfieldtest.Run(t, newPyramid(t, tc.loader, 3, vectorfield.PyramidOptions{}), tc.opts)
		})
	}
}

type missingStepLoader struct {
	inner   vectorfield.StepLoaderI
	missing int
}

func (inst *missingStepLoader) LoadStepE(ctx context.Context, step int) (vectorfield.Grid, error) {
	if step == inst.missing {
		return vectorfield.Grid{}, vectorfield.ErrStepMissing
	}
	return inst.inner.LoadStepE(ctx, step)
}

func TestConstantFieldIsConstantAtEveryLevel(t *testing.T) {
	for name, loader := range map[string]vectorfield.StepLoaderI{
		"regional": regional(vectorfield.Uniform(3, -4)),
		"global":   vectorfield.NewGlobalAnalyticLoader(0.25, vectorfield.Uniform(3, -4)),
	} {
		t.Run(name, func(t *testing.T) {
			everyLevel(t, newPyramid(t, loader, 1, vectorfield.PyramidOptions{}), func(win *vectorfield.Window) {
				for i := range win.U {
					require.InDelta(t, 3, win.U[i], 1e-5)
					require.InDelta(t, -4, win.V[i], 1e-5)
					require.InDelta(t, 5, win.Speed[i], 1e-5, "a uniform field's scalar mean equals its vector mean")
				}
			})
		})
	}
}

// A field linear in position has a box mean equal to its value at the box's
// centre, so a level whose origin is off by half a fine cell shows up here.
func TestLevelsAreRegisteredAtTheirChildrensCentre(t *testing.T) {
	fn := vectorfield.Rotation(25, 32, 0.5)
	everyLevel(t, newPyramid(t, regional(fn), 1, vectorfield.PyramidOptions{}), func(win *vectorfield.Window) {
		// The last row and column may average a partial box, whose centre is
		// not its node; everything else must be exact.
		for r := range win.Rows - 1 {
			for c := range win.Cols - 1 {
				u, v, _ := fn(win.LonAt(c), win.LatAt(r), 0)
				i := r*win.Cols + c
				require.InDelta(t, u, win.U[i], 2e-3, "u at level spacing %v, sample (%d, %d)", win.DLon, c, r)
				require.InDelta(t, v, win.V[i], 2e-3, "v at level spacing %v, sample (%d, %d)", win.DLon, c, r)
			}
		}
	})
}

func TestWindowAcrossTheSeamIsContiguous(t *testing.T) {
	// u depends on longitude through a periodic function, so a column folded
	// onto the wrong side of the seam is visible.
	fn := func(lon, lat float64, _ int) (float32, float32, bool) {
		return float32(10 * math.Sin(lon*math.Pi/180)), float32(lat / 10), true
	}
	p := newPyramid(t, vectorfield.NewGlobalAnalyticLoader(0.5, fn), 1, vectorfield.PyramidOptions{})
	for _, maxCols := range []int{4096, 60, 20} {
		win, err := p.SampleE(context.Background(), vectorfield.Request{West: 140, East: 230, South: -30, North: 30, MaxCols: maxCols, MaxRows: maxCols})
		require.NoError(t, err)
		require.Less(t, win.West, 140.0)
		require.Greater(t, win.East(), 230.0)
		// A box mean of a sine over a box of width w is the sine at the
		// centre times sinc(w/2); compare direction of change, not amplitude.
		shrink := 1.0
		if half := win.DLon * math.Pi / 360; half > 1e-9 {
			shrink = math.Sin(half) / half
		}
		for c := range win.Cols {
			want := 10 * math.Sin(win.LonAt(c)*math.Pi/180) * shrink
			require.InDelta(t, want, win.U[c], 0.05, "column %d at lon %v, spacing %v", c, win.LonAt(c), win.DLon)
		}
	}
}

func TestRepeatedCyclicColumnIsDropped(t *testing.T) {
	fn := vectorfield.Swirl(0)
	plain := newPyramid(t, &vectorfield.AnalyticLoader{West: 0, North: 90, DLon: 1, DLat: 1, Cols: 360, Rows: 181, Fn: fn}, 1, vectorfield.PyramidOptions{})
	repeated := newPyramid(t, &vectorfield.AnalyticLoader{West: 0, North: 90, DLon: 1, DLat: 1, Cols: 361, Rows: 181, Fn: fn}, 1, vectorfield.PyramidOptions{})
	require.True(t, repeated.Describe().PeriodicLon)
	require.Equal(t, plain.Describe().East, repeated.Describe().East)
	req := vectorfield.Request{West: 300, East: 420, South: -60, North: 60, MaxCols: 50, MaxRows: 50}
	a, err := plain.SampleE(context.Background(), req)
	require.NoError(t, err)
	b, err := repeated.SampleE(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, a.Cols, b.Cols)
	require.Equal(t, a.U, b.U)
	require.Equal(t, a.V, b.V)
}

// A field whose sign alternates from column to column has no net flow. A
// pyramid that decimated would report a uniform one; one that averaged speed
// and direction would too. The vector mean goes to nothing and the scalar
// mean keeps saying how hard it blows.
func TestIncoherentFieldDoesNotAliasIntoAFlow(t *testing.T) {
	fn := func(lon, _ float64, _ int) (float32, float32, bool) {
		if int(math.Round(lon/0.2))%2 == 0 {
			return 10, 0, true
		}
		return -10, 0, true
	}
	p := newPyramid(t, regional(fn), 1, vectorfield.PyramidOptions{})
	meta := p.Describe()
	win, err := p.SampleE(context.Background(), vectorfield.Request{West: meta.West, East: meta.East, South: meta.South, North: meta.North, MaxCols: 30, MaxRows: 30})
	require.NoError(t, err)
	require.Greater(t, win.Level, 0)
	for i := range win.U {
		require.InDelta(t, 0, win.U[i], 1e-4)
		require.InDelta(t, 10, win.Speed[i], 1e-4)
		require.InDelta(t, 0, win.Constancy(i%win.Cols, i/win.Cols), 1e-4)
	}
}

// The coast neither advances nor retreats as levels coarsen: at every level
// the first valid sample of a row lies within one sample of the true coast.
func TestCoastStaysPut(t *testing.T) {
	const coast = 20.05 // between two native columns
	land := func(lon, _ float64) bool { return lon < coast }
	p := newPyramid(t, regional(vectorfield.Masked(vectorfield.Uniform(2, 1), land)), 1, vectorfield.PyramidOptions{})
	everyLevel(t, p, func(win *vectorfield.Window) {
		for r := range win.Rows {
			first := -1
			for c := range win.Cols {
				if v := win.U[r*win.Cols+c]; v == v {
					first = c
					break
				}
			}
			require.GreaterOrEqual(t, first, 0)
			require.InDelta(t, coast, win.LonAt(first), win.DLon, "row %d at spacing %v", r, win.DLon)
			for c := first; c < win.Cols; c++ {
				i := r*win.Cols + c
				require.InDelta(t, 2, win.U[i], 1e-5, "a coastal mean is over valid samples only, never blended with a fill")
				require.InDelta(t, 1, win.V[i], 1e-5)
			}
		}
	})
}

type countingLoader struct {
	inner vectorfield.StepLoaderI
	loads atomic.Int64
	gate  chan struct{}
}

func (inst *countingLoader) LoadStepE(ctx context.Context, step int) (vectorfield.Grid, error) {
	inst.loads.Add(1)
	if inst.gate != nil {
		<-inst.gate
	}
	return inst.inner.LoadStepE(ctx, step)
}

func TestCacheIsBoundedInBytes(t *testing.T) {
	loader := &countingLoader{inner: regional(vectorfield.Uniform(1, 0))}
	meta := vectorfield.Meta{Steps: steps(6), West: 0, East: 51, South: 20, North: 45.4, DLon: 0.2, DLat: 0.2}
	p, err := vectorfield.NewPyramidE(context.Background(), meta, loader, vectorfield.PyramidOptions{CacheBytes: 1})
	require.NoError(t, err)
	req := vectorfield.Request{West: 10, East: 20, South: 30, North: 40, MaxCols: 50, MaxRows: 50}
	var oneStep int64
	for step := range 6 {
		req.Step = step
		_, err = p.SampleE(context.Background(), req)
		require.NoError(t, err)
		if step == 0 {
			oneStep = p.HeldBytes()
			require.Greater(t, oneStep, int64(256*128*3*4))
		}
		require.LessOrEqual(t, p.HeldBytes(), 2*oneStep, "a budget too small for anything still keeps the two steps a renderer blends, and no more")
	}
	require.EqualValues(t, 6, loader.loads.Load())
	req.Step = 5
	_, err = p.SampleE(context.Background(), req)
	require.NoError(t, err)
	req.Step = 4
	_, err = p.SampleE(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 6, loader.loads.Load(), "the two most recent steps were held")
	req.Step = 0
	_, err = p.SampleE(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 7, loader.loads.Load(), "an evicted step is read again")
}

func TestConcurrentRequestsLoadAStepOnce(t *testing.T) {
	loader := &countingLoader{inner: regional(vectorfield.Uniform(1, 0)), gate: make(chan struct{})}
	meta := vectorfield.Meta{Steps: steps(1), West: 0, East: 51, South: 20, North: 45.4, DLon: 0.2, DLat: 0.2}
	p, err := vectorfield.NewPyramidE(context.Background(), meta, loader, vectorfield.PyramidOptions{})
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.SampleE(context.Background(), vectorfield.Request{West: 10, East: 20, South: 30, North: 40, MaxCols: 50, MaxRows: 50})
			require.NoError(t, err)
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(loader.gate)
	wg.Wait()
	require.EqualValues(t, 1, loader.loads.Load())
}

func TestWaitingForALoadHonoursTheContext(t *testing.T) {
	loader := &countingLoader{inner: regional(vectorfield.Uniform(1, 0)), gate: make(chan struct{})}
	meta := vectorfield.Meta{Steps: steps(1), West: 0, East: 51, South: 20, North: 45.4, DLon: 0.2, DLat: 0.2}
	p, err := vectorfield.NewPyramidE(context.Background(), meta, loader, vectorfield.PyramidOptions{})
	require.NoError(t, err)
	req := vectorfield.Request{West: 10, East: 20, South: 30, North: 40, MaxCols: 50, MaxRows: 50}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = p.SampleE(context.Background(), req)
	}()
	for loader.loads.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = p.SampleE(ctx, req)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	close(loader.gate)
	<-done
}

func TestStepThatDiffersFromTheGeometryIsRefused(t *testing.T) {
	meta := vectorfield.Meta{Steps: steps(1), West: 0, East: 10, South: 0, North: 10, DLon: 1, DLat: 1}
	_, err := func() (vectorfield.Window, error) {
		p, err := vectorfield.NewPyramidE(context.Background(), meta, regional(vectorfield.Uniform(1, 0)), vectorfield.PyramidOptions{})
		require.NoError(t, err)
		return p.SampleE(context.Background(), vectorfield.Request{West: 1, East: 2, South: 1, North: 2, MaxCols: 8, MaxRows: 8})
	}()
	require.Error(t, err)
}
