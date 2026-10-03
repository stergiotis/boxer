package fsbrowser

// FillWidth: the columns together span the pane, and the name column is the
// one that gives when the pane is resized.
//
// The obvious construction — derive the name column as what the others leave
// and let the others be dragged — does not work, and fails in a way worth
// recording. With the name column derived, the right edge of every column
// after it sits at a position fixed by the pane (the pane's width less the
// columns to its right). The crate computes a dragged width from the pointer
// and the column's left edge; the left edge moves as the name column gives,
// so the edge under the pointer never arrives, and the width changes every
// frame by the pointer's offset: rate control where the reader expects
// position control, running to the floor or the ceiling.
//
// What works is a splitter. A drag on a column's right edge is taken from the
// column to its RIGHT, so everything left of the dragged edge stands still,
// the edge follows the pointer, and the total does not change. The last
// column's right edge is the pane's and is not dragged. Only a resize of the
// pane moves the name column on its own.
//
// The table does not always take the width it is given. A column whose
// content will not fit narrower — the outline column's indent, disclosure
// and icon — is laid out at its content's width whatever it was sent, and
// reported so every frame. Read as a drag whose giver is at its floor, that
// report would be put back by a re-apply forever, and every re-apply undoes
// whatever the reader is dragging. A refusal the table repeats is therefore
// taken as that column's floor: the column keeps what it was laid out at and
// the others make room.

// fillSettleFrames is how many reports are ignored after the layout moved on
// its own: the one already in flight, and the one the re-apply produces.
const fillSettleFrames = 2

// fillT holds the FillWidth layout, per view. It is the authority while
// FillWidth is on: the resolver is told what it holds (so a drag persists) and
// is asked again only when its own generation moves (a load, a reset).
type fillT struct {
	w []float32
	// sent is what the table was given this frame and prev what it was
	// given the frame before — the widths the report now arriving describes,
	// since a report is a frame late. A drag is a column that differs from
	// prev, not from w: w may already hold last frame's reading of the drag.
	sent []float32
	prev []float32
	// settle counts the reports still to be ignored after the layout was
	// retaken or the pane resized: they describe a table laid out under
	// other widths (or, in a new window, in its sizing pass), and read as
	// drags they would move columns nobody touched.
	settle int
	// baseEpoch is the resolver's (or the seed's) generation w was taken
	// under; epoch is this layout's own, added to it for the binding.
	baseEpoch uint32
	epoch     uint32
	paneW     float32
	// minW is a floor per column that the table imposed by refusing to lay
	// the column out narrower; 0 leaves the column at the shared floor.
	minW []float32
	// refused is the column (plus one) whose width was last put back by a
	// re-apply and refusedAt the width it was reported at; a second report
	// at that width means the table holds it there.
	refused   int
	refusedAt float32
	// refit asks the next plan to lay the columns out again after a floor
	// was learned.
	refit bool
}

// floorOf is column i's floor: the shared one, or what the table imposed.
func (inst *fillT) floorOf(i int, floor float32) float32 {
	if i < len(inst.minW) {
		return max(floor, inst.minW[i])
	}
	return floor
}

// fit lays the columns out across the pane: the name column takes what the
// others leave, and when that is less than its floor the others give, each in
// proportion to what it holds above its own floor. Only when every column is
// at its floor does the table overflow the pane and scroll. Reports whether
// a width moved.
func (inst *fillT) fit(paneW, floor float32) (moved bool) {
	target := paneW - fillSlack
	rest := float32(0)
	for _, w := range inst.w[1:] {
		rest += w
	}
	nameFloor := inst.floorOf(0, floor)
	if over := rest + nameFloor - target; over > fillStep {
		room := float32(0)
		for i := 1; i < len(inst.w); i++ {
			room += max(inst.w[i]-inst.floorOf(i, floor), 0)
		}
		if room > 0 {
			share := min(over/room, 1)
			rest = 0
			for i := 1; i < len(inst.w); i++ {
				give := max(inst.w[i]-inst.floorOf(i, floor), 0) * share
				if give > fillStep {
					inst.w[i] -= give
					moved = true
				}
				rest += inst.w[i]
			}
		}
	}
	name := max(target-rest, nameFloor)
	if d := name - inst.w[0]; d > fillStep || d < -fillStep {
		inst.w[0] = name
		moved = true
	}
	return
}

