package rutter

// CCHQuery is the elimination-tree query's scratch over one hierarchy
// (ADR-0256 §SD4): forward and backward labels stamped per run, the
// up-arc that set each, and the chain the two searches walked. One per
// goroutine; the [Weights] it reads may change between runs.
type CCHQuery struct {
	cch   *CCH
	dF    []uint32
	dB    []uint32
	pF    []int32
	pB    []int32
	sF    []uint32
	sB    []uint32
	gen   uint32
	meet  int32
	touch []int32
}

// NewCCHQuery allocates the scratch for c.
func NewCCHQuery(c *CCH) (inst *CCHQuery) {
	n := c.g.NumNodes()
	inst = &CCHQuery{
		cch: c,
		dF:  make([]uint32, n), dB: make([]uint32, n),
		pF: make([]int32, n), pB: make([]int32, n),
		sF: make([]uint32, n), sB: make([]uint32, n),
	}
	return
}

func (inst *CCHQuery) begin() {
	inst.gen++
	if inst.gen == 0 {
		clear(inst.sF)
		clear(inst.sB)
		inst.gen = 1
	}
	inst.meet = -1
	inst.touch = inst.touch[:0]
}

func (inst *CCHQuery) labelF(v int32) uint32 {
	if inst.sF[v] != inst.gen {
		return Inf
	}
	return inst.dF[v]
}

func (inst *CCHQuery) labelB(v int32) uint32 {
	if inst.sB[v] != inst.gen {
		return Inf
	}
	return inst.dB[v]
}

func (inst *CCHQuery) setF(v int32, d uint32, arc int32) {
	inst.sF[v], inst.dF[v], inst.pF[v] = inst.gen, d, arc
}

func (inst *CCHQuery) setB(v int32, d uint32, arc int32) {
	inst.sB[v], inst.dB[v], inst.pB[v] = inst.gen, d, arc
}

// relaxF pushes v's forward label along its up-arcs. A label at or past
// bound — the best distance so far — cannot improve the answer and is
// not pushed.
func (inst *CCHQuery) relaxF(w *Weights, v int32, bound uint32) {
	d := inst.labelF(v)
	if d >= bound {
		return
	}
	c := inst.cch
	for a := c.upFirst[v]; a < c.upFirst[v+1]; a++ {
		nd := addSat(d, w.up[a])
		if nd < inst.labelF(c.upHead[a]) {
			inst.setF(c.upHead[a], nd, a)
		}
	}
}

// relaxB pushes v's backward label along its up-arcs, in the down
// direction of each: the label of a head u is the length of u→…→t, so it
// grows by the u→v weight, which is the arc's down weight.
func (inst *CCHQuery) relaxB(w *Weights, v int32, bound uint32) {
	d := inst.labelB(v)
	if d >= bound {
		return
	}
	c := inst.cch
	for a := c.upFirst[v]; a < c.upFirst[v+1]; a++ {
		nd := addSat(d, w.down[a])
		if nd < inst.labelB(c.upHead[a]) {
			inst.setB(c.upHead[a], nd, a)
		}
	}
}

// Run is the distance from s to t under w, or ok=false when t is not
// reachable. The two ancestor chains are walked together in rank order;
// where they coincide both labels are final and their sum is a candidate.
func (inst *CCHQuery) Run(w *Weights, s, t int32) (dist uint32, ok bool) {
	return inst.RunSources(w, []Source{{Node: s}}, []Source{{Node: t}})
}

// RunSources is [CCHQuery.Run] from several sources with offsets to
// several targets with offsets: the four ends of two snapped points. The
// chains of every source and target are walked; the walk is a merge in
// rank order over as many chains as there are ends.
func (inst *CCHQuery) RunSources(w *Weights, sources, targets []Source) (dist uint32, ok bool) {
	inst.begin()
	c := inst.cch
	rank := c.order.Rank
	// Chain heads, forward and backward. A node may head several chains;
	// the merge handles it by processing the lowest rank first and
	// skipping heads that have moved past a processed node.
	heads := make([]int32, 0, len(sources)+len(targets))
	isFwd := make([]bool, 0, len(sources)+len(targets))
	for _, s := range sources {
		if s.Dist == Inf {
			continue
		}
		if s.Dist < inst.labelF(s.Node) {
			inst.setF(s.Node, s.Dist, -1)
		}
		heads = append(heads, s.Node)
		isFwd = append(isFwd, true)
	}
	for _, t := range targets {
		if t.Dist == Inf {
			continue
		}
		if t.Dist < inst.labelB(t.Node) {
			inst.setB(t.Node, t.Dist, -1)
		}
		heads = append(heads, t.Node)
		isFwd = append(isFwd, false)
	}
	dist = Inf
	lastF, lastB := int32(-1), int32(-1)
	for {
		// The lowest-ranked head among all chains.
		best := int32(-1)
		for _, h := range heads {
			if h >= 0 && (best < 0 || rank[h] < rank[best]) {
				best = h
			}
		}
		if best < 0 {
			break
		}
		v := best
		// Advance every chain standing at v, and note which sides want it.
		fwd, bwd := false, false
		for i, h := range heads {
			if h == v {
				heads[i] = c.parent[v]
				if isFwd[i] {
					fwd = true
				} else {
					bwd = true
				}
			}
		}
		if fwd && lastF != v {
			inst.relaxF(w, v, dist)
			lastF = v
		}
		if bwd && lastB != v {
			inst.relaxB(w, v, dist)
			lastB = v
		}
		if df, db := inst.labelF(v), inst.labelB(v); df != Inf && db != Inf {
			if d := addSat(df, db); d < dist {
				dist, inst.meet = d, v
			}
		}
	}
	return dist, dist != Inf
}

// Path appends the original arcs of the last run's shortest path, source
// first, to out. Nothing is appended when the last run found no path.
func (inst *CCHQuery) Path(w *Weights, out []int32) []int32 {
	if inst.meet < 0 {
		return out
	}
	c := inst.cch
	// Up-arcs from the source to the meeting node, collected backwards.
	var upArcs []int32
	for v := inst.meet; ; {
		a := inst.pF[v]
		if a < 0 {
			break
		}
		upArcs = append(upArcs, a)
		v = c.upTail[a]
	}
	for i := len(upArcs) - 1; i >= 0; i-- {
		out = w.unpackUp(upArcs[i], out)
	}
	// Down-arcs from the meeting node to the target, in walk order.
	for v := inst.meet; ; {
		a := inst.pB[v]
		if a < 0 {
			break
		}
		out = w.unpackDown(a, out)
		v = c.upTail[a]
	}
	return out
}

// Meet is the node the last run's two searches met at, -1 when none.
func (inst *CCHQuery) Meet() int32 { return inst.meet }
