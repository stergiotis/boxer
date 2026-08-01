package sankey

import (
	"cmp"
	"math"
	"slices"
)

// NodeLayout is one positioned node bar, in the unit box (ADR-0159 SD1):
// x and y in [0,1], y in plot convention, so Y1 is the bar's top edge on
// screen and Y0 its bottom.
type NodeLayout struct {
	ID    string
	Label string
	// Stage is the column, 0-based from the left.
	Stage int
	// Index is the position within the stage, 0 at the top.
	Index int
	// X0, X1 are the bar's left and right edges; X1-X0 is Options.NodeWidth.
	X0, X1 float64
	// Y0, Y1 are the bar's bottom and top edges, Y0 < Y1. The height is the
	// node's value through the diagram-wide scale.
	Y0, Y1 float64
	// Value is max(In, Out) — what the bar's height encodes.
	Value float64
	// In, Out are the summed inbound and outbound link values.
	In, Out float64
	// Color is Node.Color, unchanged; 0 means "renderer decides".
	Color uint32
}

// LinkLayout is one positioned ribbon. The two faces are vertical spans on
// the source's right edge and the target's left edge; both are exactly
// Value * Layout.Scale tall, which is what makes a node bar an exact
// subdivision of its own value.
type LinkLayout struct {
	// Source, Target index into Layout.Nodes.
	Source, Target int
	// SX is the source face's x (the source bar's right edge); SY0, SY1 its
	// vertical span, SY0 < SY1.
	SX, SY0, SY1 float64
	// TX is the target face's x (the target bar's left edge); TY0, TY1 its
	// vertical span.
	TX, TY0, TY1 float64
	// Value is the flow this ribbon carries.
	Value float64
	Label string
	Color uint32
}

// Thickness is the ribbon's vertical extent, equal on both faces.
func (l LinkLayout) Thickness() float64 { return l.SY1 - l.SY0 }

// Layout is the computed geometry plus what the computation noticed.
type Layout struct {
	Nodes []NodeLayout
	Links []LinkLayout
	// Stages is the number of columns.
	Stages int
	// Scale is the unit-box height of one unit of value — the single
	// diagram-wide factor. It is deliberately not per-stage: a per-stage
	// scale would let a ribbon change width mid-flight and destroy the
	// conservation reading.
	Scale float64
	// NodeWidth is the effective bar width, and NodePad the effective gap,
	// after clamping to fit the box.
	NodeWidth, NodePad float64
	Report             Report
}

// internal working node, in y-down coordinates (0 = top of the box).
type wnode struct {
	id       string
	label    string
	stage    int
	order    float64
	color    uint32
	in, out  float64
	value    float64
	y0, y1   float64
	outLinks []int
	inLinks  []int
}

func (n *wnode) center() float64 { return (n.y0 + n.y1) / 2 }

