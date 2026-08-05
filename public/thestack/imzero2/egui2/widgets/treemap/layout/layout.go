// Package layout provides a squarified treemap layout algorithm and
// immediate-mode rendering helpers for the ImZero2 egui2 framework.
package layout

import (
	"math"
	"sort"
)

// Rect describes a positioned rectangle in the treemap layout.
type Rect struct {
	X, Y, W, H float64
}

// Node is a single element in the treemap hierarchy.
// Leaf nodes have Size > 0 and no Children. A parent's size is its own
// Size plus the sum of its children's.
type Node struct {
	Name     string
	Size     float64
	Children []*Node
}

// ownSize is a node's own contribution to its total: Size when that is a
// positive, finite number, and 0 otherwise. A NaN or infinite Size is treated
// as absent rather than propagated, since either one poisons every area
// squarify derives from the total.
func (inst *Node) ownSize() float64 {
	if inst.Size > 0 && !math.IsInf(inst.Size, 1) {
		return inst.Size
	}
	return 0
}

// TotalSize returns the recursive size of the subtree: the node's OWN Size plus
// every descendant's.
//
// A parent's own size counts (ADR-0166 §SD3). It did not before — the sum ran
// over children alone — which silently dropped the value of any node that had
// both a size of its own and children, and made callers encode that value as a
// synthetic child instead. Nodes whose interior Size is zero, which is every
// caller in this tree at the time of the change, are unaffected.
//
// A childless node with a non-positive size still reports 1, so an unweighted
// tree lays out evenly rather than degenerately.
func (inst *Node) TotalSize() float64 {
	if len(inst.Children) == 0 {
		if s := inst.ownSize(); s > 0 {
			return s
		}
		return 1
	}
	s := inst.ownSize()
	for _, ch := range inst.Children {
		s += ch.TotalSize()
	}
	return s
}

// Layout maps each node to its computed rectangle.
type Layout struct {
	rects map[*Node]Rect
	// selfNode/selfRect carry the rectangle reserved for the laid-out parent's
	// OWN size. At most one node per Layout has one — the root of the call — so
	// this is a pair rather than a second map: ComputeLayoutAt runs per level
	// per frame, and the map it already allocates is the cost worth not
	// doubling.
	selfNode *Node
	selfRect Rect
}

// RectOf returns the layout rectangle for a given node.
func (inst *Layout) RectOf(node *Node) Rect {
	return inst.rects[node]
}

// SelfRectOf returns the rectangle reserved inside node's own box for node's
// own Size, and the zero Rect when there is none — when node is a leaf, when
// its Size is not positive, or when it is not the node this Layout was computed
// for.
//
// A parent's own size needs a rectangle of its own because squarify normalises
// the areas it is given to fill the box exactly: without one, a parent whose
// own size merely inflated its total would have that size redistributed among
// its children, which reads as children larger than they are (ADR-0166 §SD3).
func (inst *Layout) SelfRectOf(node *Node) Rect {
	if node != nil && node == inst.selfNode {
		return inst.selfRect
	}
	return Rect{}
}

// ComputeLayout runs the squarify algorithm on the children of root,
// placing them within the bounding box (0, 0, w, h).
func ComputeLayout(root *Node, w, h float64) *Layout {
	return ComputeLayoutAt(root, Rect{X: 0, Y: 0, W: w, H: h})
}

// ComputeLayoutAt runs squarify on root's children within an arbitrary bounding
// box.
//
// When root carries a positive Size of its own, that size is squarified as one
// more area alongside the children and the rectangle it lands on is recorded as
// the layout's self rect (SelfRectOf). The children's rects therefore tile the
// box MINUS that rectangle rather than the whole box, which is the point: the
// parent's own quantity is drawn rather than dissolved into its children.
func ComputeLayoutAt(root *Node, bounds Rect) *Layout {
	inst := &Layout{rects: make(map[*Node]Rect)}
	if len(root.Children) == 0 {
		inst.rects[root] = bounds
		return inst
	}
	n := len(root.Children)
	own := root.ownSize()
	areas := make([]float64, n, n+1)
	for i, ch := range root.Children {
		areas[i] = ch.TotalSize()
	}
	if own > 0 {
		areas = append(areas, own)
	}
	boxes := squarify(bounds, areas)
	for i, ch := range root.Children {
		inst.rects[ch] = boxes[i]
	}
	if own > 0 {
		inst.selfNode, inst.selfRect = root, boxes[n]
	}
	return inst
}

// =========================================================================
// Squarified Treemaps (Bruls, Huizing, van Wijk, 2000)
// Inlined from github.com/stergiotis/capmap (MIT license)
// =========================================================================

