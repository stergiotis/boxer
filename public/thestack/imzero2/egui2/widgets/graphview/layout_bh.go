package graphview

// Barnes–Hut repulsion (ADR-0224 §SD6). Above barnesHutMinNodes the O(n²)
// pair sum is replaced by a quadtree walk: a cell whose size is small
// relative to its distance from the body acts as one pseudo-body at its
// centre of mass, so each body sees O(log n) cells. Every body's walk is
// independent and visits cells in a fixed order, so the result is
// deterministic and splits across goroutines exactly like the exact rows.
//
// The tree is rebuilt every step, and its build is a small share of a
// step, so the tree reuse of Gove's d3-force-reuse is not worth its
// staleness here. The build inserts into struct-of-arrays cells with
// linked leaf lists; the walk reads a second copy of the tree that the
// build lays out afterwards, one record per cell in depth-first order,
// the bodies leaf by leaf beside it, so a visit is one cache line and a
// walk reads forward from a parent to its children.

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

// bhCell is a cell of the walk's tree: an internal cell has half > 0, its
// centre of mass and mass, and its children in child, -1 where empty; a
// leaf has half < 0 and its bodies as order[child[0]:child[1]]. It is one
// half of a cache line.
type bhCell struct {
	comX, comY float32
	half       float32
	mass       float32
	child      [4]int32
}

// bhVisit is a pending cell of the linearisation: the build's cell, and the
// walk-tree parent and quadrant its index is to be written to.
type bhVisit struct {
	old, parent int32
	k           int8
}

// quadtree is the Barnes–Hut tree over one step's positions, in two forms.
// The build's cells live in struct-of-arrays in creation order, a leaf
// holding a linked list of bodies through next. linearize then lays the
// walk's tree out from it: cells in depth-first order, the bodies leaf by
// leaf along that order with their positions copied alongside. The walks
// run over order too, so consecutive bodies share their path through the
// tree and a leaf's bodies are read in sequence rather than chased.
type quadtree struct {
	child    []int32 // 4 per cell, -1 when empty
	internal []bool
	first    []int32 // head of the leaf's body list, -1 when none
	cx, cy   []float32
	half     []float32 // half the cell's side
	next     []int32   // per body: next body in the same leaf, -1 at the end

	cells  []bhCell
	order  []int32
	bx, by []float32
	visit  []bhVisit // scratch for the linearisation
}

func (q *quadtree) reset(n int) {
	q.child = q.child[:0]
	q.internal = q.internal[:0]
	q.first = q.first[:0]
	q.cx = q.cx[:0]
	q.cy = q.cy[:0]
	q.half = q.half[:0]
	if cap(q.next) < n {
		q.next = make([]int32, n)
	}
	q.next = q.next[:n]
	q.cells = q.cells[:0]
	q.order = q.order[:0]
	q.bx = q.bx[:0]
	q.by = q.by[:0]
}

func (q *quadtree) newCell(cx, cy, half float32) int32 {
	c := int32(len(q.internal))
	q.child = append(q.child, -1, -1, -1, -1)
	q.internal = append(q.internal, false)
	q.first = append(q.first, -1)
	q.cx = append(q.cx, cx)
	q.cy = append(q.cy, cy)
	q.half = append(q.half, half)
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
	q.linearize(x, y)
	q.aggregate()
}

