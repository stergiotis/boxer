package graphview

import (
	"math"
	"runtime"
	"sync"
)

// forceState is the Fruchterman–Reingold bookkeeping that persists across
// frames: the displacement scratch, the step counter and the last average
// displacement the settle logic reads.
type forceState struct {
	dx, dy   []float32
	steps    uint64
	lastDisp float32
	tree     quadtree
	stacks   [][]int32 // one traversal stack per worker, index 0 for the serial walk
	// Neighbour-embedding scratch and schedule state (ADR-0227 §SD2).
	zi          []float32 // per-node partial of the repulsion normaliser Z
	lastExag    float32   // the exaggeration the last step used
	annealSteps uint64    // the schedule length the last step ran under
}

// parallelMinNodes is the node count from which the repulsion pass splits
// across goroutines. Below it the fork/join costs more than the rows.
const parallelMinNodes = 512

// idealEdgeLength is egui_graphs' prepare_constants over the canvas area
// (ADR-0224 §SD2): k = sqrt(area / n) · kScale.
func idealEdgeLength(w, h float32, n int, kScale float32) float32 {
	area := max(w*h, 1)
	k := float32(math.Sqrt(float64(area/float32(n)))) * kScale
	if math.IsNaN(float64(k)) || math.IsInf(float64(k), 0) {
		return 0
	}
	return k
}

// step advances the simulation by one iteration over the canvas w×h and
// records the average per-node displacement. Fixed nodes accumulate no
// motion. centerGravity is 0 for the plain layout. The kernel is
// p.Model's; the integrator, the tree and the settle bookkeeping are shared.
func (fs *forceState) step(g *graph, w, h float32, p ForceParams, centerGravity float32) {
	n := g.n()
	if n == 0 {
		return
	}
	k := idealEdgeLength(w, h, n, p.KScale)
	if k <= 0 {
		return
	}
	if cap(fs.dx) < n {
		fs.dx = make([]float32, n)
		fs.dy = make([]float32, n)
	}
	fs.dx = fs.dx[:n]
	fs.dy = fs.dy[:n]
	clear(fs.dx)
	clear(fs.dy)

	switch p.Model {
	case ForceModelNeighborEmbedding:
		fs.forcesNE(g, k, p)
	default:
		fs.lastExag = 0
		fs.annealSteps = 0
		fs.forcesFR(g, k, p)
	}
	if centerGravity != 0 {
		cx, cy := w/2, h/2
		for i := 0; i < n; i++ {
			fs.dx[i] += (cx - g.x[i]) * centerGravity
			fs.dy[i] += (cy - g.y[i]) * centerGravity
		}
	}
	fs.lastDisp = applyDisplacements(g, fs.dx, fs.dy, p.Dt, p.Damping, p.MaxStep)
	fs.steps++
}

// forcesFR accumulates the Fruchterman–Reingold displacements.
func (fs *forceState) forcesFR(g *graph, k float32, p ForceParams) {
	n := g.n()
	k2 := p.CRepulse * k * k
	eps2 := p.Epsilon * p.Epsilon
	workers := 1
	if n >= parallelMinNodes {
		workers = runtime.GOMAXPROCS(0)
	}
	fs.stacks = growTo(fs.stacks, workers)
	if !p.Exact && n >= barnesHutMinNodes {
		fs.tree.build(g.x, g.y)
		theta2 := p.Theta * p.Theta
		parallelRows(n, workers, func(worker, lo, hi int) {
			fs.stacks[worker] = fs.tree.repulsionBH(g.x, g.y, fs.dx, fs.dy, k2, eps2, theta2, lo, hi, fs.stacks[worker])
		})
	} else {
		parallelRows(n, workers, func(_, lo, hi int) {
			repulsionRows(g.x, g.y, fs.dx, fs.dy, k2, eps2, lo, hi)
		})
	}
	attraction(g, fs.dx, fs.dy, k, p.Epsilon, p.CAttract)
}

// neGain scales the neighbour-embedding gradient so that the widget's
// default integrator (Dt·Damping = 0.015) moves a node by about a twelfth
// of its distance to a neighbour per step — the learning rate n/12 of
// Belkina et al. (2019) that t-SNE implementations default to, here folded
// into the force so Dt and Damping keep their meaning across models.
const neGain = 6

// exaggerationAt is the schedule of ADR-0227 §SD2: geometric from
// ExaggerationStart to Exaggeration over ExaggerationSteps steps, then
// constant. Without a schedule it is Exaggeration.
func exaggerationAt(p ForceParams, step uint64) float32 {
	if p.ExaggerationStart <= 0 || p.ExaggerationSteps == 0 || step >= uint64(p.ExaggerationSteps) {
		return p.Exaggeration
	}
	t := float64(step) / float64(p.ExaggerationSteps)
	return float32(float64(p.ExaggerationStart) * math.Pow(float64(p.Exaggeration/p.ExaggerationStart), t))
}

