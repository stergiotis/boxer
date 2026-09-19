package flowoverlay

import (
	"math"
	"math/rand/v2"
)

// rect is an axis-aligned box in world units.
type rect struct{ x0, y0, x1, y1 float64 }

func (r rect) grown(frac float64) rect {
	px, py := (r.x1-r.x0)*frac, (r.y1-r.y0)*frac
	return rect{r.x0 - px, r.y0 - py, r.x1 + px, r.y1 + py}
}

func (r rect) contains(x, y float64) bool {
	return x >= r.x0 && x <= r.x1 && y >= r.y0 && y <= r.y1
}

// simParams is the part of Options the simulation reads, resolved.
type simParams struct {
	trail      int     // trail points kept per particle
	maxAge     int32   // ticks
	resetBase  float32 // probability per tick
	resetBump  float32 // more of it at full speed
	minPace    float32 // pixels per tick
	maxPace    float32
	speedMax   float32 // the magnitude that moves at maxPace
	calm       float32 // a vector mean shorter than this has no direction
	seedTries  int
	killMargin float64 // fraction of the viewport a particle may stray past it
}

// sim is the particles. Positions are float64 world units: a pan moves
// nothing, and a high zoom does not quantise them the way 32 bits would.
//
// Each particle keeps its last params.trail positions in a ring; every
// particle writes a point on every tick, so the ring's head is shared. count
// is how many of a particle's points are its own since it was last seeded — a
// new particle's trail grows from nothing. A particle that dies is not taken
// away: it stops, and for a trail's length of ticks writes the place it
// stopped at, so its stroke shrinks toward its head and is gone before the
// particle is seeded again. Taking the stroke away in one frame is the
// popping that insertion and deletion are known for.
type sim struct {
	rng    *rand.Rand
	params simParams

	n     int
	x, y  []float64
	age   []int32
	count []int32
	dying []int32 // ticks left of draining the trail; 0 while alive
	// The field where the particle stands, kept from the tick that put it
	// there: a particle only ever stands where the field has a direction.
	hereX, hereY, hereS []float32
	trailX              []float64 // n * trail, particle-major
	trailY              []float64
	trailS              []float32 // scalar-mean speed at each point, for colour
	head                int       // ring slot of the newest point
	ticks               uint64
}

func newSim(seed uint64, params simParams) (inst *sim) {
	return &sim{rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), params: params}
}

// resize sets the particle count. New particles are dormant until the next
// tick seeds them; on shrinking, the last ones go.
func (inst *sim) resize(n int) {
	if n == inst.n {
		return
	}
	k := inst.params.trail
	if n < inst.n {
		inst.x, inst.y = inst.x[:n], inst.y[:n]
		inst.age, inst.count, inst.dying = inst.age[:n], inst.count[:n], inst.dying[:n]
		inst.hereX, inst.hereY, inst.hereS = inst.hereX[:n], inst.hereY[:n], inst.hereS[:n]
		inst.trailX, inst.trailY, inst.trailS = inst.trailX[:n*k], inst.trailY[:n*k], inst.trailS[:n*k]
		inst.n = n
		return
	}
	grow := n - inst.n
	inst.x = append(inst.x, make([]float64, grow)...)
	inst.y = append(inst.y, make([]float64, grow)...)
	inst.age = append(inst.age, make([]int32, grow)...)
	inst.count = append(inst.count, make([]int32, grow)...)
	inst.dying = append(inst.dying, make([]int32, grow)...)
	inst.hereX = append(inst.hereX, make([]float32, grow)...)
	inst.hereY = append(inst.hereY, make([]float32, grow)...)
	inst.hereS = append(inst.hereS, make([]float32, grow)...)
	inst.trailX = append(inst.trailX, make([]float64, grow*k)...)
	inst.trailY = append(inst.trailY, make([]float64, grow*k)...)
	inst.trailS = append(inst.trailS, make([]float32, grow*k)...)
	inst.n = n // the new ones have no points, so the next tick seeds them
}

