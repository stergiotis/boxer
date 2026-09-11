package graphview

// Barnes–Hut repulsion (ADR-0224 §SD6). Above barnesHutMinNodes the O(n²)
// pair sum is replaced by a quadtree walk: a cell whose size is small
// relative to its distance from the body acts as one pseudo-body at its
// centre of mass, so each body sees O(log n) cells. Every body's walk is
// independent and visits cells in a fixed order, so the result is
// deterministic and splits across goroutines exactly like the exact rows.
//
// The tree is rebuilt every step; it is struct-of-arrays and its build is a
// small share of a step, so the tree reuse of Gove's d3-force-reuse is not
// worth its staleness here.

// barnesHutMinNodes is the node count from which the quadtree pays for
// itself against the exact pair sum, chosen from the package benchmark.
const barnesHutMinNodes = 256

// defaultTheta is the Barnes–Hut opening angle: a cell is aggregated when
// its width over its distance is below it. 0.9 is d3-force's default
// (theta² = 0.81) and keeps the layout visually indistinguishable from the
// exact sum for the graphs this widget draws.
const defaultTheta = 0.9

// quadMaxDepth caps subdivision so coincident bodies end in one leaf list
// instead of an unbounded descent.
const quadMaxDepth = 32

// quadtree is the Barnes–Hut tree over one step's positions. Cells live in
// struct-of-arrays; a leaf holds a linked list of bodies through next.
type quadtree struct {
	child    []int32 // 4 per cell, -1 when empty
	internal []bool
	first    []int32 // head of the leaf's body list, -1 when none
	cx, cy   []float32
	half     []float32 // half the cell's side
	mass     []float32
	comX     []float32
	comY     []float32

	next []int32 // per body: next body in the same leaf, -1 at the end
}

func (q *quadtree) reset(n int) {
	q.child = q.child[:0]
	q.internal = q.internal[:0]
	q.first = q.first[:0]
	q.cx = q.cx[:0]
	q.cy = q.cy[:0]
	q.half = q.half[:0]
	q.mass = q.mass[:0]
	q.comX = q.comX[:0]
	q.comY = q.comY[:0]
	if cap(q.next) < n {
		q.next = make([]int32, n)
	}
	q.next = q.next[:n]
}

func (q *quadtree) newCell(cx, cy, half float32) int32 {
	c := int32(len(q.internal))
	q.child = append(q.child, -1, -1, -1, -1)
	q.internal = append(q.internal, false)
	q.first = append(q.first, -1)
	q.cx = append(q.cx, cx)
	q.cy = append(q.cy, cy)
	q.half = append(q.half, half)
	q.mass = append(q.mass, 0)
	q.comX = append(q.comX, 0)
	q.comY = append(q.comY, 0)
	return c
}

// quadrant returns which child of cell c holds (x, y): bit 0 east, bit 1
// south (y grows downward in canvas space, the sign is immaterial).
func (q *quadtree) quadrant(c int32, x, y float32) int {
	qd := 0
	if x >= q.cx[c] {
		qd |= 1
	}
	if y >= q.cy[c] {
		qd |= 2
	}
	return qd
}

func (q *quadtree) childCell(c int32, qd int) int32 {
	h := q.half[c] / 2
	cx, cy := q.cx[c]-h, q.cy[c]-h
	if qd&1 != 0 {
		cx += 2 * h
	}
	if qd&2 != 0 {
		cy += 2 * h
	}
	nc := q.newCell(cx, cy, h)
	q.child[4*c+int32(qd)] = nc
	return nc
}