// forcesNE accumulates the t-SNE-kernel displacements (ADR-0227 §SD2).
// Distances are measured in units of the ideal edge length k, so KScale
// sets the picture's scale as it does for FR. Repulsion is the Barnes–Hut
// walk with the Cauchy kernel, its per-node partials of Z folded in slot
// order so the normaliser — and the result — does not depend on the worker
// count; attraction walks the adjacency with strengths normalised over the
// edge ends. Both are multiplied by n·neGain: n undoes the 1/n that
// normalised affinities carry, as t-SNE's learning rate does; the deltas
// are already world units, so no k returns.
func (fs *forceState) forcesNE(g *graph, k float32, p ForceParams) {
	n := g.n()
	exag := exaggerationAt(p, fs.steps)
	fs.lastExag = exag
	fs.annealSteps = 0
	if p.ExaggerationStart > 0 {
		fs.annealSteps = uint64(p.ExaggerationSteps)
	}
	if cap(fs.zi) < n {
		fs.zi = make([]float32, n)
	}
	fs.zi = fs.zi[:n]
	clear(fs.zi)

	invK2 := 1 / (k * k)
	workers := 1
	if n >= parallelMinNodes {
		workers = runtime.GOMAXPROCS(0)
	}
	fs.stacks = growTo(fs.stacks, workers)
	if !p.Exact && n >= barnesHutMinNodes {
		fs.tree.build(g.x, g.y)
		theta2 := p.Theta * p.Theta
		parallelRows(n, workers, func(worker, lo, hi int) {
			fs.stacks[worker] = fs.tree.repulsionNE(g.x, g.y, fs.dx, fs.dy, fs.zi, invK2, theta2, lo, hi, fs.stacks[worker])
		})
	} else {
		parallelRows(n, workers, func(_, lo, hi int) {
			repulsionRowsNE(g.x, g.y, fs.dx, fs.dy, fs.zi, invK2, lo, hi)
		})
	}
	var z float64
	for _, v := range fs.zi {
		z += float64(v)
	}
	gain := float32(neGain) * float32(n)
	if z > 0 {
		rep := gain / float32(z)
		for i := range n {
			fs.dx[i] *= rep
			fs.dy[i] *= rep
		}
	}
	attractionNE(g, fs.dx, fs.dy, invK2, exag*gain)
}

// repulsionRowsNE accumulates, for rows [lo, hi), the unnormalised Cauchy
// repulsion Σ_j q²·(u_i − u_j) in k-units and the partial normaliser
// Σ_j q, with q = 1/(1 + d²/k²) and j ≠ i. The exact pair sum; the tree
// walk in layout_bh.go approximates the same quantities.
func repulsionRowsNE(x, y, dx, dy, zi []float32, invK2 float32, lo, hi int) {
	n := len(x)
	for i := lo; i < hi; i++ {
		xi, yi := x[i], y[i]
		var ax, ay, zs float32
		for j := 0; j < n; j++ {
			if j == i {
				continue
			}
			ddx := xi - x[j]
			ddy := yi - y[j]
			q := 1 / (1 + (ddx*ddx+ddy*ddy)*invK2)
			f := q * q
			ax += ddx * f
			ay += ddy * f
			zs += q
		}
		dx[i] += ax
		dy[i] += ay
		zi[i] += zs
	}
}

// attractionNE pulls each node toward its neighbours by
// gain · (strength / Σ strengths) · q · (u_j − u_i), once per edge end, the
// strength sum taken over every adjacency entry so the affinities sum to
// one as t-SNE's do. Length is ignored: the kernel has one scale.
func attractionNE(g *graph, dx, dy []float32, invK2, gain float32) {
	var sum float64
	for i := range g.ids {
		lo, hi := g.adjStart[i], g.adjStart[i+1]
		for a := lo; a < hi; a++ {
			sum += float64(g.eStr[g.adjEdge[a]])
		}
	}
	if sum <= 0 {
		return
	}
	norm := gain / float32(sum)
	for i := range g.ids {
		xi, yi := g.x[i], g.y[i]
		var ax, ay float32
		lo, hi := g.adjStart[i], g.adjStart[i+1]
		for a := lo; a < hi; a++ {
			j := g.adjList[a]
			e := g.adjEdge[a]
			ddx := g.x[j] - xi
			ddy := g.y[j] - yi
			q := 1 / (1 + (ddx*ddx+ddy*ddy)*invK2)
			f := norm * g.eStr[e] * q
			ax += ddx * f
			ay += ddy * f
		}
		dx[i] += ax
		dy[i] += ay
	}
}