// Compute lays d out into the unit box. It validates first, so a malformed
// diagram returns an error rather than partial geometry.
//
// The result is deterministic: every ordering has an explicit tie-breaker
// (node id), and no map is iterated. Two runs on equal input are deep-equal,
// which is what lets a host memoize on a diagram fingerprint.
func Compute(d Diagram, opts Options) (*Layout, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	o := opts.withDefaults()

	index := make(map[string]int, len(d.Nodes))
	ns := make([]wnode, len(d.Nodes))
	for i, n := range d.Nodes {
		label := n.Label
		if label == "" {
			label = n.ID
		}
		ns[i] = wnode{id: n.ID, label: label, stage: n.Stage, order: n.Order, color: n.Color}
		index[n.ID] = i
	}
	type wlink struct {
		s, t  int
		value float64
		label string
		color uint32
	}
	ls := make([]wlink, len(d.Links))
	for i, l := range d.Links {
		s, t := index[l.Source], index[l.Target]
		ls[i] = wlink{s: s, t: t, value: l.Value, label: l.Label, color: l.Color}
		ns[s].out += l.Value
		ns[t].in += l.Value
		ns[s].outLinks = append(ns[s].outLinks, i)
		ns[t].inLinks = append(ns[t].inLinks, i)
	}
	for i := range ns {
		ns[i].value = math.Max(ns[i].in, ns[i].out)
	}

	stages := 1
	if d.Mode == ModeSankey {
		stages = assignStages(ns, func(i int) []int { return ns[i].outLinks },
			func(li int) int { return ls[li].t }, o.Align)
	} else {
		for i := range ns {
			stages = max(stages, ns[i].stage+1)
		}
	}

	// The bar width has to leave room for the stages either side of it, and
	// how much room there is depends on how many stages there are — which is
	// only known now.
	width := clampNodeWidth(o.NodeWidth, stages)

	// Stage membership, in insertion order — the starting point both modes
	// reorder from.
	byStage := make([][]int, stages)
	for i := range ns {
		byStage[ns[i].stage] = append(byStage[ns[i].stage], i)
	}

	// The effective pad has to leave room for the bars: a stage of many nodes
	// would otherwise ask for more padding than the box has.
	pad := o.NodePad
	widest := 0
	for _, st := range byStage {
		widest = max(widest, len(st))
	}
	if widest > 1 {
		pad = math.Min(pad, 0.9/float64(widest-1))
	}

	// One scale for the whole diagram: the tightest stage decides.
	scale := math.Inf(1)
	for _, st := range byStage {
		sum := 0.0
		for _, i := range st {
			sum += ns[i].value
		}
		if sum <= 0 {
			continue
		}
		avail := 1 - float64(len(st)-1)*pad
		if avail <= 0 {
			continue
		}
		scale = math.Min(scale, avail/sum)
	}
	if math.IsInf(scale, 1) || scale <= 0 {
		scale = 0
	}

	if d.Mode == ModeAlluvial {
		for _, st := range byStage {
			slices.SortStableFunc(st, func(a, b int) int {
				if c := cmp.Compare(ns[a].order, ns[b].order); c != 0 {
					return c
				}
				if c := cmp.Compare(ns[b].value, ns[a].value); c != 0 { // value descending
					return c
				}
				return cmp.Compare(ns[a].id, ns[b].id)
			})
		}
	}
	stackStages(ns, byStage, scale, pad)

	if d.Mode == ModeSankey {
		ends := make([]linkEnds, len(ls))
		for i := range ls {
			ends[i] = linkEnds{s: ls[i].s, t: ls[i].t, value: ls[i].value}
		}
		relax(ns, ends, byStage, pad, o.Iterations)
	}

	// Final within-stage order, top to bottom, is what Index reports.
	for _, st := range byStage {
		slices.SortStableFunc(st, func(a, b int) int {
			if c := cmp.Compare(ns[a].y0, ns[b].y0); c != 0 {
				return c
			}
			return cmp.Compare(ns[a].id, ns[b].id)
		})
	}

	lay := &Layout{
		Nodes:     make([]NodeLayout, len(ns)),
		Links:     make([]LinkLayout, len(ls)),
		Stages:    stages,
		Scale:     scale,
		NodeWidth: width,
		NodePad:   pad,
		Report:    Report{Unit: d.Unit},
	}
	for s, st := range byStage {
		x0 := stageX(s, stages, width)
		for k, i := range st {
			n := &ns[i]
			lay.Nodes[i] = NodeLayout{
				ID: n.id, Label: n.label, Stage: s, Index: k,
				X0: x0, X1: x0 + width,
				Y0: 1 - n.y1, Y1: 1 - n.y0, // flip into plot convention
				Value: n.value, In: n.in, Out: n.out, Color: n.color,
			}
		}
	}

	// Face allocation: at each node the links stack in far-end-y order, the
	// single rule that removes most local crossings.
	srcCur := make([]float64, len(ns))
	dstCur := make([]float64, len(ns))
	for i := range ns {
		srcCur[i] = ns[i].y0
		dstCur[i] = ns[i].y0
	}
	order := make([]int, len(ls))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		// Outgoing faces first, ordered by the target's centre; the incoming
		// pass below reuses the same sweep keyed on the source's centre.
		if c := cmp.Compare(ns[ls[a].s].stage, ns[ls[b].s].stage); c != 0 {
			return c
		}
		if c := cmp.Compare(ns[ls[a].s].y0, ns[ls[b].s].y0); c != 0 {
			return c
		}
		if c := cmp.Compare(ns[ls[a].t].center(), ns[ls[b].t].center()); c != 0 {
			return c
		}
		return cmp.Compare(ns[ls[a].t].id, ns[ls[b].t].id)
	})
	for _, li := range order {
		l := &ls[li]
		th := l.value * scale
		sy0 := srcCur[l.s]
		srcCur[l.s] += th
		lay.Links[li] = LinkLayout{
			Source: l.s, Target: l.t,
			SX: lay.Nodes[l.s].X1, SY0: 1 - (sy0 + th), SY1: 1 - sy0,
			TX:    lay.Nodes[l.t].X0,
			Value: l.value, Label: l.label, Color: l.color,
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		if c := cmp.Compare(ns[ls[a].t].stage, ns[ls[b].t].stage); c != 0 {
			return c
		}
		if c := cmp.Compare(ns[ls[a].t].y0, ns[ls[b].t].y0); c != 0 {
			return c
		}
		if c := cmp.Compare(ns[ls[a].s].center(), ns[ls[b].s].center()); c != 0 {
			return c
		}
		return cmp.Compare(ns[ls[a].s].id, ns[ls[b].s].id)
	})
	for _, li := range order {
		l := &ls[li]
		th := l.value * scale
		ty0 := dstCur[l.t]
		dstCur[l.t] += th
		lay.Links[li].TY0 = 1 - (ty0 + th)
		lay.Links[li].TY1 = 1 - ty0
	}

	for i := range ls {
		if ls[i].value*scale < o.ThinFrac {
			lay.Report.ThinLinks++
		}
	}
	for i := range ns {
		n := &ns[i]
		// What enters the diagram is what leaves the nodes nothing enters —
		// counting links instead would count a multi-stage flow once per
		// stage it crosses.
		if n.in == 0 {
			lay.Report.Total += n.out
		}
		if n.in > 0 && n.out > 0 && math.Abs(n.in-n.out) > conserveEps*math.Max(n.in, n.out) {
			lay.Report.NonConserving = append(lay.Report.NonConserving, n.id)
		}
	}
	return lay, nil
}

