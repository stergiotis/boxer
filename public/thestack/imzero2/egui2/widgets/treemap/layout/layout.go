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
// Leaf nodes have Size > 0 and no Children. Parent nodes
// derive their size from the sum of their children.
type Node struct {
	Name     string
	Size     float64
	Children []*Node
}

// TotalSize returns the recursive size of the subtree.
func (inst *Node) TotalSize() float64 {
	if len(inst.Children) == 0 {
		if inst.Size > 0 {
			return inst.Size
		}
		return 1
	}
	s := 0.0
	for _, ch := range inst.Children {
		s += ch.TotalSize()
	}
	return s
}

// Layout maps each node to its computed rectangle.
type Layout struct {
	rects map[*Node]Rect
}

// RectOf returns the layout rectangle for a given node.
func (inst *Layout) RectOf(node *Node) Rect {
	return inst.rects[node]
}

// ComputeLayout runs the squarify algorithm on the children of root,
// placing them within the bounding box (0, 0, w, h).
func ComputeLayout(root *Node, w, h float64) *Layout {
	inst := &Layout{rects: make(map[*Node]Rect)}
	if len(root.Children) == 0 {
		inst.rects[root] = Rect{X: 0, Y: 0, W: w, H: h}
		return inst
	}
	areas := make([]float64, len(root.Children))
	for i, ch := range root.Children {
		areas[i] = ch.TotalSize()
	}
	boxes := squarify(Rect{X: 0, Y: 0, W: w, H: h}, areas)
	for i, ch := range root.Children {
		inst.rects[ch] = boxes[i]
	}
	return inst
}

// ComputeLayoutAt runs squarify on root's children within an arbitrary bounding box.
func ComputeLayoutAt(root *Node, bounds Rect) *Layout {
	inst := &Layout{rects: make(map[*Node]Rect)}
	if len(root.Children) == 0 {
		inst.rects[root] = bounds
		return inst
	}
	areas := make([]float64, len(root.Children))
	for i, ch := range root.Children {
		areas[i] = ch.TotalSize()
	}
	boxes := squarify(bounds, areas)
	for i, ch := range root.Children {
		inst.rects[ch] = boxes[i]
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