// repulsionRows accumulates the exact repulsive displacement of rows
// [lo, hi). Full n² rather than the symmetric pair loop: every row is
// independent of every other, which is what lets the rows split across
// goroutines and keeps the inner loop free of scattered writes (ADR-0224
// §SD6). The force k²/d applied along delta/d is delta · k²/d², so no
// square root is needed. Above barnesHutMinNodes the quadtree in
// layout_bh.go replaces this unless ForceParams.Exact asks otherwise.
func repulsionRows(x, y, dx, dy []float32, k2, eps2 float32, lo, hi int) {
	n := len(x)
	for i := lo; i < hi; i++ {
		xi, yi := x[i], y[i]
		var ax, ay float32
		for j := 0; j < n; j++ {
			ddx := xi - x[j]
			ddy := yi - y[j]
			d2 := ddx*ddx + ddy*ddy
			if d2 < eps2 {
				d2 = eps2
			}
			f := k2 / d2
			ax += ddx * f
			ay += ddy * f
		}
		dx[i] += ax
		dy[i] += ay
	}
}

// parallelRows runs body over [0, n) split into up to workers contiguous
// chunks, one goroutine each; with one worker it runs inline. Each row is
// summed whole by one call, so the split changes no result.
func parallelRows(n, workers int, body func(worker, lo, hi int)) {
	if workers <= 1 || n < workers {
		body(0, 0, n)
		return
	}
	chunk := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for w, lo := 0, 0; lo < n; w, lo = w+1, lo+chunk {
		hi := min(lo+chunk, n)
		wg.Add(1)
		go func(w, lo, hi int) {
			defer wg.Done()
			body(w, lo, hi)
		}(w, lo, hi)
	}
	wg.Wait()
}

// attraction pulls each node toward its neighbours with force d²/k along the
// edge, once per edge end, which is the crate's neighbors_undirected walk.
// An edge's Length multiplies k for that edge and its Strength multiplies
// the pull (ADR-0224 §SD13); both are 1 unless declared.
func attraction(g *graph, dx, dy []float32, k, eps, cAttract float32) {
	for i := range g.ids {
		xi, yi := g.x[i], g.y[i]
		var ax, ay float32
		lo, hi := g.adjStart[i], g.adjStart[i+1]
		for a := lo; a < hi; a++ {
			j := g.adjList[a]
			e := g.adjEdge[a]
			ddx := g.x[j] - xi
			ddy := g.y[j] - yi
			d := float32(math.Sqrt(float64(ddx*ddx + ddy*ddy)))
			if d < eps {
				d = eps
			}
			// (delta / d) · (cAttract · d² / k) = delta · cAttract · d / k
			f := cAttract * g.eStr[e] * d / (k * g.eLen[e])
			ax += ddx * f
			ay += ddy * f
		}
		dx[i] += ax
		dy[i] += ay
	}
}

// applyDisplacements moves every node that is not fixed by disp · dt · damping,
// clamped to maxStep, and returns the average clamped step length — the
// settle metric, 0 when nothing could move. Non-finite results leave the
// node where it was.
func applyDisplacements(g *graph, dx, dy []float32, dt, damping, maxStep float32) float32 {
	var sum float32
	count := 0
	scale := dt * damping
	for i := range g.ids {
		if g.fixed[i] {
			continue
		}
		sx, sy := dx[i]*scale, dy[i]*scale
		l := float32(math.Sqrt(float64(sx*sx + sy*sy)))
		if l > maxStep {
			s := maxStep / l
			sx *= s
			sy *= s
			l = maxStep
		}
		nx, ny := g.x[i]+sx, g.y[i]+sy
		if math.IsNaN(float64(nx)) || math.IsInf(float64(nx), 0) || math.IsNaN(float64(ny)) || math.IsInf(float64(ny), 0) {
			continue
		}
		g.x[i], g.y[i] = nx, ny
		sum += l
		count++
	}
	if count == 0 {
		return 0
	}
	g.posVer++
	return sum / float32(count)
}

// settled reports the crate's settle predicate: at least one step, and the
// last average displacement at or under epsilon — and, under a
// neighbour-embedding schedule, the schedule run to its end, since a
// layout at rest under strong attraction is not the layout asked for.
func (fs *forceState) settled(epsilon float32) bool {
	return fs.steps > 0 && fs.steps >= fs.annealSteps && !math.IsNaN(float64(fs.lastDisp)) && fs.lastDisp <= epsilon
}

func (fs *forceState) reset() {
	fs.steps = 0
	fs.lastDisp = nan32
	fs.lastExag = 0
	fs.annealSteps = 0
}