// linkEnds is the slice of a link that relaxation cares about.
type linkEnds struct {
	s, t  int
	value float64
}

// relaxDecay damps successive sweeps so the stacks settle instead of
// oscillating between the two directions.
const relaxDecay = 0.99

// relax runs barycentre sweeps: each node is pulled toward the
// value-weighted mean of the centres it is joined to, alternating the
// direction it looks in, with collisions resolved after every sweep. This is
// the practical answer to a crossing-minimization problem that is NP-hard in
// general (ADR-0159); it is a heuristic, and the layout tests assert
// determinism and the structural invariants rather than crossing quality.
func relax(ns []wnode, ls []linkEnds, byStage [][]int, pad float64, iterations int) {
	alpha := 1.0
	for range iterations {
		alpha *= relaxDecay
		// Right to left: look at where our targets sit.
		for s := len(byStage) - 2; s >= 0; s-- {
			for _, i := range byStage[s] {
				pullToward(ns, ls, i, ns[i].outLinks, func(l linkEnds) int { return l.t }, alpha)
			}
			resolveCollisions(ns, byStage[s], pad)
		}
		// Left to right: look at where our sources sit.
		for s := 1; s < len(byStage); s++ {
			for _, i := range byStage[s] {
				pullToward(ns, ls, i, ns[i].inLinks, func(l linkEnds) int { return l.s }, alpha)
			}
			resolveCollisions(ns, byStage[s], pad)
		}
	}
}

// pullToward shifts node i by alpha times the gap between its centre and the
// value-weighted centre of the far ends of the given links.
func pullToward(ns []wnode, ls []linkEnds, i int, links []int, far func(linkEnds) int, alpha float64) {
	if len(links) == 0 {
		return
	}
	num, den := 0.0, 0.0
	for _, li := range links {
		l := ls[li]
		num += ns[far(l)].center() * l.value
		den += l.value
	}
	if den <= 0 {
		return
	}
	dy := (num/den - ns[i].center()) * alpha
	ns[i].y0 += dy
	ns[i].y1 += dy
}

// stageX places a stage's left edge so that stage 0 is flush left and the
// last stage flush right.
func stageX(s int, stages int, w float64) float64 {
	if stages <= 1 {
		return 0.5 - w/2
	}
	return float64(s) * (1 - w) / float64(stages-1)
}

// maxNodeWidthShare is how much of the distance between two stages a bar may
// take. Half leaves the ribbon at least as much room as the bar it leaves
// from.
const maxNodeWidthShare = 0.5

