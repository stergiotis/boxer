package flowoverlay

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// gridOf samples a function of world position onto a grid over [0, extent]².
func gridOf(extent float64, n int, fn func(x, y float64) (vx, vy, speed float32)) *fieldGrid {
	g := &fieldGrid{dx: extent / float64(n-1), dy: extent / float64(n-1), cols: n, rows: n}
	g.vx, g.vy, g.speed = make([]float32, n*n), make([]float32, n*n), make([]float32, n*n)
	for r := range n {
		for c := range n {
			g.vx[r*n+c], g.vy[r*n+c], g.speed[r*n+c] = fn(float64(c)*g.dx, float64(r)*g.dy)
		}
	}
	return g
}

func quietParams() simParams {
	return simParams{trail: 12, maxAge: 1 << 30, minPace: 0.35, maxPace: 3, speedMax: 10, calm: 0.01, seedTries: 4, killMargin: 10}
}

// place puts particle i somewhere by hand, alive and with one point.
func place(s *sim, f *field, i int, x, y float64) {
	s.x[i], s.y[i] = x, y
	s.count[i], s.dying[i], s.age[i] = 1, 0, 0
	s.hereX[i], s.hereY[i], s.hereS[i], _ = f.sample(x, y)
	slot := i*s.params.trail + s.head
	s.trailX[slot], s.trailY[slot] = x, y
}

// Round a closed circulation forward Euler lengthens the radius on every
// step, and a cyclone is drawn as a source. At this step and radius it would
// gain about half its radius over a particle's life; the midpoint rule keeps
// the particle on its circle.
func TestParticleStaysOnItsCircle(t *testing.T) {
	const extent, cx, cy, radius = 400.0, 200.0, 200.0, 40.0
	grid := gridOf(extent, 401, func(x, y float64) (float32, float32, float32) {
		vx, vy := -(y-cy)/4, (x-cx)/4
		return float32(vx), float32(vy), float32(math.Hypot(vx, vy))
	})
	f := field{a: grid}
	s := newSim(1, quietParams())
	s.resize(1)
	place(s, &f, 0, cx+radius, cy)
	view := rect{0, 0, extent, extent}
	worst := 0.0
	for range 150 {
		s.tick(&f, view, 1)
		require.EqualValues(t, 0, s.dying[0])
		worst = math.Max(worst, math.Abs(math.Hypot(s.x[0]-cx, s.y[0]-cy)-radius))
	}
	require.Less(t, worst, radius*0.01, "the orbit closes to within a hundredth of its radius")
	require.Greater(t, math.Abs(math.Atan2(s.y[0]-cy, s.x[0]-cx)), 0.5, "and the particle did go round")
}

// The pace is a screen quantity: the same field moves a particle as many
// pixels a tick at any zoom.
func TestPaceIsTheSameOnScreenAtAnyZoom(t *testing.T) {
	grid := gridOf(400, 5, func(_, _ float64) (float32, float32, float32) { return 5, 0, 5 })
	f := field{a: grid}
	for _, scale := range []float64{0.25, 1, 8, 4096} {
		s := newSim(1, quietParams())
		s.resize(1)
		place(s, &f, 0, 200, 200)
		s.tick(&f, rect{0, 0, 400, 400}, 1/scale)
		require.InDelta(t, 1.5, (s.x[0]-200)*scale, 1e-6, "half of speedMax moves at half of maxPace, at scale %v", scale)
		require.InDelta(t, 200, s.y[0], 1e-12)
	}
}

func TestPaceHasAFloorAndACap(t *testing.T) {
	s := newSim(1, quietParams())
	require.InDelta(t, 0.35, s.pace(0.02), 1e-6, "slow flow creeps, it does not freeze into dots")
	require.InDelta(t, 3, s.pace(250), 1e-6, "a step stays inside a sample")
	require.InDelta(t, 1.5, s.pace(5), 1e-6)
}

// A particle that dies is not taken away in one frame: it stops, and its
// stroke shrinks toward its head over a trail's length of ticks.
func TestDyingParticleDrainsItsTrail(t *testing.T) {
	grid := gridOf(400, 5, func(_, _ float64) (float32, float32, float32) { return 10, 0, 10 })
	f := field{a: grid}
	s := newSim(1, quietParams())
	s.resize(1)
	place(s, &f, 0, 100, 200)
	view := rect{0, 0, 400, 400}
	for range 20 {
		s.tick(&f, view, 1)
	}
	require.EqualValues(t, 12, s.count[0])
	s.retire(0)
	stopX := s.x[0]
	for k := range 11 {
		s.tick(&f, view, 1)
		require.Equal(t, stopX, s.x[0], "a dying particle does not move")
		headX, _, _ := s.point(0, 0)
		tailX, _, _ := s.point(0, 11)
		require.Equal(t, stopX, headX)
		require.InDelta(t, stopX-3*float64(10-k), tailX, 1e-9, "the tail closes in on the head, tick %d", k)
	}
	s.tick(&f, view, 1)
	require.EqualValues(t, 0, s.count[0], "and only then is it free to be seeded again")
}

// Particles are seeded together after a zoom-in. Were they to start at the
// same age they would die together, and the field would pulse.
func TestSeededParticlesStartAtRandomAges(t *testing.T) {
	grid := gridOf(400, 5, func(_, _ float64) (float32, float32, float32) { return 10, 0, 10 })
	f := field{a: grid}
	p := quietParams()
	p.maxAge = 150
	s := newSim(7, p)
	s.resize(2000)
	s.tick(&f, rect{100, 100, 300, 300}, 1)
	buckets := make([]int, 5)
	for i := range s.n {
		require.EqualValues(t, 1, s.count[i])
		buckets[int(s.age[i])*5/150]++
	}
	for b, n := range buckets {
		require.InDelta(t, 400, n, 90, "age fifth %d", b)
	}
}