// plan brings the layout in line with what was resolved and with the pane.
// resolved is one width per column as the resolver (or the defaults) gives
// them; its name entry is ignored, since the name column is what the others
// leave. A changed baseEpoch means the resolver's answer moved under the
// layout — stored widths loaded, a column reset — and the layout is retaken
// from it; otherwise the layout stands and only a pane of a different width
// moves the name column. paneW <= 0 (the probe has not reported) leaves the
// resolved widths as they are.
func (inst *fillT) plan(resolved []float64, baseEpoch uint32, paneW, floor float32) {
	defer func() {
		inst.prev = append(inst.prev[:0], inst.sent...)
		inst.sent = append(inst.sent[:0], inst.w...)
	}()
	retake := len(inst.w) != len(resolved) || inst.baseEpoch != baseEpoch
	if len(inst.minW) != len(resolved) {
		inst.minW = make([]float32, len(resolved))
	}
	if retake {
		inst.w = inst.w[:0]
		for i, w := range resolved {
			inst.w = append(inst.w, max(float32(w), inst.floorOf(i, floor)))
		}
		inst.baseEpoch = baseEpoch
	}
	if paneW <= 0 || len(inst.w) == 0 {
		if retake {
			inst.epoch++
			inst.settle = fillSettleFrames
		}
		return
	}
	if retake || inst.refit || paneW != inst.paneW {
		inst.paneW = paneW
		inst.refit = false
		if inst.fit(paneW, floor) || retake {
			inst.epoch++
			inst.settle = fillSettleFrames
		}
	}
}

// dragged reads the widths the table reported and, where a column differs
// from what the table was given, treats it as a drag on that column's right
// edge: the column takes the reported width and its right-hand neighbour gives
// the difference, down to the floor and no further. One edge moves at a time, so
// the first differing column is the one. The last column is not dragged; a
// report that differs there is the crate growing it to its content, and is
// taken from the name column instead. Reports whether the layout changed.
func (inst *fillT) dragged(fetched []float32, floor float32) (changed bool) {
	if inst.settle > 0 {
		inst.settle--
		return
	}
	if len(fetched) != len(inst.w) || len(inst.prev) != len(inst.w) || len(inst.w) < 2 {
		return
	}
	last := len(inst.w) - 1
	for i, got := range fetched {
		if d := got - inst.prev[i]; d <= fillStep && d >= -fillStep {
			continue
		}
		// The column goes where the report has it. w may be a frame ahead
		// of prev — it took last frame's reading of the same drag — so the
		// difference is taken against w, and may already be nothing.
		want := got - inst.w[i]
		if want <= fillStep && want >= -fillStep {
			return
		}
		giver := i + 1
		if i == last {
			giver = 0
		}
		// A column the table let narrower than a floor it imposed has no
		// such floor.
		if i < len(inst.minW) && got < inst.minW[i] {
			inst.minW[i] = got
		}
		// The giver keeps its floor; what it cannot give, the column does
		// not get, and a re-apply puts the column back — once. Reported at
		// the same width again, the column is held there by the table: it
		// keeps that width as its floor and the layout is fitted around it.
		d := min(want, inst.w[giver]-inst.floorOf(giver, floor))
		if d <= fillStep && d >= -fillStep {
			if inst.refused == i+1 && got-inst.refusedAt <= fillStep && inst.refusedAt-got <= fillStep {
				inst.minW[i] = got
				inst.w[i] = got
				inst.refused = 0
				inst.refit = true
				return true
			}
			inst.refused, inst.refusedAt = i+1, got
			inst.epoch++
			return true
		}
		inst.refused = 0
		inst.w[i] += d
		inst.w[giver] -= d
		inst.epoch++
		return true
	}
	return
}
