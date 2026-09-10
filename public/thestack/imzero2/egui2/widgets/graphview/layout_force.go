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
	stack    []int32
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
// records the average per-node displacement. Pinned nodes accumulate no
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
	parallel := n >= parallelMinNodes && runtime.GOMAXPROCS(0) > 1
	switch {
	case !p.Exact && n >= barnesHutMinNodes:
		fs.tree.build(g.x, g.y)
		theta2 := p.Theta * p.Theta
		if parallel {
			fs.tree.repulsionBHParallel(g.x, g.y, fs.dx, fs.dy, k2, eps2, theta2)
		} else {
			fs.stack = fs.tree.repulsionBH(g.x, g.y, fs.dx, fs.dy, k2, eps2, theta2, 0, n, fs.stack)
		}
	case parallel:
		repulsionParallel(g.x, g.y, fs.dx, fs.dy, k2, eps2)
	default:
		repulsionRows(g.x, g.y, fs.dx, fs.dy, k2, eps2, 0, n)
	}
	attraction(g, fs.dx, fs.dy, k, p.Epsilon, p.CAttract)
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

func repulsionParallel(x, y, dx, dy []float32, k2, eps2 float32) {
	n := len(x)
	workers := runtime.GOMAXPROCS(0)
	chunk := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for lo := 0; lo < n; lo += chunk {
		hi := min(lo+chunk, n)
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			repulsionRows(x, y, dx, dy, k2, eps2, lo, hi)
		}(lo, hi)
	}
	wg.Wait()
}

// attraction pulls each node toward its neighbours with force d²/k along the
// edge, once per edge end, which is the crate's neighbors_undirected walk.
func attraction(g *graph, dx, dy []float32, k, eps, cAttract float32) {
	for i := range g.ids {
		xi, yi := g.x[i], g.y[i]
		var ax, ay float32
		for _, j := range g.neighbors(int32(i)) {
			ddx := g.x[j] - xi
			ddy := g.y[j] - yi
			d := float32(math.Sqrt(float64(ddx*ddx + ddy*ddy)))
			if d < eps {
				d = eps
			}
			// (delta / d) · (cAttract · d² / k) = delta · cAttract · d / k
			f := cAttract * d / k
			ax += ddx * f
			ay += ddy * f
		}
		dx[i] += ax
		dy[i] += ay
	}
}

// applyDisplacements moves every unpinned node by disp · dt · damping,
// clamped to maxStep, and returns the average clamped step length — the
// settle metric. Non-finite results leave the node where it was.
func applyDisplacements(g *graph, dx, dy []float32, dt, damping, maxStep float32) float32 {
	var sum float32
	count := 0
	scale := dt * damping
	for i := range g.ids {
		if g.pinned[i] {
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
		return nan32
	}
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