// Off the data a particle finds no direction and is not placed, which is
// what gathers the particles of a regional field inside it, and a strict
// sample stops them a sample short of its edge.
func TestParticlesKeepToTheData(t *testing.T) {
	nan := float32(math.NaN())
	grid := gridOf(400, 101, func(x, _ float64) (float32, float32, float32) {
		if x < 200 {
			return nan, nan, nan
		}
		return -10, 0, 10 // blowing toward the missing half
	})
	f := field{a: grid}
	p := quietParams()
	p.maxAge = 150
	p.resetBase = 0.004
	s := newSim(3, p)
	s.resize(500)
	for range 300 {
		s.tick(&f, rect{0, 0, 400, 400}, 1)
		for i := range s.n {
			if s.count[i] > 0 {
				require.GreaterOrEqual(t, s.x[i], 200.0, "no particle is ever on the missing side")
			}
		}
	}
}

func uniformWindow(u, v float32, north, south float64) vectorfield.Window {
	win := vectorfield.Window{West: -30, North: north, DLon: 1, DLat: 1, Cols: 61, Rows: int(north-south) + 1}
	n := win.Cols * win.Rows
	win.U, win.V, win.Speed = make([]float32, n), make([]float32, n), make([]float32, n)
	for i := range n {
		win.U[i], win.V[i], win.Speed[i] = u, v, float32(math.Hypot(float64(u), float64(v)))
	}
	return win
}

// East is toward +x and north toward -y on a map, because the projection
// says so and not because a sign was remembered; and the length of a vector
// is the field's, not the projection's.
func TestDirectionGoesThroughTheProjection(t *testing.T) {
	for name, crs := range map[string]portolan.CRSI{"EPSG:3857": portolan.EPSG3857, "EPSG:4326": portolan.EPSG4326} {
		t.Run(name, func(t *testing.T) {
			proj := crsProjection{crs: crs}
			east := uniformWindow(10, 0, 70, 30)
			g := newFieldGrid(&east, proj)
			require.NotNil(t, g)
			for i := range g.vx {
				require.InDelta(t, 10, g.vx[i], 1e-3)
				require.InDelta(t, 0, g.vy[i], 1e-3)
			}
			north := uniformWindow(0, 10, 70, 30)
			g = newFieldGrid(&north, proj)
			for i := range g.vx {
				require.InDelta(t, 0, g.vx[i], 1e-3)
				require.InDelta(t, -10, g.vy[i], 1e-3, "north is up the screen")
				require.InDelta(t, 10, g.speed[i], 1e-3)
			}
		})
	}

	// A conformal projection keeps a north-easterly at 45° at every latitude.
	// Plate carrée stretches east by 1/cos(lat), so at 60° the same wind is
	// drawn twice as far east as north.
	diagonal := uniformWindow(10, 10, 61, 59)
	g := newFieldGrid(&diagonal, crsProjection{crs: portolan.EPSG3857})
	mid := (g.rows/2)*g.cols + g.cols/2
	require.InDelta(t, 1, float64(g.vx[mid])/float64(-g.vy[mid]), 1e-3)
	g = newFieldGrid(&diagonal, crsProjection{crs: portolan.EPSG4326})
	mid = (g.rows/2)*g.cols + g.cols/2
	require.InDelta(t, 2, float64(g.vx[mid])/float64(-g.vy[mid]), 2e-2)
	require.InDelta(t, math.Hypot(10, 10), math.Hypot(float64(g.vx[mid]), float64(g.vy[mid])), 1e-3, "direction goes through the projection, magnitude does not")
}

func TestFieldBlendsTwoStepsAndShowsOneAlone(t *testing.T) {
	nan := float32(math.NaN())
	a := gridOf(400, 5, func(_, _ float64) (float32, float32, float32) { return 10, 0, 10 })
	b := gridOf(400, 5, func(x, _ float64) (float32, float32, float32) {
		if x > 250 {
			return nan, nan, nan
		}
		return -10, 0, 10
	})
	f := field{a: a, b: b, weight: 0.5}
	vx, _, speed, ok := f.sample(100, 100)
	require.True(t, ok)
	require.InDelta(t, 0, vx, 1e-6, "opposed steps blend to a lull by component")
	require.InDelta(t, 10, speed, 1e-6, "while the scalar mean, blended by itself, keeps the colour honest")
	vx, _, _, ok = f.sample(390, 100)
	require.True(t, ok)
	require.InDelta(t, 10, vx, 1e-6, "where one step has nothing the other is shown alone")
}

func TestPositionOffAPeriodicGridIsFolded(t *testing.T) {
	g := gridOf(256, 65, func(x, _ float64) (float32, float32, float32) { return float32(x), 0, float32(x) })
	f := field{a: g, wrap: 256}
	for _, x := range []float64{40, 40 + 256, 40 - 256, 40 + 3*256} {
		vx, _, _, ok := f.sample(x, 10)
		require.True(t, ok, "x = %v", x)
		require.InDelta(t, 40, vx, 1e-3)
	}
	f.wrap = 0
	_, _, _, ok := f.sample(40+256, 10)
	require.False(t, ok, "a regional field is never wrapped")
}