// clampNodeWidth caps the bar width so that adjacent stages cannot overlap.
// stageX spreads the stages' left edges (1-w)/(stages-1) apart, so the exact
// bound is w <= share/(stages-1+share); share/stages is the simpler and
// slightly stricter form of it.
//
// Without the cap a wide bar on a long diagram puts a stage's right edge past
// the next stage's left one, and every ribbon then runs backwards. That is
// not merely ugly: LinkLayout.Sample's x would descend, which breaks both the
// ear clipper's simple-ring precondition (ADR-0159 SD2) and the binary search
// in Ribbon.Contains (SD3), so ribbons would fill as self-intersecting
// garbage and the hit test would miss them.
func clampNodeWidth(w float64, stages int) float64 {
	if stages <= 1 {
		return math.Min(w, 1)
	}
	return math.Min(w, maxNodeWidthShare/float64(stages))
}

// assignStages derives the column of every node by longest path, then applies
// the alignment. It assumes the graph is acyclic (Validate has checked).
func assignStages(ns []wnode, outOf func(int) []int, targetOf func(int) int, align Align) int {
	n := len(ns)
	depth := make([]int, n)
	// Longest path from the sources: repeatedly relax in topological waves.
	// The graph is acyclic, so n-1 waves suffice and usually far fewer.
	for range n {
		changed := false
		for v := range n {
			for _, li := range outOf(v) {
				w := targetOf(li)
				if depth[w] < depth[v]+1 {
					depth[w] = depth[v] + 1
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	maxDepth := 0
	for _, dv := range depth {
		maxDepth = max(maxDepth, dv)
	}
	switch align {
	case AlignLeft:
		// depth as computed
	case AlignJustify:
		for v := range n {
			if len(outOf(v)) == 0 {
				depth[v] = maxDepth
			}
		}
	case AlignRight:
		// Height = longest path to a sink; place at maxDepth - height.
		height := make([]int, n)
		for range n {
			changed := false
			for v := range n {
				for _, li := range outOf(v) {
					w := targetOf(li)
					if height[v] < height[w]+1 {
						height[v] = height[w] + 1
						changed = true
					}
				}
			}
			if !changed {
				break
			}
		}
		for v := range n {
			depth[v] = maxDepth - height[v]
		}
	case AlignCenter:
		// A node with no inbound links sits just before its earliest target.
		hasIn := make([]bool, n)
		for v := range n {
			for _, li := range outOf(v) {
				hasIn[targetOf(li)] = true
			}
		}
		for v := range n {
			if hasIn[v] || len(outOf(v)) == 0 {
				continue
			}
			best := maxDepth
			for _, li := range outOf(v) {
				best = min(best, depth[targetOf(li)])
			}
			depth[v] = max(0, best-1)
		}
	}
	for v := range n {
		ns[v].stage = depth[v]
	}
	return maxDepth + 1
}

// stackStages gives every stage a vertically centred initial stack.
func stackStages(ns []wnode, byStage [][]int, scale float64, pad float64) {
	for _, st := range byStage {
		total := 0.0
		for _, i := range st {
			total += ns[i].value * scale
		}
		total += float64(len(st)-1) * pad
		y := (1 - total) / 2
		if y < 0 {
			y = 0
		}
		for _, i := range st {
			h := ns[i].value * scale
			ns[i].y0 = y
			ns[i].y1 = y + h
			y += h + pad
		}
	}
}

// resolveCollisions restores the minimum gap inside one stage, pushing down
// from the top and then back up if the stack overflowed the box.
func resolveCollisions(ns []wnode, st []int, pad float64) {
	slices.SortStableFunc(st, func(a, b int) int {
		if c := cmp.Compare(ns[a].y0, ns[b].y0); c != 0 {
			return c
		}
		return cmp.Compare(ns[a].id, ns[b].id)
	})
	y := 0.0
	for _, i := range st {
		if ns[i].y0 < y {
			shift := y - ns[i].y0
			ns[i].y0 += shift
			ns[i].y1 += shift
		}
		y = ns[i].y1 + pad
	}
	last := st[len(st)-1]
	if ns[last].y1 <= 1 {
		return
	}
	y = 1.0
	for k := len(st) - 1; k >= 0; k-- {
		i := st[k]
		if ns[i].y1 > y {
			shift := ns[i].y1 - y
			ns[i].y0 -= shift
			ns[i].y1 -= shift
		}
		y = ns[i].y0 - pad
	}
}
