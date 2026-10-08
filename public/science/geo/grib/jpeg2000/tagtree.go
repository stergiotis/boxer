package jpeg2000

// tagTree is the hierarchical representation of B.10.2: level 0 holds the
// leaves, each higher level the minimum of up to four nodes below, and the
// root is the single node at the top. Decoding answers "is the leaf's value
// below a threshold" a bit at a time, remembering what earlier queries
// established.
type tagTree struct {
	w, h   int
	levels []tagLevel
}

type tagLevel struct {
	w, h  int
	value []int32
	known []bool
}

func newTagTree(w, h int) (t *tagTree) {
	t = &tagTree{w: w, h: h}
	for {
		t.levels = append(t.levels, tagLevel{w: w, h: h, value: make([]int32, w*h), known: make([]bool, w*h)})
		if w == 1 && h == 1 {
			break
		}
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
	return
}

// decode walks from the root to leaf (x, y), reading bits until either the
// leaf's value is known or is shown to reach threshold. It returns the
// leaf's value when known and below the threshold.
func (inst *tagTree) decode(br *headerBits, x, y int, threshold int32) (value int32, below bool) {
	var low int32
	for lv := len(inst.levels) - 1; lv >= 0; lv-- {
		l := &inst.levels[lv]
		xi := x >> uint(lv)
		yi := y >> uint(lv)
		i := yi*l.w + xi
		if l.value[i] < low {
			l.value[i] = low
		}
		for !l.known[i] && l.value[i] < threshold {
			if br.bit() == 1 {
				l.known[i] = true
			} else {
				l.value[i]++
			}
		}
		low = l.value[i]
		if !l.known[i] {
			// Reached the threshold without a decision: nothing below is
			// decidable either.
			return
		}
	}
	leaf := &inst.levels[0]
	value = leaf.value[y*leaf.w+x]
	below = value < threshold
	return
}

// decodeFull reads until leaf (x, y) is known.
func (inst *tagTree) decodeFull(br *headerBits, x, y int) (value int32) {
	for t := int32(1); ; t++ {
		v, known := inst.decode(br, x, y, t)
		if known {
			value = v
			return
		}
		if br.bad {
			return
		}
	}
}

// headerBits reads a packet header bit by bit with the stuffing rule of
// B.10.1: after a 0xFF byte the next byte carries seven bits.
type headerBits struct {
	b    []byte
	pos  int
	cur  uint8
	left int
	prev uint8
	bad  bool
}

func (inst *headerBits) bit() (v uint8) {
	if inst.left == 0 {
		if inst.pos >= len(inst.b) {
			inst.bad = true
			return
		}
		inst.prev = inst.cur
		inst.cur = inst.b[inst.pos]
		inst.pos++
		if inst.prev == 0xff {
			inst.left = 7
			inst.cur &= 0x7f
		} else {
			inst.left = 8
		}
	}
	inst.left--
	v = inst.cur >> uint(inst.left) & 1
	return
}

func (inst *headerBits) bits(n int) (v uint32) {
	for range n {
		v = v<<1 | uint32(inst.bit())
	}
	return
}

// finish aligns to the byte after the header; a trailing 0xFF is followed
// by a stuffed byte that is skipped too.
func (inst *headerBits) finish() (end int) {
	inst.left = 0
	end = inst.pos
	if inst.cur == 0xff && inst.pos < len(inst.b) {
		end++
	}
	return
}