// linearize lays the walk's tree out from the build's: a depth-first walk
// numbers the cells in preorder, children in quadrant order, and lists each
// leaf's bodies in its own order with their positions alongside. It runs
// before aggregate, which sums a leaf over its range and, every child
// being numbered after its parent, a parent over its children in one
// reverse sweep.
func (q *quadtree) linearize(x, y []float32) {
	q.visit = append(q.visit[:0], bhVisit{old: 0, parent: -1})
	for len(q.visit) > 0 {
		v := q.visit[len(q.visit)-1]
		q.visit = q.visit[:len(q.visit)-1]
		id := int32(len(q.cells))
		if v.parent >= 0 {
			q.cells[v.parent].child[v.k] = id
		}
		c := v.old
		if q.internal[c] {
			q.cells = append(q.cells, bhCell{half: q.half[c], child: [4]int32{-1, -1, -1, -1}})
			for k := 3; k >= 0; k-- {
				if ch := q.child[4*c+int32(k)]; ch >= 0 {
					q.visit = append(q.visit, bhVisit{old: ch, parent: id, k: int8(k)})
				}
			}
			continue
		}
		lo := int32(len(q.order))
		for b := q.first[c]; b >= 0; b = q.next[b] {
			q.order = append(q.order, b)
			q.bx = append(q.bx, x[b])
			q.by = append(q.by, y[b])
		}
		q.cells = append(q.cells, bhCell{half: -1, child: [4]int32{lo, int32(len(q.order)), -1, -1}})
	}
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

// aggregate fills mass and centre of mass over the walk's tree, children
// before parents.
func (q *quadtree) aggregate() {
	for c := len(q.cells) - 1; c >= 0; c-- {
		cell := &q.cells[c]
		var m, sx, sy float32
		if cell.half > 0 {
			for _, ch := range cell.child {
				if ch < 0 {
					continue
				}
				cc := &q.cells[ch]
				m += cc.mass
				sx += cc.comX * cc.mass
				sy += cc.comY * cc.mass
			}
		} else {
			for u, e := cell.child[0], cell.child[1]; u < e; u++ {
				m++
				sx += q.bx[u]
				sy += q.by[u]
			}
		}
		cell.mass = m
		if m > 0 {
			cell.comX = sx / m
			cell.comY = sy / m
		}
	}
}

// repulsionBH accumulates the approximate repulsive displacement of bodies
// [lo, hi) of the tree order with the same force law as repulsionRows;
// every body is at one position of that order, so the chunks of a row
// split cover them all. theta2 is the squared
// opening angle; stack is per-caller scratch.
func (q *quadtree) repulsionBH(x, y, dx, dy []float32, k2, eps2, theta2 float32, lo, hi int, stack []int32) []int32 {
	if len(q.cells) == 0 {
		return stack
	}
	for t := lo; t < hi; t++ {
		i := q.order[t]
		xi, yi := q.bx[t], q.by[t]
		var ax, ay float32
		stack = append(stack[:0], 0)
		for len(stack) > 0 {
			c := &q.cells[stack[len(stack)-1]]
			stack = stack[:len(stack)-1]
			if c.half > 0 {
				ddx := xi - c.comX
				ddy := yi - c.comY
				d2 := ddx*ddx + ddy*ddy
				s := 2 * c.half
				if s*s < theta2*d2 {
					if d2 < eps2 {
						d2 = eps2
					}
					f := c.mass * k2 / d2
					ax += ddx * f
					ay += ddy * f
					continue
				}
				for _, ch := range c.child {
					if ch >= 0 {
						stack = append(stack, ch)
					}
				}
				continue
			}
			for u, e := c.child[0], c.child[1]; u < e; u++ {
				if q.order[u] == i {
					continue
				}
				ddx := xi - q.bx[u]
				ddy := yi - q.by[u]
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

// repulsionNE is the walk of repulsionBH with the Cauchy kernel of
// ForceModelNeighborEmbedding: a far cell of mass m contributes
// m·q²·(u_i − c) to the displacement and m·q to the normaliser partial
// zi[i], q = 1/(1 + d²/k²), the form of van der Maaten's tree-based t-SNE
// (2014). Same cell order, same determinism.
func (q *quadtree) repulsionNE(x, y, dx, dy, zi []float32, invK2, theta2 float32, lo, hi int, stack []int32) []int32 {
	if len(q.cells) == 0 {
		return stack
	}
	for t := lo; t < hi; t++ {
		i := q.order[t]
		xi, yi := q.bx[t], q.by[t]
		var ax, ay, zs float32
		stack = append(stack[:0], 0)
		for len(stack) > 0 {
			c := &q.cells[stack[len(stack)-1]]
			stack = stack[:len(stack)-1]
			if c.half > 0 {
				ddx := xi - c.comX
				ddy := yi - c.comY
				d2 := ddx*ddx + ddy*ddy
				s := 2 * c.half
				if s*s < theta2*d2 {
					qq := 1 / (1 + d2*invK2)
					f := c.mass * qq * qq
					ax += ddx * f
					ay += ddy * f
					zs += c.mass * qq
					continue
				}
				for _, ch := range c.child {
					if ch >= 0 {
						stack = append(stack, ch)
					}
				}
				continue
			}
			for u, e := c.child[0], c.child[1]; u < e; u++ {
				if q.order[u] == i {
					continue
				}
				ddx := xi - q.bx[u]
				ddy := yi - q.by[u]
				qq := 1 / (1 + (ddx*ddx+ddy*ddy)*invK2)
				f := qq * qq
				ax += ddx * f
				ay += ddy * f
				zs += qq
			}
		}
		dx[i] += ax
		dy[i] += ay
		zi[i] += zs
	}
	return stack
}