func squarify(box Rect, areas []float64) []Rect {
	type wrapped struct {
		i    int
		area float64
	}
	// Degenerate input: every area zero/negative or no areas at all.
	// Return zero rects in original order so RectOf still answers consistently;
	// the renderer's min-pixel cull will hide them.
	total := 0.0
	for _, a := range areas {
		if a > 0 {
			total += a
		}
	}
	if total <= 0 || box.W <= 0 || box.H <= 0 {
		return make([]Rect, len(areas))
	}
	target := box.W * box.H
	sorted := make([]wrapped, 0, len(areas))
	for i, a := range areas {
		if a > 0 {
			sorted = append(sorted, wrapped{i: i, area: target * a / total})
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].area > sorted[j].area })

	clean := make([]float64, 0, len(areas))
	for _, v := range sorted {
		if v.area > 0 {
			clean = append(clean, v.area)
		}
	}

	s := &sqState{freeSpace: box}
	s.run(clean)

	// Clamp overflows
	maxX := box.X + box.W
	maxY := box.Y + box.H
	for i, b := range s.boxes {
		if d := (b.X + b.W) - maxX; d > 0 {
			s.boxes[i].W -= d
		}
		if d := (b.Y + b.H) - maxY; d > 0 {
			s.boxes[i].H -= d
		}
	}

	// Restore original order
	res := make([]Rect, len(areas))
	for i, wr := range sorted {
		if i < len(clean) && i < len(s.boxes) {
			res[wr.i] = s.boxes[i]
		}
	}
	return res
}

type sqState struct {
	boxes     []Rect
	freeSpace Rect
}

func (inst *sqState) run(areas []float64) {
	inst.sq(areas, nil, math.Min(inst.freeSpace.W, inst.freeSpace.H))
}

func (inst *sqState) sq(unassigned, stack []float64, w float64) {
	if len(unassigned) == 0 {
		inst.stackBoxes(stack)
		return
	}
	if len(stack) == 0 {
		inst.sq(unassigned[1:], []float64{unassigned[0]}, w)
		return
	}
	trial := append(append([]float64(nil), stack...), unassigned[0])
	if highestAR(stack, w) > highestAR(trial, w) {
		inst.sq(unassigned[1:], trial, w)
	} else {
		inst.stackBoxes(stack)
		inst.sq(unassigned, nil, math.Min(inst.freeSpace.W, inst.freeSpace.H))
	}
}

func (inst *sqState) stackBoxes(areas []float64) {
	if len(areas) == 0 {
		return
	}
	stackArea := 0.0
	for _, a := range areas {
		stackArea += a
	}
	if stackArea == 0 {
		return
	}
	totalArea := inst.freeSpace.W * inst.freeSpace.H
	if totalArea == 0 {
		return
	}
	if inst.freeSpace.W >= inst.freeSpace.H {
		// Vertical stacking
		offset := inst.freeSpace.Y
		for _, a := range areas {
			h := inst.freeSpace.H * a / stackArea
			inst.boxes = append(inst.boxes, Rect{
				X: inst.freeSpace.X,
				W: inst.freeSpace.W * stackArea / totalArea,
				Y: offset, H: h,
			})
			offset += h
		}
		used := inst.freeSpace.W * stackArea / totalArea
		inst.freeSpace = Rect{
			X: inst.freeSpace.X + used, W: inst.freeSpace.W - used,
			Y: inst.freeSpace.Y, H: inst.freeSpace.H,
		}
	} else {
		// Horizontal stacking
		offset := inst.freeSpace.X
		for _, a := range areas {
			w := inst.freeSpace.W * a / stackArea
			inst.boxes = append(inst.boxes, Rect{
				X: offset, W: w,
				Y: inst.freeSpace.Y,
				H: inst.freeSpace.H * stackArea / totalArea,
			})
			offset += w
		}
		used := inst.freeSpace.H * stackArea / totalArea
		inst.freeSpace = Rect{
			X: inst.freeSpace.X, W: inst.freeSpace.W,
			Y: inst.freeSpace.Y + used, H: inst.freeSpace.H - used,
		}
	}
}

// highestAR scores a candidate strip (Bruls/Huizing/van Wijk 2000): it
// returns the worst aspect ratio among the strip's tiles assuming they pack
// across a strip of width w. Squarify uses this monotonically: as long as the
// score keeps decreasing when adding another tile, keep extending the strip.
//
// Invariant: callers (squarify) drop zero-area entries before calling. If
// minA == 0 here, v2 = +Inf, which makes Max return +Inf and the caller
// flushes the current strip — that's the right behavior, but the case only
// occurs if the precondition is violated.
func highestAR(areas []float64, w float64) float64 {
	var minA, maxA, totalA float64
	for i, a := range areas {
		totalA += a
		if i == 0 || a < minA {
			minA = a
		}
		if i == 0 || a > maxA {
			maxA = a
		}
	}
	v1 := w * w * maxA / (totalA * totalA)
	v2 := totalA * totalA / (w * w * minA)
	return math.Max(v1, v2)
}