// build makes the tree over the positions. Bodies insert one by one; a leaf
// holding one body splits when a second arrives, until quadMaxDepth, where
// bodies share a leaf list.
func (q *quadtree) build(x, y []float32) {
	n := len(x)
	q.reset(n)
	if n == 0 {
		return
	}
	minX, minY, maxX, maxY := x[0], y[0], x[0], y[0]
	for i := 1; i < n; i++ {
		minX = min(minX, x[i])
		maxX = max(maxX, x[i])
		minY = min(minY, y[i])
		maxY = max(maxY, y[i])
	}
	half := max(maxX-minX, maxY-minY)/2*1.001 + 1e-3
	root := q.newCell((minX+maxX)/2, (minY+maxY)/2, half)
	for b := 0; b < n; b++ {
		q.insert(root, int32(b), x, y)
	}
	q.aggregate(x, y)
}

func (q *quadtree) insert(root, b int32, x, y []float32) {
	bx, by := x[b], y[b]
	c := root
	depth := 0
	for {
		if q.internal[c] {
			qd := q.quadrant(c, bx, by)
			nc := q.child[4*c+int32(qd)]
			if nc < 0 {
				nc = q.childCell(c, qd)
			}
			c = nc
			depth++
			continue
		}
		if q.first[c] < 0 || depth >= quadMaxDepth {
			// Empty leaf, or a full-depth leaf: join the list.
			q.next[b] = q.first[c]
			q.first[c] = b
			return
		}
		// Occupied leaf: push its body down one level, then keep going.
		other := q.first[c]
		q.first[c] = -1
		q.internal[c] = true
		oq := q.quadrant(c, x[other], y[other])
		oc := q.childCell(c, oq)
		q.next[other] = -1
		q.first[oc] = other
	}
}

// aggregate fills mass and centre of mass. Children are created after their
// parent, so a reverse sweep sees every child before its parent.
func (q *quadtree) aggregate(x, y []float32) {
	for c := len(q.internal) - 1; c >= 0; c-- {
		var m, sx, sy float32
		if q.internal[c] {
			for k := 0; k < 4; k++ {
				ch := q.child[4*c+k]
				if ch < 0 {
					continue
				}
				m += q.mass[ch]
				sx += q.comX[ch] * q.mass[ch]
				sy += q.comY[ch] * q.mass[ch]
			}
		} else {
			for b := q.first[c]; b >= 0; b = q.next[b] {
				m++
				sx += x[b]
				sy += y[b]
			}
		}
		q.mass[c] = m
		if m > 0 {
			q.comX[c] = sx / m
			q.comY[c] = sy / m
		}
	}
}

// repulsionBH accumulates the approximate repulsive displacement of bodies
// [lo, hi) with the same force law as repulsionRows. theta2 is the squared
// opening angle; stack is per-caller scratch.
func (q *quadtree) repulsionBH(x, y, dx, dy []float32, k2, eps2, theta2 float32, lo, hi int, stack []int32) []int32 {
	if len(q.internal) == 0 {
		return stack
	}
	for i := lo; i < hi; i++ {
		xi, yi := x[i], y[i]
		var ax, ay float32
		stack = append(stack[:0], 0)
		for len(stack) > 0 {
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if q.internal[c] {
				ddx := xi - q.comX[c]
				ddy := yi - q.comY[c]
				d2 := ddx*ddx + ddy*ddy
				s := 2 * q.half[c]
				if s*s < theta2*d2 {
					if d2 < eps2 {
						d2 = eps2
					}
					f := q.mass[c] * k2 / d2
					ax += ddx * f
					ay += ddy * f
					continue
				}
				for k := 0; k < 4; k++ {
					if ch := q.child[4*c+int32(k)]; ch >= 0 {
						stack = append(stack, ch)
					}
				}
				continue
			}
			for b := q.first[c]; b >= 0; b = q.next[b] {
				if int(b) == i {
					continue
				}
				ddx := xi - x[b]
				ddy := yi - y[b]
				d2 := ddx*ddx + ddy*ddy
				if d2 < eps2 {
					d2 = eps2
				}
				f := k2 / d2
				ax += ddx * f
				ay += ddy * f
			}
		}
		dx[i] += ax
		dy[i] += ay
	}
	return stack
}