// seed places particle i uniformly in the viewport, where the field has a
// direction; it stays dormant this tick if a few tries find none, which is
// what concentrates the particles of a regional field inside it. A seeded
// particle starts at a random age, so that those seeded together — all of
// them, after a zoom-in — do not die together and pulse.
func (inst *sim) seed(i int, f *field, viewport rect) (ok bool) {
	for range inst.params.seedTries {
		x := viewport.x0 + inst.rng.Float64()*(viewport.x1-viewport.x0)
		y := viewport.y0 + inst.rng.Float64()*(viewport.y1-viewport.y0)
		vx, vy, speed, has := f.sample(x, y)
		if !has || float32(math.Hypot(float64(vx), float64(vy))) < inst.params.calm {
			continue
		}
		inst.x[i], inst.y[i] = x, y
		inst.hereX[i], inst.hereY[i], inst.hereS[i] = vx, vy, speed
		inst.age[i] = inst.rng.Int32N(inst.params.maxAge)
		inst.count[i] = 0
		inst.dying[i] = 0
		return true
	}
	inst.count[i] = 0
	inst.dying[i] = 0
	return false
}

// retire stops particle i and starts draining its trail.
func (inst *sim) retire(i int) {
	inst.dying[i] = int32(inst.params.trail)
}

// pace is how far a particle moves in a tick, in pixels: linear in the
// magnitude, with a floor so slow flow creeps instead of freezing into dots,
// and a cap that keeps a step inside a sample.
func (inst *sim) pace(magnitude float32) float32 {
	p := inst.params
	return min(max(magnitude/p.speedMax*p.maxPace, p.minPace), p.maxPace)
}

// tick advances every particle once. worldPerPixel converts the pace, which
// is a screen quantity, to world units at the current zoom.
func (inst *sim) tick(f *field, viewport rect, worldPerPixel float64) {
	if inst.n == 0 || f.isEmpty() {
		return
	}
	p := inst.params
	k := p.trail
	inst.head = (inst.head + 1) % k
	inst.ticks++
	keep := viewport.grown(p.killMargin)

	for i := range inst.n {
		base := i * k
		slot := base + inst.head
		if inst.count[i] == 0 {
			if inst.seed(i, f, viewport) {
				inst.trailX[slot], inst.trailY[slot], inst.trailS[slot] = inst.x[i], inst.y[i], inst.hereS[i]
				inst.count[i] = 1
			}
			continue
		}
		if inst.dying[i] > 0 {
			prev := base + (inst.head-1+k)%k
			inst.trailX[slot], inst.trailY[slot], inst.trailS[slot] = inst.x[i], inst.y[i], inst.trailS[prev]
			if inst.count[i] < int32(k) {
				inst.count[i]++
			}
			inst.dying[i]--
			if inst.dying[i] == 0 {
				inst.count[i] = 0
			}
			continue
		}

		// The midpoint rule: half a step along the direction here, then the
		// whole step along the direction found there. The step is taken only
		// if the field has a direction where it lands, so a particle stops
		// short of a coast, a calm or the data's edge and never stands past it.
		x, y := inst.x[i], inst.y[i]
		vx, vy := inst.hereX[i], inst.hereY[i]
		m := float32(math.Hypot(float64(vx), float64(vy)))
		h := float64(inst.pace(m)) * worldPerPixel
		vx, vy, speed, ok := f.sample(x+float64(vx/m)*h/2, y+float64(vy/m)*h/2)
		m = float32(math.Hypot(float64(vx), float64(vy)))
		if ok && m >= p.calm {
			h = float64(inst.pace(m)) * worldPerPixel
			x += float64(vx/m) * h
			y += float64(vy/m) * h
			vx, vy, speed, ok = f.sample(x, y)
			m = float32(math.Hypot(float64(vx), float64(vy)))
		}
		if !ok || m < p.calm {
			inst.retire(i)
			prev := base + (inst.head-1+k)%k
			inst.trailX[slot], inst.trailY[slot], inst.trailS[slot] = inst.x[i], inst.y[i], inst.trailS[prev]
			continue
		}
		inst.hereX[i], inst.hereY[i], inst.hereS[i] = vx, vy, speed

		inst.x[i], inst.y[i] = x, y
		inst.trailX[slot], inst.trailY[slot], inst.trailS[slot] = x, y, speed
		if inst.count[i] < int32(k) {
			inst.count[i]++
		}
		inst.age[i]++

		// Fast particles cover more pixels, so fast regions would look
		// denser; re-seeding them sooner evens it out.
		rel := min(speed/p.speedMax, 1)
		if inst.age[i] >= p.maxAge || !keep.contains(x, y) || inst.rng.Float32() < p.resetBase+p.resetBump*rel {
			inst.retire(i)
		}
	}
}

// point returns particle i's trail point back ticks before the newest.
func (inst *sim) point(i, back int) (x, y float64, s float32) {
	k := inst.params.trail
	slot := i*k + ((inst.head-back)%k+k)%k
	return inst.trailX[slot], inst.trailY[slot], inst.trailS[slot]
}
