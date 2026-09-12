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
// motion. centerGravity is 0 for the plain layout.
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
	if centerGravity != 0 {
		cx, cy := w/2, h/2
		for i := 0; i < n; i++ {
			fs.dx[i] += (cx - g.x[i]) * centerGravity
			fs.dy[i] += (cy - g.y[i]) * centerGravity
		}
	}
	// Soft pins: the same term as centre gravity above, per node and per axis
	// (ADR-0224 §SD16). Skipped whole when nothing declared one.
	if g.anyPull {
		for i := 0; i < n; i++ {
			pl := g.pull[i]
			if pl.StrengthX != 0 {
				fs.dx[i] += (pl.X - g.x[i]) * pl.StrengthX
			}
			if pl.StrengthY != 0 {
				fs.dy[i] += (pl.Y - g.y[i]) * pl.StrengthY
			}
		}
	}
	fs.lastDisp = applyDisplacements(g, fs.dx, fs.dy, p.Dt, p.Damping, p.MaxStep)
	fs.steps++
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
// last average displacement at or under epsilon.
func (fs *forceState) settled(epsilon float32) bool {
	return fs.steps > 0 && !math.IsNaN(float64(fs.lastDisp)) && fs.lastDisp <= epsilon
}

func (fs *forceState) reset() {
	fs.steps = 0
	fs.lastDisp = nan32
}
