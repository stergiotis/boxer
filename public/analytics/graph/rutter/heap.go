package rutter

// heap4 is a 4-ary min-heap of nodes keyed by a uint32 with decrease-key,
// over slot ids (ADR-0256 §SD2). The position array is sized to the graph
// once and the heap is reused across searches; a node's position is -1
// while it is out.
type heap4 struct {
	items []int32
	keys  []uint32
	pos   []int32
}

func newHeap4(n int32) (inst *heap4) {
	inst = &heap4{pos: make([]int32, n)}
	for i := range inst.pos {
		inst.pos[i] = -1
	}
	return
}

func (inst *heap4) len() int { return len(inst.items) }

// peek is the minimum key without removing it; the heap must not be empty.
func (inst *heap4) peek() uint32 { return inst.keys[0] }

func (inst *heap4) reset() {
	for _, v := range inst.items {
		inst.pos[v] = -1
	}
	inst.items = inst.items[:0]
	inst.keys = inst.keys[:0]
}

// push inserts v, or decreases its key when it is already in and the new
// key is smaller. A larger key for a node already in is ignored, which is
// what a label-setting search wants.
func (inst *heap4) push(v int32, key uint32) {
	p := inst.pos[v]
	if p >= 0 {
		if key < inst.keys[p] {
			inst.keys[p] = key
			inst.up(int(p))
		}
		return
	}
	inst.items = append(inst.items, v)
	inst.keys = append(inst.keys, key)
	i := len(inst.items) - 1
	inst.pos[v] = int32(i)
	inst.up(i)
}

func (inst *heap4) pop() (v int32, key uint32) {
	v, key = inst.items[0], inst.keys[0]
	inst.pos[v] = -1
	last := len(inst.items) - 1
	if last > 0 {
		inst.items[0], inst.keys[0] = inst.items[last], inst.keys[last]
		inst.pos[inst.items[0]] = 0
	}
	inst.items = inst.items[:last]
	inst.keys = inst.keys[:last]
	if last > 0 {
		inst.down(0)
	}
	return
}

func (inst *heap4) up(i int) {
	v, k := inst.items[i], inst.keys[i]
	for i > 0 {
		parent := (i - 1) >> 2
		if inst.keys[parent] <= k {
			break
		}
		inst.items[i], inst.keys[i] = inst.items[parent], inst.keys[parent]
		inst.pos[inst.items[i]] = int32(i)
		i = parent
	}
	inst.items[i], inst.keys[i] = v, k
	inst.pos[v] = int32(i)
}

func (inst *heap4) down(i int) {
	n := len(inst.items)
	v, k := inst.items[i], inst.keys[i]
	for {
		first := i<<2 + 1
		if first >= n {
			break
		}
		best := first
		bestKey := inst.keys[first]
		last := min(first+4, n)
		for c := first + 1; c < last; c++ {
			if inst.keys[c] < bestKey {
				best, bestKey = c, inst.keys[c]
			}
		}
		if bestKey >= k {
			break
		}
		inst.items[i], inst.keys[i] = inst.items[best], bestKey
		inst.pos[inst.items[i]] = int32(i)
		i = best
	}
	inst.items[i], inst.keys[i] = v, k
	inst.pos[v] = int32(i)
}
